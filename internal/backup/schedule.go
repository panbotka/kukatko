package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	// robfig/cron is the de-facto standard cron-expression parser for Go; the
	// standard library has no cron support and a correct parser (ranges, steps,
	// lists, descriptors) is non-trivial to hand-roll. Only its parser is used —
	// the run loop below is our own so it honours context cancellation.
	"github.com/robfig/cron/v3"
)

// Sentinel errors for schedule handling.
var (
	// ErrNoSchedule indicates an empty schedule string; scheduled backups are
	// disabled (manual runs still work).
	ErrNoSchedule = errors.New("backup: no schedule configured")
	// ErrInvalidSchedule indicates the schedule string is not a valid cron
	// expression or descriptor.
	ErrInvalidSchedule = errors.New("backup: invalid schedule")
)

// ParseSchedule parses a cron schedule string into a cron.Schedule. It accepts
// standard 5-field cron expressions and the @hourly/@daily/@weekly/@monthly and
// @every <duration> descriptors. An empty string returns ErrNoSchedule; an
// unparseable one returns ErrInvalidSchedule.
func ParseSchedule(spec string) (cron.Schedule, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return nil, ErrNoSchedule
	}
	schedule, err := cron.ParseStandard(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidSchedule, trimmed, err)
	}
	return schedule, nil
}

// RunSchedule runs a backup on every tick of the cron schedule until ctx is
// cancelled. An empty or invalid schedule disables scheduled backups (it logs
// and returns at once); manual runs via Run/Trigger are unaffected. Intended to
// be launched in a goroutine for the lifetime of the server process.
func (s *Service) RunSchedule(ctx context.Context, spec string) {
	schedule, err := ParseSchedule(spec)
	if err != nil {
		if errors.Is(err, ErrNoSchedule) {
			s.logger.Printf("backup: scheduled backups disabled (no schedule configured)")
		} else {
			s.logger.Printf("backup: scheduled backups disabled: %v", err)
		}
		return
	}
	s.logger.Printf("backup: scheduled backups enabled (%q)", strings.TrimSpace(spec))
	s.loopSchedule(ctx, schedule)
}

// loopSchedule waits for each scheduled fire time and runs a backup, stopping
// promptly when ctx is cancelled. A run that overlaps the next fire time simply
// delays it; Run serialises so overlapping ticks never start a second run.
func (s *Service) loopSchedule(ctx context.Context, schedule cron.Schedule) {
	for {
		next := schedule.Next(time.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case fireTime := <-timer.C:
			s.runScheduled(ctx, fireTime)
		}
	}
}

// runScheduled runs one scheduled backup as of fireTime and logs its outcome,
// swallowing errors (other than logging them) so a transient failure never
// stops the schedule loop.
func (s *Service) runScheduled(ctx context.Context, fireTime time.Time) {
	if _, err := s.Run(ctx, fireTime); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logger.Printf("backup: scheduled run failed: %v", err)
	}
}
