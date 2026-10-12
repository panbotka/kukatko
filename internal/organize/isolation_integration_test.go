//go:build integration

package organize_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestListings_scopedByProject reads every organise listing for A and for B
// and asserts each side sees only its own rows: the album index, the labels,
// the featured strip, the searches, the covers and a photo's memberships.
func TestListings_scopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := organize.NewStore(db.Pool())
	ctx := t.Context()
	until := time.Now().Add(24 * time.Hour)
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		if _, err := store.FeatureAlbumAudited(ctx, tn.ProjectUID, tn.AlbumUID, until, "", audit.Entry{
			Action: audit.ActionAlbumFeature, TargetType: "albums",
		}); err != nil {
			t.Fatalf("featuring %s's album: %v", side, err)
		}
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.ListAlbums(ctx, projectUID)
		return albumSummaryUIDs(list), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.ListLabels(ctx, projectUID)
		return labelCountUIDs(list), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.ListFeaturedAlbums(ctx, projectUID, true)
		uids := make([]string, 0, len(list))
		for _, fa := range list {
			uids = append(uids, fa.UID)
		}
		return uids, err
	})
	// Both sides' titles hold the word "album"/"štítek", so a search for it
	// matches both rows — and must still return only the asking side's.
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.SearchAlbums(ctx, projectUID, "album", 10)
		uids := make([]string, 0, len(list))
		for _, a := range list {
			uids = append(uids, a.UID)
		}
		return uids, err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.SearchLabels(ctx, projectUID, "stitek", 10)
		return labelCountUIDs(list), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		covers, err := store.AlbumCovers(ctx, projectUID, []string{f.A.AlbumUID, f.B.AlbumUID})
		return coverPhotoUIDs(covers), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		covers, err := store.LabelCovers(ctx, projectUID, []string{f.A.LabelUID, f.B.LabelUID})
		return coverPhotoUIDs(covers), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		byPhoto, err := store.AlbumUIDsForPhotos(ctx, projectUID, []string{f.A.PhotoUID, f.B.PhotoUID})
		uids := make([]string, 0, len(byPhoto))
		for _, albums := range byPhoto {
			uids = append(uids, albums...)
		}
		return uids, err
	})
	if _, err := store.ListAlbums(ctx, ""); !errors.Is(err, organize.ErrNoProject) {
		t.Errorf("ListAlbums without a project = %v, want ErrNoProject", err)
	}
}

// TestLookups_foreignIsMissing asks B for A's album and label by uid and slug,
// for A's album's photos and for A's photo's memberships: every answer is the
// one a missing row gets.
func TestLookups_foreignIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := organize.NewStore(db.Pool())
	ctx := t.Context()
	inB := f.B.ProjectUID

	if _, err := store.GetAlbumByUID(ctx, inB, f.A.AlbumUID); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("GetAlbumByUID(A's album) in B = %v, want ErrAlbumNotFound", err)
	}
	if _, err := store.GetLabelByUID(ctx, inB, f.A.LabelUID); !errors.Is(err, organize.ErrLabelNotFound) {
		t.Errorf("GetLabelByUID(A's label) in B = %v, want ErrLabelNotFound", err)
	}
	if _, err := store.GetAlbumBySlug(ctx, inB, "album-"+f.A.Token); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("GetAlbumBySlug(A's slug) in B = %v, want ErrAlbumNotFound", err)
	}
	if uids, err := store.ListPhotoUIDs(ctx, inB, f.A.AlbumUID); err != nil || len(uids) != 0 {
		t.Errorf("ListPhotoUIDs(A's album) in B = %v, %v; want empty", uids, err)
	}
	if uids, err := store.ListPhotoUIDsByLabel(ctx, inB, f.A.LabelUID); err != nil || len(uids) != 0 {
		t.Errorf("ListPhotoUIDsByLabel(A's label) in B = %v, %v; want empty", uids, err)
	}
	if albums, err := store.AlbumsForPhoto(ctx, inB, f.A.PhotoUID); err != nil || len(albums) != 0 {
		t.Errorf("AlbumsForPhoto(A's photo) in B = %v, %v; want empty", albums, err)
	}
	if labels, err := store.LabelsForPhoto(ctx, inB, f.A.PhotoUID); err != nil || len(labels) != 0 {
		t.Errorf("LabelsForPhoto(A's photo) in B = %v, %v; want empty", labels, err)
	}
}

// TestSlugs_uniquePerProject creates an album and a label of one title in both
// projects: each keeps the bare slug, because slugs are unique per project.
func TestSlugs_uniquePerProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := organize.NewStore(db.Pool())
	ctx := t.Context()
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		projectUID := f.Tenant(side).ProjectUID
		album, err := store.CreateAlbum(ctx, projectUID, organize.Album{Title: "Pouť"})
		if err != nil || album.Slug != "pout" {
			t.Errorf("CreateAlbum(Pouť) in %s = %q, %v; want slug pout", side, album.Slug, err)
		}
		label, err := store.CreateLabel(ctx, projectUID, organize.Label{Name: "Pouť"})
		if err != nil || label.Slug != "pout" {
			t.Errorf("CreateLabel(Pouť) in %s = %q, %v; want slug pout", side, label.Slug, err)
		}
	}
}

// TestWrites_foreignTargetRefused aims every organise write made inside B at
// A's album, label or photo: each is refused like a missing target, the batch
// holding one foreign photo adds nothing — not even its own photos — and no
// refused write leaves an audit row.
func TestWrites_foreignTargetRefused(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	store := organize.NewStore(pool)
	ctx := t.Context()
	inB := f.B.ProjectUID
	extraB := insertPhoto(t, pool, inB)
	entry := audit.Entry{Action: audit.ActionAlbumAddPhotos, TargetType: "albums"}

	err := store.AddPhotosAudited(ctx, inB, f.B.AlbumUID, []string{extraB, f.A.PhotoUID}, entry)
	if !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("AddPhotosAudited(B's album, [B's photo, A's photo]) = %v, want ErrPhotoNotFound", err)
	}
	if uids, err := store.ListPhotoUIDs(ctx, inB, f.B.AlbumUID); err != nil || len(uids) != 1 {
		t.Errorf("B's album after the refused batch = %v, %v; want only its seeded photo", uids, err)
	}
	if err := store.AddPhotosAudited(ctx, inB, f.A.AlbumUID, []string{extraB}, entry); !errors.Is(
		err, organize.ErrAlbumNotFound) {
		t.Errorf("AddPhotosAudited(A's album) in B = %v, want ErrAlbumNotFound", err)
	}
	assertNoAudit(t, pool, audit.ActionAlbumAddPhotos)

	refused := []struct {
		name string
		err  error
		want error
	}{
		{"AddPhoto(A's photo)", store.AddPhoto(ctx, inB, f.B.AlbumUID, f.A.PhotoUID), organize.ErrPhotoNotFound},
		{"RemovePhotosAudited(A's album)", store.RemovePhotosAudited(ctx, inB, f.A.AlbumUID,
			[]string{f.A.PhotoUID}, entry), organize.ErrAlbumNotFound},
		{"DeleteAlbumAudited(A's album)", store.DeleteAlbumAudited(ctx, inB, f.A.AlbumUID, entry),
			organize.ErrAlbumNotFound},
		{"AttachLabel(A's label)", store.AttachLabel(ctx, inB, f.B.PhotoUID, f.A.LabelUID, "", 0),
			organize.ErrLabelNotFound},
		{"AttachLabel(A's photo)", store.AttachLabel(ctx, inB, f.A.PhotoUID, f.B.LabelUID, "", 0),
			organize.ErrPhotoNotFound},
		{"DetachLabelAudited(A's label)", store.DetachLabelAudited(ctx, inB, f.A.PhotoUID, f.A.LabelUID, entry),
			organize.ErrLabelNotFound},
		{"DeleteLabelAudited(A's label)", store.DeleteLabelAudited(ctx, inB, f.A.LabelUID, entry),
			organize.ErrLabelNotFound},
		{"AddFavorite(A's photo)", store.AddFavorite(ctx, inB, f.B.UserUID, f.A.PhotoUID), organize.ErrPhotoNotFound},
		{"SetRating(A's photo)", store.SetRating(ctx, inB, f.B.UserUID, f.A.PhotoUID, 4), organize.ErrPhotoNotFound},
	}
	for _, c := range refused {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s in B = %v, want %v", c.name, c.err, c.want)
		}
	}
	cover := f.A.PhotoUID
	if _, err := store.UpdateAlbumAudited(ctx, inB, f.B.AlbumUID, organize.AlbumUpdate{
		Title: "B", CoverPhotoUID: &cover,
	}, entry); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("UpdateAlbumAudited(B's album, A's photo as cover) = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.FeatureAlbumAudited(ctx, inB, f.A.AlbumUID, time.Now().Add(time.Hour), "", entry); !errors.Is(
		err, organize.ErrAlbumNotFound) {
		t.Errorf("FeatureAlbumAudited(A's album) in B = %v, want ErrAlbumNotFound", err)
	}
	if _, err := store.GetAlbumByUID(ctx, f.A.ProjectUID, f.A.AlbumUID); err != nil {
		t.Errorf("A's album after B's refused writes: %v", err)
	}
	if _, err := store.GetLabelByUID(ctx, f.A.ProjectUID, f.A.LabelUID); err != nil {
		t.Errorf("A's label after B's refused writes: %v", err)
	}
	assertNoAudit(t, pool, audit.ActionAlbumAddPhotos)
}

// TestCuration_scopedByProject has one account favourite and rate a photo in
// each project: every per-user read made inside one project sees only that
// project's photo.
func TestCuration_scopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := organize.NewStore(db.Pool())
	ctx := t.Context()
	user := f.A.UserUID
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		if err := store.AddFavorite(ctx, tn.ProjectUID, user, tn.PhotoUID); err != nil {
			t.Fatalf("AddFavorite in %s: %v", side, err)
		}
		if err := store.SetRating(ctx, tn.ProjectUID, user, tn.PhotoUID, 5); err != nil {
			t.Fatalf("SetRating in %s: %v", side, err)
		}
	}
	both := []string{f.A.PhotoUID, f.B.PhotoUID}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		return store.ListFavorites(ctx, projectUID, user)
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		set, err := store.FavoritedAmong(ctx, projectUID, user, both)
		return setKeys(set), err
	})
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		ratings, err := store.RatingsAmong(ctx, projectUID, user, both)
		uids := make([]string, 0, len(ratings))
		for uid := range ratings {
			uids = append(uids, uid)
		}
		return uids, err
	})
	if fav, err := store.IsFavorite(ctx, f.B.ProjectUID, user, f.A.PhotoUID); err != nil || fav {
		t.Errorf("IsFavorite(A's photo) in B = %v, %v; want false", fav, err)
	}
	if err := store.RemoveFavorite(ctx, f.B.ProjectUID, user, f.A.PhotoUID); err != nil {
		t.Fatalf("RemoveFavorite(A's photo) in B: %v", err)
	}
	if err := store.ClearRating(ctx, f.B.ProjectUID, user, f.A.PhotoUID); err != nil {
		t.Fatalf("ClearRating(A's photo) in B: %v", err)
	}
	if fav, err := store.IsFavorite(ctx, f.A.ProjectUID, user, f.A.PhotoUID); err != nil || !fav {
		t.Errorf("A's favourite after B removed it = %v, %v; want still there", fav, err)
	}
	if rating, err := store.GetRating(ctx, f.A.ProjectUID, user, f.A.PhotoUID); err != nil || rating.Rating != 5 {
		t.Errorf("A's rating after B cleared it = %+v, %v; want 5", rating, err)
	}
}

// insertPhoto inserts one more photo into the project and returns its uid.
func insertPhoto(t *testing.T, pool *pgxpool.Pool, projectUID string) string {
	t.Helper()
	uid := projecttest.MissingUID("ph")
	sum := sha256.Sum256([]byte(uid))
	if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, file_mime, file_width, file_height, taken_at)
		VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
		uid, projectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
		t.Fatalf("inserting a photo into %s: %v", projectUID, err)
	}
	return uid
}

// assertNoAudit fails t if the audit log holds an entry of action.
func assertNoAudit(t *testing.T, pool *pgxpool.Pool, action string) {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM audit_log WHERE action = $1", action).Scan(&n); err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	if n != 0 {
		t.Errorf("refused writes left %d audit entries of %s", n, action)
	}
}

// albumSummaryUIDs returns the uids of an album index.
func albumSummaryUIDs(list []organize.AlbumSummary) []string {
	uids := make([]string, 0, len(list))
	for _, a := range list {
		uids = append(uids, a.UID)
	}
	return uids
}

// labelCountUIDs returns the uids of a label listing.
func labelCountUIDs(list []organize.LabelCount) []string {
	uids := make([]string, 0, len(list))
	for _, l := range list {
		uids = append(uids, l.UID)
	}
	return uids
}

// coverPhotoUIDs returns the photos a cover map points at.
func coverPhotoUIDs(covers map[string]organize.Cover) []string {
	uids := make([]string, 0, len(covers))
	for _, c := range covers {
		uids = append(uids, c.PhotoUID)
	}
	return uids
}

// setKeys returns the keys of a set.
func setKeys(set map[string]bool) []string {
	uids := make([]string, 0, len(set))
	for uid := range set {
		uids = append(uids, uid)
	}
	return uids
}
