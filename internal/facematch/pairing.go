package facematch

import (
	"cmp"
	"slices"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// markerMatch is the marker one face claims in an exclusive pairing, together
// with the overlap that won it.
type markerMatch struct {
	// MarkerUID is the claimed marker.
	MarkerUID string
	// IoU is the overlap between the face's box and that marker's box.
	IoU float64
}

// pairing is one photo's exclusive face↔marker assignment, keyed by the face's
// per-photo index (unique within a photo, so it identifies the face row).
type pairing map[int]markerMatch

// claims inverts a pairing into "marker uid → the face index that claimed it",
// the lookup every consumer of the pairing needs: whether a marker was matched at
// all, and by which face.
func (p pairing) claims() map[string]int {
	claimed := make(map[string]int, len(p))
	for faceIndex, match := range p {
		claimed[match.MarkerUID] = faceIndex
	}
	return claimed
}

// overlap is one face↔marker pair whose IoU clears the matching threshold, held
// while the exclusive pairing is resolved.
type overlap struct {
	faceIndex int
	markerUID string
	iou       float64
}

// matchMarkers pairs a photo's detected faces with its markers so that every
// marker is claimed by at most one face and every face claims at most one marker.
//
// It scores every (face, marker) combination that clears threshold, sorts the
// pairs by descending IoU and takes them greedily while both sides are still
// free. Greedy over the sorted pairs — rather than the per-face "best marker" it
// replaces — is what makes the pairing exclusive: a second face can no longer
// claim a marker a closer face already took, which is how one person came to be
// rendered twice on a photo (and cached twice onto the face rows).
//
// Ties are broken by face index, then marker uid, so the same input always yields
// the same pairing; an unstable choice would have consecutive reads of one photo
// rewrite the cached links back and forth. Only face-type, non-invalid markers
// take part. For the handful of faces and markers a photo has, greedy over the
// sorted pairs is ample — an optimal (Hungarian) assignment would buy nothing
// measurable here.
func matchMarkers(faces []vectors.Face, markers []people.Marker, threshold float64) pairing {
	overlaps := collectOverlaps(faces, markers, threshold)
	slices.SortFunc(overlaps, func(a, b overlap) int {
		if c := cmp.Compare(b.iou, a.iou); c != 0 {
			return c
		}
		if c := cmp.Compare(a.faceIndex, b.faceIndex); c != 0 {
			return c
		}
		return cmp.Compare(a.markerUID, b.markerUID)
	})

	matched := make(pairing, min(len(faces), len(markers)))
	taken := make(map[string]bool, len(markers))
	for _, o := range overlaps {
		if _, busy := matched[o.faceIndex]; busy || taken[o.markerUID] {
			continue
		}
		matched[o.faceIndex] = markerMatch{MarkerUID: o.markerUID, IoU: o.iou}
		taken[o.markerUID] = true
	}
	return matched
}

// collectOverlaps returns every face↔marker pair whose IoU is positive and at
// least threshold, skipping markers that are not face regions or were marked
// invalid.
func collectOverlaps(faces []vectors.Face, markers []people.Marker, threshold float64) []overlap {
	overlaps := make([]overlap, 0, len(faces))
	for i := range faces {
		for j := range markers {
			if markers[j].Type != people.MarkerFace || markers[j].Invalid {
				continue
			}
			iou := IoU(faces[i].BBox, markerBox(markers[j]))
			if iou <= 0 || iou < threshold {
				continue
			}
			overlaps = append(overlaps, overlap{
				faceIndex: faces[i].FaceIndex,
				markerUID: markers[j].UID,
				iou:       iou,
			})
		}
	}
	return overlaps
}

// surplusFaces returns the face indexes whose cached marker link must be dropped
// for the invariant "a marker is claimed by at most one face" to hold on this
// photo.
//
// A cached marker's rightful keeper is the face the exclusive pairing awarded it
// to. A marker no face overlaps enough to claim any more — a link an import wrote
// from foreign data, or a region a later edit moved away from the face — has no
// such keeper; there the lowest face index keeps it. That rule exists purely so
// the repair converges: without it a duplicate nobody claims would survive every
// run and the scan would report it forever.
func surplusFaces(faces []vectors.Face, matched pairing) map[int]bool {
	keeper := matched.claims()
	unclaimed := make(map[string]int, len(faces))
	for i := range faces {
		uid := derefSubject(faces[i].MarkerUID)
		if uid == "" {
			continue
		}
		if _, claimed := keeper[uid]; claimed {
			continue
		}
		if lowest, seen := unclaimed[uid]; !seen || faces[i].FaceIndex < lowest {
			unclaimed[uid] = faces[i].FaceIndex
		}
	}

	surplus := make(map[int]bool)
	for i := range faces {
		uid := derefSubject(faces[i].MarkerUID)
		if uid == "" {
			continue
		}
		keeps, ok := keeper[uid]
		if !ok {
			keeps = unclaimed[uid]
		}
		if keeps != faces[i].FaceIndex {
			surplus[faces[i].FaceIndex] = true
		}
	}
	return surplus
}

// markersByUID indexes a photo's markers by uid so a resolved pairing can read
// back the marker it named.
func markersByUID(markers []people.Marker) map[string]people.Marker {
	byUID := make(map[string]people.Marker, len(markers))
	for i := range markers {
		byUID[markers[i].UID] = markers[i]
	}
	return byUID
}
