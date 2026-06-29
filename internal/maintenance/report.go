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
	// MissingEmbeddings are photos with no CLIP image embedding yet.
	MissingEmbeddings Finding `json:"missing_embeddings"`
	// MissingFaces are photos that have never had face detection run.
	MissingFaces Finding `json:"missing_faces"`
	// MissingPhashes are photos with no perceptual hashes yet.
	MissingPhashes Finding `json:"missing_phashes"`
}

// Clean reports whether the scan found no problems at all, i.e. every Finding has
// a zero Count.
func (r Report) Clean() bool {
	return r.MissingOriginals.Count == 0 &&
		r.OrphanFiles.Count == 0 &&
		r.MissingThumbnails.Count == 0 &&
		r.MissingEmbeddings.Count == 0 &&
		r.MissingFaces.Count == 0 &&
		r.MissingPhashes.Count == 0
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
