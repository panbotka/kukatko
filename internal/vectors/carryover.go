package vectors

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// CarryAssignmentIoU is the minimum overlap between a face about to be deleted
// and a face about to replace it for the replacement to inherit the marker and
// subject the old row cached — the answer to "is this the same face, detected
// again?".
//
// It is deliberately far above the face↔marker matching threshold (0.1, see
// facematch): that one asks whether a detected face and a hand-drawn region
// describe the same area of a photo, where a generous overlap is the right call.
// This one decides whether to move somebody's name onto a box the detector has
// just produced, and the cost of being wrong is a photo attributed to the wrong
// person. Re-detecting the same picture puts the box back within a few pixels of
// where it was, so 0.5 clears every genuine re-detection with room to spare while
// refusing two different faces that merely stand close together.
//
// It is a geometric overlap, not a cosine distance, which is why it lives here
// rather than in docs/THRESHOLDS.md's recalibration procedure.
const CarryAssignmentIoU = 0.5

// IoU returns the Intersection-over-Union of two normalised boxes a and b, each
// in [x, y, w, h] form. The result is 0 when the boxes do not overlap (or either
// has a non-positive area) and 1 when they coincide exactly.
//
// It lives in this package because both users are here in spirit: the face rows
// stored by this store, and the face↔marker matching of internal/facematch that
// reads them (facematch.IoU is this function). One implementation of a geometric
// primitive is the point — two would drift.
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

// assignedFace is one of a photo's existing face rows that carries an assignment
// worth keeping: where it sits and whose it is. It is what a re-detection reads
// before it replaces the rows.
type assignedFace struct {
	// FaceIndex identifies the row it came from, and breaks ties deterministically.
	FaceIndex int
	// BBox is the normalised box the assignment was made against.
	BBox [4]float64
	// MarkerUID, SubjectUID and SubjectName are the cached link being carried.
	MarkerUID   *string
	SubjectUID  *string
	SubjectName string
}

// listAssignedFacesSQL reads the face rows of one photo that name somebody: a
// row with neither a marker nor a subject caches nothing worth carrying, and
// reading it would only add candidates to the pairing.
const listAssignedFacesSQL = `
SELECT face_index, bbox, marker_uid, subject_uid, subject_name
FROM faces
WHERE photo_uid = $1
  AND (marker_uid IS NOT NULL OR subject_uid IS NOT NULL)
ORDER BY face_index`

// listAssignedFaces reads photoUID's assigned face rows within tx, so a
// re-detection sees exactly the rows it is about to delete.
func listAssignedFaces(ctx context.Context, tx pgx.Tx, photoUID string) ([]assignedFace, error) {
	rows, err := tx.Query(ctx, listAssignedFacesSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("listing assigned faces for %s: %w", photoUID, err)
	}
	defer rows.Close()

	var assigned []assignedFace
	for rows.Next() {
		var (
			face assignedFace
			bbox []float64
		)
		if scanErr := rows.Scan(&face.FaceIndex, &bbox, &face.MarkerUID,
			&face.SubjectUID, &face.SubjectName); scanErr != nil {
			return nil, fmt.Errorf("scanning assigned face of %s: %w", photoUID, scanErr)
		}
		copy(face.BBox[:], bbox)
		assigned = append(assigned, face)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating assigned faces of %s: %w", photoUID, err)
	}
	return assigned, nil
}

// carryPair is one candidate hand-over: a new face, an old assigned face, and
// how much their boxes overlap.
type carryPair struct {
	newIndex int
	oldIndex int
	iou      float64
}

// carryAssignments copies the marker and subject cached on the faces being
// replaced onto the faces replacing them, pairing old and new exclusively by
// bounding-box overlap. It edits faces in place and returns how many assignments
// it moved.
//
// Re-running the detector on a photo must not un-name the people on it. The
// markers themselves are a separate table and survive untouched, so the naming is
// never actually lost — internal/facematch re-derives the cache from them the next
// time somebody opens the photo. But faces.subject_uid is what a person's gallery
// and a `person:` search read, and a photo that quietly leaves somebody's gallery
// until it is next viewed is a regression nobody would connect to the rebuild.
// Restoring the cache here makes the re-detection whole at the moment it commits.
//
// The pairing is greedy over the overlaps sorted by descending IoU, exclusive on
// both sides, exactly as facematch pairs faces with markers: a name moves onto at
// most one face, and a face takes at most one name. Ties are broken by the two
// indexes so the same input always yields the same result. A new face that
// overlaps nothing above CarryAssignmentIoU stays unassigned, which is the honest
// answer — the detector found something that was not there before.
func carryAssignments(faces []Face, assigned []assignedFace) int {
	if len(faces) == 0 || len(assigned) == 0 {
		return 0
	}
	pairs := collectCarryPairs(faces, assigned)
	slices.SortFunc(pairs, func(a, b carryPair) int {
		if c := cmp.Compare(b.iou, a.iou); c != 0 {
			return c
		}
		if c := cmp.Compare(a.newIndex, b.newIndex); c != 0 {
			return c
		}
		return cmp.Compare(a.oldIndex, b.oldIndex)
	})

	carried := 0
	takenNew := make(map[int]bool, len(faces))
	takenOld := make(map[int]bool, len(assigned))
	for _, pair := range pairs {
		if takenNew[pair.newIndex] || takenOld[pair.oldIndex] {
			continue
		}
		old := assigned[pair.oldIndex]
		faces[pair.newIndex].MarkerUID = old.MarkerUID
		faces[pair.newIndex].SubjectUID = old.SubjectUID
		faces[pair.newIndex].SubjectName = old.SubjectName
		takenNew[pair.newIndex] = true
		takenOld[pair.oldIndex] = true
		carried++
	}
	return carried
}

// collectCarryPairs returns every (new face, old assigned face) combination whose
// boxes overlap by at least CarryAssignmentIoU, in no particular order. A new face
// the caller already named is left out entirely: an explicit assignment is never
// replaced by an inherited one.
func collectCarryPairs(faces []Face, assigned []assignedFace) []carryPair {
	pairs := make([]carryPair, 0, len(faces))
	for newIndex, face := range faces {
		if face.MarkerUID != nil || face.SubjectUID != nil {
			continue
		}
		for oldIndex, old := range assigned {
			iou := IoU(face.BBox, old.BBox)
			if iou < CarryAssignmentIoU {
				continue
			}
			pairs = append(pairs, carryPair{newIndex: newIndex, oldIndex: oldIndex, iou: iou})
		}
	}
	return pairs
}
