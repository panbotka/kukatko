package importverify

// Report is the full outcome of a reconciliation pass: how the source libraries
// (PhotoPrism and, when configured, photo-sorter's feeds) compare against the
// Kukátko catalogue, plus a single Complete flag summarising whether nothing is
// left to import. It is JSON-serialised verbatim by the API and the CLI, so the
// field names and json tags are part of the contract.
type Report struct {
	// PhotoPrism reconciles the PhotoPrism photo library against the catalogue.
	PhotoPrism PhotoPrismReport `json:"photoprism"`
	// Vectors reconciles photo-sorter's embeddings/faces feeds against the
	// catalogue; NotConfigured when no feeds source was supplied.
	Vectors VectorsReport `json:"vectors"`
	// Structure reconciles albums, labels and subjects (people).
	Structure StructureReport `json:"structure"`
	// Complete is true only when every section reports nothing missing.
	Complete bool `json:"complete"`
}

// PhotoPrismReport reconciles the whole PhotoPrism photo library against the
// catalogue: how many photos the source holds (in total and per media type), how
// many are imported, how many are covered by SHA dedup, and a capped, concrete
// list of what is still missing plus per-photo file gaps.
type PhotoPrismReport struct {
	// SourceTotal is the number of photos enumerated from PhotoPrism.
	SourceTotal int `json:"source_total"`
	// SourceByType buckets the source photos by their lowercased media type
	// (e.g. "image", "raw", "video", "live").
	SourceByType map[string]int `json:"source_by_type"`
	// ImportedCount is how many source photos are present in the catalogue by
	// their PhotoPrism uid.
	ImportedCount int `json:"imported_count"`
	// DeduplicatedCount is how many source photos are absent by uid but already
	// present under a different uid via a shared file hash (SHA dedup).
	DeduplicatedCount int `json:"deduplicated_count"`
	// MissingCount is how many source photos are neither imported nor deduplicated.
	MissingCount int `json:"missing_count"`
	// MissingUIDs lists the PhotoPrism uids of missing photos, capped at the
	// service's SampleLimit while MissingCount stays the full total.
	MissingUIDs []string `json:"missing_uids"`
	// FileGapCount is how many imported photos have fewer catalogue original files
	// than PhotoPrism reports files for them (e.g. a dropped RAW sibling).
	FileGapCount int `json:"file_gap_count"`
	// FileGaps lists the offending photos, capped at SampleLimit while
	// FileGapCount stays the full total.
	FileGaps []FileGap `json:"file_gaps"`
}

// FileGap records one imported photo whose catalogue original-file count is below
// the file count PhotoPrism reports for it.
type FileGap struct {
	// PhotoprismUID is the PhotoPrism uid of the photo with the gap.
	PhotoprismUID string `json:"photoprism_uid"`
	// Expected is the number of files PhotoPrism reports for the photo.
	Expected int `json:"expected"`
	// Actual is the number of role='original' photo_files in the catalogue.
	Actual int `json:"actual"`
}

// VectorsReport reconciles photo-sorter's pre-computed embeddings and faces (read
// from its HTTP feeds) against the catalogue's embeddings/faces for the
// PhotoPrism-imported population. When no feeds source is configured the whole
// section is inert and NotConfigured is set.
//
// The section answers two different questions and names them apart, because
// conflating them is how a report of an all-but-empty catalogue used to read as
// finished. The "missing for imported photos" counters are scoped to photos
// ALREADY in the catalogue — a vector cannot attach to a photo that was never
// imported, so they legitimately sit at 0 on a catalogue holding 280 of 20 670
// photos. The "source coverage" ratios are the share of the SOURCE's vectors
// Kukátko actually holds, and on that same catalogue they read 0.0025. Read
// together, the section can no longer be mistaken for full coverage.
type VectorsReport struct {
	// NotConfigured is true when no feeds source was supplied; every other field
	// is then zero and this section is ignored by Complete.
	NotConfigured bool `json:"not_configured"`
	// SourceTotalPhotos is photo-sorter's total photo count.
	SourceTotalPhotos int `json:"source_total_photos"`
	// SourcePhotosWithEmbeddings is how many photos photo-sorter has embeddings for.
	SourcePhotosWithEmbeddings int `json:"source_photos_with_embeddings"`
	// SourcePhotosWithFaces is how many photos photo-sorter has faces for.
	SourcePhotosWithFaces int `json:"source_photos_with_faces"`
	// SourceTotalFaces is photo-sorter's total face count.
	SourceTotalFaces int `json:"source_total_faces"`
	// CatalogEmbeddings is the catalogue's embeddings count over imported photos.
	CatalogEmbeddings int `json:"catalog_embeddings"`
	// CatalogFacePhotos is the catalogue's count of imported photos that have faces.
	CatalogFacePhotos int `json:"catalog_face_photos"`
	// CatalogFaces is the catalogue's total face count over imported photos.
	CatalogFaces int `json:"catalog_faces"`
	// EmbeddingsSourceCoverage is CatalogEmbeddings / SourcePhotosWithEmbeddings —
	// the share of the source's embeddings the catalogue holds, in [0,1]. Below 1
	// means the vector migration is unfinished no matter what the two
	// MissingFor… counters say.
	EmbeddingsSourceCoverage float64 `json:"embeddings_source_coverage"`
	// FacesSourceCoverage is CatalogFaces / SourceTotalFaces, in [0,1], with the
	// same meaning for faces.
	FacesSourceCoverage float64 `json:"faces_source_coverage"`
	// EmbeddingsMissingForImportedPhotos is how many photos ALREADY IN the
	// catalogue lack an embeddings row. It says nothing about source photos that
	// were never imported — that is what EmbeddingsSourceCoverage is for.
	EmbeddingsMissingForImportedPhotos int `json:"embeddings_missing_for_imported_photos"`
	// EmbeddingsMissingUIDs samples those photos' PhotoPrism uids, capped at
	// SampleLimit while the count above stays the full total.
	EmbeddingsMissingUIDs []string `json:"embeddings_missing_uids"`
	// FacesMissingForImportedPhotos is how many photos ALREADY IN the catalogue
	// lack a face-detection record; same scoping caveat as above.
	FacesMissingForImportedPhotos int `json:"faces_missing_for_imported_photos"`
	// FacesMissingUIDs samples those photos' PhotoPrism uids, capped at SampleLimit.
	FacesMissingUIDs []string `json:"faces_missing_uids"`
}

// FullSourceCoverage reports whether the catalogue holds every embedding and every
// face the source has. It is deliberately NOT part of Complete: photo-sorter's
// population and PhotoPrism's need not line up exactly, so gating completeness on
// it could make a genuinely finished import unreachable. It exists so a caller —
// the CLI summary, the frontend — can flag a section whose zero "missing"
// counters would otherwise read as a finished vector migration.
func (v VectorsReport) FullSourceCoverage() bool {
	return v.EmbeddingsSourceCoverage >= 1 && v.FacesSourceCoverage >= 1
}

// StructureReport reconciles the three structural entities — albums, labels and
// subjects — between the source and the catalogue.
type StructureReport struct {
	// Albums reconciles PhotoPrism album titles against catalogue album titles.
	Albums AlbumReport `json:"albums"`
	// Labels reconciles PhotoPrism label names against catalogue label names.
	Labels EntityReport `json:"labels"`
	// Subjects reconciles PhotoPrism subject names against catalogue subject names.
	Subjects EntityReport `json:"subjects"`
}

// AlbumReport reconciles albums: an EntityReport over the album types the
// importer actually maps, plus a tally of the albums of the types it skips on
// purpose. PhotoPrism serves five album types and generates most of them itself;
// the import maps ppimport.DefaultAlbumTypes and leaves out "month" — one
// auto-generated album per calendar month, hundreds of them on a real library,
// already covered by Kukátko's timeline. Those albums are counted here rather
// than listed as missing, so a complete import can actually report clean while
// the report still accounts for the whole source album catalogue.
//
// The embedded EntityReport is flattened into the same JSON object, so the album
// section keeps the shape of the other two plus the two fields below.
type AlbumReport struct {
	EntityReport
	// SkippedTypes are the PhotoPrism album types deliberately left out of the
	// reconciliation because the importer does not map them.
	SkippedTypes []string `json:"skipped_types"`
	// SkippedByDesignCount is how many distinct source album titles those types
	// hold that the reconciled types do not; they are never reported missing.
	SkippedByDesignCount int `json:"skipped_by_design_count"`
}

// EntityReport reconciles one structural entity: how many distinct names the
// source and the catalogue hold, which source names are absent from the catalogue
// and which catalogue names the source does not have.
//
// The surplus half exists because "missing = 0" hid a real defect: the report read
// `people: source=104 kukatko=105 missing=0` while that one extra subject was an
// empty-named catch-all an importer had minted, holding 16 532 markers. A
// reconciliation that only ever looks for absences cannot see a row that should
// not exist.
type EntityReport struct {
	// SourceCount is the number of distinct source names/titles.
	SourceCount int `json:"source_count"`
	// CatalogCount is the catalogue's row count for the entity.
	CatalogCount int `json:"catalog_count"`
	// MissingCount is how many source names are absent from the catalogue.
	MissingCount int `json:"missing_count"`
	// Missing lists those source names, sorted and capped at SampleLimit while
	// MissingCount stays the full total.
	Missing []string `json:"missing"`
	// SurplusCount is how many distinct catalogue names have no source counterpart.
	// It never gates Complete: a surplus is usually legitimate (anything created in
	// Kukátko itself), so it is reported for a human to read, not enforced.
	SurplusCount int `json:"surplus_count"`
	// Surplus lists those catalogue names, sorted and capped at SampleLimit while
	// SurplusCount stays the full total. An empty string in here is the tell-tale
	// of a nameless subject; `kukatko maintenance nameless-subjects` reports what
	// hangs off it.
	Surplus []string `json:"surplus"`
}
