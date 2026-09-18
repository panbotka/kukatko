package phototask

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// ref is a fixed instant the pure rules are evaluated against, so "now" in an
// assertion is a value rather than a moving target.
var ref = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// earlier is the timestamp a stored task carries before the rules run.
var earlier = ref.Add(-48 * time.Hour)

// TestStatePredicates verifies the three questions asked of a state: is it one
// of ours, does it end the task, and is a person being waited on.
func TestStatePredicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state      State
		valid      bool
		closed     bool
		needsHuman bool
	}{
		{state: StateQuestion, valid: true, closed: false, needsHuman: true},
		{state: StateWorking, valid: true, closed: false, needsHuman: false},
		{state: StateReview, valid: true, closed: false, needsHuman: false},
		{state: StateDone, valid: true, closed: true, needsHuman: false},
		{state: StateRejected, valid: true, closed: true, needsHuman: false},
		{state: State("archived"), valid: false, closed: false, needsHuman: false},
		{state: State(""), valid: false, closed: false, needsHuman: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			if got := tt.state.Valid(); got != tt.valid {
				t.Errorf("%q.Valid() = %v, want %v", tt.state, got, tt.valid)
			}
			if got := tt.state.Closed(); got != tt.closed {
				t.Errorf("%q.Closed() = %v, want %v", tt.state, got, tt.closed)
			}
			if got := tt.state.NeedsHuman(); got != tt.needsHuman {
				t.Errorf("%q.NeedsHuman() = %v, want %v", tt.state, got, tt.needsHuman)
			}
		})
	}
}

// TestOpenStatesAreOpen verifies the shorthand list and the predicate cannot
// drift apart: every state in OpenStates is a valid, non-closed one, and every
// state missing from it is closed.
func TestOpenStatesAreOpen(t *testing.T) {
	t.Parallel()

	open := make(map[State]bool, len(OpenStates))
	for _, s := range OpenStates {
		if !s.Valid() || s.Closed() {
			t.Errorf("OpenStates contains %q, which is not an open state", s)
		}
		open[s] = true
	}
	for _, s := range States {
		if !open[s] && !s.Closed() {
			t.Errorf("state %q is open but missing from OpenStates", s)
		}
	}
}

// TestNormalizeTitle verifies the question is trimmed, required and bounded.
func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "trimmed", in: "  V kterém roce?  ", want: "V kterém roce?"},
		{name: "empty", in: "", wantErr: ErrEmptyTitle},
		{name: "whitespace only", in: " \n\t ", wantErr: ErrEmptyTitle},
		{name: "at the limit", in: strings.Repeat("á", MaxTitleLen), want: strings.Repeat("á", MaxTitleLen)},
		{name: "over the limit", in: strings.Repeat("á", MaxTitleLen+1), wantErr: ErrTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeTitle(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeTitle error = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("normalizeTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNormalizeText verifies the optional fields are trimmed, may be empty, and
// name themselves when too long.
func TestNormalizeText(t *testing.T) {
	t.Parallel()

	if got, err := normalizeText("body", "  text  ", MaxBodyLen); err != nil || got != "text" {
		t.Errorf("normalizeText = %q, %v, want %q and no error", got, err, "text")
	}
	if got, err := normalizeText("body", "   ", MaxBodyLen); err != nil || got != "" {
		t.Errorf("normalizeText(blank) = %q, %v, want an empty string and no error", got, err)
	}
	_, err := normalizeText("resolution", strings.Repeat("a", MaxResolutionLen+1), MaxResolutionLen)
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("normalizeText(over) error = %v, want ErrTooLong", err)
	}
	if !strings.Contains(err.Error(), "resolution") {
		t.Errorf("error %q does not name the field", err)
	}
}

// TestNewFields verifies what a task may be opened with: the default state, the
// closing rules, and the validation shared with editing.
func TestNewFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		task      Task
		wantState State
		wantErr   error
	}{
		{name: "defaults to question", task: Task{Title: "Kdy?"}, wantState: StateQuestion},
		{
			name:      "explicit open state",
			task:      Task{Title: "Kdy?", State: StateWorking},
			wantState: StateWorking,
		},
		{
			name:      "closed with a resolution",
			task:      Task{Title: "Kdy?", State: StateDone, Resolution: "1987, potvrdil pamětník"},
			wantState: StateDone,
		},
		{
			name:    "closed without a resolution",
			task:    Task{Title: "Kdy?", State: StateRejected},
			wantErr: ErrClosedNeedsResolution,
		},
		{name: "unknown state", task: Task{Title: "Kdy?", State: "parked"}, wantErr: ErrInvalidState},
		{name: "no title", task: Task{}, wantErr: ErrEmptyTitle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newFields(tt.task, "us1", ref)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("newFields error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.State != tt.wantState {
				t.Errorf("state = %q, want %q", got.State, tt.wantState)
			}
			if !got.StateAt.Equal(ref) {
				t.Errorf("state_at = %v, want %v", got.StateAt, ref)
			}
			assertClosure(t, got, "us1")
		})
	}
}

// assertClosure verifies the closing marks match the state: both set for a
// closed task, both absent for an open one.
func assertClosure(t *testing.T, f fields, actorUID string) {
	t.Helper()
	if f.State.Closed() {
		if f.ClosedAt == nil || f.ClosedBy != actorUID {
			t.Errorf("closed task has closed_at = %v, closed_by = %q", f.ClosedAt, f.ClosedBy)
		}
		return
	}
	if f.ClosedAt != nil || f.ClosedBy != "" {
		t.Errorf("open task has closed_at = %v, closed_by = %q", f.ClosedAt, f.ClosedBy)
	}
}

// stored is the task the update tests fold onto: open, waiting for an answer,
// last moved two days ago.
func stored() Task {
	return Task{
		Title: "V kterém roce?", Body: "Tři fotky přestavby.", State: StateQuestion,
		Query: "camera:Olympus dated:no", StateAt: earlier,
	}
}

// TestApplyUpdate_fields verifies a partial edit touches only what it names, and
// that an untouched state leaves state_at where it was — which is what keeps
// "has anybody replied since" meaningful across an ordinary edit.
func TestApplyUpdate_fields(t *testing.T) {
	t.Parallel()

	next, changes, err := applyUpdate(stored(), Update{Body: new("  Čtyři fotky.  ")}, "us1", ref)
	if err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if next.Body != "Čtyři fotky." {
		t.Errorf("body = %q, want the trimmed text", next.Body)
	}
	if next.Title != stored().Title || next.State != StateQuestion {
		t.Errorf("an edit of the body changed something else: %+v", next)
	}
	if !next.StateAt.Equal(earlier) {
		t.Errorf("state_at = %v, want it left at %v", next.StateAt, earlier)
	}
	if changes.Len() != 1 {
		t.Errorf("changes = %v, want exactly the body", changes.Map())
	}
}

// TestApplyUpdate_stateMoves verifies advancing the state stamps state_at, and
// that closing records both the resolution and who closed it.
func TestApplyUpdate_stateMoves(t *testing.T) {
	t.Parallel()

	next, changes, err := applyUpdate(stored(), Update{State: new(StateWorking)}, "us1", ref)
	if err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if !next.StateAt.Equal(ref) {
		t.Errorf("state_at = %v, want %v", next.StateAt, ref)
	}
	if next.ClosedAt != nil {
		t.Errorf("an open state stamped closed_at = %v", next.ClosedAt)
	}
	if changes.Len() != 1 {
		t.Errorf("changes = %v, want exactly the state", changes.Map())
	}

	closed, _, err := applyUpdate(stored(), Update{
		State:      new(StateDone),
		Resolution: new("1987 podle kroniky"),
	}, "us2", ref)
	if err != nil {
		t.Fatalf("applyUpdate(close): %v", err)
	}
	if closed.ClosedAt == nil || !closed.ClosedAt.Equal(ref) || closed.ClosedBy != "us2" {
		t.Errorf("closed_at = %v, closed_by = %q, want %v and us2", closed.ClosedAt, closed.ClosedBy, ref)
	}
}

// TestApplyUpdate_closingRules verifies the one rule a closed task may never
// break: it always says how it ended, whether it is being closed now or was
// closed already.
func TestApplyUpdate_closingRules(t *testing.T) {
	t.Parallel()

	if _, _, err := applyUpdate(stored(), Update{State: new(StateDone)}, "us1", ref); !errors.Is(
		err, ErrClosedNeedsResolution,
	) {
		t.Errorf("closing without a resolution: error = %v, want ErrClosedNeedsResolution", err)
	}

	closed := stored()
	closed.State = StateDone
	closed.Resolution = "1987"
	closed.ClosedAt = &earlier
	closed.ClosedByUID = "us1"
	if _, _, err := applyUpdate(closed, Update{Resolution: new("")}, "us2", ref); !errors.Is(
		err, ErrClosedNeedsResolution,
	) {
		t.Errorf("emptying a closed task's resolution: error = %v, want ErrClosedNeedsResolution", err)
	}
}

// TestApplyUpdate_reopen verifies reopening clears the closing marks, so a task
// that is demonstrably open never carries a stale "closed by".
func TestApplyUpdate_reopen(t *testing.T) {
	t.Parallel()

	closed := stored()
	closed.State = StateDone
	closed.Resolution = "1987"
	closed.ClosedAt = &earlier
	closed.ClosedByUID = "us1"

	next, _, err := applyUpdate(closed, Update{State: new(StateQuestion)}, "us2", ref)
	if err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if next.ClosedAt != nil || next.ClosedBy != "" {
		t.Errorf("reopened task kept closed_at = %v, closed_by = %q", next.ClosedAt, next.ClosedBy)
	}
	if next.Resolution != "1987" {
		t.Errorf("resolution = %q, want the text kept as the record of the earlier decision", next.Resolution)
	}
}

// TestApplyUpdate_rejects verifies an unknown state and an over-long field are
// refused before anything is written.
func TestApplyUpdate_rejects(t *testing.T) {
	t.Parallel()

	if _, _, err := applyUpdate(stored(), Update{State: new(State("parked"))}, "us1", ref); !errors.Is(
		err, ErrInvalidState,
	) {
		t.Errorf("unknown state: error = %v, want ErrInvalidState", err)
	}
	long := strings.Repeat("a", MaxBodyLen+1)
	if _, _, err := applyUpdate(stored(), Update{Body: &long}, "us1", ref); !errors.Is(err, ErrTooLong) {
		t.Errorf("over-long body: error = %v, want ErrTooLong", err)
	}
	if _, _, err := applyUpdate(stored(), Update{Title: new("  ")}, "us1", ref); !errors.Is(
		err, ErrEmptyTitle,
	) {
		t.Errorf("blank title: error = %v, want ErrEmptyTitle", err)
	}
}

// TestDiff verifies the audit diff records the fields that moved and leaves the
// derived closing marks out of it.
func TestDiff(t *testing.T) {
	t.Parallel()

	cur := stored()
	next := fields{
		Title: cur.Title, Body: "nový popis", State: StateWorking,
		Resolution: cur.Resolution, Query: cur.Query, StateAt: ref,
	}
	got := diff(cur, next).Map()
	want := []string{"body", "state"}
	if len(got) != len(want) {
		t.Fatalf("diff = %v, want exactly %v", got, want)
	}
	for _, field := range want {
		change, ok := got[field].(audit.Change)
		if !ok {
			t.Fatalf("diff[%s] = %v, want an audit.Change", field, got[field])
		}
		if change.Old == change.New {
			t.Errorf("diff[%s] records no change: %+v", field, change)
		}
	}
}
