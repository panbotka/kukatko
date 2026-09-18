package phototask

import (
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// fields is the set of columns a write actually sets: the text, the state, and
// the three values the state drags along with it. Keeping them in one struct is
// what lets the rules below be a pure function — the store writes whatever this
// returns and decides nothing itself.
type fields struct {
	Title      string
	Body       string
	State      State
	Resolution string
	Query      string
	StateAt    time.Time
	ClosedAt   *time.Time
	ClosedBy   string
}

// newFields validates the values a task is opened with and returns the columns
// to insert. A task may be opened in any valid state, closed ones included (an
// agent recording a decision already taken), but a closed one must say how it
// ended. now stamps both state_at and, for a closed task, closed_at.
func newFields(t Task, actorUID string, now time.Time) (fields, error) {
	title, err := normalizeTitle(t.Title)
	if err != nil {
		return fields{}, err
	}
	text, err := normalizeTexts(t.Body, t.Resolution, t.Query)
	if err != nil {
		return fields{}, err
	}
	state := t.State
	if state == "" {
		state = StateQuestion
	}
	if !state.Valid() {
		return fields{}, fmt.Errorf("%w: %q", ErrInvalidState, state)
	}
	closedAt, closedBy, err := closure(state, text.resolution, actorUID, now)
	if err != nil {
		return fields{}, err
	}
	return fields{
		Title: title, Body: text.body, State: state, Resolution: text.resolution,
		Query: text.query, StateAt: now, ClosedAt: closedAt, ClosedBy: closedBy,
	}, nil
}

// applyUpdate folds upd onto the stored task and returns the columns to write
// plus the diff to audit. A nil field is left alone; changing the state moves
// state_at and recomputes the closing marks, and a state that stays put keeps the
// state_at it had — which is what makes "has anybody replied since" mean
// something. actorUID is stamped as the person who closed it.
func applyUpdate(cur Task, upd Update, actorUID string, now time.Time) (fields, *audit.ChangeSet, error) {
	next := fields{
		Title: cur.Title, Body: cur.Body, State: cur.State, Resolution: cur.Resolution,
		Query: cur.Query, StateAt: cur.StateAt, ClosedAt: cur.ClosedAt, ClosedBy: cur.ClosedByUID,
	}
	if err := applyText(&next, upd); err != nil {
		return fields{}, nil, err
	}
	if upd.State != nil && !upd.State.Valid() {
		return fields{}, nil, fmt.Errorf("%w: %q", ErrInvalidState, *upd.State)
	}
	if upd.State != nil {
		next.State = *upd.State
	}
	if next.State != cur.State {
		next.StateAt = now
		closedAt, closedBy, err := closure(next.State, next.Resolution, actorUID, now)
		if err != nil {
			return fields{}, nil, err
		}
		next.ClosedAt, next.ClosedBy = closedAt, closedBy
	} else if next.State.Closed() && next.Resolution == "" {
		// The state did not move, but emptying the resolution would leave a
		// closed task silent — the one thing a closed task may never be.
		return fields{}, nil, ErrClosedNeedsResolution
	}
	return next, diff(cur, next), nil
}

// applyText folds the four free-text fields of upd onto next, normalising each.
func applyText(next *fields, upd Update) error {
	if upd.Title != nil {
		title, err := normalizeTitle(*upd.Title)
		if err != nil {
			return err
		}
		next.Title = title
	}
	for _, f := range []struct {
		value *string
		into  *string
		name  string
		limit int
	}{
		{upd.Body, &next.Body, "body", MaxBodyLen},
		{upd.Resolution, &next.Resolution, "resolution", MaxResolutionLen},
		{upd.Query, &next.Query, "query", MaxQueryLen},
	} {
		if f.value == nil {
			continue
		}
		text, err := normalizeText(f.name, *f.value, f.limit)
		if err != nil {
			return err
		}
		*f.into = text
	}
	return nil
}

// normalizedTexts holds the three optional text fields after trimming.
type normalizedTexts struct {
	body       string
	resolution string
	query      string
}

// normalizeTexts trims and length-checks the three optional text fields at once,
// so opening a task and editing one accept exactly the same values.
func normalizeTexts(body, resolution, searchQuery string) (normalizedTexts, error) {
	out := normalizedTexts{}
	var err error
	if out.body, err = normalizeText("body", body, MaxBodyLen); err != nil {
		return normalizedTexts{}, err
	}
	if out.resolution, err = normalizeText("resolution", resolution, MaxResolutionLen); err != nil {
		return normalizedTexts{}, err
	}
	if out.query, err = normalizeText("query", searchQuery, MaxQueryLen); err != nil {
		return normalizedTexts{}, err
	}
	return out, nil
}

// closure returns the closing marks a state implies: the timestamp and the
// person for a closed state, nothing for an open one. Closing without a
// resolution is refused here, which is the single place that rule lives.
func closure(state State, resolution, actorUID string, now time.Time) (*time.Time, string, error) {
	if !state.Closed() {
		return nil, "", nil
	}
	if resolution == "" {
		return nil, "", ErrClosedNeedsResolution
	}
	closedAt := now
	return &closedAt, actorUID, nil
}

// diff records which fields an update actually changed, for the audit entry. The
// derived marks (state_at, closed_at, closed_by) are left out: they are
// consequences of the state, and the state itself is in the diff.
func diff(cur Task, next fields) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("title", cur.Title, next.Title)
	changes.Add("body", cur.Body, next.Body)
	changes.Add("state", string(cur.State), string(next.State))
	changes.Add("resolution", cur.Resolution, next.Resolution)
	changes.Add("query", cur.Query, next.Query)
	return changes
}
