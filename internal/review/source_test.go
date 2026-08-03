package review

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/sweep"
)

// mixedLibrary scripts one in-band face candidate and one in-band label
// candidate, so a queue built from it has exactly one question per source and
// any restriction is visible in the result.
func mixedLibrary(f *fixture) {
	f.sweeper.people = []*sweep.Person{scannedPerson("subj1", 0.4)}
	f.organize.labels = []organize.LabelCount{labelCount("lab1", 3)}
	f.expander.results["lab1"] = labelResult("lab1", 0.5)
}

func TestParseSource(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]Source{
		"":       SourceBoth,
		"both":   SourceBoth,
		"people": SourcePeople,
		"labels": SourceLabels,
	} {
		got, err := ParseSource(raw)
		if err != nil {
			t.Errorf("ParseSource(%q): %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSource(%q) = %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{"faces", "BOTH", "people,labels", "none"} {
		if _, err := ParseSource(raw); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("ParseSource(%q) error = %v, want ErrInvalidSource", raw, err)
		}
	}
}

func TestQueue_peopleSourceNeverRunsTheLabelSearch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, mixedLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourcePeople, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Source != SourcePeople {
		t.Errorf("echoed source = %q, want %q", res.Source, SourcePeople)
	}
	if len(res.Questions) != 1 || res.Questions[0].Kind != KindFace {
		t.Fatalf("questions = %+v, want a single face question", res.Questions)
	}
	// The point of the selection: the label search is not run and then filtered,
	// it is not run at all — the scans are what a rebuild costs.
	if f.expander.calls != 0 {
		t.Errorf("label similarity ran %d times for a people-only queue, want 0", f.expander.calls)
	}
}

func TestQueue_labelsSourceNeverRunsTheSubjectSweep(t *testing.T) {
	t.Parallel()
	f := newFixture(t, mixedLibrary)
	res, err := f.svc.Queue(context.Background(), "user", SourceLabels, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Source != SourceLabels {
		t.Errorf("echoed source = %q, want %q", res.Source, SourceLabels)
	}
	if len(res.Questions) != 1 || res.Questions[0].Kind != KindLabel {
		t.Fatalf("questions = %+v, want a single label question", res.Questions)
	}
	if f.sweeper.calls != 0 {
		t.Errorf("subject sweep ran %d times for a labels-only queue, want 0", f.sweeper.calls)
	}
}

func TestQueue_bothIsTheDefaultForAnUnknownSource(t *testing.T) {
	t.Parallel()
	f := newFixture(t, mixedLibrary)
	res, err := f.svc.Queue(context.Background(), "user", Source("nonsense"), 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if res.Source != SourceBoth || len(res.Questions) != 2 {
		t.Fatalf("source = %q with %d questions, want both with 2", res.Source, len(res.Questions))
	}
}

func TestQueue_switchingSourceRebuildsInsideTheCacheTTL(t *testing.T) {
	t.Parallel()
	f := newFixture(t, mixedLibrary)
	ctx := context.Background()
	if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	sweeps := f.sweeper.calls

	// The clock does not move, so the cache is warm: a repeat of the same source
	// must not re-run the searches. Without this the test below would prove
	// nothing — every call would rebuild anyway.
	if _, err := f.svc.Queue(ctx, "user", SourceBoth, 0); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if f.sweeper.calls != sweeps {
		t.Fatalf("warm cache re-ran the subject sweep (%d → %d)", sweeps, f.sweeper.calls)
	}

	// A changed source must not be served that warm batch: it holds exactly the
	// questions the player just asked not to be asked.
	res, err := f.svc.Queue(ctx, "user", SourceLabels, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) == 0 {
		t.Fatalf("labels-only queue is empty (reason %q), want the label question", res.Reason)
	}
	for _, q := range res.Questions {
		if q.Kind != KindLabel {
			t.Errorf("question %q is a %s question in a labels-only queue", q.ID, q.Kind)
		}
	}
}

func TestQueue_emptyChosenSourceSaysWhich(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		source Source
		setup  func(*fixture)
		want   string
	}{
		"people but nobody is named": {
			source: SourcePeople,
			setup: func(f *fixture) {
				f.organize.labels = []organize.LabelCount{labelCount("lab1", 3)}
				f.expander.results["lab1"] = labelResult("lab1", 0.5)
			},
			want: ReasonNoPeople,
		},
		"labels but none exist": {
			source: SourceLabels,
			setup: func(f *fixture) {
				f.sweeper.people = []*sweep.Person{scannedPerson("subj1", 0.4)}
			},
			want: ReasonNoLabels,
		},
		"people exist but neither tier has anything": {
			source: SourcePeople,
			setup: func(f *fixture) {
				// Confidence 0.30: below the band, so the guess is noise rather than
				// a fair question, and nowhere near the confident tier either.
				f.sweeper.people = []*sweep.Person{scannedPerson("subj1", 0.7)}
			},
			want: ReasonNoCandidates,
		},
		"neither source has anything": {
			source: SourceBoth,
			setup:  func(*fixture) {},
			want:   ReasonNoSources,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tc.setup)
			res, err := f.svc.Queue(context.Background(), "user", tc.source, 0)
			if err != nil {
				t.Fatalf("Queue: %v", err)
			}
			if len(res.Questions) != 0 {
				t.Fatalf("questions = %+v, want an empty queue", res.Questions)
			}
			if res.Reason != tc.want {
				t.Errorf("reason = %q, want %q", res.Reason, tc.want)
			}
		})
	}
}

func TestQueue_skipsCarryAcrossSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t, mixedLibrary)
	ctx := context.Background()
	res, err := f.svc.Queue(ctx, "user", SourceLabels, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(res.Questions) != 1 {
		t.Fatalf("questions = %+v, want the one label question", res.Questions)
	}
	skipped := res.Questions[0].ID
	if _, err := f.svc.Answer(ctx, "user", skipped, AnswerSkip, audit.Meta{}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	// "Don't know" is a statement about the question, not about the toggle that
	// surfaced it, so switching back to both must not offer it again.
	back, err := f.svc.Queue(ctx, "user", SourceBoth, 0)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	for _, q := range back.Questions {
		if q.ID == skipped {
			t.Fatalf("skipped question %q came back under another source", skipped)
		}
	}
}
