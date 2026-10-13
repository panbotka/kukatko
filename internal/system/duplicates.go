package system

import (
	"context"
	"fmt"
	"time"
)

// defaultDuplicateTTL is how long a duplicate scan's answer stays good enough.
// It is minutes rather than seconds because the scan is the one aggregation on
// this dashboard that is genuinely expensive — every embedding's nearest
// neighbours through the HNSW index, plus the perceptual-hash bands — and
// because the number it produces is a backlog: it changes when somebody merges
// duplicates, which is not something that happens between two polls.
const defaultDuplicateTTL = 15 * time.Minute

// defaultDuplicateTimeout bounds one background scan. A scan that has not
// finished by then is abandoned and retried after the TTL rather than being left
// to hold a database connection indefinitely.
const defaultDuplicateTimeout = 2 * time.Minute

// DuplicateCounter counts one project's near-duplicate photo groups — a group
// never spans projects, so there is no instance-wide count to take. It is
// satisfied by *duplicates.Service; an interface (returning a plain count
// rather than the finder's Result) so this package neither imports the
// duplicates package nor grows a second opinion about what a duplicate is.
type DuplicateCounter interface {
	// CountGroups returns how many duplicate groups the project currently holds.
	CountGroups(ctx context.Context, projectUID string) (int, error)
}

// DuplicateScan is the near-duplicate finder's last answer, and the one section
// of the dashboard that reports its own freshness. Every other number is a SQL
// count taken while the request was being served; this one is computed in the
// background (see asyncCache) because the scan is far too expensive to run on a
// polled endpoint, so the reader is told when it was taken.
type DuplicateScan struct {
	// Configured is true when a duplicate finder is wired at all. When false the
	// rest of this section is meaningless.
	Configured bool `json:"configured"`
	// Available is true once a scan has finished. Until then Groups is not a
	// count of zero — it is no count at all, and must not be rendered as one.
	Available bool `json:"available"`
	// Groups is how many duplicate groups the last finished scan found, summed
	// over the active projects (each project's own is in ProjectSummary).
	Groups int `json:"groups"`
	// ComputedAt is when that scan finished; nil while none has.
	ComputedAt *time.Time `json:"computed_at,omitempty"`
}

// newDuplicateCache returns the background-refreshed duplicate-group counts
// over counter — one count per active project, keyed by project uid, which svc
// enumerates — or nil when no counter is wired (an instance without a
// duplicate finder reports the section as not configured rather than as an
// empty scan).
func newDuplicateCache(
	svc *Service, counter DuplicateCounter, ttl, timeout time.Duration, now func() time.Time,
) *asyncCache[map[string]int] {
	if counter == nil {
		return nil
	}
	scan := func(ctx context.Context) (map[string]int, error) {
		projectUIDs, err := svc.activeProjects(ctx)
		if err != nil {
			return nil, err
		}
		counts := make(map[string]int, len(projectUIDs))
		for _, projectUID := range projectUIDs {
			n, err := counter.CountGroups(ctx, projectUID)
			if err != nil {
				return nil, fmt.Errorf("counting the duplicate groups of project %s: %w", projectUID, err)
			}
			counts[projectUID] = n
		}
		return counts, nil
	}
	return newAsyncCache(scan, ttl, defaultDuplicateTTL, timeout, defaultDuplicateTimeout, now)
}

// collectDuplicates reads the last finished scan, scheduling a fresh one when the
// answer has gone stale, and returns the instance total together with the
// per-project counts. It never waits for the scan: an unconfigured or
// not-yet-scanned instance is reported as such (and with no per-project counts).
func (s *Service) collectDuplicates() (DuplicateScan, map[string]int) {
	if s.duplicates == nil {
		return DuplicateScan{}, nil
	}
	perProject, computedAt, ok := s.duplicates.get()
	scan := DuplicateScan{Configured: true, Available: ok}
	for _, n := range perProject {
		scan.Groups += n
	}
	if ok {
		at := computedAt
		scan.ComputedAt = &at
	}
	return scan, perProject
}
