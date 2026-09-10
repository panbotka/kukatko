package facematch

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// handAttachedMarker is the row a hand attach writes: a person named on the
// photo, with no region at all.
func handAttachedMarker(uid, subjectUID string) people.Marker {
	return people.Marker{UID: uid, Type: people.MarkerPerson, SubjectUID: &subjectUID}
}

// TestPhotoPeople_ignoresHandAttached checks the per-photo roll-call reports only
// what has a box.
//
// The roll-call is what the detail UI draws the face overlay from, and every
// entry it carries is rendered as a rectangle. A hand-attached person has none,
// so including them would paint a box at (0, 0, 0, 0) — a person shown in the
// photo's top-left corner, which is a claim nobody made. They are reported by the
// detail response's own `people` block instead.
func TestPhotoPeople_ignoresHandAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fp := &fakePhotos{photo: photos.Photo{FileWidth: 4000, FileHeight: 3000, FileOrientation: 1}}
	ff := &fakeFaces{}
	pe := &fakePeople{
		markers:       []people.Marker{handAttachedMarker("mk1", "su_alice")},
		subjectsByUID: map[string]people.Subject{"su_alice": {UID: "su_alice", Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	onPhoto, err := svc.PhotoPeople(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoPeople: %v", err)
	}
	if len(onPhoto) != 0 {
		t.Fatalf("roll-call = %+v, want nobody: a hand-attached person has no box", onPhoto)
	}
}

// TestPhotoFaces_ignoresHandAttached checks the face editor's payload the same
// way: the hand-attached person is not one of the photo's face views.
func TestPhotoFaces_ignoresHandAttached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fp := &fakePhotos{photo: photos.Photo{FileWidth: 4000, FileHeight: 3000, FileOrientation: 1}}
	ff := &fakeFaces{}
	pe := &fakePeople{
		markers:       []people.Marker{handAttachedMarker("mk1", "su_alice")},
		subjectsByUID: map[string]people.Subject{"su_alice": {UID: "su_alice", Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	if len(resp.Faces) != 0 {
		t.Fatalf("faces = %+v, want none", resp.Faces)
	}
}

// TestPhotoFaces_handAttachedStaysSuggestable checks a hand-attached person is
// still offered as a candidate for an unnamed face on the same photo.
//
// The exclusion set exists so a face does not suggest somebody already placed on
// the photo by a marker. A hand-attached link is not that: it exists *because*
// the detector never found this person's face, so once detection does produce
// one, that person is the likeliest answer for it — not the one candidate to
// hide.
func TestPhotoFaces_handAttachedStaysSuggestable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fp := &fakePhotos{photo: photos.Photo{FileWidth: 4000, FileHeight: 3000, FileOrientation: 1}}
	ff := &fakeFaces{
		list: []vectors.Face{{
			FaceIndex: 0, Vector: make([]float32, vectors.FaceDim),
			BBox: [4]float64{0.1, 0.1, 0.3, 0.3},
		}},
		candidates: []vectors.FaceCandidate{cand("p2", "su_alice", "Alice", 0.1, 0.3)},
	}
	pe := &fakePeople{
		markers:       []people.Marker{handAttachedMarker("mk1", "su_alice")},
		subjectsByUID: map[string]people.Subject{"su_alice": {UID: "su_alice", Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	if len(resp.Faces) != 1 {
		t.Fatalf("faces = %+v, want the one detection", resp.Faces)
	}
	suggestions := resp.Faces[0].Suggestions
	if len(suggestions) != 1 || suggestions[0].SubjectUID != "su_alice" {
		t.Fatalf("suggestions = %+v, want Alice offered for the unnamed face", suggestions)
	}
}
