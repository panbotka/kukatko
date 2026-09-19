package phototask

import (
	"errors"
	"slices"
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

// TestStateBy verifies the mover is recorded with the move and only with it:
// opening a task names its opener, changing the state names the actor, and an
// edit that leaves the state alone keeps whoever moved it last.
func TestStateBy(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
	opened, err := newFields(Task{Title: "Kdy?"}, "us-opener", ref)
	if err != nil {
		t.Fatalf("newFields: %v", err)
	}
	if opened.StateBy != "us-opener" {
		t.Errorf("a new task's state_by = %q, want the opener", opened.StateBy)
	}

	cur := Task{Title: "Kdy?", State: StateQuestion, StateAt: ref.Add(-time.Hour), StateByUID: "us-opener"}
	tests := []struct {
		name string
		upd  Update
		want string
	}{
		{name: "moving the state names the actor", upd: Update{State: new(StateWorking)}, want: "us-agent"},
		{name: "an edit keeps the last mover", upd: Update{Body: new("more context")}, want: "us-opener"},
		{name: "the same state again is not a move", upd: Update{State: new(StateQuestion)}, want: "us-opener"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next, _, err := applyUpdate(cur, tt.upd, "us-agent", ref)
			if err != nil {
				t.Fatalf("applyUpdate: %v", err)
			}
			if next.StateBy != tt.want {
				t.Errorf("state_by = %q, want %q", next.StateBy, tt.want)
			}
		})
	}
}

// TestNormalizeOptions verifies the rules an answer set is held to: the count,
// blank entries, the per-option length, duplicates after trimming — and that a
// good set comes back trimmed, in order, never nil.
func TestNormalizeOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr error
	}{
		{name: "nil is an empty set", in: nil, want: []string{}},
		{name: "trimmed and ordered", in: []string{" 1936 ", "1938", "nevím"}, want: []string{"1936", "1938", "nevím"}},
		{name: "five is the limit", in: []string{"a", "b", "c", "d", "e"}, want: []string{"a", "b", "c", "d", "e"}},
		{name: "six is too many", in: []string{"a", "b", "c", "d", "e", "f"}, wantErr: ErrTooManyOptions},
		{name: "blank option", in: []string{"1936", "   "}, wantErr: ErrEmptyOption},
		{name: "over-long option", in: []string{strings.Repeat("ž", MaxOptionLen+1)}, wantErr: ErrTooLong},
		{name: "sixty characters fit", in: []string{strings.Repeat("ž", MaxOptionLen)}, want: []string{strings.Repeat("ž", MaxOptionLen)}},
		{name: "duplicate after trimming", in: []string{"1936", " 1936"}, wantErr: ErrDuplicateOption},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeOptions(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeOptions(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got == nil {
				t.Fatal("normalizeOptions returned nil, want an empty slice")
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("normalizeOptions(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestOptions_openAndEdit verifies the options travel through both write paths:
// opening a task validates and keeps them, an edit that names them replaces the
// whole set (an empty set clears it), and one that does not leaves them alone.
func TestOptions_openAndEdit(t *testing.T) {
	t.Parallel()

	opened, err := newFields(Task{Title: "Kdy?", Options: []string{"1936", " 1938 "}}, "us1", ref)
	if err != nil {
		t.Fatalf("newFields: %v", err)
	}
	if !slices.Equal(opened.Options, []string{"1936", "1938"}) {
		t.Errorf("opened options = %q", opened.Options)
	}
	if _, err := newFields(Task{Title: "Kdy?", Options: []string{"a", "a"}}, "us1", ref); !errors.Is(
		err, ErrDuplicateOption,
	) {
		t.Errorf("duplicate on open: error = %v, want ErrDuplicateOption", err)
	}

	cur := stored()
	cur.Options = []string{"1936", "1938"}

	kept, changes, err := applyUpdate(cur, Update{Body: new("jiný kontext")}, "us1", ref)
	if err != nil {
		t.Fatalf("edit without options: %v", err)
	}
	if !slices.Equal(kept.Options, cur.Options) {
		t.Errorf("an edit naming no options changed them to %q", kept.Options)
	}
	if _, ok := changes.Map()["options"]; ok {
		t.Error("an untouched option set entered the audit diff")
	}

	replaced, changes, err := applyUpdate(cur, Update{Options: &[]string{"nevím"}}, "us1", ref)
	if err != nil {
		t.Fatalf("replacing options: %v", err)
	}
	if !slices.Equal(replaced.Options, []string{"nevím"}) {
		t.Errorf("replaced options = %q", replaced.Options)
	}
	if _, ok := changes.Map()["options"]; !ok {
		t.Error("a replaced option set is missing from the audit diff")
	}

	cleared, _, err := applyUpdate(cur, Update{Options: &[]string{}}, "us1", ref)
	if err != nil {
		t.Fatalf("clearing options: %v", err)
	}
	if cleared.Options == nil || len(cleared.Options) != 0 {
		t.Errorf("cleared options = %v, want an empty slice", cleared.Options)
	}

	if _, _, err := applyUpdate(cur, Update{Options: &[]string{"a", "b", "c", "d", "e", "f"}}, "us1", ref); !errors.Is(
		err, ErrTooManyOptions,
	) {
		t.Errorf("six options: error = %v, want ErrTooManyOptions", err)
	}
}

// TestDiff_optionsNilEqualsEmpty verifies a stored task from before the column
// (options nil) diffs as unchanged against a normalised empty set — otherwise
// every edit of an older task would record a phantom options change.
func TestDiff_optionsNilEqualsEmpty(t *testing.T) {
	t.Parallel()

	cur := stored()
	next := fields{
		Title: cur.Title, Body: cur.Body, State: cur.State, Resolution: cur.Resolution,
		Query: cur.Query, Options: []string{}, StateAt: cur.StateAt,
	}
	if got := diff(cur, next).Map(); got != nil {
		t.Errorf("diff = %v, want none", got)
	}
}
