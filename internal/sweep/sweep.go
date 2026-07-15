// Package sweep answers "who else is in the library, unnamed?" for everyone at
// once. It composes the per-subject candidate search (internal/candidates) across
// every named subject that has faces, at a high confidence (a tight distance), and
// streams the result grouped by person — a work list that shrinks as it is cleared.
//
// The sweep is read-only and never auto-accepts: confidence only narrows the list,
// every confirmation still goes through the existing face-assignment path
// (POST /photos/{uid}/faces/assign). A future iteration must not "helpfully" add
// auto-assign here — the whole point is that a human decides each match.
//
// Concurrency is bounded server-side by a small worker pool (this box is
// RAM-constrained), and the number of subjects scanned is capped, with the cap made
// visible in the summary rather than silently truncating.
package sweep

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
)

// Default tunables, applied when a Config field is left non-positive.
const (
	// DefaultConcurrency is the fallback bound on how many subjects are scanned at
	// once. It stacks on candidates.concurrency, so it is deliberately small.
	DefaultConcurrency = 4
	// DefaultMaxSubjects is the fallback cap on how many subjects one sweep scans.
	DefaultMaxSubjects = 500
)

// Finder runs the per-subject candidate search. It is an interface so the sweep is
// testable with a fake and stays decoupled from the candidates package's wiring;
// *candidates.Service satisfies it.
type Finder interface {
	// Find returns the untagged-face candidates for the subject, or
	// people.ErrSubjectNotFound when no such subject exists.
	Find(ctx context.Context, subjectUID string, req candidates.Request) (candidates.Result, error)
}

// SubjectLister lists the named subjects the sweep iterates. *people.Store satisfies
// it.
type SubjectLister interface {
	// ListSubjects returns every subject with its non-invalid marker count, ordered by
	// name.
	ListSubjects(ctx context.Context) ([]people.SubjectCount, error)
}

// Config bundles the Service's collaborators and tunables. Subjects and Finder are
// required; the numeric tunables fall back to their Default* when non-positive, and
// a nil Log is replaced with slog.Default().
type Config struct {
	// Subjects lists the subjects to scan.
	Subjects SubjectLister
	// Finder runs the per-subject candidate search.
	Finder Finder
	// Concurrency bounds how many subjects are scanned at once.
	Concurrency int
	// MaxSubjects caps how many subjects one sweep scans.
	MaxSubjects int
	// Log records a per-subject scan failure without aborting the whole sweep.
	Log *slog.Logger
}

// Service runs a recognition sweep across every named subject that has faces.
type Service struct {
	subjects    SubjectLister
	finder      Finder
	concurrency int
	maxSubjects int
	log         *slog.Logger
}

// New returns a Service from cfg, applying the Default* tunables where cfg leaves a
// value non-positive. It panics if Subjects or Finder is nil, treating that as a
// startup wiring bug rather than a per-request error.
func New(cfg Config) *Service {
	if cfg.Subjects == nil || cfg.Finder == nil {
		panic("sweep: New requires non-nil Subjects and Finder")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		subjects:    cfg.Subjects,
		finder:      cfg.Finder,
		concurrency: orDefault(cfg.Concurrency, DefaultConcurrency),
		maxSubjects: orDefault(cfg.MaxSubjects, DefaultMaxSubjects),
		log:         log,
	}
}

// Params are the per-sweep search parameters, passed straight through to the
// per-subject search.
type Params struct {
	// Threshold is the maximum cosine distance a candidate may sit from an exemplar; a
	// non-positive value uses the candidate search's configured default.
	Threshold float64
	// Limit caps how many candidates each person contributes; 0 means all.
	Limit int
}

// plan is the set of subjects a single sweep will scan, plus how many had faces
// before the cap.
type plan struct {
	subjects []people.SubjectCount
	total    int
	capped   bool
}

// Sweep scans every named subject that has faces and calls emit for each streamed
// event: a Progress per scanned subject, a Person for each subject with actionable
// candidates, and a final Summary. emit is called serially (never concurrently), so
// a caller can write it straight to an HTTP response without locking. An emit that
// returns an error stops the sweep and is returned; a per-subject search failure is
// logged and skipped rather than aborting the whole run. Listing the subjects is the
// only fatal step and returns its error before any event is emitted.
func (s *Service) Sweep(ctx context.Context, params Params, emit func(Event) error) error {
	pl, err := s.buildPlan(ctx)
	if err != nil {
		return err
	}
	return s.run(ctx, pl, params, emit)
}

// buildPlan lists the subjects, keeps only those with at least one marker (a face to
// search from), and caps the count, reporting whether the cap bit.
func (s *Service) buildPlan(ctx context.Context) (plan, error) {
	all, err := s.subjects.ListSubjects(ctx)
	if err != nil {
		return plan{}, fmt.Errorf("listing subjects: %w", err)
	}
	withFaces := make([]people.SubjectCount, 0, len(all))
	for _, subj := range all {
		if subj.MarkerCount > 0 {
			withFaces = append(withFaces, subj)
		}
	}
	total := len(withFaces)
	if s.maxSubjects > 0 && total > s.maxSubjects {
		return plan{subjects: withFaces[:s.maxSubjects], total: total, capped: true}, nil
	}
	return plan{subjects: withFaces, total: total, capped: false}, nil
}

// run scans the planned subjects with a bounded worker pool and funnels each result
// to a single consumer that emits the events and accumulates the summary. Cancelling
// ctx (or an emit failure) stops the workers cleanly without leaking a goroutine.
func (s *Service) run(ctx context.Context, pl plan, params Params, emit func(Event) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan personResult)
	go s.scanAll(ctx, pl.subjects, params, results)

	summary := Summary{SubjectsTotal: pl.total, Capped: pl.capped}
	var emitErr error
	for r := range results {
		if emitErr != nil {
			continue // keep draining so the workers unblock and the channel closes
		}
		if err := s.emitResult(r, len(pl.subjects), &summary, emit); err != nil {
			emitErr = err
			cancel()
		}
	}
	if emitErr != nil {
		return emitErr
	}
	return emit(Event{Type: EventSummary, Summary: &summary})
}

// scanAll runs one scanSubject per subject under a concurrency-bounded errgroup,
// sending each result on results and closing it when every subject is done. A send
// races the context so a cancelled sweep never blocks on a consumer that stopped
// reading.
func (s *Service) scanAll(
	ctx context.Context, subjects []people.SubjectCount, params Params, results chan<- personResult,
) {
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(s.concurrency)
	for _, subj := range subjects {
		grp.Go(func() error {
			r := s.scanSubject(gctx, subj, params)
			select {
			case results <- r:
				return nil
			case <-gctx.Done():
				return gctx.Err()
			}
		})
	}
	_ = grp.Wait()
	close(results)
}

// personResult is one subject's scan outcome, carried from a worker to the consumer.
type personResult struct {
	subject     people.SubjectCount
	candidates  []candidates.Candidate
	counts      candidates.Counts
	alreadyDone int
	err         error
}

// scanSubject runs the per-subject candidate search and reduces it to the actionable
// candidates (already-done ones filtered out). A search error is carried on the
// result so the consumer can log and skip it without aborting the sweep.
func (s *Service) scanSubject(
	ctx context.Context, subj people.SubjectCount, params Params,
) personResult {
	res, err := s.finder.Find(ctx, subj.UID, candidates.Request{
		Threshold: params.Threshold,
		Limit:     params.Limit,
	})
	if err != nil {
		return personResult{subject: subj, err: err}
	}
	actionable := actionableCandidates(res.Candidates)
	return personResult{
		subject:     subj,
		candidates:  actionable,
		counts:      countActions(actionable),
		alreadyDone: res.Counts.AlreadyDone,
	}
}

// emitResult emits the progress (always) and the person (only when there are
// actionable candidates) for one scanned subject, updating the running summary. A
// per-subject search failure is logged and counted as scanned with no matches.
func (s *Service) emitResult(
	r personResult, total int, summary *Summary, emit func(Event) error,
) error {
	summary.PeopleScanned++
	progress := &Progress{Scanned: summary.PeopleScanned, Total: total, Name: r.subject.Name}
	if r.err != nil {
		s.log.Warn("recognition sweep: subject scan failed",
			"subject", r.subject.UID, "error", r.err)
		return emit(Event{Type: EventProgress, Progress: progress})
	}
	summary.TotalAlreadyDone += r.alreadyDone
	if err := emit(Event{Type: EventProgress, Progress: progress}); err != nil {
		return err
	}
	if len(r.candidates) == 0 {
		return nil
	}
	summary.PeopleWithMatches++
	summary.TotalActionable += len(r.candidates)
	return emit(Event{Type: EventPerson, Person: &Person{
		Subject:    r.subject.Subject,
		Candidates: r.candidates,
		Counts:     r.counts,
		Actionable: len(r.candidates),
	}})
}

// actionableCandidates returns the candidates that still need a human decision,
// dropping the rare already-done ones so the work list stays a work list.
func actionableCandidates(cands []candidates.Candidate) []candidates.Candidate {
	out := make([]candidates.Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Action != candidates.ActionAlreadyDone {
			out = append(out, c)
		}
	}
	return out
}

// countActions tallies actionable candidates by the action confirming them would
// take, mirroring candidates.Counts for the per-person summary.
func countActions(cands []candidates.Candidate) candidates.Counts {
	var counts candidates.Counts
	for _, c := range cands {
		switch c.Action {
		case candidates.ActionCreateMarker:
			counts.CreateMarker++
		case candidates.ActionAssignPerson:
			counts.AssignPerson++
		case candidates.ActionAlreadyDone:
			counts.AlreadyDone++
		}
	}
	return counts
}

// orDefault returns value when positive, else fallback.
func orDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
