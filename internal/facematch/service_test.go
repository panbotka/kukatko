package facematch

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakePhotos is an in-memory PhotoStore.
type fakePhotos struct {
	photo photos.Photo
	err   error
}

func (f *fakePhotos) GetByUID(_ context.Context, _ string) (photos.Photo, error) {
	return f.photo, f.err
}

// fakeFaces is an in-memory FaceStore recording the last UpdateFaceMarker call and
// the distance cutoff of every candidate search, so a test can tell the primary pass
// from the widening fallback (which searches with no cutoff, i.e. 0).
type fakeFaces struct {
	list        []vectors.Face
	candidates  []vectors.FaceCandidate
	searchDists []float64
	lastMarker  string
	lastSubject string
	lastFaceIdx int
	updates     int
}

func (f *fakeFaces) ListFaces(_ context.Context, _ string) ([]vectors.Face, error) {
	return f.list, nil
}

func (f *fakeFaces) FindSimilarFaceCandidates(
	_ context.Context, _ []float32, _ int, maxDistance float64,
) ([]vectors.FaceCandidate, error) {
	f.searchDists = append(f.searchDists, maxDistance)
	return f.candidates, nil
}

func (f *fakeFaces) UpdateFaceMarker(
	_ context.Context, _ string, faceIndex int, markerUID, subjectUID, _ string,
) error {
	f.updates++
	f.lastFaceIdx = faceIndex
	f.lastMarker = markerUID
	f.lastSubject = subjectUID
	return nil
}

// fakePeople is an in-memory PeopleStore for the assignment state machine.
type fakePeople struct {
	markers        []people.Marker
	subjectsByUID  map[string]people.Subject
	subjectsBySlug map[string]people.Subject
	created        []people.Subject
	createdMarker  *people.Marker
	assigned       [2]string
	reviewed       *bool
	unassigned     string
	nextUID        int
	lastEntry      audit.Entry
}

func (f *fakePeople) ListMarkersByPhoto(_ context.Context, _ string) ([]people.Marker, error) {
	return f.markers, nil
}

func (f *fakePeople) CreateMarkerAudited(
	_ context.Context, m people.Marker, entry audit.Entry,
) (people.Marker, error) {
	m.UID = "mk_new"
	f.createdMarker = &m
	f.lastEntry = entry
	return m, nil
}

func (f *fakePeople) AssignSubjectAudited(
	_ context.Context, markerUID, subjectUID string, entry audit.Entry,
) (people.Marker, error) {
	f.assigned = [2]string{markerUID, subjectUID}
	f.lastEntry = entry
	return people.Marker{UID: markerUID, SubjectUID: &subjectUID}, nil
}

func (f *fakePeople) UnassignSubjectAudited(
	_ context.Context, markerUID string, entry audit.Entry,
) (people.Marker, error) {
	f.unassigned = markerUID
	f.lastEntry = entry
	return people.Marker{UID: markerUID}, nil
}

func (f *fakePeople) SetMarkerReviewed(_ context.Context, uid string, reviewed bool) (people.Marker, error) {
	f.reviewed = &reviewed
	return people.Marker{UID: uid, Reviewed: reviewed}, nil
}

func (f *fakePeople) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	if s, ok := f.subjectsByUID[uid]; ok {
		return s, nil
	}
	return people.Subject{}, people.ErrSubjectNotFound
}

func (f *fakePeople) GetSubjectBySlug(_ context.Context, slug string) (people.Subject, error) {
	if s, ok := f.subjectsBySlug[slug]; ok {
		return s, nil
	}
	return people.Subject{}, people.ErrSubjectNotFound
}

func (f *fakePeople) CreateSubject(_ context.Context, s people.Subject) (people.Subject, error) {
	f.nextUID++
	s.UID = "su_created"
	s.Slug = people.Slugify(s.Name)
	f.created = append(f.created, s)
	return s, nil
}

// newService builds a Service over the three fakes with default tunables.
func newService(p PhotoStore, fc FaceStore, pe PeopleStore) *Service {
	return New(Config{Photos: p, Faces: fc, People: pe})
}

// TestPhotoFaces_actions checks that IoU matching drives the recommended action and
// caches the match on the face row.
func TestPhotoFaces_actions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	box := [4]float64{0.1, 0.1, 0.3, 0.3}
	fp := &fakePhotos{photo: photos.Photo{FileWidth: 4000, FileHeight: 3000, FileOrientation: 1}}
	ff := &fakeFaces{list: []vectors.Face{{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: box}}}
	subjUID := "su_alice"
	pe := &fakePeople{
		markers: []people.Marker{{
			UID: "mk1", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3],
			SubjectUID: &subjUID,
		}},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	if len(resp.Faces) != 1 {
		t.Fatalf("got %d faces, want 1", len(resp.Faces))
	}
	face := resp.Faces[0]
	if face.Action != ActionAlreadyDone || face.MarkerUID != "mk1" || face.SubjectName != "Alice" {
		t.Errorf("face = %+v, want already_done/mk1/Alice", face)
	}
	if ff.updates != 1 || ff.lastMarker != "mk1" || ff.lastSubject != subjUID {
		t.Errorf("cache not written: updates=%d marker=%s subject=%s", ff.updates, ff.lastMarker, ff.lastSubject)
	}
}

// TestPhotoFaces_createMarkerAndSuggestions checks an unmatched face reports
// create_marker and gets suggestions from assigned neighbours.
func TestPhotoFaces_createMarkerAndSuggestions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fp := &fakePhotos{photo: photos.Photo{FileWidth: 100, FileHeight: 100, FileOrientation: 1}}
	ff := &fakeFaces{
		list:       []vectors.Face{{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: [4]float64{0.5, 0.5, 0.2, 0.2}}},
		candidates: []vectors.FaceCandidate{cand("p2", "su_bob", "Bob", 0.1, 0.3)},
	}
	pe := &fakePeople{} // no markers
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	face := resp.Faces[0]
	if face.Action != ActionCreateMarker || face.MarkerUID != "" {
		t.Errorf("face = %+v, want create_marker with no marker", face)
	}
	if len(face.Suggestions) != 1 || face.Suggestions[0].SubjectUID != "su_bob" {
		t.Errorf("suggestions = %+v, want one Bob suggestion", face.Suggestions)
	}
}

// TestPhotoFaces_suggestionsForAssignedFace checks an assigned face also gets
// suggestions — the alternatives a reassignment can pick from — and that the subject
// it already names is not among them.
func TestPhotoFaces_suggestionsForAssignedFace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	box := [4]float64{0.1, 0.1, 0.3, 0.3}
	subjUID := "su_alice"
	fp := &fakePhotos{photo: photos.Photo{FileWidth: 100, FileHeight: 100, FileOrientation: 1}}
	ff := &fakeFaces{
		list: []vectors.Face{{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: box}},
		// Alice is the face's own subject, Bob a nearby stranger: only Bob may be offered.
		candidates: []vectors.FaceCandidate{
			cand("p2", subjUID, "Alice", 0.1, 0.3),
			cand("p3", "su_bob", "Bob", 0.2, 0.3),
		},
	}
	pe := &fakePeople{
		markers: []people.Marker{{
			UID: "mk1", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3],
			SubjectUID: &subjUID,
		}},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	face := resp.Faces[0]
	if face.Action != ActionAlreadyDone || face.SubjectName != "Alice" {
		t.Fatalf("face = %+v, want already_done/Alice", face)
	}
	if len(face.Suggestions) != 1 || face.Suggestions[0].SubjectUID != "su_bob" {
		t.Errorf("suggestions = %+v, want only Bob (Alice names the face already)", face.Suggestions)
	}
}

// TestPhotoFaces_assignedFaceDoesNotWidenSearch checks an assigned face runs only the
// primary, distance-capped search: widening to no cutoff would offer every named person
// in the library as a "reassignment" for a face that has no close alternative.
func TestPhotoFaces_assignedFaceDoesNotWidenSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	box := [4]float64{0.1, 0.1, 0.3, 0.3}
	subjUID := "su_alice"
	fp := &fakePhotos{photo: photos.Photo{FileWidth: 100, FileHeight: 100, FileOrientation: 1}}
	ff := &fakeFaces{
		list: []vectors.Face{{FaceIndex: 0, Vector: make([]float32, vectors.FaceDim), BBox: box}},
		// Only the face's own subject is near: the primary pass yields nothing, and the
		// fallback must not run to fill the gap.
		candidates: []vectors.FaceCandidate{cand("p2", subjUID, "Alice", 0.1, 0.3)},
	}
	pe := &fakePeople{
		markers: []people.Marker{{
			UID: "mk1", Type: people.MarkerFace, X: box[0], Y: box[1], W: box[2], H: box[3],
			SubjectUID: &subjUID,
		}},
		subjectsByUID: map[string]people.Subject{subjUID: {UID: subjUID, Name: "Alice"}},
	}
	svc := newService(fp, ff, pe)

	resp, err := svc.PhotoFaces(ctx, "p1")
	if err != nil {
		t.Fatalf("PhotoFaces: %v", err)
	}
	if got := resp.Faces[0].Suggestions; len(got) != 0 {
		t.Errorf("suggestions = %+v, want none (no plausible alternative)", got)
	}
	want := []float64{DefaultSuggestionMaxDistance}
	if !slices.Equal(ff.searchDists, want) {
		t.Errorf("candidate searches ran with cutoffs %v, want exactly %v (no widening pass)", ff.searchDists, want)
	}
}

// TestApply_createMarkerFindOrCreate checks create_marker auto-creates a subject by
// name and links the face.
func TestApply_createMarkerFindOrCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ff := &fakeFaces{}
	pe := &fakePeople{subjectsBySlug: map[string]people.Subject{}}
	svc := newService(&fakePhotos{}, ff, pe)

	idx := 0
	box := [4]float64{0.1, 0.1, 0.2, 0.2}
	res, err := svc.Apply(ctx, AssignRequest{
		PhotoUID: "p1", Action: ActionCreateMarker, SubjectName: "New Person",
		FaceIndex: &idx, BBox: &box,
	}, audit.Meta{ActorUID: "usr_actor"})
	if err != nil {
		t.Fatalf("Apply create_marker: %v", err)
	}
	if len(pe.created) != 1 || pe.created[0].Name != "New Person" {
		t.Fatalf("subject not created: %+v", pe.created)
	}
	if pe.lastEntry.Action != audit.ActionFaceAssign || pe.lastEntry.ActorUID != "usr_actor" {
		t.Errorf("create_marker audit entry = %+v, want face.assign by usr_actor", pe.lastEntry)
	}
	if pe.createdMarker == nil || !pe.createdMarker.Reviewed {
		t.Errorf("marker not created reviewed: %+v", pe.createdMarker)
	}
	if res.Subject == nil || res.Subject.UID != "su_created" {
		t.Errorf("result subject = %+v, want su_created", res.Subject)
	}
	if ff.updates != 1 || ff.lastMarker != "mk_new" {
		t.Errorf("face not linked: updates=%d marker=%s", ff.updates, ff.lastMarker)
	}
}

// TestApply_findExistingSubjectBySlug checks an existing subject is reused, not
// duplicated.
func TestApply_findExistingSubjectBySlug(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	existing := people.Subject{UID: "su_existing", Name: "Anna", Slug: "anna"}
	pe := &fakePeople{
		markers:        []people.Marker{{UID: "mk1", Type: people.MarkerFace}},
		subjectsBySlug: map[string]people.Subject{"anna": existing},
	}
	svc := newService(&fakePhotos{}, &fakeFaces{}, pe)

	res, err := svc.Apply(ctx, AssignRequest{
		PhotoUID: "p1", Action: ActionAssignPerson, MarkerUID: "mk1", SubjectName: "Anna",
	}, audit.Meta{ActorUID: "usr_actor"})
	if err != nil {
		t.Fatalf("Apply assign_person: %v", err)
	}
	if pe.lastEntry.Action != audit.ActionFaceAssign || pe.lastEntry.TargetUID != "mk1" {
		t.Errorf("assign_person audit entry = %+v, want face.assign targeting mk1", pe.lastEntry)
	}
	if len(pe.created) != 0 {
		t.Errorf("created %d subjects, want 0 (reuse existing)", len(pe.created))
	}
	if pe.assigned != [2]string{"mk1", "su_existing"} {
		t.Errorf("assigned = %v, want mk1/su_existing", pe.assigned)
	}
	if pe.reviewed == nil || !*pe.reviewed {
		t.Errorf("marker not marked reviewed on assign")
	}
	if res.Subject == nil || res.Subject.UID != "su_existing" {
		t.Errorf("result subject = %+v, want su_existing", res.Subject)
	}
}

// TestApply_unassignClearsReviewed checks unassign clears the subject and reviewed.
func TestApply_unassignClearsReviewed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pe := &fakePeople{}
	svc := newService(&fakePhotos{}, &fakeFaces{}, pe)

	res, err := svc.Apply(ctx, AssignRequest{
		PhotoUID: "p1", Action: ActionUnassignPerson, MarkerUID: "mk1",
	}, audit.Meta{ActorUID: "usr_actor"})
	if err != nil {
		t.Fatalf("Apply unassign_person: %v", err)
	}
	if pe.unassigned != "mk1" {
		t.Errorf("unassigned = %q, want mk1", pe.unassigned)
	}
	if pe.lastEntry.Action != audit.ActionFaceUnassign || pe.lastEntry.TargetUID != "mk1" {
		t.Errorf("unassign audit entry = %+v, want face.unassign targeting mk1", pe.lastEntry)
	}
	if pe.reviewed == nil || *pe.reviewed {
		t.Errorf("marker reviewed not cleared on unassign")
	}
	if res.Subject != nil {
		t.Errorf("unassign result subject = %+v, want nil", res.Subject)
	}
}

// TestApply_validation checks the request-validation error paths.
func TestApply_validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(&fakePhotos{}, &fakeFaces{}, &fakePeople{subjectsBySlug: map[string]people.Subject{}})

	tests := []struct {
		name    string
		req     AssignRequest
		wantErr error
	}{
		{"unknown action", AssignRequest{Action: "frobnicate"}, ErrInvalidAction},
		{"create without bbox", AssignRequest{Action: ActionCreateMarker, SubjectName: "X"}, ErrMissingBBox},
		{"assign without marker", AssignRequest{Action: ActionAssignPerson, SubjectName: "X"}, ErrMissingMarker},
		{"unassign without marker", AssignRequest{Action: ActionUnassignPerson}, ErrMissingMarker},
		{
			"create without subject",
			AssignRequest{Action: ActionCreateMarker, BBox: &[4]float64{0, 0, 0.1, 0.1}},
			ErrMissingSubject,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := svc.Apply(ctx, tt.req, audit.Meta{}); !errors.Is(err, tt.wantErr) {
				t.Errorf("Apply(%+v) err = %v, want %v", tt.req, err, tt.wantErr)
			}
		})
	}
}
