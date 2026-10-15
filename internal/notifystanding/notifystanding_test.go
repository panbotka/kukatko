package notifystanding

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/notification"
)

// TestAddressee_Valid checks an addressee needs both an account and a project.
func TestAddressee_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    Addressee
		want bool
	}{
		{name: "both", a: Addressee{UserUID: "us1", ProjectUID: "pj1"}, want: true},
		{name: "no account", a: Addressee{ProjectUID: "pj1"}},
		{name: "blank account", a: Addressee{UserUID: " ", ProjectUID: "pj1"}},
		{name: "no project", a: Addressee{UserUID: "us1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.a.Valid(); got != tt.want {
				t.Errorf("Valid(%+v) = %v, want %v", tt.a, got, tt.want)
			}
		})
	}
}

// TestReachesMaintainers checks only the registration-waiting kind is
// addressed to maintainers who are no members.
func TestReachesMaintainers(t *testing.T) {
	t.Parallel()

	for _, kind := range notification.Kinds() {
		want := kind == notification.KindRegistrationPending
		if got := ReachesMaintainers(string(kind)); got != want {
			t.Errorf("ReachesMaintainers(%s) = %v, want %v", kind, got, want)
		}
	}
	if ReachesMaintainers("task_digest") || ReachesMaintainers("") {
		t.Error("a kind that is no notification kind reaches maintainers")
	}
}

// TestMemberSQL_namesItsArguments checks the fragment compares the given
// expressions and carries every part of the rule.
func TestMemberSQL_namesItsArguments(t *testing.T) {
	t.Parallel()

	got := MemberSQL("u.uid", "s.project_id")
	for _, want := range []string{
		"ns_m.user_uid = u.uid", "ns_m.project_uid = s.project_id",
		"ns_m.approved_at IS NOT NULL", "NOT ns_m.identity_withheld", "NOT ns_u.disabled", "ns_p.active",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("MemberSQL lacks %q:\n%s", want, got)
		}
	}
	if m := MaintainerSQL("$1", "$2"); !strings.Contains(m, "ns_u.role = 'maintainer'") ||
		!strings.Contains(m, "ns_p.uid = $2") || !strings.Contains(m, "ns_p.active") {
		t.Errorf("MaintainerSQL = %s", m)
	}
}

// fakeQuerier answers the standing query with ok, or fails with err, and
// records the query it was asked.
type fakeQuerier struct {
	ok    bool
	err   error
	query string
	args  []any
}

// QueryRow records the question and returns a row answering it.
func (f *fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.query, f.args = sql, args
	return fakeRow{ok: f.ok, err: f.err}
}

// fakeRow scans one boolean.
type fakeRow struct {
	ok  bool
	err error
}

// Scan fills the boolean destination or returns the configured failure.
func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.ok
	return nil
}

// TestInStanding_asksTheRightQuestion checks the member question for an
// ordinary kind, the member-or-maintainer question for the registration kind,
// no question at all for an incomplete addressee, and a failure passed on.
func TestInStanding_asksTheRightQuestion(t *testing.T) {
	t.Parallel()

	q := &fakeQuerier{ok: true}
	ok, err := InStanding(t.Context(), q, Addressee{UserUID: "us1", ProjectUID: "pj1", Kind: "tagged"})
	if err != nil || !ok || q.query != memberStandingSQL || q.args[0] != "us1" || q.args[1] != "pj1" {
		t.Fatalf("tagged: %v, %v, asked %q %v", ok, err, q.query, q.args)
	}
	q = &fakeQuerier{ok: true}
	if _, err := InStanding(t.Context(), q, Addressee{
		UserUID: "us1", ProjectUID: "pj1", Kind: string(notification.KindRegistrationPending),
	}); err != nil || q.query != maintainerStandingSQL {
		t.Fatalf("registration_pending: %v, asked %q", err, q.query)
	}
	q = &fakeQuerier{ok: true}
	if ok, err := InStanding(t.Context(), q, Addressee{UserUID: "us1"}); ok || err != nil || q.query != "" {
		t.Fatalf("incomplete addressee: %v, %v, asked %q; want no, no error, no question", ok, err, q.query)
	}
	boom := errors.New("db down")
	checker := NewChecker(&fakeQuerier{err: boom})
	if _, err := checker.InStanding(t.Context(), Addressee{UserUID: "us1", ProjectUID: "pj1"}); !errors.Is(err, boom) {
		t.Fatalf("failing query: %v, want %v", err, boom)
	}
}

// TestNewChecker_requiresADatabase checks the wiring bug panics.
func TestNewChecker_requiresADatabase(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewChecker(nil) did not panic")
		}
	}()
	NewChecker(nil)
}

// TestCountDrop_countsEveryPair checks every defined pair is reported, a drop
// raises its own pair only, and an undefined pair is ignored.
func TestCountDrop_countsEveryPair(t *testing.T) {
	t.Parallel()

	count := func(stage Stage, reason Reason) uint64 {
		for _, d := range Drops() {
			if d.Stage == stage && d.Reason == reason {
				return d.Count
			}
		}
		t.Fatalf("Drops lacks %s/%s", stage, reason)
		return 0
	}
	if n := len(Drops()); n != len(dropCells) {
		t.Fatalf("Drops reports %d pairs, want %d", n, len(dropCells))
	}
	before := count(StagePush, ReasonDeviceMoved)
	CountDrop(StagePush, ReasonDeviceMoved)
	CountDrop(StageDeliver, ReasonDeviceMoved) // undefined pair: ignored
	if after := count(StagePush, ReasonDeviceMoved); after < before+1 {
		t.Errorf("push/device_moved = %d after one drop, want at least %d", after, before+1)
	}
}
