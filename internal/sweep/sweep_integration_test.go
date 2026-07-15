//go:build integration

package sweep_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against KUKATKO_TEST_DATABASE_URL.
// They share one database and truncate between cases, so they do not run in parallel.
// They exercise the sweep over the REAL candidate service and stores; the worker-pool
// concurrency bound is proven deterministically by TestSweep_concurrencyBounded (the
// unit test with a fake finder), which does not need a database.

// sweepHarness bundles the stores and the sweep over a freshly truncated database.
type sweepHarness struct {
	faces    *vectors.Store
	people   *people.Store
	feedback *feedback.Store
	photos   *photos.Store
	svc      *sweep.Service
}

// newSweepHarness returns a harness over a truncated integration database, wiring the
// real candidate service as the sweep's finder.
func newSweepHarness(t *testing.T) sweepHarness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	faceStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	feedbackStore := feedback.NewStore(db.Pool())
	photoStore := photos.NewStore(db.Pool())
	cand := candidates.New(candidates.Config{
		Faces: faceStore, People: peopleStore, Feedback: feedbackStore, Photos: photoStore,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 4, MinFaceRel: 0.02,
	})
	svc := sweep.New(sweep.Config{
		Subjects:    peopleStore,
		Finder:      cand,
		Concurrency: 3,
		MaxSubjects: 500,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return sweepHarness{faces: faceStore, people: peopleStore, feedback: feedbackStore, photos: photoStore, svc: svc}
}

// reviewableBox is a normalised face box large enough to clear both size floors on an
// 800px-tall photo (0.3*1000 = 300px wide).
var reviewableBox = [4]float64{0.3, 0.3, 0.3, 0.3}

// vec builds a FaceDim vector from index→value overrides.
func vec(set map[int]float32) []float32 {
	v := make([]float32, vectors.FaceDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// nearE0 is a face vector 0.2 cosine-distance from e0 (well within 0.5).
func nearE0() []float32 { return vec(map[int]float32{0: 0.8, 1: 0.6}) }

// makePhoto inserts a reviewable 1000x800 photo and returns its uid.
func (h sweepHarness) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 800, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// saveUnassigned writes one unassigned face onto a fresh photo and returns the uid.
func (h sweepHarness) saveUnassigned(t *testing.T, hash string, v []float32) string {
	t.Helper()
	photoUID := h.makePhoto(t, hash)
	face := vectors.Face{
		FaceIndex: 0, Vector: v, DetScore: 0.9, BBox: reviewableBox,
		PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1,
	}
	if err := h.faces.SaveFaces(context.Background(), photoUID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces(%s): %v", hash, err)
	}
	return photoUID
}

// exemplar plants an assigned face for a subject the production way — a marker plus a
// faces-cache row that points at it — so the subject both has an exemplar to search
// from and shows up under ListSubjects with a marker count > 0.
func (h sweepHarness) exemplar(t *testing.T, hash, subjectUID string, v []float32) {
	t.Helper()
	ctx := context.Background()
	photoUID := h.makePhoto(t, hash)
	marker, err := h.people.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID, Type: people.MarkerFace,
		X: 0.3, Y: 0.3, W: 0.3, H: 0.3, Reviewed: true,
	})
	if err != nil {
		t.Fatalf("CreateMarker(%s): %v", hash, err)
	}
	face := vectors.Face{
		FaceIndex: 0, Vector: v, DetScore: 0.95, BBox: reviewableBox,
		SubjectUID: &subjectUID, MarkerUID: &marker.UID,
		PhotoWidth: 1000, PhotoHeight: 800, Orientation: 1,
	}
	if err := h.faces.SaveFaces(ctx, photoUID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces(%s): %v", hash, err)
	}
}

// run executes the sweep and returns every emitted event.
func (h sweepHarness) run(t *testing.T) []sweep.Event {
	t.Helper()
	var events []sweep.Event
	err := h.svc.Sweep(context.Background(), sweep.Params{}, func(ev sweep.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return events
}

// persons returns the person payloads from a stream.
func persons(events []sweep.Event) []sweep.Person {
	var out []sweep.Person
	for _, ev := range events {
		if ev.Type == sweep.EventPerson && ev.Person != nil {
			out = append(out, *ev.Person)
		}
	}
	return out
}

// summary returns the terminal summary payload.
func summary(t *testing.T, events []sweep.Event) sweep.Summary {
	t.Helper()
	last := events[len(events)-1]
	if last.Type != sweep.EventSummary || last.Summary == nil {
		t.Fatalf("last event = %+v, want summary", last)
	}
	return *last.Summary
}

// TestSweep_onlyMatchingSubjectAppearsDB checks that of three subjects — one with an
// unnamed lookalike, one with only mismatched faces, one with no faces at all — only
// the first produces a person card, and the faceless subject is skipped.
func TestSweep_onlyMatchingSubjectAppearsDB(t *testing.T) {
	h := newSweepHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject Alice: %v", err)
	}
	bob, err := h.people.CreateSubject(ctx, people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject Bob: %v", err)
	}
	if _, err = h.people.CreateSubject(ctx, people.Subject{Name: "Cara"}); err != nil {
		t.Fatalf("CreateSubject Cara: %v", err)
	}
	// Alice has an exemplar at e0 and one unnamed lookalike nearby; Bob has an exemplar
	// far away at e5 and no unnamed lookalike near it; Cara has nothing.
	h.exemplar(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))
	h.exemplar(t, "bob-src", bob.UID, vec(map[int]float32{5: 1}))
	wanted := h.saveUnassigned(t, "alice-lookalike", nearE0())

	events := h.run(t)

	got := persons(events)
	if len(got) != 1 || got[0].Subject.UID != alice.UID {
		t.Fatalf("persons = %+v, want one for Alice", got)
	}
	if len(got[0].Candidates) != 1 || got[0].Candidates[0].Photo.UID != wanted {
		t.Errorf("Alice candidates = %+v, want the planted lookalike %s", got[0].Candidates, wanted)
	}
	sum := summary(t, events)
	if sum.PeopleScanned != 2 || sum.SubjectsTotal != 2 || sum.PeopleWithMatches != 1 {
		t.Errorf("summary = %+v, want scanned 2 (Cara skipped), matches 1", sum)
	}
}

// TestSweep_rejectedCandidateNotShownDB checks a face the user rejected for a subject
// never appears in that subject's sweep card.
func TestSweep_rejectedCandidateNotShownDB(t *testing.T) {
	h := newSweepHarness(t)
	ctx := t.Context()
	alice, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	h.exemplar(t, "alice-src", alice.UID, vec(map[int]float32{0: 1}))
	keep := h.saveUnassigned(t, "keep", vec(map[int]float32{0: 1}))
	rejected := h.saveUnassigned(t, "rejected", nearE0())

	entry := audit.Entry{Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: alice.UID}
	key := feedback.FaceRejectionKey{PhotoUID: rejected, FaceIndex: 0, SubjectUID: alice.UID}
	if err := h.feedback.RejectFace(ctx, key, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}

	got := persons(h.run(t))
	if len(got) != 1 {
		t.Fatalf("persons = %+v, want one for Alice", got)
	}
	for _, c := range got[0].Candidates {
		if c.Photo.UID == rejected {
			t.Fatalf("rejected face %s appeared in the sweep: %+v", rejected, got[0].Candidates)
		}
	}
	if len(got[0].Candidates) != 1 || got[0].Candidates[0].Photo.UID != keep {
		t.Errorf("Alice candidates = %+v, want only the kept face %s", got[0].Candidates, keep)
	}
}

// TestSweep_noFacesSubjectSkippedDB checks a subject with no faces produces no card
// and no error, and is not counted among scanned subjects.
func TestSweep_noFacesSubjectSkippedDB(t *testing.T) {
	h := newSweepHarness(t)
	if _, err := h.people.CreateSubject(t.Context(), people.Subject{Name: "Nobody"}); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	events := h.run(t)
	if got := persons(events); len(got) != 0 {
		t.Fatalf("persons = %+v, want none", got)
	}
	sum := summary(t, events)
	if sum.PeopleScanned != 0 || sum.SubjectsTotal != 0 || sum.PeopleWithMatches != 0 {
		t.Errorf("summary = %+v, want an empty sweep", sum)
	}
}
