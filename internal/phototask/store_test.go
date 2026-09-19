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

	// The caller is always the first argument, bound by the projection whether
	// or not a clause needs it — so even the zero filter carries one.
	if where, args := (Filter{}).conditions(); len(where) != 0 || len(args) != 1 {
		t.Errorf("the zero filter compiled to %v / %v, want no clauses and just the caller", where, args)
	}

	where, args := Filter{
		CallerUID: "us-me", States: []State{StateQuestion, StateReview}, Answered: true, Waiting: true,
		Search: "dům", PhotoUID: "ph1",
	}.conditions()
	joined := strings.Join(where, " AND ")
	for _, want := range []string{
		"t.state = ANY($2)",
		// Both caller-relative filters are the very fragments the projection
		// computes the flags from, so a badge and a listing cannot disagree.
		answeredSQL,
		waitingSQL,
		// Both sides pass through immutable_unaccent, so "dum" finds "dům" —
		// the same shape every other text search in the library uses.
		"immutable_unaccent(t.title) ILIKE immutable_unaccent($3)",
		"immutable_unaccent(t.body) ILIKE immutable_unaccent($3)",
		"tp.photo_uid = $4",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("clauses %q do not contain %q", joined, want)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args = %v, want four (caller, states, pattern, photo)", args)
	}
	if got, ok := args[0].(string); !ok || got != "us-me" {
		t.Errorf("first argument = %v, want the caller", args[0])
	}
	if got, ok := args[2].(string); !ok || got != "%dům%" {
		t.Errorf("search argument = %v, want the wrapped pattern", args[2])
	}
}

// TestCallerRelativeSQL pins the shape of the two caller-relative fragments:
// they read the caller from the first parameter only, the reply comparison
// ignores the caller's own comments (that is th.last_reply_at, not
// th.last_comment_at), and a closed task can never be waiting on anybody.
func TestCallerRelativeSQL(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{answeredSQL, waitingSQL, taskJoins, taskColumns} {
		if strings.Contains(fragment, "$2") {
			t.Errorf("fragment binds a second parameter, the reads own only the caller:\n%s", fragment)
		}
	}
	if !strings.Contains(answeredSQL, "th.last_reply_at > t.state_at") ||
		strings.Contains(answeredSQL, "last_comment_at") {
		t.Errorf("answeredSQL = %q, want it measured on the newest reply by somebody else", answeredSQL)
	}
	if !strings.Contains(waitingSQL, "t.state NOT IN ('done', 'rejected')") ||
		!strings.Contains(waitingSQL, "la.by IS DISTINCT FROM "+callerParam) {
		t.Errorf("waitingSQL = %q, want open + last actor not the caller", waitingSQL)
	}
	if !strings.Contains(taskJoins, "c.author_uid IS DISTINCT FROM "+callerParam+") AS last_reply_at") {
		t.Errorf("the thread summary does not exclude the caller's own comments:\n%s", taskJoins)
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
func TestScanTaskReadsFlags(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 19, 17, 21, 0, 0, time.UTC)
	got, err := scanTask(fakeRow{
		stateAt: at.Add(-time.Hour), hasNewAnswer: true, waiting: true,
		lastActivityAt: at, lastActivityBy: "us2", lastActivityByName: "Tomáš Kozák",
	})
	if err != nil {
		t.Fatalf("scanTask: %v", err)
	}
	if !got.HasNewAnswer || !got.WaitingOnMe {
		t.Errorf("flags = answer %v / waiting %v, want both as the row said", got.HasNewAnswer, got.WaitingOnMe)
	}
	if !got.LastActivityAt.Equal(at) || got.LastActivityByUID != "us2" || got.LastActivityByName != "Tomáš Kozák" {
		t.Errorf("last activity = %v by %q (%q), want the row's", got.LastActivityAt,
			got.LastActivityByUID, got.LastActivityByName)
	}

	quiet, err := scanTask(fakeRow{stateAt: at})
	if err != nil {
		t.Fatalf("scanTask(quiet): %v", err)
	}
	if quiet.HasNewAnswer || quiet.WaitingOnMe {
		t.Errorf("a row saying neither read as answer %v / waiting %v", quiet.HasNewAnswer, quiet.WaitingOnMe)
	}
}

// fakeRow feeds scanTask the columns the tests care about — the state stamp,
// the two flags the query computes, the last activity and the participants
// aggregate — leaving every other column at its zero value.
type fakeRow struct {
	stateAt            time.Time
	hasNewAnswer       bool
	waiting            bool
	lastActivityAt     time.Time
	lastActivityBy     string
	lastActivityByName string
	// participants is the raw JSON the query's aggregate produces; nil stands
	// for an absent value, which a read must survive.
	participants []byte
}

// Scan fills the destinations in taskColumns order.
func (f fakeRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = "tk1"
	*(dest[1].(*string)) = "Kdy?"
	*(dest[3].(*State)) = StateQuestion
	*(dest[10].(*time.Time)) = f.stateAt
	*(dest[19].(*bool)) = f.hasNewAnswer
	*(dest[20].(*time.Time)) = f.lastActivityAt
	*(dest[21].(*string)) = f.lastActivityBy
	*(dest[22].(*string)) = f.lastActivityByName
	*(dest[23].(*bool)) = f.waiting
	*(dest[24].(*[]byte)) = f.participants
	return nil
}

// TestScanTaskParticipants verifies the aggregate is decoded, and that a task
// with nobody on it — or a row where the aggregate is absent altogether — reads
// as an empty list rather than failing.
func TestScanTaskParticipants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want []Participant
	}{
		{name: "absent", raw: nil, want: []Participant{}},
		{name: "empty aggregate", raw: []byte(`[]`), want: []Participant{}},
		{
			name: "one person, joined by acting",
			raw:  []byte(`[{"user_uid":"u1","name":"Anna","added_by":""}]`),
			want: []Participant{{UserUID: "u1", Name: "Anna"}},
		},
		{
			name: "one person, put there by somebody",
			raw:  []byte(`[{"user_uid":"u1","name":"Anna","added_by":"u2","added_by_name":"Bob"}]`),
			want: []Participant{{UserUID: "u1", Name: "Anna", AddedByUID: "u2", AddedByName: "Bob"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := scanTask(fakeRow{participants: tt.raw})
			if err != nil {
				t.Fatalf("scanTask: %v", err)
			}
			if len(got.Participants) != len(tt.want) {
				t.Fatalf("Participants = %v, want %v", got.Participants, tt.want)
			}
			for i, want := range tt.want {
				if got.Participants[i] != want {
					t.Errorf("Participants[%d] = %+v, want %+v", i, got.Participants[i], want)
				}
			}
		})
	}
}
