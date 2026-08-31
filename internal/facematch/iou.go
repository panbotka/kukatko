// Package facematch connects detected faces to markers and subjects. It computes
// the Intersection-over-Union (IoU) overlap between a face's bounding box and the
// existing markers on a photo to decide whether they describe the same region,
// drives the editor-facing assignment state machine (create a marker, assign or
// unassign a subject), and suggests likely identities for an unnamed face from the
// nearest assigned face embeddings.
//
// All coordinates are normalised [x, y, w, h] boxes in the 0..1 display space
// shared by faces.bbox and markers, so geometry needs no per-photo scaling. The
// matching threshold and suggestion tunables mirror photo-sorter (IoU ≥ 0.1).
//
// Every collaborator is an interface so the Service unit-tests its pure geometry
// and suggestion-filtering logic without a database, and integration-tests the
// assignment and suggestion flows against the real stores.
package facematch

import "github.com/panbotka/kukatko/internal/vectors"

// IoU returns the Intersection-over-Union of two normalised boxes a and b, each in
// [x, y, w, h] form. The result is 0 when the boxes do not overlap (or either has a
// non-positive area) and 1 when they coincide exactly. It is the overlap score
// face↔marker matching thresholds against.
//
// The geometry itself lives in internal/vectors, which needs the same measure to
// hand a re-detected face the assignment its predecessor carried. Two copies of a
// primitive drift; this one is the name this package matches by.
func IoU(a, b [4]float64) float64 {
	return vectors.IoU(a, b)
}
