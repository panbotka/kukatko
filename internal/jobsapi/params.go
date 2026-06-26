package jobsapi

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/panbotka/kukatko/internal/jobs"
)

// defaultListLimit mirrors the store's default page size so the list endpoint can
// report the effective limit when the request did not set one.
const defaultListLimit = 100

// Sentinel parse errors returned by parseListOptions so handleList can answer
// 400 with a clear message.
var (
	// errInvalidState indicates the state filter is not a recognised job state.
	errInvalidState = errors.New("invalid state filter")
	// errInvalidLimit indicates limit is not a non-negative integer within range.
	errInvalidLimit = errors.New("invalid limit")
	// errInvalidOffset indicates offset is not a non-negative integer.
	errInvalidOffset = errors.New("invalid offset")
)

// validStates is the set of lifecycle states accepted as the state filter.
var validStates = map[jobs.State]bool{
	jobs.StateQueued:  true,
	jobs.StateRunning: true,
	jobs.StateDone:    true,
	jobs.StateFailed:  true,
	jobs.StateDead:    true,
}

// parseListOptions validates the list query parameters (state, limit, offset)
// into a jobs.ListOptions, returning one of the package's sentinel errors on
// invalid input.
func parseListOptions(q url.Values) (jobs.ListOptions, error) {
	var opts jobs.ListOptions
	if raw := q.Get("state"); raw != "" {
		state := jobs.State(raw)
		if !validStates[state] {
			return jobs.ListOptions{}, errInvalidState
		}
		opts.State = &state
	}
	limit, err := nonNegativeInt(q.Get("limit"))
	if err != nil || limit > maxListLimit {
		return jobs.ListOptions{}, errInvalidLimit
	}
	opts.Limit = limit
	offset, err := nonNegativeInt(q.Get("offset"))
	if err != nil {
		return jobs.ListOptions{}, errInvalidOffset
	}
	opts.Offset = offset
	return opts, nil
}

// nonNegativeInt parses raw as a non-negative integer, treating the empty string
// as 0. It returns an error for a malformed or negative value.
func nonNegativeInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing integer %q: %w", raw, err)
	}
	if n < 0 {
		return 0, strconv.ErrRange
	}
	return n, nil
}
