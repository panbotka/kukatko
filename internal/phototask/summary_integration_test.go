//go:build integration

package phototask_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/phototask"
)

// TestSummary_countsTheQueuePerCaller builds a small queue and reads it as the
// person and as the agent: the per-state counts are the same for both, the
// waiting and answered counts are each reader's own, and a closed task never
// waits on anybody.
func TestSummary_countsTheQueuePerCaller(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.makeUser(t, agentUID, "panbotka-agent", "Agent")

	// Asked and untouched: waits on the agent.
	asked := f.mustCreate(t, "V kterém roce?")
	if err := f.tasks.Assign(ctx, asked.UID, agentUID, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	// The agent asked back: waits on the person, and is an answer for them.
	replied := f.mustCreate(t, "Kdo je vlevo?")
	f.comment(t, replied.UID, agentUID, "Není to Anna?")
	// In review by the agent, the person on it: waits on the person.
	review := f.mustCreate(t, "Schválit rok 1987?")
	f.move(t, review.UID, agentUID, phototask.StateReview, "")
	// Closed, with a late reply: nobody's move, and not "answered" either —
	// the chip opens the open listing.
	done := f.mustCreate(t, "Hotová věc")
	f.move(t, done.UID, agentUID, phototask.StateDone, "Opraveno.")
	f.comment(t, done.UID, agentUID, "Díky.")

	person, err := f.tasks.Summary(ctx, actor)
	if err != nil {
		t.Fatalf("Summary(person): %v", err)
	}
	agent, err := f.tasks.Summary(ctx, agentUID)
	if err != nil {
		t.Fatalf("Summary(agent): %v", err)
	}

	wantByState := map[phototask.State]int{
		phototask.StateQuestion: 2, phototask.StateWorking: 0, phototask.StateReview: 1,
		phototask.StateDone: 1, phototask.StateRejected: 0,
	}
	for _, got := range []phototask.Summary{person, agent} {
		for state, want := range wantByState {
			if got.ByState[state] != want {
				t.Errorf("by_state[%s] = %d, want %d", state, got.ByState[state], want)
			}
		}
		if got.Open != 3 {
			t.Errorf("open = %d, want 3", got.Open)
		}
	}
	if person.WaitingOnMe != 2 {
		t.Errorf("person: waiting_on_me = %d, want 2 (the reply and the review)", person.WaitingOnMe)
	}
	if person.Answered != 1 {
		t.Errorf("person: answered = %d, want 1 (the agent's reply; the closed thanks does not count)",
			person.Answered)
	}
	if agent.WaitingOnMe != 1 {
		t.Errorf("agent: waiting_on_me = %d, want 1 (the untouched question)", agent.WaitingOnMe)
	}
	if agent.Answered != 0 {
		t.Errorf("agent: answered = %d, want 0 — its own words are not answers", agent.Answered)
	}

	// The badge count is the same number under its own name.
	waiting, err := f.tasks.WaitingOnMe(ctx, actor)
	if err != nil {
		t.Fatalf("WaitingOnMe: %v", err)
	}
	if waiting != person.WaitingOnMe {
		t.Errorf("WaitingOnMe = %d, want the summary's %d", waiting, person.WaitingOnMe)
	}

	// A reader on nothing: every state still named, nothing waits on them, and
	// — the convention every read shares — every comment is somebody else's.
	stranger, err := f.tasks.Summary(ctx, "us-nobody")
	if err != nil {
		t.Fatalf("Summary(stranger): %v", err)
	}
	if len(stranger.ByState) != len(phototask.States) || stranger.WaitingOnMe != 0 || stranger.Answered != 1 {
		t.Errorf("stranger's summary = %+v, want every state named, nothing waiting, one answered", stranger)
	}
}
