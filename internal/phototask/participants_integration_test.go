//go:build integration

package phototask_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/project"
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
	people, err := f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
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
	second := f.makeMember(t, "us-second", "pametnik", "Pamětník")
	if _, err := f.tasks.Update(ctx, project.BootstrapUID, task.UID,
		phototask.Update{State: statePtr(phototask.StateWorking)},
		audit.Entry{ActorUID: second, Action: audit.ActionTaskUpdate}); err != nil {
		t.Fatalf("advancing: %v", err)
	}
	people, err = f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("after an edit by somebody else, participants = %v, want two", names(people))
	}

	// Acting again changes nothing: the join is idempotent.
	if _, err := f.tasks.AddPhotos(ctx, project.BootstrapUID, task.UID, []string{f.makePhoto(t, "b").UID},
		audit.Entry{ActorUID: second, Action: audit.ActionTaskAddPhotos}); err != nil {
		t.Fatalf("adding photos: %v", err)
	}
	people, _ = f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
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
	asked := f.makeMember(t, "us-asked", "teta", "Teta")

	if err := f.tasks.Assign(ctx, project.BootstrapUID, task.UID, asked, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	people, err := f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
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
	if err := f.tasks.Assign(ctx, project.BootstrapUID, task.UID, asked, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign (again): %v", err)
	}

	removed, err := f.tasks.Unassign(ctx, project.BootstrapUID, task.UID, asked, entry(audit.ActionTaskUnassign))
	if err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	if !removed {
		t.Error("Unassign reported nothing removed")
	}
	// Removing somebody who is not there is a no-op, not a failure.
	removed, err = f.tasks.Unassign(ctx, project.BootstrapUID, task.UID, asked, entry(audit.ActionTaskUnassign))
	if err != nil {
		t.Fatalf("Unassign (again): %v", err)
	}
	if removed {
		t.Error("Unassign reported a removal twice")
	}
}

// recordingObserver remembers every assignment the store reports.
type recordingObserver struct {
	heard []phototask.Assignment
}

// Assigned records a.
func (r *recordingObserver) Assigned(_ context.Context, _ pgx.Tx, a phototask.Assignment) error {
	r.heard = append(r.heard, a)
	return nil
}

// TestParticipants_observerHearsFreshAssignmentsOnly verifies what the store
// tells its assign observer: each person asked at an opening (not the actor,
// who joined by opening it), a person asked afterwards once — re-asking them
// is silent — and never somebody who is already on the task by having acted.
func TestParticipants_observerHearsFreshAssignmentsOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	obs := &recordingObserver{}
	tasks := f.tasks.WithAssignObserver(obs)
	anna := f.makeMember(t, "us-anna", "anna", "Anna")
	bedrich := f.makeMember(t, "us-bedrich", "bedrich", "Bedřich")
	// The opening asks the actor too, and only an approved member can be asked.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE project_members SET approved_at = now() WHERE user_uid = $1`,
		actor); err != nil {
		t.Fatalf("approving the actor: %v", err)
	}

	task, err := tasks.Create(ctx, project.BootstrapUID, phototask.Task{Title: "Kdy to bylo?"},
		phototask.Opening{AskedUIDs: []string{anna, actor, anna}}, entry(audit.ActionTaskCreate))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := []phototask.Assignment{{TaskUID: task.UID, UserUID: anna, ActorUID: actor}}
	if !reflect.DeepEqual(obs.heard, want) {
		t.Fatalf("after an opening asking Anna and the actor, heard %+v, want %+v", obs.heard, want)
	}

	if err := tasks.Assign(ctx, project.BootstrapUID, task.UID, anna, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign (again): %v", err)
	}
	if !reflect.DeepEqual(obs.heard, want) {
		t.Fatalf("re-asking Anna: heard %+v, want unchanged %+v", obs.heard, want)
	}

	if _, err := tasks.Update(ctx, project.BootstrapUID, task.UID, phototask.Update{State: statePtr(phototask.StateWorking)},
		audit.Entry{ActorUID: bedrich, Action: audit.ActionTaskUpdate}); err != nil {
		t.Fatalf("Update by Bedřich: %v", err)
	}
	if err := tasks.Assign(ctx, project.BootstrapUID, task.UID, bedrich, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign Bedřich: %v", err)
	}
	if !reflect.DeepEqual(obs.heard, want) {
		t.Fatalf("asking Bedřich, who acted already: heard %+v, want unchanged %+v", obs.heard, want)
	}

	other := f.mustCreate(t, "Druhý")
	if err := tasks.Assign(ctx, project.BootstrapUID, other.UID, bedrich, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign Bedřich on another task: %v", err)
	}
	want = append(want, phototask.Assignment{TaskUID: other.UID, UserUID: bedrich, ActorUID: actor})
	if !reflect.DeepEqual(obs.heard, want) {
		t.Fatalf("asking Bedřich on a fresh task: heard %+v, want %+v", obs.heard, want)
	}
}

// TestParticipants_refusesTheImpossible verifies the two foreign keys surface as
// their own errors rather than as a bare database failure.
func TestParticipants_refusesTheImpossible(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "Kdy?")

	if err := f.tasks.Assign(ctx, project.BootstrapUID, task.UID, "us-nobody", entry(audit.ActionTaskAssign)); !errors.Is(
		err, phototask.ErrUserNotFound,
	) {
		t.Errorf("assigning a missing account = %v, want ErrUserNotFound", err)
	}
	if err := f.tasks.Assign(ctx, project.BootstrapUID, "tk-nothing", actor, entry(audit.ActionTaskAssign)); !errors.Is(
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
	answerer := f.makeMember(t, "us-viewer", "soused", "Soused")

	if _, err := f.comments.Create(ctx, project.BootstrapUID, comments.TaskSubject(task.UID), answerer, "1987.",
		audit.Entry{ActorUID: answerer, Action: audit.ActionCommentCreate}); err != nil {
		t.Fatalf("commenting: %v", err)
	}
	if err := f.tasks.Join(ctx, project.BootstrapUID, task.UID, answerer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	people, _ := f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
	if len(people) != 2 {
		t.Fatalf("participants = %v, want the author and the answerer", names(people))
	}
}

// TestList_byParticipant verifies the listing this table exists for: "what am I
// on?", combining with the other filters rather than replacing them.
func TestList_byParticipant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := f.makeMember(t, "us-other", "jiny", "Jiný")

	mine := f.mustCreate(t, "Moje otázka")
	theirs, err := f.tasks.Create(ctx, project.BootstrapUID, phototask.Task{Title: "Cizí otázka"}, phototask.Opening{},
		audit.Entry{ActorUID: other, Action: audit.ActionTaskCreate})
	if err != nil {
		t.Fatalf("creating the other task: %v", err)
	}
	closed := f.mustCreate(t, "Moje hotová")
	if _, err := f.tasks.Update(ctx, project.BootstrapUID, closed.UID, phototask.Update{
		State: statePtr(phototask.StateDone), Resolution: new("hotovo"),
	}, entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("closing: %v", err)
	}

	got, total, err := f.tasks.List(ctx, project.BootstrapUID, phototask.Filter{ParticipantUID: actor})
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
	open, _, err := f.tasks.List(ctx, project.BootstrapUID, phototask.Filter{ParticipantUID: actor, Open: true})
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

	got, _, err := f.tasks.List(ctx, project.BootstrapUID, phototask.Filter{})
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

	one, err := f.tasks.Get(ctx, project.BootstrapUID, task.UID, actor)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(one.Participants) != 1 {
		t.Errorf("Get participants = %v, want the same one", names(one.Participants))
	}
}

// TestCreate_askedAtOpening verifies handing a task to somebody as it is opened
// reads exactly as handing it over afterwards: each asked person carries the
// creator as added_by, the creator is on it by acting, a repeated uid is asked
// once, and every hand-over is its own assign in the trail.
func TestCreate_askedAtOpening(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	anna := f.makeMember(t, "us-anna", "anna", "Anna")
	agent := f.makeMember(t, "us-agent", "agent", "Agent")

	task, err := f.tasks.Create(ctx, project.BootstrapUID, phototask.Task{Title: "Přejmenovat wf: štítky", State: phototask.StateWorking},
		phototask.Opening{AskedUIDs: []string{anna, agent, anna}}, entry(audit.ActionTaskCreate))
	if err != nil {
		t.Fatalf("Create with asked participants: %v", err)
	}
	if task.PhotoCount != 0 || task.CoverPhotoUID != "" {
		t.Errorf("task over nothing = %d photos, cover %q; want none", task.PhotoCount, task.CoverPhotoUID)
	}
	if task.State != phototask.StateWorking {
		t.Errorf("state = %q, want working as asked", task.State)
	}

	addedBy := make(map[string]string, len(task.Participants))
	for _, p := range task.Participants {
		addedBy[p.UserUID] = p.AddedByUID
	}
	want := map[string]string{actor: "", anna: actor, agent: actor}
	if len(addedBy) != len(want) {
		t.Fatalf("participants = %+v, want the creator and the two asked", task.Participants)
	}
	for uid, by := range want {
		if got, ok := addedBy[uid]; !ok || got != by {
			t.Errorf("participant %s added_by = %q (present %v), want %q", uid, got, ok, by)
		}
	}
	if n := f.auditCount(t, audit.ActionTaskAssign); n != 2 {
		t.Errorf("assign audit rows = %d, want one per person asked", n)
	}
	if n := f.auditCount(t, audit.ActionTaskCreate); n != 1 {
		t.Errorf("create audit rows = %d, want 1", n)
	}
}

// TestCreate_askedUnknownWritesNothing verifies a hand-over to a person with no
// account fails the whole opening: no task, no participants, no audit rows.
func TestCreate_askedUnknownWritesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.tasks.Create(ctx, project.BootstrapUID, phototask.Task{Title: "Komu?"},
		phototask.Opening{AskedUIDs: []string{"us-nobody"}}, entry(audit.ActionTaskCreate))
	if !errors.Is(err, phototask.ErrUserNotFound) {
		t.Fatalf("Create with an unknown person: err = %v, want ErrUserNotFound", err)
	}
	if _, total, err := f.tasks.List(ctx, project.BootstrapUID, phototask.Filter{}); err != nil || total != 0 {
		t.Errorf("after the refusal: total = %d, err = %v; want no task at all", total, err)
	}
	if n := f.auditCount(t, audit.ActionTaskCreate) + f.auditCount(t, audit.ActionTaskAssign); n != 0 {
		t.Errorf("audit rows after the refusal = %d, want none", n)
	}
}

// TestParticipants_formerMembers verifies a participant, and whoever asked
// them, read as former members once they have left the task's project — no
// name, the flag set — in the participants listing and in the task alike.
func TestParticipants_formerMembers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "Kdo je to?")
	asked := f.makeMember(t, "us-asked", "teta", "Teta")
	if err := f.tasks.Assign(ctx, project.BootstrapUID, task.UID, asked, entry(audit.ActionTaskAssign)); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if _, err := f.db.Pool().Exec(ctx, `DELETE FROM project_members WHERE project_uid = $1`,
		project.BootstrapUID); err != nil {
		t.Fatalf("emptying the project: %v", err)
	}

	people, err := f.tasks.Participants(ctx, project.BootstrapUID, task.UID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	one, err := f.tasks.Get(ctx, project.BootstrapUID, task.UID, actor)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for source, list := range map[string][]phototask.Participant{"Participants": people, "Get": one.Participants} {
		if len(list) != 2 {
			t.Fatalf("%s = %v, want the author and the asked person", source, names(list))
		}
		for _, p := range list {
			if !p.Former || p.Name != "" {
				t.Errorf("%s: %s reads %q former=%v, want a nameless former member", source, p.UserUID, p.Name, p.Former)
			}
			if p.UserUID == asked && (!p.AddedByFormer || p.AddedByName != "" || p.AddedByUID != actor) {
				t.Errorf("%s: asker = %q %q former=%v, want a nameless former member", source, p.AddedByUID,
					p.AddedByName, p.AddedByFormer)
			}
		}
	}
}
