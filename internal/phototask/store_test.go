package phototask

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
)

// TestFilterEffectiveStates verifies an explicit choice of states always wins
// over the "open" shorthand, and that the zero filter selects everything.
func TestFilterEffectiveStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter Filter
		want   []State
	}{
		{name: "zero filter selects every state", filter: Filter{}, want: nil},
		{name: "open shorthand", filter: Filter{Open: true}, want: OpenStates},
		{
			name:   "explicit states win over the shorthand",
			filter: Filter{Open: true, States: []State{StateDone}},
			want:   []State{StateDone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.filter.effectiveStates()
			if len(got) != len(tt.want) {
				t.Fatalf("effectiveStates() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("effectiveStates() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestFilterPage verifies the paging defaults and bounds.
func TestFilterPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     Filter
		wantLimit  int
		wantOffset int
	}{
		{name: "defaults", filter: Filter{}, wantLimit: DefaultLimit},
		{name: "explicit", filter: Filter{Limit: 10, Offset: 20}, wantLimit: 10, wantOffset: 20},
		{name: "clamped to the maximum", filter: Filter{Limit: MaxLimit + 1}, wantLimit: MaxLimit},
		{name: "negative offset floors at zero", filter: Filter{Offset: -5}, wantLimit: DefaultLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limit, offset := tt.filter.Page()
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Errorf("Page() = %d, %d, want %d, %d", limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

// TestFilterConditions verifies each filter compiles to its own clause, that the
// placeholders are numbered in the order the arguments are collected, and that
// the zero filter adds nothing.
func TestFilterConditions(t *testing.T) {
	t.Parallel()

	if where, args := (Filter{}).conditions(); len(where) != 0 || len(args) != 0 {
		t.Errorf("the zero filter compiled to %v / %v, want nothing", where, args)
	}

	where, args := Filter{
		States: []State{StateQuestion, StateReview}, Answered: true,
		Search: "dům", PhotoUID: "ph1",
	}.conditions()
	joined := strings.Join(where, " AND ")
	for _, want := range []string{
		"t.state = ANY($1)",
		"th.last_comment_at > t.state_at",
		"t.title ILIKE $2",
		"t.body ILIKE $2",
		"tp.photo_uid = $3",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("clauses %q do not contain %q", joined, want)
		}
	}
	if len(args) != 3 {
		t.Fatalf("args = %v, want three (states, pattern, photo)", args)
	}
	if got, ok := args[1].(string); !ok || got != "%dům%" {
		t.Errorf("search argument = %v, want the wrapped pattern", args[1])
	}
}

// TestLikeEscape verifies a search term's LIKE wildcards are neutralised, so a
// question mentioning a per-cent sign is searched for rather than matching all.
func TestLikeEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "dům", want: "dům"},
		{in: "50 %", want: `50 \%`},
		{in: "a_b", want: `a\_b`},
		{in: `back\slash`, want: `back\\slash`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := likeEscape(tt.in); got != tt.want {
				t.Errorf("likeEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestStateStringsAndWhereSQL verifies the two small SQL helpers.
func TestStateStringsAndWhereSQL(t *testing.T) {
	t.Parallel()

	got := stateStrings([]State{StateQuestion, StateDone})
	if len(got) != 2 || got[0] != "question" || got[1] != "done" {
		t.Errorf("stateStrings = %v", got)
	}
	if whereSQL(nil) != "" {
		t.Errorf("whereSQL(nil) = %q, want an empty string", whereSQL(nil))
	}
	if !strings.HasPrefix(strings.TrimSpace(whereSQL([]string{"a", "b"})), "WHERE a") {
		t.Errorf("whereSQL = %q", whereSQL([]string{"a", "b"}))
	}
	if !strings.Contains(whereSQL([]string{"a", "b"}), "AND b") {
		t.Errorf("whereSQL does not AND the clauses: %q", whereSQL([]string{"a", "b"}))
	}
}

// TestWithTarget verifies the audit entry is pointed at the task, that a target
// the caller chose is respected, and that the caller's details map is copied
// rather than written into.
func TestWithTarget(t *testing.T) {
	t.Parallel()

	callers := map[string]any{"via": "ctl"}
	entry := withTarget(audit.Entry{Details: callers}, "tk1", "photo_uids", []string{"ph1"})
	if entry.TargetType != "photo_tasks" || entry.TargetUID != "tk1" {
		t.Errorf("target = %s/%s, want photo_tasks/tk1", entry.TargetType, entry.TargetUID)
	}
	if _, ok := callers["photo_uids"]; ok {
		t.Error("withTarget wrote into the caller's details map")
	}
	if entry.Details["via"] != "ctl" {
		t.Error("withTarget dropped the caller's own details")
	}

	kept := withTarget(audit.Entry{TargetType: "photos", TargetUID: "ph9"}, "tk1", "", nil)
	if kept.TargetType != "photos" || kept.TargetUID != "ph9" {
		t.Errorf("withTarget overwrote a target the caller chose: %s/%s", kept.TargetType, kept.TargetUID)
	}
}

// TestWithChanges verifies the field diff reaches the entry's details under the
// shared changes key, and that an edit changing nothing records no diff.
func TestWithChanges(t *testing.T) {
	t.Parallel()

	changes := audit.NewChangeSet()
	changes.Add("state", "question", "working")
	entry := withChanges(audit.Entry{}, "tk1", changes)
	if _, ok := entry.Details[audit.ChangesKey]; !ok {
		t.Errorf("details = %v, want a %q key", entry.Details, audit.ChangesKey)
	}
	if empty := withChanges(audit.Entry{}, "tk1", audit.NewChangeSet()); empty.Details[audit.ChangesKey] != nil {
		t.Errorf("a no-op edit recorded a diff: %v", empty.Details)
	}
}

// fkError builds a foreign-key violation naming the given constraint, as pgx
// surfaces one.
func fkError(constraint string) error {
	return &pgconn.PgError{Code: foreignKeyViolation, ConstraintName: constraint}
}

// TestTranslate verifies the error classification: the package's own sentinels
// pass through untouched, a foreign key names which side is missing, and
// anything else keeps its cause with the operation for context.
func TestTranslate(t *testing.T) {
	t.Parallel()

	other := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "sentinel passes through", err: ErrClosedNeedsResolution, want: ErrClosedNeedsResolution},
		{name: "wrapped sentinel passes through", err: fmtWrap(ErrTooManyPhotos), want: ErrTooManyPhotos},
		{name: "photo foreign key", err: fkError("photo_task_photos_photo_uid_fkey"), want: ErrPhotoNotFound},
		{name: "task foreign key", err: fkError("photo_task_photos_task_uid_fkey"), want: ErrNotFound},
		{name: "no rows is not special here", err: pgx.ErrNoRows, want: nil},
		{name: "anything else is wrapped", err: other, want: other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translate(tt.err, "testing")
			if tt.want != nil {
				if !errors.Is(got, tt.want) {
					t.Fatalf("translate(%v) = %v, want %v", tt.err, got, tt.want)
				}
				return
			}
			if !strings.Contains(got.Error(), "testing") {
				t.Errorf("wrapped error %q does not mention the operation", got)
			}
		})
	}
}

// fmtWrap wraps err the way the store's own helpers do, to prove a sentinel
// survives being given context.
func fmtWrap(err error) error {
	return errors.Join(err, errors.New("with context"))
}

// TestScanTaskDerivesNewAnswer verifies the derived flag: a reply newer than the
// last state move is a new answer, one older than it is not, and a thread nobody
// has written in never is.
func TestScanTaskDerivesNewAnswer(t *testing.T) {
	t.Parallel()

	stateAt := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	after := stateAt.Add(time.Hour)
	before := stateAt.Add(-time.Hour)

	tests := []struct {
		name          string
		lastCommentAt *time.Time
		want          bool
	}{
		{name: "no comments", lastCommentAt: nil, want: false},
		{name: "reply after the state moved", lastCommentAt: &after, want: true},
		{name: "reply before the state moved", lastCommentAt: &before, want: false},
		{name: "reply at the same instant", lastCommentAt: &stateAt, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := scanTask(fakeRow{stateAt: stateAt, lastCommentAt: tt.lastCommentAt})
			if err != nil {
				t.Fatalf("scanTask: %v", err)
			}
			if got.HasNewAnswer != tt.want {
				t.Errorf("HasNewAnswer = %v, want %v", got.HasNewAnswer, tt.want)
			}
		})
	}
}

// fakeRow feeds scanTask the two timestamps the derived flag is computed from,
// leaving every other column at its zero value.
type fakeRow struct {
	stateAt       time.Time
	lastCommentAt *time.Time
}

// Scan fills the destinations in taskColumns order.
func (f fakeRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = "tk1"
	*(dest[1].(*string)) = "Kdy?"
	*(dest[3].(*State)) = StateQuestion
	*(dest[10].(*time.Time)) = f.stateAt
	*(dest[17].(**time.Time)) = f.lastCommentAt
	return nil
}
