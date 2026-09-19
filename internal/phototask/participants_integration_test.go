//go:build integration

package phototask_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// names reduces a participant slice to its user UIDs, for readable failures.
func names(ps []phototask.Participant) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.UserUID)
	}
	return out
}

// TestParticipants_actingJoins verifies the rule the table exists for: the
// people on a task are the people who have done something to it, without anybody
// having to say so.
func TestParticipants_actingJoins(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	photo := f.makePhoto(t, "a")

	// Opening a question puts its author on it.
	task := f.mustCreate(t, "Kdy to bylo?", photo.UID)
	people, err := f.tasks.Participants(ctx, task.UID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(people) != 1 || people[0].UserUID != actor {
		t.Fatalf("after create, participants = %v, want just the author", names(people))
	}
	if people[0].AddedByUID != "" {
		t.Errorf("the author was recorded as added by %q, want joined by acting", people[0].AddedByUID)
	}

	// A second person moving it along joins by that act alone.
	second := f.makeUser(t, "us-second", "pametnik", "Pamětník")
	if _, err := f.tasks.Update(ctx, task.UID,
		phototask.Update{State: statePtr(phototask.StateWorking)},
		audit.Entry{ActorUID: second, Action: audit.ActionTaskUpdate}); err != nil {
		t.Fatalf("advancing: %v", err)
	}
	people, err = f.tasks.Participants(ctx, task.UID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("after an edit by somebody else, participants = %v, want two", names(people))
	}

	// Acting again changes nothing: the join is idempotent.
	if _, err := f.tasks.AddPhotos(ctx, task.UID, []string{f.makePhoto(t, "b").UID},
		audit.Entry{ActorUID: second, Action: audit.ActionTaskAddPhotos}); err != nil {
		t.Fatalf("adding photos: %v", err)
	}
	people, _ = f.tasks.Participants(ctx, task.UID)
	if len(people) != 2 {
		t.Errorf("acting twice produced %v, want the same two people", names(people))
	}
}

// TestParticipants_assignAndRemove verifies the explicit half: somebody can be
// put on a task before they have done anything, the record says who put them
// there, and they can be taken off again.
func TestParticipants_assignAndRemove(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "Kdo je to?")
	asked := f.makeUser(t, "us-asked", "teta", "Teta")

	if err := f.tasks.Assign(ctx, task.UID, asked, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	people, err := f.tasks.Participants(ctx, task.UID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	var found *phototask.Participant
	for i := range people {
		if people[i].UserUID == asked {
			found = &people[i]
		}
	}
	if found == nil {
		t.Fatalf("participants = %v, want the asked person among them", names(people))
	}
	// The difference the column exists for: this one was asked, by the actor.
	if found.AddedByUID != actor {
		t.Errorf("added_by = %q, want the person who asked", found.AddedByUID)
	}
	if found.AddedByName != "Pan Botka" {
		t.Errorf("added_by_name = %q, want the asker's display name", found.AddedByName)
	}

	// Assigning twice is not an error — the end state is what was asked for.
	if err := f.tasks.Assign(ctx, task.UID, asked, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign (again): %v", err)
	}

	removed, err := f.tasks.Unassign(ctx, task.UID, asked, entry(audit.ActionTaskUnassign))
	if err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	if !removed {
		t.Error("Unassign reported nothing removed")
	}
	// Removing somebody who is not there is a no-op, not a failure.
	removed, err = f.tasks.Unassign(ctx, task.UID, asked, entry(audit.ActionTaskUnassign))
	if err != nil {
		t.Fatalf("Unassign (again): %v", err)
	}
	if removed {
		t.Error("Unassign reported a removal twice")
	}
}

// TestParticipants_refusesTheImpossible verifies the two foreign keys surface as
// their own errors rather than as a bare database failure.
func TestParticipants_refusesTheImpossible(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "Kdy?")

	if err := f.tasks.Assign(ctx, task.UID, "us-nobody", entry(audit.ActionTaskAssign)); !errors.Is(
		err, phototask.ErrUserNotFound,
	) {
		t.Errorf("assigning a missing account = %v, want ErrUserNotFound", err)
	}
	if err := f.tasks.Assign(ctx, "tk-nothing", actor, entry(audit.ActionTaskAssign)); !errors.Is(
		err, phototask.ErrNotFound,
	) {
		t.Errorf("assigning on a missing task = %v, want ErrNotFound", err)
	}
}

// TestParticipants_answeringJoins verifies the path that matters most for a
// viewer: the only thing they can do to a task is answer it, and answering is
// what has to put them on it.
func TestParticipants_answeringJoins(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "V kterém roce?")
	answerer := f.makeUser(t, "us-viewer", "soused", "Soused")

	if _, err := f.comments.Create(ctx, comments.TaskSubject(task.UID), answerer, "1987.",
		audit.Entry{ActorUID: answerer, Action: audit.ActionCommentCreate}); err != nil {
		t.Fatalf("commenting: %v", err)
	}
	if err := f.tasks.Join(ctx, task.UID, answerer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	people, _ := f.tasks.Participants(ctx, task.UID)
	if len(people) != 2 {
		t.Fatalf("participants = %v, want the author and the answerer", names(people))
	}
}

// TestList_byParticipant verifies the listing this table exists for: "what am I
// on?", combining with the other filters rather than replacing them.
func TestList_byParticipant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := f.makeUser(t, "us-other", "jiny", "Jiný")

	mine := f.mustCreate(t, "Moje otázka")
	theirs, err := f.tasks.Create(ctx, phototask.Task{Title: "Cizí otázka"}, nil,
		audit.Entry{ActorUID: other, Action: audit.ActionTaskCreate})
	if err != nil {
		t.Fatalf("creating the other task: %v", err)
	}
	closed := f.mustCreate(t, "Moje hotová")
	if _, err := f.tasks.Update(ctx, closed.UID, phototask.Update{
		State: statePtr(phototask.StateDone), Resolution: new("hotovo"),
	}, entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("closing: %v", err)
	}

	got, total, err := f.tasks.List(ctx, phototask.Filter{ParticipantUID: actor})
	if err != nil {
		t.Fatalf("List(participant): %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want the two tasks the actor is on", total)
	}
	for _, task := range got {
		if task.UID == theirs.UID {
			t.Errorf("the listing contains a task the actor is not on: %v", uids(got))
		}
	}

	// It combines: "mine, still open" is one query, which is what the page
	// showing somebody their own work actually asks for.
	open, _, err := f.tasks.List(ctx, phototask.Filter{ParticipantUID: actor, Open: true})
	if err != nil {
		t.Fatalf("List(participant, open): %v", err)
	}
	if len(open) != 1 || open[0].UID != mine.UID {
		t.Errorf("open+mine = %v, want just the open one", uids(open))
	}
}

// TestList_carriesParticipants verifies the listing renders who is on a task
// without a second request — the aggregate rides along with every read.
func TestList_carriesParticipants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "Kdo?")

	got, _, err := f.tasks.List(ctx, phototask.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d tasks, want one", len(got))
	}
	if len(got[0].Participants) != 1 || got[0].Participants[0].UserUID != actor {
		t.Errorf("listing participants = %v, want the author", names(got[0].Participants))
	}
	if got[0].Participants[0].Name != "Pan Botka" {
		t.Errorf("participant name = %q, want the display name", got[0].Participants[0].Name)
	}

	one, err := f.tasks.Get(ctx, task.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(one.Participants) != 1 {
		t.Errorf("Get participants = %v, want the same one", names(one.Participants))
	}
}
