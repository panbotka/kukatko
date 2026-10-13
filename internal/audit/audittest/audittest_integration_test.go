//go:build integration

package audittest_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
)

// TestUnscoped proves the guard's verdict: a project-scoped action without a
// project is reported, the same action with one is not, an instance action
// without one is not, and nothing before the mark is looked at.
func TestUnscoped(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := audit.NewStore(db.Pool())
	record := func(entry audit.Entry) {
		t.Helper()
		if err := store.Record(t.Context(), entry); err != nil {
			t.Fatalf("recording %s: %v", entry.Action, err)
		}
	}

	record(audit.Entry{Action: audit.ActionAlbumCreate, TargetType: "albums"}) // before the mark
	got, err := audittest.Unscoped(t.Context(), db.Pool(), 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("Unscoped from 0 = %v, %v; want the one album.create", got, err)
	}
	mark := got[0].ID

	record(audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos"})
	record(audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos", ProjectUID: project.BootstrapUID})
	record(audit.Entry{Action: audit.ActionUserRename, TargetType: "users"})

	got, err = audittest.Unscoped(t.Context(), db.Pool(), mark)
	if err != nil {
		t.Fatalf("Unscoped: %v", err)
	}
	if len(got) != 1 || got[0].Action != audit.ActionPhotoUpdate {
		t.Errorf("Unscoped = %+v, want only the photo.update without a project", got)
	}
}

// TestGuard_passesAScopedTest proves Guard stays quiet over a test that files
// its project-scoped entry under a project.
func TestGuard_passesAScopedTest(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	entry := audit.Entry{Action: audit.ActionAlbumCreate, TargetType: "albums", ProjectUID: project.BootstrapUID}
	if err := audit.NewStore(db.Pool()).Record(t.Context(), entry); err != nil {
		t.Fatalf("recording: %v", err)
	}
}
