//go:build integration

package phototask_test

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// These tests cover "whose turn is it": the caller-relative has_new_answer, the
// last activity and waiting_on_me. The scenario throughout is the one the queue
// exists for — a person (actor, the fixture's default account) and an agent
// (agentUID) taking turns on one question.

// agentUID is the second account in these tests, standing in for the agent that
// works through `kukatko ctl`.
const agentUID = "us-agent"

// settle lets the clock move between two acts, so the database's now() of a
// comment and the Go-side now of a state change never land on the same instant.
func settle() { time.Sleep(10 * time.Millisecond) }

// comment writes body on the task as author and joins them to it, the way the
// HTTP layer does after a comment lands.
func (f *fixture) comment(t *testing.T, taskUID, author, body string) comments.Comment {
	t.Helper()
	settle()
	c, err := f.comments.Create(context.Background(), comments.TaskSubject(taskUID), author, body,
		audit.Entry{ActorUID: author, Action: audit.ActionCommentCreate,
			TargetType: "photo_tasks", TargetUID: taskUID})
	if err != nil {
		t.Fatalf("commenting as %s: %v", author, err)
	}
	if err := f.tasks.Join(context.Background(), taskUID, author); err != nil {
		t.Fatalf("Join(%s): %v", author, err)
	}
	return c
}

// read returns the task as caller sees it.
func (f *fixture) read(t *testing.T, taskUID, caller string) phototask.Task {
	t.Helper()
	task, err := f.tasks.Get(context.Background(), taskUID, caller)
	if err != nil {
		t.Fatalf("Get(%s as %s): %v", taskUID, caller, err)
	}
	return task
}

// move advances the task's state as actorUID.
func (f *fixture) move(t *testing.T, taskUID, actorUID string, state phototask.State, resolution string) {
	t.Helper()
	settle()
	upd := phototask.Update{State: &state}
	if resolution != "" {
		upd.Resolution = &resolution
	}
	if _, err := f.tasks.Update(context.Background(), taskUID, upd,
		audit.Entry{ActorUID: actorUID, Action: audit.ActionTaskUpdate}); err != nil {
		t.Fatalf("moving to %s as %s: %v", state, actorUID, err)
	}
}

// expectTurn asserts the two caller-relative flags for one reader.
func expectTurn(t *testing.T, label string, task phototask.Task, wantAnswer, wantWaiting bool) {
	t.Helper()
	if task.HasNewAnswer != wantAnswer {
		t.Errorf("%s: has_new_answer = %v, want %v", label, task.HasNewAnswer, wantAnswer)
	}
	if task.WaitingOnMe != wantWaiting {
		t.Errorf("%s: waiting_on_me = %v, want %v", label, task.WaitingOnMe, wantWaiting)
	}
}

// TestHasNewAnswer_isCallerRelative verifies the flag ignores the reader's own
// comments, lights up for everybody else's, is cleared by a state change, and
// never counts a soft-deleted comment.
func TestHasNewAnswer_isCallerRelative(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.makeUser(t, agentUID, "panbotka-agent", "Agent")
	task := f.mustCreate(t, "V kterém roce?")

	// The author adds context to their own task: no answer for them, an answer
	// for the agent who reads it.
	f.comment(t, task.UID, actor, "Doplňuji kontext.")
	if got := f.read(t, task.UID, actor); got.HasNewAnswer {
		t.Error("the author's own comment flagged their task as answered")
	}
	if got := f.read(t, task.UID, agentUID); !got.HasNewAnswer {
		t.Error("somebody else's comment did not flag the task for the agent")
	}

	// The same semantic in the listing filter.
	forAuthor, _, err := f.tasks.List(ctx, phototask.Filter{Answered: true, CallerUID: actor})
	if err != nil {
		t.Fatalf("List(answered as author): %v", err)
	}
	forAgent, _, err := f.tasks.List(ctx, phototask.Filter{Answered: true, CallerUID: agentUID})
	if err != nil {
		t.Fatalf("List(answered as agent): %v", err)
	}
	if len(forAuthor) != 0 || len(forAgent) != 1 {
		t.Errorf("answered listing = %d for the author / %d for the agent, want 0 / 1",
			len(forAuthor), len(forAgent))
	}

	// The agent moves the state: the reply is now older than the move.
	f.move(t, task.UID, agentUID, phototask.StateWorking, "")
	if got := f.read(t, task.UID, agentUID); got.HasNewAnswer {
		t.Error("a state change did not clear the flag for the mover")
	}

	// A reply that is then deleted never happened, for either side.
	late := f.comment(t, task.UID, actor, "Vlastně 1988.")
	if got := f.read(t, task.UID, agentUID); !got.HasNewAnswer {
		t.Fatal("a fresh reply did not flag the task")
	}
	if err := f.comments.Delete(ctx, late.UID,
		audit.Entry{ActorUID: actor, Action: audit.ActionCommentDelete}); err != nil {
		t.Fatalf("deleting the comment: %v", err)
	}
	if got := f.read(t, task.UID, agentUID); got.HasNewAnswer {
		t.Error("a soft-deleted comment still counts as an answer")
	}
	if got := f.read(t, task.UID, agentUID); got.LastActivityByUID != agentUID {
		t.Errorf("last activity by %q after the reply was deleted, want the agent's move", got.LastActivityByUID)
	}
}

// TestWaitingOnMe_followsTheConversation walks the loop the queue is for: the
// person opens a question and asks the agent, the agent comments, the person
// replies, the agent closes — and at every step the move belongs to exactly one
// of them, to nobody once it is closed, and never to somebody who is not on it.
func TestWaitingOnMe_followsTheConversation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.makeUser(t, agentUID, "panbotka-agent", "Agent")
	bystander := f.makeUser(t, "us-bystander", "kolemjdouci", "Kolemjdoucí")
	task := f.mustCreate(t, "V kterém roce?")

	// Just opened: the opener is alone on it, so nobody is waited on yet.
	expectTurn(t, "fresh, opener", f.read(t, task.UID, actor), false, false)

	// Asked and untouched: waits on the agent, not on the person who asked.
	if err := f.tasks.Assign(ctx, task.UID, agentUID, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	asked := f.read(t, task.UID, agentUID)
	expectTurn(t, "asked, agent", asked, false, true)
	expectTurn(t, "asked, person", f.read(t, task.UID, actor), false, false)
	expectTurn(t, "asked, bystander", f.read(t, task.UID, bystander), false, false)
	if asked.LastActivityByUID != actor || asked.LastActivityByName != "Pan Botka" {
		t.Errorf("last activity = %q (%q), want the opener", asked.LastActivityByUID, asked.LastActivityByName)
	}
	if !asked.LastActivityAt.Equal(asked.CreatedAt) && !asked.LastActivityAt.Equal(asked.StateAt) {
		t.Errorf("last activity at %v, want the opening (%v / %v)", asked.LastActivityAt, asked.CreatedAt, asked.StateAt)
	}

	// The agent comments: now it waits on the person, and the agent sees no
	// answer in its own words.
	f.comment(t, task.UID, agentUID, "Podle EXIFu je to 1987, souhlasí?")
	expectTurn(t, "agent commented, agent", f.read(t, task.UID, agentUID), false, false)
	expectTurn(t, "agent commented, person", f.read(t, task.UID, actor), true, true)

	// The person replies: back to the agent.
	f.comment(t, task.UID, actor, "Ano, 1987.")
	replied := f.read(t, task.UID, agentUID)
	expectTurn(t, "person replied, agent", replied, true, true)
	// Replying does not clear one's own flag — the agent's question is still
	// newer than the last state change, and only a move resets that. What the
	// reply does settle is the finer signal: it is no longer the person's move.
	expectTurn(t, "person replied, person", f.read(t, task.UID, actor), true, false)
	if replied.LastActivityByUID != actor {
		t.Errorf("last activity by %q after the reply, want the person", replied.LastActivityByUID)
	}

	// The waiting filter is the same predicate, evaluated server-side.
	waiting, total, err := f.tasks.List(ctx, phototask.Filter{Waiting: true, CallerUID: agentUID})
	if err != nil {
		t.Fatalf("List(waiting as agent): %v", err)
	}
	if total != 1 || len(waiting) != 1 || waiting[0].UID != task.UID || !waiting[0].WaitingOnMe {
		t.Errorf("waiting listing for the agent = %v (total %d), want just this task", uids(waiting), total)
	}
	none, _, err := f.tasks.List(ctx, phototask.Filter{Waiting: true, CallerUID: actor})
	if err != nil {
		t.Fatalf("List(waiting as person): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("waiting listing for the person = %v, want none", uids(none))
	}

	// Closed: nobody's move, even after a late comment — which is still a new
	// answer worth seeing.
	f.move(t, task.UID, agentUID, phototask.StateDone, "Opraveno na 1987.")
	expectTurn(t, "closed, person", f.read(t, task.UID, actor), false, false)
	expectTurn(t, "closed, agent", f.read(t, task.UID, agentUID), false, false)
	f.comment(t, task.UID, actor, "Díky!")
	expectTurn(t, "late comment, agent", f.read(t, task.UID, agentUID), true, false)
	closed := f.read(t, task.UID, actor)
	if closed.StateByUID != agentUID || closed.LastActivityByUID != actor {
		t.Errorf("state_by = %q, last activity by %q; want the agent's close and the person's thanks",
			closed.StateByUID, closed.LastActivityByUID)
	}
}

// TestStateBy_legacyRowReadsAsCreator verifies a task from before migration
// 0082 — state_by NULL — attributes its state to the creator, so the flags stay
// meaningful for the rows that predate the column.
func TestStateBy_legacyRowReadsAsCreator(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.makeUser(t, agentUID, "panbotka-agent", "Agent")
	task := f.mustCreate(t, "Stará otázka")
	if _, err := f.db.Pool().Exec(ctx, "UPDATE photo_tasks SET state_by = NULL WHERE uid = $1", task.UID); err != nil {
		t.Fatalf("clearing state_by: %v", err)
	}
	if err := f.tasks.Assign(ctx, task.UID, agentUID, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	legacy := f.read(t, task.UID, agentUID)
	if legacy.StateByUID != "" || legacy.LastActivityByUID != actor {
		t.Errorf("legacy row: state_by = %q, last activity by %q; want empty and the creator",
			legacy.StateByUID, legacy.LastActivityByUID)
	}
	expectTurn(t, "legacy, agent", legacy, false, true)
}
