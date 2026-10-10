package maintenance

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/processing"
)

// ErrProcessingUnavailable indicates the unscheduled-steps repair was requested
// on a Service built without a ProcessingGaps collaborator.
var ErrProcessingUnavailable = errors.New("maintenance: processing gap repair not configured")

// ProcessingGaps finds and schedules the processing steps live photos are owed
// and nothing is going to deliver: no evidence the step ran and no job of its
// type in the queue. It is satisfied by *processing.Service, so "owed" here means
// precisely what `pending` means in a photo's processing report — and steps
// switched off on this instance are never counted.
//
// A nil ProcessingGaps leaves the finding empty and makes the repair refuse.
type ProcessingGaps interface {
	// Unscheduled returns every (photo, step) gap in the live library.
	Unscheduled(ctx context.Context) ([]processing.Gap, error)
	// Run schedules one step for one photo, re-checking that it still applies.
	Run(ctx context.Context, photoUID string, step processing.Step) (processing.Status, error)
}

// gapSample is how a gap is named in the finding's samples: photo uid and step,
// the same <photo_uid>/<what> shape the missing renditions use.
func gapSample(gap processing.Gap) string {
	return gap.PhotoUID + "/" + string(gap.Step)
}

// scanUnscheduledSteps turns the library's processing gaps into a Finding,
// counted per (photo, step) and sampled as <photo_uid>/<step>. It is the dry run
// of `maintenance repair --unscheduled-steps`: every gap it counts is one that
// repair enqueues a job for.
//
// These are the photos nothing else will ever fix on its own. A missing
// embedding with its job waiting for the box is merely late; a missing embedding
// with no job is what an upload cut short after its original was stored leaves
// behind (2026-10-05), and it stays missing until somebody schedules it.
func (s *Service) scanUnscheduledSteps(ctx context.Context) (Finding, error) {
	if s.processing == nil {
		return Finding{Samples: []string{}}, nil
	}
	gaps, err := s.processing.Unscheduled(ctx)
	if err != nil {
		return Finding{}, fmt.Errorf("maintenance: listing unscheduled processing steps: %w", err)
	}
	ids := make([]string, len(gaps))
	for i, gap := range gaps {
		ids[i] = gapSample(gap)
	}
	return findingFrom(ids, s.sampleLimit), nil
}

// repairUnscheduledSteps schedules every processing gap the scan reports, when
// that repair is selected, through the same single-step path as a photo's "run
// now" button — so each step keeps its own scheduling semantics and a step that
// stopped applying in the meantime is skipped rather than queued. An already
// queued job absorbs the request, so a second run converges.
func (s *Service) repairUnscheduledSteps(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.UnscheduledSteps {
		return nil
	}
	if s.processing == nil {
		return ErrProcessingUnavailable
	}
	gaps, err := s.processing.Unscheduled(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing unscheduled processing steps: %w", err)
	}
	for _, gap := range gaps {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: unscheduled-step repair interrupted: %w", ctxErr)
		}
		_, runErr := s.processing.Run(ctx, gap.PhotoUID, gap.Step)
		if errors.Is(runErr, processing.ErrStepNotApplicable) {
			continue
		}
		if runErr != nil {
			return fmt.Errorf("maintenance: scheduling %s: %w", gapSample(gap), runErr)
		}
		res.StepsScheduled++
	}
	return nil
}
