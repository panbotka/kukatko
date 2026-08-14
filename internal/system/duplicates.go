package system

import (
	"context"
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

// DuplicateCounter counts the near-duplicate photo groups. It is satisfied by
// *duplicates.Service; an interface (returning a plain count rather than the
// finder's Result) so this package neither imports the duplicates package nor
// grows a second opinion about what a duplicate is.
type DuplicateCounter interface {
	// CountGroups returns how many duplicate groups the catalogue currently holds.
	CountGroups(ctx context.Context) (int, error)
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
	// Groups is how many duplicate groups the last finished scan found.
	Groups int `json:"groups"`
	// ComputedAt is when that scan finished; nil while none has.
	ComputedAt *time.Time `json:"computed_at,omitempty"`
}

// newDuplicateCache returns the background-refreshed duplicate-group count over
// counter, or nil when no counter is wired (an instance without a duplicate
// finder reports the section as not configured rather than as an empty scan).
func newDuplicateCache(
	counter DuplicateCounter, ttl, timeout time.Duration, now func() time.Time,
) *asyncCache[int] {
	if counter == nil {
		return nil
	}
	return newAsyncCache(counter.CountGroups,
		ttl, defaultDuplicateTTL, timeout, defaultDuplicateTimeout, now)
}

// collectDuplicates reads the last finished scan, scheduling a fresh one when the
// answer has gone stale. It never waits for the scan: an unconfigured or
// not-yet-scanned instance is reported as such.
func (s *Service) collectDuplicates() DuplicateScan {
	if s.duplicates == nil {
		return DuplicateScan{}
	}
	groups, computedAt, ok := s.duplicates.get()
	scan := DuplicateScan{Configured: true, Available: ok, Groups: groups}
	if ok {
		at := computedAt
		scan.ComputedAt = &at
	}
	return scan
}
