package review

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeSweeper replays a scripted event stream and counts invocations.
type fakeSweeper struct {
	mu     sync.Mutex
	events []sweep.Event
	calls  int
	err    error
}

// Sweep replays the scripted events to emit.
func (f *fakeSweeper) Sweep(_ context.Context, _ sweep.Params, emit func(sweep.Event) error) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	for _, ev := range f.events {
		if err := emit(ev); err != nil {
			return err
		}
	}
	return f.err
}

// fakeExpander returns scripted per-label results.
type fakeExpander struct {
	mu      sync.Mutex
	results map[string]expand.Result
	errs    map[string]error
	calls   int
}

// Label returns the scripted result for labelUID.
func (f *fakeExpander) Label(_ context.Context, labelUID string, _ expand.Request) (expand.Result, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if err := f.errs[labelUID]; err != nil {
		return expand.Result{}, err
	}
	return f.results[labelUID], nil
}

// fakeOrganize lists scripted labels and records attaches.
type fakeOrganize struct {
	labels    []organize.LabelCount
	attached  []string
	attachErr error
}

// ListLabels returns the scripted labels.
func (f *fakeOrganize) ListLabels(context.Context) ([]organize.LabelCount, error) {
	return f.labels, nil
}

// AttachLabelAudited records the attach as "photo/label".
func (f *fakeOrganize) AttachLabelAudited(
	_ context.Context, photoUID, labelUID string, _ organize.LabelSource, _ int, _ audit.Entry,
) error {
	if f.attachErr != nil {
		return f.attachErr
	}
	f.attached = append(f.attached, photoUID+"/"+labelUID)
	return nil
}

// fakeFaces serves faces from a keyed map.
type fakeFaces struct {
	faces map[vectors.FaceKey]vectors.Face
}

// FacesByKeys returns the known faces among keys.
func (f *fakeFaces) FacesByKeys(_ context.Context, keys []vectors.FaceKey) ([]vectors.Face, error) {
	var out []vectors.Face
	for _, key := range keys {
		if face, ok := f.faces[key]; ok {
			out = append(out, face)
		}
	}
	return out, nil
}

// fakeFeedback records rejections.
type fakeFeedback struct {
	faceRejects  []feedback.FaceRejectionKey
	labelRejects []feedback.LabelRejectionKey
	err          error
}

// RejectFace records the face rejection.
func (f *fakeFeedback) RejectFace(_ context.Context, key feedback.FaceRejectionKey, _ audit.Entry) error {
	if f.err != nil {
		return f.err
	}
	f.faceRejects = append(f.faceRejects, key)
	return nil
}

// RejectLabel records the label rejection.
func (f *fakeFeedback) RejectLabel(_ context.Context, key feedback.LabelRejectionKey, _ audit.Entry) error {
	if f.err != nil {
		return f.err
	}
	f.labelRejects = append(f.labelRejects, key)
	return nil
}

// fakeAssigner records assign requests.
type fakeAssigner struct {
	reqs []facematch.AssignRequest
	err  error
}

// Apply records the request.
func (f *fakeAssigner) Apply(
	_ context.Context, req facematch.AssignRequest, _ audit.Meta,
) (facematch.AssignResult, error) {
	if f.err != nil {
		return facematch.AssignResult{}, f.err
	}
	f.reqs = append(f.reqs, req)
	return facematch.AssignResult{Action: req.Action}, nil
}

// fixture bundles the fakes behind a Service for tests.
type fixture struct {
	sweeper  *fakeSweeper
	expander *fakeExpander
	organize *fakeOrganize
	faces    *fakeFaces
	feedback *fakeFeedback
	assigner *fakeAssigner
	now      *time.Time
	svc      *Service
}

// newFixture builds a Service over fresh fakes with a controllable clock.
func newFixture(t *testing.T, mutate func(*fixture)) *fixture {
	t.Helper()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	f := &fixture{
		sweeper:  &fakeSweeper{},
		expander: &fakeExpander{results: map[string]expand.Result{}, errs: map[string]error{}},
		organize: &fakeOrganize{},
		faces:    &fakeFaces{faces: map[vectors.FaceKey]vectors.Face{}},
		feedback: &fakeFeedback{},
		assigner: &fakeAssigner{},
		now:      &now,
	}
	if mutate != nil {
		mutate(f)
	}
	f.svc = New(Config{
		Sweeper:  f.sweeper,
		Expander: f.expander,
		Organize: f.organize,
		Faces:    f.faces,
		Feedback: f.feedback,
		Assigner: f.assigner,
		Now:      func() time.Time { return *f.now },
	})
	return f
}

// personEvent builds a sweep person event with face candidates at the given
// cosine distances for the subject.
func personEvent(subjectUID string, distances ...float64) sweep.Event {
	person := sweep.Person{Subject: people.Subject{UID: subjectUID, Name: "Person " + subjectUID}}
	for i, dist := range distances {
		person.Candidates = append(person.Candidates, candidates.Candidate{
			Photo:     photos.Photo{UID: fmt.Sprintf("photo-%s-%d", subjectUID, i)},
			FaceIndex: 0,
			Distance:  dist,
			Action:    candidates.ActionCreateMarker,
		})
	}
	return sweep.Event{Type: sweep.EventPerson, Person: &person}
}

// summaryEvent builds a sweep summary event reporting total named subjects.
func summaryEvent(subjectsTotal int) sweep.Event {
	return sweep.Event{Type: sweep.EventSummary, Summary: &sweep.Summary{SubjectsTotal: subjectsTotal}}
}

// labelResult builds an expand result with candidates at the given similarities.
func labelResult(labelUID string, similarities ...float64) expand.Result {
	res := expand.Result{Kind: "label", CollectionUID: labelUID}
	for i, sim := range similarities {
		res.Candidates = append(res.Candidates, expand.Candidate{
			Photo:      photos.Photo{UID: fmt.Sprintf("photo-%s-%d", labelUID, i)},
			Distance:   1 - sim,
			Similarity: sim,
		})
	}
	return res
}

// labelCount builds a LabelCount fixture.
func labelCount(uid string, photoCount int) organize.LabelCount {
	return organize.LabelCount{
		Label:      organize.Label{UID: uid, Name: "Label " + uid},
		PhotoCount: photoCount,
	}
}

func TestQueue_bandFilter(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		// Confidences 0.9 (too certain), 0.6 (in band), 0.44 (below band).
		f.sweeper.events = []sweep.Event{personEvent("subj1", 0.1, 0.4, 0.56), summaryEvent(1)}
		f.organize.labels = []organize.LabelCount{labelCount("lab1", 3)}
		// Similarities 0.8 (too certain), 0.5 (in band).
		f.expander.results["lab1"] = labelResult("lab1", 0.8, 0.5)
	})
	res, err := f.svc.Queue(context.Background(), "user", 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 2 {
		t.Fatalf("questions = %d, want 2 (one per kind in band): %+v", len(res.Questions), res.Questions)
	}
	byKind := map[Kind]Question{}
	for _, q := range res.Questions {
		byKind[q.Kind] = q
	}
	face, ok := byKind[KindFace]
	if !ok || face.Confidence != 0.6 {
		t.Errorf("face question = %+v, want confidence 0.6", face)
	}
	if face.Subject == nil || face.Subject.UID != "subj1" || face.BBox == nil || face.FaceIndex == nil {
		t.Errorf("face question missing subject/bbox/face_index: %+v", face)
	}
	label, ok := byKind[KindLabel]
	if !ok || label.Confidence != 0.5 {
		t.Errorf("label question = %+v, want confidence 0.5", label)
	}
	if label.Label == nil || label.Label.UID != "lab1" {
		t.Errorf("label question missing label: %+v", label)
	}
	if res.Remaining != 2 || res.Answered != 0 {
		t.Errorf("counters = answered %d remaining %d, want 0/2", res.Answered, res.Remaining)
	}
}

func TestQueue_alreadyDoneExcluded(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		ev := personEvent("subj1", 0.4)
		ev.Person.Candidates[0].Action = candidates.ActionAlreadyDone
		f.sweeper.events = []sweep.Event{ev, summaryEvent(1)}
	})
	res, err := f.svc.Queue(context.Background(), "user", 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 0 {
		t.Fatalf("questions = %+v, want none (already_done excluded)", res.Questions)
	}
}

func TestQueue_ordersByBoundaryDistanceAndInterleaves(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		// Band mid is 0.60: face confidences 0.50, 0.61, 0.70 → order 0.61, 0.70, 0.50.
		f.sweeper.events = []sweep.Event{personEvent("subj1", 0.50, 0.39, 0.30), summaryEvent(1)}
		f.organize.labels = []organize.LabelCount{labelCount("lab1", 1)}
		f.expander.results["lab1"] = labelResult("lab1", 0.46)
	})
	res, err := f.svc.Queue(context.Background(), "user", 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	got := make([]string, 0, len(res.Questions))
	for _, q := range res.Questions {
		got = append(got, fmt.Sprintf("%s@%.2f", q.Kind, q.Confidence))
	}
	// 3 faces vs 1 label: the label lands mid-sequence (position 1/2 ∈ (1/6, 3/6]),
	// faces keep informativeness order (0.61 is nearest the 0.60 midpoint, then
	// 0.50 edges out 0.70 by float rounding of the midpoint distance).
	want := []string{"face@0.61", "face@0.50", "label@0.46", "face@0.70"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestQueue_deterministicAcrossRebuilds(t *testing.T) {
	t.Parallel()
	build := func() []string {
		f := newFixture(t, func(f *fixture) {
			f.sweeper.events = []sweep.Event{
				personEvent("subj2", 0.42, 0.35),
				personEvent("subj1", 0.40, 0.28),
				summaryEvent(2),
			}
			f.organize.labels = []organize.LabelCount{labelCount("lab1", 1), labelCount("lab2", 1)}
			f.expander.results["lab1"] = labelResult("lab1", 0.55, 0.65)
			f.expander.results["lab2"] = labelResult("lab2", 0.6)
		})
		res, err := f.svc.Queue(context.Background(), "user", 0)
		if err != nil {
			t.Fatalf("Queue: %v", err)
		}
		ids := make([]string, 0, len(res.Questions))
		for _, q := range res.Questions {
			ids = append(ids, q.ID)
		}
		return ids
	}
	first, second := build(), build()
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("queue not deterministic:\n first = %v\nsecond = %v", first, second)
	}
	if len(first) != 7 {
		t.Fatalf("questions = %d, want 7", len(first))
	}
}

func TestQueue_cacheTTL(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.events = []sweep.Event{personEvent("subj1", 0.4), summaryEvent(1)}
	})
	ctx := context.Background()
	for range 3 {
		if _, err := f.svc.Queue(ctx, "user", 0); err != nil {
			t.Fatalf("Queue: %v", err)
		}
	}
	if f.sweeper.calls != 1 {
		t.Fatalf("sweep calls within TTL = %d, want 1 (cached)", f.sweeper.calls)
	}
	*f.now = f.now.Add(2 * DefaultCacheTTL)
	if _, err := f.svc.Queue(ctx, "user", 0); err != nil {
		t.Fatalf("Queue after TTL: %v", err)
	}
	if f.sweeper.calls != 2 {
		t.Fatalf("sweep calls after TTL = %d, want 2 (rebuilt)", f.sweeper.calls)
	}
}

func TestQueue_limit(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.events = []sweep.Event{
			personEvent("subj1", 0.4, 0.41, 0.42, 0.43, 0.44),
			summaryEvent(1),
		}
	})
	res, err := f.svc.Queue(context.Background(), "user", 2)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 2 || res.Remaining != 5 {
		t.Fatalf("batch = %d remaining = %d, want 2/5", len(res.Questions), res.Remaining)
	}
}

func TestQueue_emptyReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name:   "no people and no labels",
			mutate: func(f *fixture) { f.sweeper.events = []sweep.Event{summaryEvent(0)} },
			want:   ReasonNoSources,
		},
		{
			name: "sources exist but nothing in band",
			mutate: func(f *fixture) {
				f.sweeper.events = []sweep.Event{personEvent("subj1", 0.05), summaryEvent(1)}
				f.organize.labels = []organize.LabelCount{labelCount("lab1", 2)}
				f.expander.results["lab1"] = labelResult("lab1", 0.95)
			},
			want: ReasonNoCandidates,
		},
		{
			name: "labels exist with zero photos",
			mutate: func(f *fixture) {
				f.sweeper.events = []sweep.Event{summaryEvent(0)}
				f.organize.labels = []organize.LabelCount{labelCount("lab1", 0)}
			},
			want: ReasonNoSources,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tt.mutate)
			res, err := f.svc.Queue(context.Background(), "user", 0)
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			if len(res.Questions) != 0 || res.Reason != tt.want {
				t.Fatalf("got %d questions, reason %q; want 0 questions, reason %q",
					len(res.Questions), res.Reason, tt.want)
			}
		})
	}
}

func TestQueue_labelSearchFailureSkipsLabel(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) {
		f.sweeper.events = []sweep.Event{summaryEvent(0)}
		f.organize.labels = []organize.LabelCount{labelCount("bad", 1), labelCount("good", 1)}
		f.expander.errs["bad"] = errors.New("boom")
		f.expander.results["good"] = labelResult("good", 0.6)
	})
	res, err := f.svc.Queue(context.Background(), "user", 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 || res.Questions[0].Label.UID != "good" {
		t.Fatalf("questions = %+v, want just the good label's", res.Questions)
	}
}

func TestQueue_sweepFailureFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(f *fixture) { f.sweeper.err = errors.New("boom") })
	if _, err := f.svc.Queue(context.Background(), "user", 0); err == nil {
		t.Fatal("Queue with failing sweep: want error, got nil")
	}
}

func TestInterleave_proportions(t *testing.T) {
	t.Parallel()
	mk := func(kind Kind, n int) []Question {
		qs := make([]Question, n)
		for i := range qs {
			qs[i] = Question{ID: fmt.Sprintf("%s-%d", kind, i), Kind: kind}
		}
		return qs
	}
	tests := []struct {
		name         string
		faces, label int
		want         []Kind
	}{
		{"equal counts alternate", 2, 2, []Kind{KindFace, KindLabel, KindFace, KindLabel}},
		{"skewed toward faces", 4, 2,
			[]Kind{KindFace, KindLabel, KindFace, KindFace, KindLabel, KindFace}},
		{"only labels", 0, 2, []Kind{KindLabel, KindLabel}},
		{"only faces", 2, 0, []Kind{KindFace, KindFace}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			merged := interleave(mk(KindFace, tt.faces), mk(KindLabel, tt.label))
			var got []Kind
			for _, q := range merged {
				got = append(got, q.Kind)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("interleave = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseQuestionID_roundTrip(t *testing.T) {
	t.Parallel()
	faceID := faceQuestionID("photo1", 2, "subj1")
	ref, err := parseQuestionID(faceID)
	if err != nil {
		t.Fatalf("parse(%q): %v", faceID, err)
	}
	want := questionRef{Kind: KindFace, PhotoUID: "photo1", FaceIndex: 2, SubjectUID: "subj1"}
	if ref != want {
		t.Errorf("parse(%q) = %+v, want %+v", faceID, ref, want)
	}
	labelID := labelQuestionID("photo1", "lab1")
	ref, err = parseQuestionID(labelID)
	if err != nil {
		t.Fatalf("parse(%q): %v", labelID, err)
	}
	want = questionRef{Kind: KindLabel, PhotoUID: "photo1", LabelUID: "lab1"}
	if ref != want {
		t.Errorf("parse(%q) = %+v, want %+v", labelID, ref, want)
	}
}

func TestParseQuestionID_invalid(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		"", "bogus", "face:photo1", "face:photo1:x:subj", "face::0:subj",
		"face:photo1:-1:subj", "face:photo1:0:", "label:photo1", "label::lab", "label:photo1:",
	} {
		if _, err := parseQuestionID(id); !errors.Is(err, ErrInvalidQuestion) {
			t.Errorf("parse(%q) error = %v, want ErrInvalidQuestion", id, err)
		}
	}
}
