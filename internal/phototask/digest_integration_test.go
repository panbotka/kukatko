//go:build integration

package phototask_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/phototask"
)

// makeAccount inserts an account that can sign in — approved, and disabled only
// when asked — so the digest query's account filter can be exercised both ways.
// The plain fixture seeds unapproved accounts, which the digest deliberately
// leaves out.
func (f *fixture) makeAccount(t *testing.T, uid, username string, disabled bool) string {
	t.Helper()
	approved := time.Now().Add(-time.Hour)
	if err := f.users.CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, Email: username + "@example.test",
		DisplayName: "Osoba " + username, PasswordHash: "x", Role: auth.RoleViewer,
		Disabled: disabled, ApprovedAt: &approved,
	}); err != nil {
		t.Fatalf("creating account %s: %v", username, err)
	}
	return uid
}

// digestFor returns the digest addressed to uid, or nil when uid gets none.
func digestFor(digests []phototask.Digest, uid string) *phototask.Digest {
	for i := range digests {
		if digests[i].UserUID == uid {
			return &digests[i]
		}
	}
	return nil
}

// digestUIDs lists who gets a digest, in the store's order.
func digestUIDs(digests []phototask.Digest) []string {
	out := make([]string, 0, len(digests))
	for _, d := range digests {
		out = append(out, d.UserUID)
	}
	return out
}

// expectDigests asserts exactly the given accounts get a digest.
func expectDigests(t *testing.T, label string, digests []phototask.Digest, want ...string) {
	t.Helper()
	got := digestUIDs(digests)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: digests for %v, want %v", label, got, want)
	}
}

// assign puts uid on the task as somebody asked outright.
func (f *fixture) assign(t *testing.T, taskUID, uid string) {
	t.Helper()
	if err := f.tasks.Assign(context.Background(), taskUID, uid, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign(%s): %v", uid, err)
	}
}

// TestDigests_whoGetsOne verifies the digest goes to exactly the people an open
// task waits on — a participant whose move it is, with an account that can sign
// in — and follows the conversation exactly as the listing's "waiting on me"
// does: a reply hands the wait to the other side, a close ends it.
func TestDigests_whoGetsOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// The fixture's actor opens every task; approve them so they can be waited on.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE users SET approved_at = now() WHERE uid = $1`, actor); err != nil {
		t.Fatalf("approving the actor: %v", err)
	}
	agent := f.makeAccount(t, "us-agent", "agent", false)
	person := f.makeAccount(t, "us-person", "person", false)
	blocked := f.makeAccount(t, "us-blocked", "blocked", true)
	pending := f.makeUser(t, "us-pending", "pending", "Nepotvrzený")

	task := f.mustCreate(t, "V kterém roce?")
	f.assign(t, task.UID, agent)
	f.assign(t, task.UID, person)
	f.assign(t, task.UID, blocked)
	f.assign(t, task.UID, pending)

	// Freshly asked: the opener acted last, so it waits on everybody else who can
	// sign in — not on the opener, not on a disabled or unapproved account.
	digests, err := f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests: %v", err)
	}
	expectDigests(t, "asked", digests, agent, person)
	got := digestFor(digests, person)
	if got.Email != "person@example.test" || got.DisplayName != "Osoba person" || got.DigestedAt != nil {
		t.Errorf("person's digest = %+v, want the address, the name and no stamp", *got)
	}
	if got.Total != 1 || len(got.Tasks) != 1 || got.Tasks[0].UID != task.UID ||
		got.Tasks[0].Title != "V kterém roce?" || got.Tasks[0].State != phototask.StateQuestion {
		t.Errorf("person's digest tasks = %+v (total %d), want just the question", got.Tasks, got.Total)
	}
	if !got.NewestActivityAt.Equal(got.Tasks[0].LastActivityAt) {
		t.Errorf("newest activity %v, want the one task's %v", got.NewestActivityAt, got.Tasks[0].LastActivityAt)
	}

	// The agent replies: the wait moves to the person and the opener, and the
	// agent — who acted last — drops out.
	f.comment(t, task.UID, agent, "Podle EXIFu 1987, souhlasí?")
	digests, err = f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests after the reply: %v", err)
	}
	expectDigests(t, "agent replied", digests, actor, person)

	// Closed: nobody is waited on, whatever is written under it afterwards.
	f.move(t, task.UID, agent, phototask.StateDone, "Opraveno.")
	f.comment(t, task.UID, agent, "Díky!")
	digests, err = f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests after the close: %v", err)
	}
	expectDigests(t, "closed", digests)
}

// TestDigests_onlyWhenSomethingChanged verifies the stamp: once a person's
// digest is marked, they get another only when a waiting task has moved past
// the stamp — a queue that has not changed since yesterday sends nothing today.
func TestDigests_onlyWhenSomethingChanged(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	agent := f.makeAccount(t, "us-agent", "agent", false)
	person := f.makeAccount(t, "us-person", "person", false)

	task := f.mustCreate(t, "Kdo je na fotce?")
	f.assign(t, task.UID, agent)
	f.assign(t, task.UID, person)

	digests, err := f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests: %v", err)
	}
	expectDigests(t, "unstamped", digests, agent, person)

	// Stamp the person at the last activity (the comparison is strict, so
	// "nothing newer than the stamp" includes the activity it was taken from):
	// nothing new for them, the agent (never stamped) is still due.
	stamp := digestFor(digests, person).NewestActivityAt
	if err := f.tasks.MarkDigested(ctx, person, stamp); err != nil {
		t.Fatalf("MarkDigested: %v", err)
	}
	digests, err = f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests after the stamp: %v", err)
	}
	expectDigests(t, "person stamped", digests, agent)

	// A stamp older than the activity is not "nothing new": the digest reports
	// it, and carries the stamp it was compared with.
	if err := f.tasks.MarkDigested(ctx, person, stamp.Add(-time.Hour)); err != nil {
		t.Fatalf("MarkDigested (older): %v", err)
	}
	digests, err = f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests after the older stamp: %v", err)
	}
	expectDigests(t, "older stamp", digests, agent, person)
	if got := digestFor(digests, person); got.DigestedAt == nil || !got.DigestedAt.Equal(stamp.Add(-time.Hour)) {
		t.Errorf("person's DigestedAt = %v, want the stamp", got.DigestedAt)
	}

	// Stamp both past the activity, then let the opener add a second task: only
	// the person on it hears about it, and the digest lists both tasks — the
	// whole queue, not just what is new.
	for _, uid := range []string{agent, person} {
		if err := f.tasks.MarkDigested(ctx, uid, stamp); err != nil {
			t.Fatalf("MarkDigested(%s): %v", uid, err)
		}
	}
	settle()
	second := f.mustCreate(t, "Kde to je?")
	f.assign(t, second.UID, person)
	digests, err = f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests after the second task: %v", err)
	}
	expectDigests(t, "second task", digests, person)
	got := digestFor(digests, person)
	if got.Total != 2 || len(got.Tasks) != 2 || got.Tasks[0].UID != second.UID || got.Tasks[1].UID != task.UID {
		t.Errorf("person's digest tasks = %+v, want both, newest first", got.Tasks)
	}

	// Stamping an unknown account is not an error: it may have been deleted
	// between the read and the stamp.
	if err := f.tasks.MarkDigested(ctx, "us-gone", stamp); err != nil {
		t.Errorf("MarkDigested(unknown) = %v, want nil", err)
	}
}

// TestDigests_listIsCapped verifies a person with more waiting tasks than
// DigestLimit gets the newest ones and the true total.
func TestDigests_listIsCapped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	person := f.makeAccount(t, "us-person", "person", false)

	const extra = 3
	var newest string
	for i := range phototask.DigestLimit + extra {
		task := f.mustCreate(t, fmt.Sprintf("Otázka %d", i))
		f.assign(t, task.UID, person)
		newest = task.UID
	}

	digests, err := f.tasks.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests: %v", err)
	}
	expectDigests(t, "capped", digests, person)
	got := digestFor(digests, person)
	if got.Total != phototask.DigestLimit+extra || len(got.Tasks) != phototask.DigestLimit {
		t.Fatalf("digest lists %d of %d, want %d of %d",
			len(got.Tasks), got.Total, phototask.DigestLimit, phototask.DigestLimit+extra)
	}
	if got.Tasks[0].UID != newest {
		t.Errorf("first listed task = %s, want the newest %s", got.Tasks[0].UID, newest)
	}
	for i := 1; i < len(got.Tasks); i++ {
		if got.Tasks[i].LastActivityAt.After(got.Tasks[i-1].LastActivityAt) {
			t.Errorf("task %d is newer than task %d; want newest first", i, i-1)
		}
	}
}
