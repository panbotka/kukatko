package maintenance

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

// StoreInventory is what the scan saw in the store that holds the originals:
// which store that is, how many originals it holds, and — when the listing failed
// — why the count is missing.
//
// The failure is carried rather than returned because the rest of the scan is
// still worth reporting: the missing-original half asks the store per photo and
// answers honestly even when the listing does not. What must not happen is the
// two being told apart nowhere, i.e. "the store holds nothing" and "nobody could
// ask the store" printing the same zero.
type StoreInventory struct {
	// Kind names the store the inventory came from: the local originals root or
	// the object store.
	Kind StoreKind `json:"kind"`
	// Originals is the number of originals the store holds. It is zero when the
	// listing failed, which is what Error is for.
	Originals int `json:"originals"`
	// Error is the store listing's failure message, empty when the listing ran.
	Error string `json:"error,omitempty"`
}

// Listed reports whether the inventory actually ran, i.e. whether Originals is a
// count rather than an unknown.
func (i StoreInventory) Listed() bool {
	return i.Error == ""
}

// Report is the result of an integrity scan: the catalogue/store totals plus one
// Finding per problem class. A library with no problems has a zero Count in every
// Finding.
type Report struct {
	// Photos is the total number of catalogued photos (including archived).
	Photos int `json:"photos"`
	// FilesInDB is the number of catalogued files (originals plus sidecars).
	FilesInDB int `json:"files_in_db"`
	// Store is the inventory of the store the originals live in — the local root
	// on a filesystem instance, the bucket on an object-store one — together with
	// the reason it could not be read, if it could not.
	Store StoreInventory `json:"store"`
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
	// MissingPlaces are live photos that carry coordinates but have no cached
	// place yet — the reverse-geocode backlog, and the dry run of
	// `maintenance repair --places`. A photo with no coordinates never appears
	// here (there is nothing to look up), and neither does anything at all when
	// no mapy.com key is configured: with geocoding off the backlog is not a gap
	// that can be filled, so reporting it would only make the scan permanently
	// dirty.
	MissingPlaces Finding `json:"missing_places"`
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

// findings returns every Finding in the report, so an aggregate over all of them
// is written once rather than restated as a chain that grows with each new
// problem class.
func (r Report) findings() []Finding {
	return []Finding{
		r.MissingOriginals, r.OrphanFiles, r.MissingThumbnails, r.MissingEmbeddings,
		r.MissingFaces, r.MissingPhashes, r.MissingPlaces, r.TransposedDimensions,
		r.TransposedFaceBoxes, r.DuplicateFaceMarkers, r.SidewaysFaceDetections,
	}
}

// Clean reports whether the scan found no problems at all, i.e. every Finding has
// a zero Count.
//
// A scan whose store listing failed is never clean: it did not look at half of
// what it reconciles, and "the catalogue and the files agree" is a claim nobody
// checked. Reporting it as clean is how a store nobody could read passes for an
// empty one.
func (r Report) Clean() bool {
	if !r.Store.Listed() {
		return false
	}
	for _, finding := range r.findings() {
		if finding.Count > 0 {
			return false
		}
	}
	return true
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

// keySet turns the catalogued file paths into a lookup set, so the store listing
// can be streamed against it one key at a time instead of being collected first.
// It is a pure function, so the set-difference is exercised without any I/O.
func keySet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}
	return set
}
