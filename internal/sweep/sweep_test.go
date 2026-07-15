package sweep

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeLister returns a fixed subject list (or an error).
type fakeLister struct {
	subjects []people.SubjectCount
	err      error
}

// ListSubjects returns the fake's canned subjects or error.
func (f *fakeLister) ListSubjects(context.Context) ([]people.SubjectCount, error) {
	return f.subjects, f.err
}

// fakeFinder answers Find from a per-subject table and records every subject it was
// asked about, tracking peak concurrency so a test can assert the worker-pool bound.
type fakeFinder struct {
	results map[string]candidates.Result
	errs    map[string]error
	delay   time.Duration

	mu      sync.Mutex
	queried []string

	active  atomic.Int32
	maxSeen atomic.Int32
}

// Find records the query, tracks concurrency, and returns the canned result/error.
func (f *fakeFinder) Find(
	ctx context.Context, subjectUID string, _ candidates.Request,
) (candidates.Result, error) {
	f.mu.Lock()
	f.queried = append(f.queried, subjectUID)
	f.mu.Unlock()

	now := f.active.Add(1)
	for {
		peak := f.maxSeen.Load()
		if now <= peak || f.maxSeen.CompareAndSwap(peak, now) {
			break
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
		}
	}
	f.active.Add(-1)

	if err, ok := f.errs[subjectUID]; ok {
		return candidates.Result{}, err
	}
	return f.results[subjectUID], nil
}

// subjectN builds a SubjectCount with the given uid, name and marker count.
func subjectN(uid, name string, markers int) people.SubjectCount {
	return people.SubjectCount{
		Subject:     people.Subject{UID: uid, Name: name},
		MarkerCount: markers,
	}
}

// candidate builds a candidate on the given photo with the given action.
func candidate(photoUID string, action candidates.Action) candidates.Candidate {
	return candidates.Candidate{
		Photo:    photos.Photo{UID: photoUID},
		Action:   action,
		Distance: 0.1,
	}
}

// quietService builds a Service with a discard logger so a skipped-subject warning
// does not spam the test output.
func quietService(t *testing.T, cfg Config) *Service {
	t.Helper()
	cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg)
}

// collect runs a sweep and returns every emitted event in order.
func collect(t *testing.T, svc *Service, params Params) []Event {
	t.Helper()
	var events []Event
	if err := svc.Sweep(context.Background(), params, func(ev Event) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return events
}

// summaryOf returns the single summary event's payload, failing if it is missing or
// not the last event.
func summaryOf(t *testing.T, events []Event) Summary {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	last := events[len(events)-1]
	if last.Type != EventSummary || last.Summary == nil {
		t.Fatalf("last event = %+v, want summary", last)
	}
	return *last.Summary
}

// personEvents returns just the person payloads from a stream.
func personEvents(events []Event) []Person {
	var persons []Person
	for _, ev := range events {
		if ev.Type == EventPerson && ev.Person != nil {
			persons = append(persons, *ev.Person)
		}
	}
	return persons
}

// TestSweep_onlyMatchedSubjectsEmitPerson checks that of three subjects only the one
// with actionable candidates yields a Person, while all three still report progress.
func TestSweep_onlyMatchedSubjectsEmitPerson(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{
		subjectN("s1", "Alice", 3),
		subjectN("s2", "Bob", 2),
		subjectN("s3", "Cara", 1),
	}}
	finder := &fakeFinder{results: map[string]candidates.Result{
		"s2": {Candidates: []candidates.Candidate{candidate("p1", candidates.ActionCreateMarker)}},
	}}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	events := collect(t, svc, Params{})

	persons := personEvents(events)
	if len(persons) != 1 || persons[0].Subject.UID != "s2" {
		t.Fatalf("persons = %+v, want one for s2", persons)
	}
	if persons[0].Actionable != 1 || persons[0].Counts.CreateMarker != 1 {
		t.Errorf("person actionable/counts = %d/%+v, want 1/create_marker", persons[0].Actionable, persons[0].Counts)
	}
	sum := summaryOf(t, events)
	if sum.PeopleScanned != 3 || sum.PeopleWithMatches != 1 || sum.TotalActionable != 1 {
		t.Errorf("summary = %+v, want scanned 3, withMatches 1, actionable 1", sum)
	}
	if sum.Capped || sum.SubjectsTotal != 3 {
		t.Errorf("summary cap = %+v, want not capped, total 3", sum)
	}
}

// TestSweep_skipsSubjectsWithoutFaces checks a subject with zero markers is never
// searched and does not error.
func TestSweep_skipsSubjectsWithoutFaces(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{
		subjectN("s1", "Alice", 2),
		subjectN("s2", "NoFaces", 0),
	}}
	finder := &fakeFinder{results: map[string]candidates.Result{}}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	sum := summaryOf(t, collect(t, svc, Params{}))

	if sum.PeopleScanned != 1 || sum.SubjectsTotal != 1 {
		t.Errorf("summary = %+v, want one subject scanned", sum)
	}
	for _, uid := range finder.queried {
		if uid == "s2" {
			t.Fatalf("faceless subject s2 was searched: %v", finder.queried)
		}
	}
}

// TestSweep_dropsAlreadyDone checks already-done candidates are filtered from the
// work list but still counted in the summary.
func TestSweep_dropsAlreadyDone(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{subjectN("s1", "Alice", 2)}}
	finder := &fakeFinder{results: map[string]candidates.Result{
		"s1": {
			Candidates: []candidates.Candidate{
				candidate("p1", candidates.ActionCreateMarker),
				candidate("p2", candidates.ActionAlreadyDone),
			},
			Counts: candidates.Counts{CreateMarker: 1, AlreadyDone: 1},
		},
	}}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	events := collect(t, svc, Params{})
	persons := personEvents(events)
	if len(persons) != 1 {
		t.Fatalf("persons = %d, want 1", len(persons))
	}
	if len(persons[0].Candidates) != 1 || persons[0].Candidates[0].Photo.UID != "p1" {
		t.Errorf("candidates = %+v, want only actionable p1", persons[0].Candidates)
	}
	sum := summaryOf(t, events)
	if sum.TotalActionable != 1 || sum.TotalAlreadyDone != 1 {
		t.Errorf("summary = %+v, want actionable 1, alreadyDone 1", sum)
	}
}

// TestSweep_subjectWithOnlyAlreadyDoneOmitted checks a subject whose only candidate
// is already-done contributes no Person card but still counts toward already-done.
func TestSweep_subjectWithOnlyAlreadyDoneOmitted(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{subjectN("s1", "Alice", 2)}}
	finder := &fakeFinder{results: map[string]candidates.Result{
		"s1": {
			Candidates: []candidates.Candidate{candidate("p1", candidates.ActionAlreadyDone)},
			Counts:     candidates.Counts{AlreadyDone: 1},
		},
	}}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	events := collect(t, svc, Params{})
	if got := personEvents(events); len(got) != 0 {
		t.Fatalf("persons = %+v, want none", got)
	}
	sum := summaryOf(t, events)
	if sum.PeopleWithMatches != 0 || sum.TotalActionable != 0 || sum.TotalAlreadyDone != 1 {
		t.Errorf("summary = %+v, want no matches, alreadyDone 1", sum)
	}
}

// TestSweep_capsSubjects checks that with more faced subjects than MaxSubjects only
// the cap is scanned and the summary flags it, without silently dropping the total.
func TestSweep_capsSubjects(t *testing.T) {
	t.Parallel()
	subjects := make([]people.SubjectCount, 5)
	for i := range subjects {
		subjects[i] = subjectN("s"+string(rune('1'+i)), "P", 1)
	}
	lister := &fakeLister{subjects: subjects}
	finder := &fakeFinder{results: map[string]candidates.Result{}}
	svc := quietService(t, Config{Subjects: lister, Finder: finder, MaxSubjects: 2})

	events := collect(t, svc, Params{})
	sum := summaryOf(t, events)
	if sum.PeopleScanned != 2 || sum.SubjectsTotal != 5 || !sum.Capped {
		t.Errorf("summary = %+v, want scanned 2, total 5, capped", sum)
	}
	for _, ev := range events {
		if ev.Type == EventProgress && ev.Progress.Total != 2 {
			t.Errorf("progress total = %d, want 2", ev.Progress.Total)
		}
	}
}

// TestSweep_concurrencyBounded checks the worker pool never runs more Find calls at
// once than the configured concurrency.
func TestSweep_concurrencyBounded(t *testing.T) {
	t.Parallel()
	subjects := make([]people.SubjectCount, 12)
	for i := range subjects {
		subjects[i] = subjectN("s"+string(rune('a'+i)), "P", 1)
	}
	lister := &fakeLister{subjects: subjects}
	finder := &fakeFinder{results: map[string]candidates.Result{}, delay: 5 * time.Millisecond}
	svc := quietService(t, Config{Subjects: lister, Finder: finder, Concurrency: 3})

	summaryOf(t, collect(t, svc, Params{}))

	if peak := finder.maxSeen.Load(); peak > 3 {
		t.Errorf("peak concurrency = %d, want <= 3", peak)
	}
	if len(finder.queried) != 12 {
		t.Errorf("queried %d subjects, want 12", len(finder.queried))
	}
}

// TestSweep_perSubjectErrorSkipped checks a search error on one subject is logged and
// skipped (progress still reported, no Person) without failing the whole sweep.
func TestSweep_perSubjectErrorSkipped(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{
		subjectN("s1", "Alice", 1),
		subjectN("s2", "Bob", 1),
	}}
	finder := &fakeFinder{
		results: map[string]candidates.Result{
			"s2": {Candidates: []candidates.Candidate{candidate("p1", candidates.ActionCreateMarker)}},
		},
		errs: map[string]error{"s1": errors.New("boom")},
	}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	events := collect(t, svc, Params{})
	if persons := personEvents(events); len(persons) != 1 || persons[0].Subject.UID != "s2" {
		t.Fatalf("persons = %+v, want only s2", persons)
	}
	if sum := summaryOf(t, events); sum.PeopleScanned != 2 || sum.PeopleWithMatches != 1 {
		t.Errorf("summary = %+v, want scanned 2, matches 1", sum)
	}
}

// TestSweep_listErrorFatal checks a subject-listing failure aborts before any event.
func TestSweep_listErrorFatal(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{err: errors.New("db down")}
	finder := &fakeFinder{}
	svc := quietService(t, Config{Subjects: lister, Finder: finder})

	var emitted int
	err := svc.Sweep(context.Background(), Params{}, func(Event) error {
		emitted++
		return nil
	})
	if err == nil {
		t.Fatal("Sweep err = nil, want listing error")
	}
	if emitted != 0 {
		t.Errorf("emitted %d events, want 0 before a fatal list error", emitted)
	}
}

// TestSweep_emitFailureStops checks that an emit error stops the sweep, is returned,
// and does not deadlock the worker pool.
func TestSweep_emitFailureStops(t *testing.T) {
	t.Parallel()
	subjects := make([]people.SubjectCount, 20)
	for i := range subjects {
		subjects[i] = subjectN("s"+string(rune('a'+i)), "P", 1)
	}
	lister := &fakeLister{subjects: subjects}
	finder := &fakeFinder{results: map[string]candidates.Result{}, delay: time.Millisecond}
	svc := quietService(t, Config{Subjects: lister, Finder: finder, Concurrency: 2})

	want := errors.New("client gone")
	done := make(chan error, 1)
	go func() {
		done <- svc.Sweep(context.Background(), Params{}, func(Event) error { return want })
	}()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("Sweep err = %v, want %v", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sweep did not return after an emit failure (deadlock?)")
	}
}

// TestNew_panicsOnNilDeps checks New treats a missing store as a wiring bug.
func TestNew_panicsOnNilDeps(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("New did not panic on a nil Finder")
		}
	}()
	New(Config{Subjects: &fakeLister{}, Finder: nil})
}
