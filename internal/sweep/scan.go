package sweep

// Bounded scanning.
//
// Sweep is a streaming endpoint's shape: it walks every named subject while the
// client watches a progress bar, and the cost — subjects × exemplars × faces —
// is paid in the open. A caller that only needs a handful of candidates (the
// review game filling one batch of questions) must not pay for the whole
// library, because it pays before the first byte of its response is written.
//
// Scan is that bounded form: a rotating window over the same plan, a hard cap on
// how many subjects one run may dispatch, and an early stop as soon as the
// caller says it has enough. Feeding Coverage.NextOffset back into the next run
// rotates through the whole library over successive runs, so bounding one run
// does not mean permanently ignoring the tail of the subject list.

import (
	"context"
	"sync/atomic"

	"github.com/panbotka/kukatko/internal/people"
)

// Window bounds one Scan run over the planned subjects.
type Window struct {
	// Offset is the index into the planned subject list the run starts at. It
	// wraps, so a caller can keep feeding back Coverage.NextOffset (or let its
	// cursor grow past the end) without ever indexing out of range.
	Offset int
	// Budget caps how many subjects the run may dispatch. A non-positive budget
	// means every planned subject — the same coverage as Sweep.
	Budget int
}

// Coverage reports what one bounded scan actually covered.
type Coverage struct {
	// SubjectsTotal is how many named subjects have at least one face, before
	// the window or the MaxSubjects cap narrows anything. It is the same total
	// Summary.SubjectsTotal carries, so a bounded caller can still tell "no
	// named people at all" from "nobody in this window matched".
	SubjectsTotal int
	// Scanned is how many subjects this run dispatched — at most Budget, and
	// usually far fewer when the collector stopped it early. A stop cannot
	// un-dispatch what the worker pool already started, so it can overshoot the
	// stopping point by up to Concurrency subjects; those are collected too, so
	// the count never hides work that was done.
	Scanned int
	// NextOffset is where the following run should start so the rotation
	// continues without leaving a gap.
	NextOffset int
}

// Collect consumes one scanned subject's actionable candidates and reports
// whether the caller has seen enough. Returning true stops further dispatch;
// subjects already in flight still finish and are still handed to Collect (so no
// computed work is thrown away), which means Collect can be called a few more
// times after it said "enough". Collect is called serially, never concurrently,
// so it needs no locking. An error from Collect aborts the scan and is returned
// by Scan.
type Collect func(person *Person) (enough bool, err error)

// dispatchState is the state shared between a scan's dispatcher goroutine and
// its consumer: the flag the consumer raises when it has enough, and the count
// of subjects the dispatcher actually started.
type dispatchState struct {
	// stop tells the dispatcher to start no further subjects.
	stop atomic.Bool
	// dispatched counts the subjects the dispatcher started.
	dispatched atomic.Int64
}

// Scan runs the per-subject candidate search over a bounded window of the named
// subjects and hands every subject that yielded actionable candidates to
// collect, stopping as soon as collect reports it has enough. It returns the
// coverage the run achieved: the library-wide subject total (so an empty result
// can still be explained), how many subjects were scanned, and where the next
// run should resume.
//
// Listing the subjects is the only fatal step; a per-subject search failure is
// logged and skipped exactly as in Sweep, so one broken subject cannot fail a
// caller's whole request. The work is bounded by Budget subjects regardless of
// how large the library grows.
func (s *Service) Scan(ctx context.Context, params Params, win Window, collect Collect) (Coverage, error) {
	pl, err := s.buildPlan(ctx)
	if err != nil {
		return Coverage{}, err
	}
	planned := len(pl.subjects)
	if planned == 0 {
		return Coverage{SubjectsTotal: pl.total}, nil
	}
	offset := wrapOffset(win.Offset, planned)
	scanned, err := s.collectWindow(ctx, rotate(pl.subjects, offset, win.Budget), params, collect)
	if err != nil {
		return Coverage{}, err
	}
	return Coverage{
		SubjectsTotal: pl.total,
		Scanned:       scanned,
		NextOffset:    wrapOffset(offset+scanned, planned),
	}, nil
}

// collectWindow scans one window with the bounded worker pool and feeds each
// result to collect, returning how many subjects the dispatcher started. The
// consumer keeps draining after a stop so every subject already in flight is
// still collected and the dispatcher goroutine always finishes.
func (s *Service) collectWindow(
	ctx context.Context, window []people.SubjectCount, params Params, collect Collect,
) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	state := new(dispatchState)
	results := make(chan personResult)
	go s.scanAll(ctx, window, params, results, state)

	var collectErr error
	for r := range results {
		if collectErr != nil {
			continue // keep draining so the workers unblock and the channel closes
		}
		if err := s.collectResult(r, collect, state); err != nil {
			collectErr = err
			state.stop.Store(true)
			cancel()
		}
	}
	if collectErr != nil {
		return 0, collectErr
	}
	return int(state.dispatched.Load()), nil
}

// collectResult hands one scanned subject's actionable candidates to collect,
// raising the dispatcher's stop flag when the collector has enough.
func (s *Service) collectResult(r personResult, collect Collect, state *dispatchState) error {
	if !s.reportable(r) {
		return nil
	}
	enough, err := collect(personOf(r))
	if err != nil {
		return err
	}
	if enough {
		state.stop.Store(true)
	}
	return nil
}

// reportable decides whether a scanned subject is worth handing to the
// collector. A subject whose search failed is logged and dropped — the same
// per-subject policy Sweep applies, so one broken subject cannot fail the
// caller's whole request — and a subject with no actionable candidates has
// nothing to report.
func (s *Service) reportable(r personResult) bool {
	if r.err != nil {
		s.log.Warn("recognition scan: subject scan failed",
			"subject", r.subject.UID, "error", r.err)
		return false
	}
	return len(r.candidates) > 0
}

// rotate returns the Budget-long window of subjects starting at offset, wrapping
// past the end of the list. A non-positive budget — or one at least as large as
// the list — returns every planned subject, still starting at offset.
func rotate(subjects []people.SubjectCount, offset, budget int) []people.SubjectCount {
	total := len(subjects)
	if budget <= 0 || budget > total {
		budget = total
	}
	out := make([]people.SubjectCount, 0, budget)
	for i := range budget {
		out = append(out, subjects[(offset+i)%total])
	}
	return out
}

// wrapOffset folds an arbitrary offset into [0, total), so a caller's cursor can
// keep growing (or arrive negative) without indexing out of range.
func wrapOffset(offset, total int) int {
	if total <= 0 {
		return 0
	}
	offset %= total
	if offset < 0 {
		offset += total
	}
	return offset
}
