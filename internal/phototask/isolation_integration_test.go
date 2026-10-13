//go:build integration

package phototask_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// isolationFixture seeds the two tenants and returns them with a task store.
func isolationFixture(t *testing.T) (*projecttest.Fixture, *phototask.Store) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return projecttest.Seed(t, db.Pool()), phototask.NewStore(db.Pool())
}

// taskUIDs returns the uids of tasks.
func taskUIDs(tasks []phototask.Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.UID)
	}
	return out
}

// TestStore_readsAreScopedByProject asserts the listing, the summary behind the
// navbar badge and the per-photo open-task chip each see only the project's
// own task.
func TestStore_readsAreScopedByProject(t *testing.T) {
	f, store := isolationFixture(t)
	ctx := t.Context()

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, total, err := store.List(ctx, projectUID, phototask.Filter{})
		if err == nil && total != len(list) {
			t.Errorf("List(%s) total = %d, want %d", projectUID, total, len(list))
		}
		return taskUIDs(list), err
	})
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		summary, err := store.Summary(ctx, tn.ProjectUID, tn.UserUID)
		if err != nil {
			t.Fatalf("Summary(%s): %v", side, err)
		}
		if summary.Open != 1 || summary.ByState[phototask.StateQuestion] != 1 {
			t.Errorf("Summary(%s) = %+v, want exactly the side's one open question", side, summary)
		}
	}

	open, err := store.OpenForPhotos(ctx, f.A.ProjectUID, []string{f.A.PhotoUID, f.B.PhotoUID})
	if err != nil {
		t.Fatalf("OpenForPhotos: %v", err)
	}
	if len(open[f.A.PhotoUID]) != 1 || open[f.A.PhotoUID][0].UID != f.A.TaskUID {
		t.Errorf("OpenForPhotos(A)[A's photo] = %+v, want A's task", open[f.A.PhotoUID])
	}
	if refs, ok := open[f.B.PhotoUID]; ok {
		t.Errorf("OpenForPhotos(A) names B's photo's tasks %+v", refs)
	}

	inA, err := store.InProject(ctx, f.A.ProjectUID, []string{f.A.TaskUID})
	if err != nil || !inA {
		t.Errorf("InProject(A, A's task) = %v, %v; want true", inA, err)
	}
	if inA, err := store.InProject(ctx, f.A.ProjectUID, []string{f.A.TaskUID, f.B.TaskUID}); err != nil || inA {
		t.Errorf("InProject(A, A's and B's task) = %v, %v; want false", inA, err)
	}
	if _, _, err := store.List(ctx, "", phototask.Filter{}); !errors.Is(err, phototask.ErrNoProject) {
		t.Errorf("List without a project = %v, want ErrNoProject", err)
	}
}

// TestStore_foreignTaskIsMissing addresses B's task from inside A through every
// read and write and asserts each is the ErrNotFound a missing task gets, and
// that B's task is unchanged afterwards.
func TestStore_foreignTaskIsMissing(t *testing.T) {
	f, store := isolationFixture(t)
	ctx := t.Context()
	a, foreign := f.A.ProjectUID, f.B.TaskUID
	entry := audit.Entry{ActorUID: f.A.UserUID, Action: audit.ActionTaskUpdate}
	title := "Uneseno"

	checks := map[string]error{}
	_, checks["Get"] = store.Get(ctx, a, foreign, f.A.UserUID)
	_, checks["Update"] = store.Update(ctx, a, foreign, phototask.Update{Title: &title}, entry)
	_, checks["AddPhotos"] = store.AddPhotos(ctx, a, foreign, []string{f.A.PhotoUID}, entry)
	_, checks["RemovePhotos"] = store.RemovePhotos(ctx, a, foreign, []string{f.B.PhotoUID}, entry)
	_, checks["Participants"] = store.Participants(ctx, a, foreign)
	checks["Assign"] = store.Assign(ctx, a, foreign, f.A.UserUID, entry)
	_, checks["Unassign"] = store.Unassign(ctx, a, foreign, f.B.UserUID, entry)
	checks["Delete"] = store.Delete(ctx, a, foreign, entry)
	for op, err := range checks {
		if !errors.Is(err, phototask.ErrNotFound) {
			t.Errorf("%s of B's task from A = %v, want ErrNotFound", op, err)
		}
	}
	if err := store.Join(ctx, a, foreign, f.A.UserUID); err != nil {
		t.Errorf("Join of B's task from A = %v, want a silent no-op", err)
	}

	task, err := store.Get(ctx, f.B.ProjectUID, foreign, f.B.UserUID)
	if err != nil {
		t.Fatalf("Get in B after A's attempts: %v", err)
	}
	if task.Title == title || task.PhotoCount != 1 || len(task.Participants) != 0 {
		t.Errorf("B's task after A's attempts = %+v, want it untouched", task)
	}
}

// TestStore_freezesOnlyItsOwnPhotos asserts a task can neither be opened over
// nor extended with a photo of another project: the whole batch is refused with
// ErrPhotoNotFound, and nothing is created or added.
func TestStore_freezesOnlyItsOwnPhotos(t *testing.T) {
	f, store := isolationFixture(t)
	ctx := t.Context()
	entry := audit.Entry{ActorUID: f.A.UserUID, Action: audit.ActionTaskCreate}

	_, err := store.Create(ctx, f.A.ProjectUID, phototask.Task{Title: "Přes hranici"},
		phototask.Opening{PhotoUIDs: []string{f.A.PhotoUID, f.B.PhotoUID}}, entry)
	if !errors.Is(err, phototask.ErrPhotoNotFound) {
		t.Fatalf("Create in A over B's photo = %v, want ErrPhotoNotFound", err)
	}
	_, total, err := store.List(ctx, f.A.ProjectUID, phototask.Filter{})
	if err != nil || total != 1 {
		t.Errorf("A's tasks after the refused create = %d, %v; want only the seeded one", total, err)
	}

	added, err := store.AddPhotos(ctx, f.A.ProjectUID, f.A.TaskUID, []string{f.B.PhotoUID}, entry)
	if !errors.Is(err, phototask.ErrPhotoNotFound) || added != 0 {
		t.Errorf("AddPhotos(B's photo) to A's task = %d, %v; want 0, ErrPhotoNotFound", added, err)
	}
	task, err := store.Get(ctx, f.A.ProjectUID, f.A.TaskUID, "")
	if err != nil {
		t.Fatalf("Get A's task: %v", err)
	}
	if task.PhotoCount != 1 || task.CoverPhotoUID != f.A.PhotoUID {
		t.Errorf("A's task after the refused add = %d photos (cover %s), want just A's photo",
			task.PhotoCount, task.CoverPhotoUID)
	}

	created, err := store.Create(ctx, f.B.ProjectUID, phototask.Task{Title: "V B"},
		phototask.Opening{PhotoUIDs: []string{f.B.PhotoUID}}, audit.Entry{ActorUID: f.B.UserUID})
	if err != nil {
		t.Fatalf("Create in B over B's photo: %v", err)
	}
	if _, err := store.Get(ctx, f.A.ProjectUID, created.UID, ""); !errors.Is(err, phototask.ErrNotFound) {
		t.Errorf("B's new task read from A = %v, want ErrNotFound", err)
	}
}
