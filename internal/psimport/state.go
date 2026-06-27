package psimport

import (
	"time"

	"github.com/panbotka/kukatko/internal/importer"
)

// outcome classifies how a single photo was handled, so the run counts can be
// tallied by category.
type outcome int

const (
	// outcomeImported is a new photo whose original was copied and catalogued.
	outcomeImported outcome = iota
	// outcomeUpdated is an existing photo whose metadata changed on migration.
	outcomeUpdated
	// outcomeSkipped is a photo already catalogued (matched by photosorter_uid or
	// file_hash); its satellites are still transferred idempotently.
	outcomeSkipped
)

// runState accumulates the per-run tally and the timestamps needed to compute a
// safe resume watermark. The watermark advances only as far as it can without
// skipping a failed photo on the next run.
type runState struct {
	// since is the resume cursor inherited from the last successful run.
	since time.Time
	// counts is the running imported/updated/skipped/failed tally.
	counts importer.Counts
	// maxSuccess is the largest updated_at of a successfully migrated photo.
	maxSuccess time.Time
	// minFailed is the earliest updated_at of any failed photo.
	minFailed time.Time
	// hasFailed records whether any photo failed (so minFailed is meaningful).
	hasFailed bool
	// sawAny records whether any photo was processed at all.
	sawAny bool
}

// recordSuccess advances the success watermark to include updatedAt.
func (st *runState) recordSuccess(updatedAt time.Time) {
	st.sawAny = true
	if updatedAt.After(st.maxSuccess) {
		st.maxSuccess = updatedAt
	}
}

// recordFailure tallies a failed photo and tracks the earliest failure timestamp
// so the watermark never advances past it.
func (st *runState) recordFailure(updatedAt time.Time) {
	st.sawAny = true
	st.counts.Failed++
	if !st.hasFailed || updatedAt.Before(st.minFailed) {
		st.minFailed = updatedAt
		st.hasFailed = true
	}
}

// watermark returns the resume cursor to record for the next run: the largest
// successfully processed timestamp, but never at or beyond the earliest failure
// (so a failed photo is re-listed inclusively next run). It returns nil when
// nothing was processed or no usable cursor was produced.
func (st *runState) watermark() *time.Time {
	if !st.sawAny {
		return nil
	}
	cursor := st.maxSuccess
	if st.hasFailed {
		// Cap strictly below the earliest failure so it is revisited next run.
		bound := st.minFailed.Add(-time.Nanosecond)
		if bound.Before(cursor) {
			cursor = bound
		}
	}
	if !cursor.After(st.since) {
		// No forward progress beyond the prior watermark.
		if st.since.IsZero() {
			return nil
		}
		cursor = st.since
	}
	if cursor.IsZero() {
		return nil
	}
	return &cursor
}
