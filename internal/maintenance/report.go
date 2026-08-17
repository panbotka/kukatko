package maintenance

import "sort"

// Finding summarises one class of integrity problem: how many items are affected
// and a bounded sample of their identifiers (photo uids for catalogue-side
// findings, storage keys for orphan files) for display without dumping the whole
// list.
type Finding struct {
	// Count is the total number of affected items.
	Count int `json:"count"`
	// Samples holds up to the configured sample limit of affected identifiers, in
	// a stable order. It is never nil (an empty finding serialises to []).
	Samples []string `json:"samples"`
}

// Report is the result of an integrity scan: the catalogue/disk totals plus one
// Finding per problem class. A library with no problems has a zero Count in every
// Finding.
type Report struct {
	// Photos is the total number of catalogued photos (including archived).
	Photos int `json:"photos"`
	// FilesInDB is the number of catalogued files (originals plus sidecars).
	FilesInDB int `json:"files_in_db"`
	// OriginalsOnDisk is the number of files found under the originals root.
	OriginalsOnDisk int `json:"originals_on_disk"`
	// MissingOriginals are photos whose primary original is absent on disk.
	MissingOriginals Finding `json:"missing_originals"`
	// OrphanFiles are originals on disk with no catalogue row.
	OrphanFiles Finding `json:"orphan_files"`
	// MissingThumbnails are photos whose representative thumbnail is not cached.
	MissingThumbnails Finding `json:"missing_thumbnails"`
	// MissingEmbeddings are photos with no image embedding yet.
	MissingEmbeddings Finding `json:"missing_embeddings"`
	// MissingFaces are photos that have never had face detection run.
	MissingFaces Finding `json:"missing_faces"`
	// MissingPhashes are photos with no perceptual hashes yet.
	MissingPhashes Finding `json:"missing_phashes"`
	// TransposedDimensions are quarter-turned photos whose file_width/file_height
	// hold the displayed frame instead of the stored one, so every consumer that
	// applies the orientation to them rotates a second time. Listing them is the
	// dry run of `maintenance repair --dimensions`.
	TransposedDimensions Finding `json:"transposed_dimensions"`
	// TransposedFaceBoxes are the face rows `maintenance repair --dimensions` would
	// rewrite: rows whose cached frame is their photo's stored pair transposed and
	// whose coordinate space the photo's markers establish. Counted per face row,
	// sampled by photo uid.
	//
	// Rows with the same defect whose space the evidence cannot establish are
	// deliberately not counted: the repair leaves them exactly as they are so a later
	// run — once the photo has a marker to reconcile them against — can still pick
	// them up, and a finding is what the repair would do, not what is wrong.
	TransposedFaceBoxes Finding `json:"transposed_face_boxes"`
	// DuplicateFaceMarkers are markers cached on more than one detected face,
	// sampled by marker uid. A marker describes one region, so a second face
	// claiming it is a surplus link — it renders one person twice on the photo and
	// misleads everything that reads faces.subject_uid. Listing them is the dry run
	// of `maintenance repair --face-markers`.
	DuplicateFaceMarkers Finding `json:"duplicate_face_markers"`
	// SidewaysFaceDetections are quarter-turned photos whose recorded face detection
	// ran on a sideways image: the sidecar does not apply EXIF, so until the
	// face_detect job rotated before sending, the detector saw those photos on their
	// side. Their boxes are in a frame nobody displays and the faces the detector
	// missed on a turned picture are absent outright, which is why listing them is the
	// dry run of `maintenance repair --sideways-faces` (a re-detection) and not of a
	// coordinate fix. A photo whose detection is recorded against the display frame
	// never appears here, so the count goes to zero and stays there.
	SidewaysFaceDetections Finding `json:"sideways_face_detections"`
}

// Clean reports whether the scan found no problems at all, i.e. every Finding has
// a zero Count.
func (r Report) Clean() bool {
	return r.MissingOriginals.Count == 0 &&
		r.OrphanFiles.Count == 0 &&
		r.MissingThumbnails.Count == 0 &&
		r.MissingEmbeddings.Count == 0 &&
		r.MissingFaces.Count == 0 &&
		r.MissingPhashes.Count == 0 &&
		r.TransposedDimensions.Count == 0 &&
		r.TransposedFaceBoxes.Count == 0 &&
		r.DuplicateFaceMarkers.Count == 0 &&
		r.SidewaysFaceDetections.Count == 0
}

// findingCollector accumulates affected identifiers while iterating, counting
// every one but retaining only the first limit identifiers as samples.
type findingCollector struct {
	count   int
	limit   int
	samples []string
}

// newFindingCollector returns a collector that keeps at most limit samples.
func newFindingCollector(limit int) *findingCollector {
	return &findingCollector{limit: limit, samples: make([]string, 0, limit)}
}

// add records one affected identifier, keeping it as a sample only while the
// sample budget is not yet exhausted.
func (c *findingCollector) add(id string) {
	c.count++
	if len(c.samples) < c.limit {
		c.samples = append(c.samples, id)
	}
}

// finding returns the accumulated Finding.
func (c *findingCollector) finding() Finding {
	return Finding{Count: c.count, Samples: c.samples}
}

// findingFrom builds a Finding from a full list of affected identifiers, keeping
// at most limit of them as samples. The input order is preserved.
func findingFrom(ids []string, limit int) Finding {
	samples := make([]string, 0, limit)
	for _, id := range ids {
		if len(samples) >= limit {
			break
		}
		samples = append(samples, id)
	}
	return Finding{Count: len(ids), Samples: samples}
}

// orphanKeys returns the storage keys present on disk but absent from the
// catalogue, sorted for a deterministic result. It is a pure function so the
// set-difference is exercised without any I/O.
func orphanKeys(dbPaths, diskKeys []string) []string {
	dbSet := make(map[string]struct{}, len(dbPaths))
	for _, p := range dbPaths {
		dbSet[p] = struct{}{}
	}
	orphans := make([]string, 0)
	for _, key := range diskKeys {
		if _, ok := dbSet[key]; !ok {
			orphans = append(orphans, key)
		}
	}
	sort.Strings(orphans)
	return orphans
}
