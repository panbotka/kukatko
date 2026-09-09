package hlsjob

import (
	"context"
	"fmt"
)

// BackfillHLS enqueues an `hls_transcode` job for every video the encoder has
// never produced a rendition for, and returns how many were scheduled. When all
// is true it schedules every non-archived video instead — a forced full re-run,
// which is how a library picks up a newly enabled quality level or a changed
// segment length, since the job replaces a rendition rather than adding to it.
//
// It only schedules: the encode itself is by far the most expensive job in the
// queue and drains one clip at a time, so a backfill over a library of videos is
// hours of background work and never anything a request waits on.
//
// It returns ErrBackfillUnavailable on a Service built without the backfill
// collaborators, and stops at the first enqueue failure — reporting how many had
// been scheduled by then, since those jobs are already in the queue.
func (s *Service) BackfillHLS(ctx context.Context, all bool) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.backfillCandidates(ctx, all)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueHLSTranscode(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("hlsjob: enqueuing hls_transcode for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// backfillCandidates returns the uids the backfill should schedule: every
// non-archived video when all is set, otherwise only those with no rendition.
func (s *Service) backfillCandidates(ctx context.Context, all bool) ([]string, error) {
	if all {
		uids, err := s.lister.ListActiveVideoUIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("hlsjob: listing active videos: %w", err)
		}
		return uids, nil
	}
	uids, err := s.lister.ListVideosMissingHLS(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("hlsjob: listing videos missing HLS: %w", err)
	}
	return uids, nil
}
