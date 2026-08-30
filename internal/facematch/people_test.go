package facematch

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestPhotoPeople_namedAndUnassigned checks the roll-call reports a detection
// whose marker names somebody as that person, and one nobody has named as an
// unassigned detection carrying its detector score.
func TestPhotoPeople_namedAndUnassigned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	box := [4]float64{0.1, 0.1, 0.3, 0.3}
	fp := &fakePhotos{photo: photos.Photo{FileWidth: 4000, FileHeight: 3000, FileOrientation: 1}}
	ff := &fakeFaces{list: []vectors.Face{
		{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: box, DetScore: 0.94},
		{FaceIndex: 1, Vector: make([]float32, vectors.FaceDim), BBox: [4]float64{0.6, 0.6, 0.2, 0.2}, DetScore: 0.71},
	}}
	subjUID := "su_alice"
	pe := &fakePeople{
		markers: []people.Marker{{
			UID: "mk1", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3],
			SubjectUID: &subjUID,
		}},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	onPhoto, err := svc.PhotoPeople(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoPeople: %v", err)
	}
	if len(onPhoto) != 2 {
		t.Fatalf("got %d people, want 2: %+v", len(onPhoto), onPhoto)
	}
	named := onPhoto[0]
	if named.SubjectUID != subjUID || named.SubjectName != "Alice" || named.MarkerUID != "mk1" {
		t.Errorf("first = %+v, want Alice on mk1", named)
	}
	if named.DetScore != 0.94 {
		t.Errorf("first det_score = %v, want the detector's own 0.94", named.DetScore)
	}
	unassigned := onPhoto[1]
	if unassigned.SubjectUID != "" || unassigned.SubjectName != "" || unassigned.MarkerUID != "" {
		t.Errorf("second = %+v, want an unnamed detection", unassigned)
	}
	if unassigned.DetScore != 0.71 {
		t.Errorf("second det_score = %v, want 0.71", unassigned.DetScore)
	}
}

// TestPhotoPeople_isReadOnly checks the roll-call neither writes the face↔marker
// cache nor runs a suggestion search — the two things that make PhotoFaces too
// expensive to answer a plain detail request with.
func TestPhotoPeople_isReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	box := [4]float64{0.1, 0.1, 0.3, 0.3}
	fp := &fakePhotos{photo: photos.Photo{FileWidth: 100, FileHeight: 100, FileOrientation: 1}}
	ff := &fakeFaces{
		list:       []vectors.Face{{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: box}},
		candidates: []vectors.FaceCandidate{cand("p2", "su_bob", "Bob", 0.1, 0.3)},
	}
	subjUID := "su_alice"
	pe := &fakePeople{
		markers: []people.Marker{{
			UID: "mk1", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3],
			SubjectUID: &subjUID,
		}},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	if _, err := svc.PhotoPeople(ctx, "p1"); err != nil {
		t.Fatalf("PhotoPeople: %v", err)
	}
	if ff.updates != 0 {
		t.Errorf("wrote the face cache %d times, want none from a read", ff.updates)
	}
	if len(ff.searchDists) != 0 {
		t.Errorf("ran %d suggestion searches, want none", len(ff.searchDists))
	}
}

// TestPhotoPeople_handDrawnMarker checks a face-type marker that no detection
// claimed is still reported — a region somebody drew by hand is a person on the
// photo even though the detector never saw a face there.
func TestPhotoPeople_handDrawnMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fp := &fakePhotos{photo: photos.Photo{FileWidth: 100, FileHeight: 100, FileOrientation: 1}}
	ff := &fakeFaces{}
	subjUID := "su_alice"
	invalid := "su_ghost"
	pe := &fakePeople{
		markers: []people.Marker{
			{UID: "mk1", Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.2, H: 0.2, SubjectUID: &subjUID},
			// Neither of these belongs in a roll-call: one is not a face, the other
			// was marked invalid.
			{UID: "mk2", Type: people.MarkerLabel, X: 0.4, Y: 0.4, W: 0.2, H: 0.2},
			{UID: "mk3", Type: people.MarkerFace, X: 0.7, Y: 0.7, W: 0.2, H: 0.2,
				SubjectUID: &invalid, Invalid: true},
		},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	onPhoto, err := svc.PhotoPeople(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoPeople: %v", err)
	}
	if len(onPhoto) != 1 {
		t.Fatalf("got %d people, want only the hand-drawn face marker: %+v", len(onPhoto), onPhoto)
	}
	if onPhoto[0].MarkerUID != "mk1" || onPhoto[0].SubjectName != "Alice" {
		t.Errorf("person = %+v, want Alice on mk1", onPhoto[0])
	}
	if onPhoto[0].DetScore != 0 {
		t.Errorf("det_score = %v, want 0 for a marker no detection matched", onPhoto[0].DetScore)
	}
}

// TestPhotoPeople_empty checks a photo with neither faces nor markers yields an
// empty, non-nil slice, so the detail response can tell "nobody" from "not asked".
func TestPhotoPeople_empty(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotos{}, &fakeFaces{}, &fakePeople{})
	onPhoto, err := svc.PhotoPeople(context.Background(), "p1")
	if err != nil {
		t.Fatalf("PhotoPeople: %v", err)
	}
	if onPhoto == nil || len(onPhoto) != 0 {
		t.Errorf("people = %+v, want an empty non-nil slice", onPhoto)
	}
}
