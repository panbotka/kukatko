//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
)

// blankPhoto inserts a catalogued bootstrap photo with no metadata at all.
func blankPhoto(t *testing.T, store *photos.Store, hash string) photos.Photo {
	t.Helper()
	created, err := store.Create(t.Context(), photos.Photo{
		ProjectID: project.BootstrapUID,
		FileHash:  hash, FilePath: "2016/02/" + hash + ".jpg",
		FileName: hash + ".jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

// alias is one recorded photoprism_aliases row, as the tests read it back.
type alias struct {
	ppUID, photoUID, fileHash string
}

// listAliases reads every recorded alias straight from the table, ordered by
// source uid: the store has no instance-wide listing any more.
func listAliases(t *testing.T, db *database.DB) []alias {
	t.Helper()
	rows, err := db.Pool().Query(t.Context(), `SELECT photoprism_uid, photo_uid, coalesce(photoprism_file_hash, '')
		FROM photoprism_aliases ORDER BY photoprism_uid`)
	if err != nil {
		t.Fatalf("listing aliases: %v", err)
	}
	defer rows.Close()
	var out []alias
	for rows.Next() {
		var a alias
		if err := rows.Scan(&a.ppUID, &a.photoUID, &a.fileHash); err != nil {
			t.Fatalf("scanning an alias: %v", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing aliases: %v", err)
	}
	return out
}

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They cover the photoprism_aliases table (migration
// 0046): the record of a source photo that collapsed onto a catalogue row already
// holding its exact content under another source uid.

// TestPhotoprismAlias_resolvesToTheHoldingPhoto verifies the round trip an alias
// exists for: a source uid with no row of its own still resolves to the row that
// holds its content, while an unaliased uid stays not-found.
func TestPhotoprismAlias_resolvesToTheHoldingPhoto(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	winner := blankPhoto(t, store, "aliashash1")
	if err := store.AddPhotoprismAlias(ctx, project.BootstrapUID, "ppDup", winner.UID, "sha1-of-dup"); err != nil {
		t.Fatalf("AddPhotoprismAlias: %v", err)
	}

	got, err := store.GetByPhotoprismAlias(ctx, project.BootstrapUID, "ppDup")
	if err != nil {
		t.Fatalf("GetByPhotoprismAlias: %v", err)
	}
	if got.UID != winner.UID {
		t.Errorf("alias resolved to %q, want %q", got.UID, winner.UID)
	}
	if _, err := store.GetByPhotoprismAlias(ctx, project.BootstrapUID, "ppUnknown"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("unaliased uid error = %v, want ErrPhotoNotFound", err)
	}

	aliases := listAliases(t, db)
	if len(aliases) != 1 || aliases[0].ppUID != "ppDup" || aliases[0].photoUID != winner.UID {
		t.Fatalf("aliases = %+v, want one ppDup -> %s", aliases, winner.UID)
	}
	if aliases[0].fileHash != "sha1-of-dup" {
		t.Errorf("alias file hash = %q, want sha1-of-dup", aliases[0].fileHash)
	}
}

// TestPhotoprismAlias_isIdempotentAndRepointable verifies re-recording the same
// alias is a no-op — the import re-derives it on every pass — and that an alias
// whose content ended up on another row is re-pointed rather than rejected.
func TestPhotoprismAlias_isIdempotentAndRepointable(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	first := blankPhoto(t, store, "aliashash2")
	second := blankPhoto(t, store, "aliashash3")
	for range 2 {
		if err := store.AddPhotoprismAlias(ctx, project.BootstrapUID, "ppDup", first.UID, "sha1-a"); err != nil {
			t.Fatalf("AddPhotoprismAlias: %v", err)
		}
	}
	if err := store.AddPhotoprismAlias(ctx, project.BootstrapUID, "ppDup", second.UID, "sha1-a"); err != nil {
		t.Fatalf("re-pointing alias: %v", err)
	}

	if aliases := listAliases(t, db); len(aliases) != 1 || aliases[0].photoUID != second.UID {
		t.Errorf("aliases = %+v, want one pointing at %s", aliases, second.UID)
	}
}

// TestPhotoprismAlias_cascadesWithTheHoldingPhoto verifies the alias dies with the
// row it points at: the content is gone, so the source photo must become
// importable again rather than resolving to a photo that no longer exists.
func TestPhotoprismAlias_cascadesWithTheHoldingPhoto(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	winner := blankPhoto(t, store, "aliashash4")
	if err := store.AddPhotoprismAlias(ctx, project.BootstrapUID, "ppDup", winner.UID, ""); err != nil {
		t.Fatalf("AddPhotoprismAlias: %v", err)
	}
	if err := store.Delete(ctx, winner.UID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.GetByPhotoprismAlias(ctx, project.BootstrapUID, "ppDup"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("alias error after delete = %v, want ErrPhotoNotFound", err)
	}
}

// TestPhotoprismAlias_onlyForAPhotoOfTheProject verifies an alias is written
// only for a photo of the project it names: a photo of another project (or none
// at all) is ErrPhotoNotFound and records nothing, an empty project ErrNoProject.
func TestPhotoprismAlias_onlyForAPhotoOfTheProject(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()
	photo := blankPhoto(t, store, "aliashash5")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO projects (uid, slug, name) VALUES ('pjaliasother', 'alias-other', 'Other')`); err != nil {
		t.Fatalf("creating a second project: %v", err)
	}

	if err := store.AddPhotoprismAlias(ctx, "pjaliasother", "ppDup", photo.UID, ""); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("alias in another project = %v, want ErrPhotoNotFound", err)
	}
	if err := store.AddPhotoprismAlias(ctx, project.BootstrapUID, "ppDup", "ph_missing", ""); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("alias of a missing photo = %v, want ErrPhotoNotFound", err)
	}
	if err := store.AddPhotoprismAlias(ctx, "", "ppDup", photo.UID, ""); !errors.Is(err, photos.ErrNoProject) {
		t.Errorf("alias without a project = %v, want ErrNoProject", err)
	}
	if aliases := listAliases(t, db); len(aliases) != 0 {
		t.Errorf("refused aliases were recorded: %+v", aliases)
	}
}
