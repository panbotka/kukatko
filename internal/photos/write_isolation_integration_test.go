//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These tests are the store-level tenant isolation of the photo write side:
// every write addressed to A's photo from B's project must answer
// ErrPhotoNotFound — exactly like a photo that does not exist — and leave A's
// photo, its files, its edit, its stack and the audit trail untouched.

// writeAttempt is one store write aimed at uid from inside projectUID.
type writeAttempt struct {
	name string
	run  func(projectUID, uid string) error
}

// writeAttempts lists every project-scoped write of the store, each with a
// valid argument set, so the only reason to refuse is the project.
func writeAttempts(t *testing.T, store *photos.Store) []writeAttempt {
	t.Helper()
	ctx := t.Context()
	entry := func() audit.Entry {
		return audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos"}
	}
	update := photos.MetadataUpdate{Title: "overwritten from the other project"}
	return []writeAttempt{
		{"UpdateMetadata", func(p, uid string) error {
			_, err := store.UpdateMetadata(ctx, p, uid, update)
			return err
		}},
		{"UpdateMetadataAudited", func(p, uid string) error {
			_, err := store.UpdateMetadataAudited(ctx, p, uid, update, entry())
			return err
		}},
		{"Archive", func(p, uid string) error { _, err := store.Archive(ctx, p, uid); return err }},
		{"ArchiveAudited", func(p, uid string) error { _, err := store.ArchiveAudited(ctx, p, uid, entry()); return err }},
		{"Unarchive", func(p, uid string) error { _, err := store.Unarchive(ctx, p, uid); return err }},
		{"UnarchiveAudited", func(p, uid string) error {
			_, err := store.UnarchiveAudited(ctx, p, uid, entry())
			return err
		}},
		{"SetHiddenFromLibrary", func(p, uid string) error {
			_, err := store.SetHiddenFromLibrary(ctx, p, uid, true)
			return err
		}},
		{"SetHiddenFromLibraryAudited", func(p, uid string) error {
			_, err := store.SetHiddenFromLibraryAudited(ctx, p, uid, true, entry())
			return err
		}},
		{"SetEdit", func(p, uid string) error {
			return store.SetEdit(ctx, p, photos.Edit{PhotoUID: uid, Rotation: 90, Contrast: 0.5})
		}},
		{"CreateFile", func(p, uid string) error {
			_, err := store.CreateFile(ctx, p, photos.PhotoFile{
				PhotoUID: uid, FilePath: "2026/10/sidecar-" + uid + ".xmp", FileHash: "deadbeef",
				FileSize: 1, FileMime: "application/rdf+xml", Role: photos.RoleSidecar,
			})
			return err
		}},
		{"SetStackPrimary", func(p, uid string) error { _, err := store.SetStackPrimary(ctx, p, uid); return err }},
		{"SetStackPrimaryAudited", func(p, uid string) error {
			_, err := store.SetStackPrimaryAudited(ctx, p, uid, entry())
			return err
		}},
		{"UnstackMember", func(p, uid string) error { _, err := store.UnstackMember(ctx, p, uid); return err }},
		{"UnstackMemberAudited", func(p, uid string) error {
			_, err := store.UnstackMemberAudited(ctx, p, uid, entry())
			return err
		}},
		{"UnstackAll", func(p, uid string) error { _, err := store.UnstackAll(ctx, p, uid); return err }},
		{"DeleteAudited", func(p, uid string) error { return store.DeleteAudited(ctx, p, uid, entry()) }},
		{"UnstackAllAudited", func(p, uid string) error {
			_, err := store.UnstackAllAudited(ctx, p, uid, entry())
			return err
		}},
	}
}

// TestWrites_foreignPhotoIsNotFoundAndUnchanged fires every scoped write at A's
// photo from B's project and asserts ErrPhotoNotFound and an unchanged row, and
// that the same write without a project is ErrNoProject.
func TestWrites_foreignPhotoIsNotFoundAndUnchanged(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	before, err := store.GetByUIDInProject(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("loading A's photo: %v", err)
	}
	filesBefore, err := store.ListFiles(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("listing A's files: %v", err)
	}

	for _, w := range writeAttempts(t, store) {
		t.Run(w.name, func(t *testing.T) {
			if err := w.run(f.B.ProjectUID, f.A.PhotoUID); !errors.Is(err, photos.ErrPhotoNotFound) {
				t.Errorf("%s on A's photo from B = %v, want ErrPhotoNotFound", w.name, err)
			}
			if err := w.run("", f.A.PhotoUID); !errors.Is(err, photos.ErrNoProject) {
				t.Errorf("%s without a project = %v, want ErrNoProject", w.name, err)
			}
		})
	}

	after, err := store.GetByUIDInProject(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("reloading A's photo: %v", err)
	}
	if after.Title != before.Title || after.ArchivedAt != nil || after.HiddenFromLibrary ||
		after.StackUID != nil || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("A's photo changed: before %+v, after %+v", before, after)
	}
	if _, err := store.GetEdit(ctx, f.A.PhotoUID); !errors.Is(err, photos.ErrEditNotFound) {
		t.Errorf("A's photo gained an edit (err %v)", err)
	}
	filesAfter, err := store.ListFiles(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("relisting A's files: %v", err)
	}
	if len(filesAfter) != len(filesBefore) {
		t.Errorf("A's photo has %d files, had %d", len(filesAfter), len(filesBefore))
	}
	var audited int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE target_uid = $1`, f.A.PhotoUID).Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 0 {
		t.Errorf("a refused write left %d audit row(s) about A's photo", audited)
	}
}

// TestWrites_ownPhotoSucceedsAndIsAuditedInItsProject proves the scoping does
// not refuse a write inside the photo's own project, and that an audited write
// records the project it happened in.
func TestWrites_ownPhotoSucceedsAndIsAuditedInItsProject(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	updated, err := store.UpdateMetadataAudited(ctx, f.B.ProjectUID, f.B.PhotoUID,
		photos.MetadataUpdate{Title: "B's own"}, audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos"})
	if err != nil {
		t.Fatalf("UpdateMetadataAudited on B's own photo: %v", err)
	}
	if updated.Title != "B's own" || updated.ProjectID != f.B.ProjectUID {
		t.Errorf("updated photo = title %q project %q", updated.Title, updated.ProjectID)
	}
	if err := store.SetEdit(ctx, f.B.ProjectUID, photos.Edit{PhotoUID: f.B.PhotoUID, Rotation: 90}); err != nil {
		t.Errorf("SetEdit on B's own photo: %v", err)
	}
	var project string
	if err := db.Pool().QueryRow(ctx,
		`SELECT project_id FROM audit_log WHERE target_uid = $1`, f.B.PhotoUID).Scan(&project); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if project != f.B.ProjectUID {
		t.Errorf("audit row recorded project %q, want %q", project, f.B.ProjectUID)
	}
}

// TestCreateStack_neverSpansProjects proves a stack is formed only from photos
// of the given project: naming A's photo next to B's from B's project is
// ErrPhotoNotFound and stacks neither.
func TestCreateStack_neverSpansProjects(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	members := []string{f.B.PhotoUID, f.A.PhotoUID}
	if _, err := store.CreateStack(ctx, f.B.ProjectUID, f.B.PhotoUID, members); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("CreateStack across projects = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.CreateStackAudited(ctx, f.B.ProjectUID, f.B.PhotoUID, members,
		audit.Entry{Action: audit.ActionPhotosStack, TargetType: "photos"}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("CreateStackAudited across projects = %v, want ErrPhotoNotFound", err)
	}
	plans := []photos.StackPlan{{PrimaryUID: f.B.PhotoUID, MemberUIDs: members}}
	if _, err := store.CreateStacksAudited(ctx, f.B.ProjectUID, plans,
		audit.Entry{Action: audit.ActionStacksDetect, TargetType: "photos"}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("CreateStacksAudited across projects = %v, want ErrPhotoNotFound", err)
	}
	for _, side := range []*projecttest.Tenant{&f.A, &f.B} {
		photo, err := store.GetForJob(ctx, side.PhotoUID)
		if err != nil {
			t.Fatalf("loading %s: %v", side.PhotoUID, err)
		}
		if photo.StackUID != nil {
			t.Errorf("photo %s was stacked across projects", side.PhotoUID)
		}
	}
}

// TestForJobLoaders_readAnyProjectAndCarryIt proves the unscoped job loaders
// find a photo of either project and that the row names its project, which is
// how a job learns whom it works for.
func TestForJobLoaders_readAnyProjectAndCarryIt(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	for _, side := range []*projecttest.Tenant{&f.A, &f.B} {
		photo, err := store.GetForJob(ctx, side.PhotoUID)
		if err != nil {
			t.Fatalf("GetForJob(%s): %v", side.PhotoUID, err)
		}
		if photo.ProjectID != side.ProjectUID {
			t.Errorf("GetForJob(%s).ProjectID = %q, want %q", side.PhotoUID, photo.ProjectID, side.ProjectUID)
		}
	}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		var uids []string
		for _, uid := range []string{f.A.PhotoUID, f.B.PhotoUID} {
			files, err := store.ListFiles(ctx, projectUID, uid)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				uids = append(uids, file.PhotoUID)
			}
		}
		return uids, nil
	})
}
