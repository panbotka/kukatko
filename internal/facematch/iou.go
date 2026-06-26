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

// IoU returns the Intersection-over-Union of two normalised boxes a and b, each in
// [x, y, w, h] form. The result is 0 when the boxes do not overlap (or either has a
// non-positive area) and 1 when they coincide exactly. It is the overlap score
// face↔marker matching thresholds against.
func IoU(a, b [4]float64) float64 {
	ax1, ay1, ax2, ay2 := a[0], a[1], a[0]+a[2], a[1]+a[3]
	bx1, by1, bx2, by2 := b[0], b[1], b[0]+b[2], b[1]+b[3]

	interX1, interY1 := max(ax1, bx1), max(ay1, by1)
	interX2, interY2 := min(ax2, bx2), min(ay2, by2)
	if interX2 <= interX1 || interY2 <= interY1 {
		return 0 // no overlap
	}
	intersection := (interX2 - interX1) * (interY2 - interY1)

	areaA := a[2] * a[3]
	areaB := b[2] * b[3]
	union := areaA + areaB - intersection
	if union <= 0 {
		return 0
	}
	return intersection / union
}
