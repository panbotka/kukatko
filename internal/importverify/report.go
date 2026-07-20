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
	// MissingEmbeddingsCount is how many imported photos lack an embeddings row.
	MissingEmbeddingsCount int `json:"missing_embeddings_count"`
	// MissingEmbeddings lists their PhotoPrism uids, capped at SampleLimit.
	MissingEmbeddings []string `json:"missing_embeddings"`
	// MissingFacesCount is how many imported photos lack a face-detection record.
	MissingFacesCount int `json:"missing_faces_count"`
	// MissingFaces lists their PhotoPrism uids, capped at SampleLimit.
	MissingFaces []string `json:"missing_faces"`
}

// StructureReport reconciles the three structural entities — albums, labels and
// subjects — between the source and the catalogue.
type StructureReport struct {
	// Albums reconciles PhotoPrism album titles against catalogue album titles.
	Albums EntityReport `json:"albums"`
	// Labels reconciles PhotoPrism label names against catalogue label names.
	Labels EntityReport `json:"labels"`
	// Subjects reconciles PhotoPrism subject names against catalogue subject names.
	Subjects EntityReport `json:"subjects"`
}

// EntityReport reconciles one structural entity: how many distinct names the
// source and the catalogue hold, and which source names are absent from the
// catalogue.
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
}
