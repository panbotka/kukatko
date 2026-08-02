package sweep

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
)

// scanFixture builds a quiet service over n subjects, each of which yields one
// actionable candidate, plus the finder so a test can inspect what was queried.
func scanFixture(t *testing.T, n, concurrency int) (*Service, *fakeFinder) {
	t.Helper()
	lister := &fakeLister{}
	finder := &fakeFinder{results: map[string]candidates.Result{}, errs: map[string]error{}}
	for i := range n {
		uid := fmt.Sprintf("subj%03d", i)
		lister.subjects = append(lister.subjects, subjectN(uid, "Person "+uid, 1))
		finder.results[uid] = candidates.Result{
			SubjectUID: uid,
			Candidates: []candidates.Candidate{candidate("photo-"+uid, candidates.ActionCreateMarker)},
		}
	}
	svc := quietService(t, Config{Subjects: lister, Finder: finder, Concurrency: concurrency})
	return svc, finder
}

// scanned returns the subject uids the collector saw, in collection order.
func scanned(persons []*Person) []string {
	out := make([]string, 0, len(persons))
	for _, p := range persons {
		out = append(out, p.Subject.UID)
	}
	return out
}

func TestScan_budgetBoundsTheWork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		subjects    int
		budget      int
		wantQueried int
	}{
		{name: "budget smaller than the library", subjects: 50, budget: 4, wantQueried: 4},
		{name: "budget larger than the library", subjects: 3, budget: 10, wantQueried: 3},
		{name: "no budget scans everything", subjects: 7, budget: 0, wantQueried: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, finder := scanFixture(t, tt.subjects, 1)
			var seen []*Person
			cov, err := svc.Scan(context.Background(), Params{}, Window{Budget: tt.budget},
				func(person *Person) (bool, error) {
					seen = append(seen, person)
					return false, nil
				})
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if len(finder.queried) != tt.wantQueried {
				t.Errorf("subjects queried = %d, want %d", len(finder.queried), tt.wantQueried)
			}
			if cov.Scanned != tt.wantQueried {
				t.Errorf("Coverage.Scanned = %d, want %d", cov.Scanned, tt.wantQueried)
			}
			if cov.SubjectsTotal != tt.subjects {
				t.Errorf("Coverage.SubjectsTotal = %d, want %d (the whole library)",
					cov.SubjectsTotal, tt.subjects)
			}
		})
	}
}

func TestScan_rotatesThroughTheLibrary(t *testing.T) {
	t.Parallel()
	svc, _ := scanFixture(t, 5, 1)
	offset := 0
	order := make([]string, 0, 6)
	for range 3 {
		var seen []*Person
		cov, err := svc.Scan(context.Background(), Params{}, Window{Offset: offset, Budget: 2},
			func(person *Person) (bool, error) {
				seen = append(seen, person)
				return false, nil
			})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		order = append(order, scanned(seen)...)
		offset = cov.NextOffset
	}
	want := []string{"subj000", "subj001", "subj002", "subj003", "subj004", "subj000"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("rotation = %v, want %v (wraps, no gaps)", order, want)
	}
	if offset != 1 {
		t.Errorf("cursor after wrapping = %d, want 1", offset)
	}
}

func TestScan_stopsEarlyWhenTheCollectorHasEnough(t *testing.T) {
	t.Parallel()
	svc, finder := scanFixture(t, 40, 1)
	var seen []*Person
	cov, err := svc.Scan(context.Background(), Params{}, Window{Budget: 20},
		func(person *Person) (bool, error) {
			seen = append(seen, person)
			return len(seen) >= 2, nil
		})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// The stop cannot un-dispatch what the pool already started, so the count may
	// overshoot by up to the concurrency bound — but nowhere near the budget.
	if cov.Scanned < 2 || cov.Scanned > 3 {
		t.Fatalf("Coverage.Scanned = %d, want 2..3 (stop plus at most one in flight), budget was 20",
			cov.Scanned)
	}
	if len(finder.queried) != cov.Scanned {
		t.Errorf("queried %d subjects, Coverage.Scanned = %d — they must agree",
			len(finder.queried), cov.Scanned)
	}
	if len(seen) != cov.Scanned {
		t.Errorf("collected %d subjects of %d scanned — an overshooting scan must not throw work away",
			len(seen), cov.Scanned)
	}
	if cov.NextOffset != cov.Scanned {
		t.Errorf("NextOffset = %d, want %d (resume right after what was scanned, no gap)",
			cov.NextOffset, cov.Scanned)
	}
}

func TestScan_offsetWrapsAndSubjectsWithoutFacesAreSkipped(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{subjects: []people.SubjectCount{
		subjectN("a", "A", 1), subjectN("none", "None", 0), subjectN("b", "B", 1),
	}}
	finder := &fakeFinder{
		results: map[string]candidates.Result{
			"a": {Candidates: []candidates.Candidate{candidate("pa", candidates.ActionCreateMarker)}},
			"b": {Candidates: []candidates.Candidate{candidate("pb", candidates.ActionCreateMarker)}},
		},
		errs: map[string]error{},
	}
	svc := quietService(t, Config{Subjects: lister, Finder: finder, Concurrency: 1})
	var seen []*Person
	// Offset 7 over the two subjects with faces wraps to index 1 → "b".
	cov, err := svc.Scan(context.Background(), Params{}, Window{Offset: 7, Budget: 1},
		func(person *Person) (bool, error) {
			seen = append(seen, person)
			return false, nil
		})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := scanned(seen); len(got) != 1 || got[0] != "b" {
		t.Fatalf("scanned = %v, want [b] (offset 7 wraps into a 2-subject plan)", got)
	}
	if cov.SubjectsTotal != 2 || cov.NextOffset != 0 {
		t.Errorf("coverage = %+v, want SubjectsTotal 2 and NextOffset 0", cov)
	}
}

func TestScan_subjectFailureIsSkippedNotFatal(t *testing.T) {
	t.Parallel()
	svc, finder := scanFixture(t, 3, 1)
	finder.errs["subj001"] = errors.New("boom")
	var seen []*Person
	if _, err := svc.Scan(context.Background(), Params{}, Window{},
		func(person *Person) (bool, error) {
			seen = append(seen, person)
			return false, nil
		}); err != nil {
		t.Fatalf("Scan with one failing subject: %v", err)
	}
	if got := scanned(seen); fmt.Sprint(got) != fmt.Sprint([]string{"subj000", "subj002"}) {
		t.Fatalf("collected = %v, want the two healthy subjects", got)
	}
}

func TestScan_collectorErrorAborts(t *testing.T) {
	t.Parallel()
	svc, _ := scanFixture(t, 5, 1)
	wantErr := errors.New("collector said no")
	if _, err := svc.Scan(context.Background(), Params{}, Window{},
		func(*Person) (bool, error) { return false, wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("Scan error = %v, want %v", err, wantErr)
	}
}

func TestScan_emptyLibraryReportsTotals(t *testing.T) {
	t.Parallel()
	svc := quietService(t, Config{Subjects: &fakeLister{}, Finder: &fakeFinder{}, Concurrency: 1})
	cov, err := svc.Scan(context.Background(), Params{}, Window{Budget: 4},
		func(*Person) (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if cov != (Coverage{}) {
		t.Fatalf("coverage = %+v, want the zero value for an empty library", cov)
	}
}

func TestScan_listSubjectsFailureIsFatal(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("db down")
	svc := quietService(t, Config{
		Subjects: &fakeLister{err: wantErr}, Finder: &fakeFinder{}, Concurrency: 1,
	})
	if _, err := svc.Scan(context.Background(), Params{}, Window{},
		func(*Person) (bool, error) { return false, nil }); !errors.Is(err, wantErr) {
		t.Fatalf("Scan error = %v, want %v", err, wantErr)
	}
}

func TestWrapOffset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		offset, total, want int
	}{
		{offset: 0, total: 5, want: 0},
		{offset: 4, total: 5, want: 4},
		{offset: 5, total: 5, want: 0},
		{offset: 12, total: 5, want: 2},
		{offset: -1, total: 5, want: 4},
		{offset: 3, total: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_of_%d", tt.offset, tt.total), func(t *testing.T) {
			t.Parallel()
			if got := wrapOffset(tt.offset, tt.total); got != tt.want {
				t.Errorf("wrapOffset(%d, %d) = %d, want %d", tt.offset, tt.total, got, tt.want)
			}
		})
	}
}
