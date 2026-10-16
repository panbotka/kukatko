//go:build integration

package organize_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// requireProjectCover asserts the cover ProjectCover picks for projectUID is
// want ("" for none).
func requireProjectCover(t *testing.T, store *organize.Store, projectUID string, includePrivate bool, want string) {
	t.Helper()
	cover, ok, err := store.ProjectCover(t.Context(), projectUID, includePrivate)
	if err != nil {
		t.Fatalf("ProjectCover: %v", err)
	}
	got := ""
	if ok {
		got = cover.PhotoUID
		if cover.FileHash == "" || cover.FilePath == "" {
			t.Errorf("cover %s carries no file hash/path: %+v", got, cover)
		}
	}
	if got != want {
		t.Errorf("project cover (private %v) = %q, want %q", includePrivate, got, want)
	}
}

// featureAlbum features the album of testProject for a day.
func featureAlbum(t *testing.T, store *organize.Store, albumUID string) {
	t.Helper()
	if _, err := store.FeatureAlbumAudited(t.Context(), testProject, albumUID, time.Now().Add(24*time.Hour), "",
		audit.Entry{Action: audit.ActionAlbumFeature, TargetType: "albums"}); err != nil {
		t.Fatalf("featuring %s: %v", albumUID, err)
	}
}

// TestProjectCover_fallbackChain walks the chooser's picture rule: nothing for
// an empty library, then the most recently added showable photo, then the
// newest photo of the first featured album, then that album's hand-picked
// cover — and a private featured album only for a reader who may see it.
func TestProjectCover_fallbackChain(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := t.Context()
	requireProjectCover(t, store, testProject, true, "")

	uids := makeCoverPhotos(t, photoStore, "pcover")
	// Added in reverse order of capture: the oldest shot is the newest upload.
	added := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	for i, uid := range uids {
		if _, err := db.Pool().Exec(ctx, "UPDATE photos SET created_at = $2 WHERE uid = $1",
			uid, added.Add(-time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("dating %s: %v", uid, err)
		}
	}
	requireProjectCover(t, store, testProject, false, uids[0])

	// A newer upload that is hidden, private or archived never stands for the library.
	for i, column := range []string{"hidden_from_library = true", "private = true", "archived_at = now()"} {
		uid := makePhoto(t, photoStore, "pcover-skip-"+string(rune('a'+i)))
		if _, err := db.Pool().Exec(ctx, "UPDATE photos SET created_at = $2, "+column+" WHERE uid = $1",
			uid, added.Add(time.Hour)); err != nil {
			t.Fatalf("marking %s: %v", uid, err)
		}
	}
	requireProjectCover(t, store, testProject, false, uids[0])

	// A featured album wins: its newest capture, not the newest upload.
	album, err := store.CreateAlbum(ctx, testProject, organize.Album{Title: "Doporučené"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	addPhotos(t, store, album.UID, uids...)
	featureAlbum(t, store, album.UID)
	requireProjectCover(t, store, testProject, false, uids[2])

	// Its hand-picked cover wins over the derived one.
	if _, err := store.SetCover(ctx, testProject, album.UID, &uids[1]); err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	requireProjectCover(t, store, testProject, false, uids[1])

	// A private album featured later counts only for a reader who may see it.
	private, err := store.CreateAlbum(ctx, testProject, organize.Album{Title: "Soukromé", Private: true})
	if err != nil {
		t.Fatalf("CreateAlbum private: %v", err)
	}
	addPhotos(t, store, private.UID, uids[0])
	featureAlbum(t, store, private.UID)
	requireProjectCover(t, store, testProject, false, uids[1])
	requireProjectCover(t, store, testProject, true, uids[0])
}

// TestProjectCover_scopedByProject proves a project's picture is always one of
// its own photos: B's newer upload and B's featured album never become A's
// cover, and each side gets its own seeded photo.
func TestProjectCover_scopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := organize.NewStore(db.Pool())
	ctx := t.Context()

	if _, err := db.Pool().Exec(ctx, "UPDATE photos SET created_at = now() + interval '1 day' WHERE uid = $1",
		f.B.PhotoUID); err != nil {
		t.Fatalf("dating B's photo: %v", err)
	}
	if err := store.AddPhoto(ctx, f.B.ProjectUID, f.B.AlbumUID, f.B.PhotoUID); err != nil {
		t.Fatalf("filling B's album: %v", err)
	}
	if _, err := store.FeatureAlbumAudited(ctx, f.B.ProjectUID, f.B.AlbumUID, time.Now().Add(time.Hour), "",
		audit.Entry{Action: audit.ActionAlbumFeature, TargetType: "albums"}); err != nil {
		t.Fatalf("featuring B's album: %v", err)
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		cover, ok, err := store.ProjectCover(ctx, projectUID, true)
		if !ok {
			return nil, err
		}
		return []string{cover.PhotoUID}, err
	})
	requireProjectCover(t, store, f.A.ProjectUID, true, f.A.PhotoUID)
	requireProjectCover(t, store, f.B.ProjectUID, true, f.B.PhotoUID)
}
