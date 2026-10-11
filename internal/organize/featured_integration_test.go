//go:build integration

package organize_test

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
)

// featuredUIDs returns the uids of the listed featured albums in order.
func featuredUIDs(list []organize.FeaturedAlbum) []string {
	out := make([]string, 0, len(list))
	for _, fa := range list {
		out = append(out, fa.UID)
	}
	return out
}

// TestFeatureAlbum_lifecycle features an album, extends it, reads it back
// through every album payload and unfeatures it, checking the audit trail.
func TestFeatureAlbum_lifecycle(t *testing.T) {
	store, photoStore, authStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, authStore, "feat_actor", "feat")
	photoUID := makePhoto(t, photoStore, "featuredcover")

	album, err := store.CreateAlbum(ctx, organize.Album{Title: "Pouť 2026"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	addPhotos(t, store, album.UID, photoUID)
	if album.FeaturedUntil != nil {
		t.Fatalf("a new album is featured until %v", album.FeaturedUntil)
	}

	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	got, err := store.FeatureAlbumAudited(ctx, album.UID, until, actor,
		actorEntry(actor, audit.ActionAlbumFeature, "albums", album.UID, nil))
	if err != nil {
		t.Fatalf("FeatureAlbumAudited: %v", err)
	}
	if got.FeaturedUntil == nil || !got.FeaturedUntil.Equal(until) {
		t.Fatalf("featured_until = %v, want %v", got.FeaturedUntil, until)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumFeature, actor, album.UID)

	list, err := store.ListFeaturedAlbums(ctx, false)
	if err != nil {
		t.Fatalf("ListFeaturedAlbums: %v", err)
	}
	if len(list) != 1 || list[0].UID != album.UID {
		t.Fatalf("featured = %v, want [%s]", featuredUIDs(list), album.UID)
	}
	first := list[0]
	if first.PhotoCount != 1 || first.CoverUID == nil || *first.CoverUID != photoUID {
		t.Errorf("featured count/cover = %d/%v, want 1/%s", first.PhotoCount, first.CoverUID, photoUID)
	}
	if first.CoverFileHash != "featuredcover" {
		t.Errorf("cover file hash = %q, want featuredcover", first.CoverFileHash)
	}

	// Every album payload carries the active end: detail, index and search.
	detail, err := store.GetAlbumByUID(ctx, album.UID)
	if err != nil || detail.FeaturedUntil == nil {
		t.Fatalf("GetAlbumByUID featured_until = %v (err %v), want set", detail.FeaturedUntil, err)
	}
	all, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if albumByUID(t, all, album.UID).FeaturedUntil == nil {
		t.Error("ListAlbums does not carry featured_until")
	}
	hits, err := store.SearchAlbums(ctx, "Pout", 5)
	if err != nil || len(hits) != 1 || hits[0].FeaturedUntil == nil {
		t.Errorf("SearchAlbums = %+v (err %v), want one featured hit", hits, err)
	}

	// Extending keeps the moment the feature began, so the strip's order holds.
	later := until.Add(30 * 24 * time.Hour)
	if _, err := store.FeatureAlbumAudited(ctx, album.UID, later, actor,
		actorEntry(actor, audit.ActionAlbumFeature, "albums", album.UID, nil)); err != nil {
		t.Fatalf("extending: %v", err)
	}
	list, err = store.ListFeaturedAlbums(ctx, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListFeaturedAlbums after extend = %v (err %v)", featuredUIDs(list), err)
	}
	if !list[0].FeaturedAt.Equal(first.FeaturedAt) {
		t.Errorf("featured_at moved on extend: %v → %v", first.FeaturedAt, list[0].FeaturedAt)
	}
	if !list[0].FeaturedUntil.Equal(later) {
		t.Errorf("featured_until after extend = %v, want %v", list[0].FeaturedUntil, later)
	}

	off, err := store.UnfeatureAlbumAudited(ctx, album.UID,
		actorEntry(actor, audit.ActionAlbumUnfeature, "albums", album.UID, nil))
	if err != nil {
		t.Fatalf("UnfeatureAlbumAudited: %v", err)
	}
	if off.FeaturedUntil != nil {
		t.Errorf("featured_until after unfeature = %v, want nil", off.FeaturedUntil)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionAlbumUnfeature, actor, album.UID)
	if list, _ := store.ListFeaturedAlbums(ctx, true); len(list) != 0 {
		t.Errorf("featured after unfeature = %v, want none", featuredUIDs(list))
	}
}

// TestFeatureAlbum_expiredIsNotFeatured proves expiry needs no job: a feature
// whose end has passed is absent from the strip and reads as null on the album,
// although the stale value is still in the row.
func TestFeatureAlbum_expiredIsNotFeatured(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	album, err := store.CreateAlbum(ctx, organize.Album{Title: "Old news"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE albums SET featured_until = now() - interval '1 hour',
		        featured_at = now() - interval '2 months' WHERE uid = $1`, album.UID); err != nil {
		t.Fatalf("backdating the feature: %v", err)
	}
	list, err := store.ListFeaturedAlbums(ctx, true)
	if err != nil {
		t.Fatalf("ListFeaturedAlbums: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("featured = %v, want none (expired)", featuredUIDs(list))
	}
	got, err := store.GetAlbumByUID(ctx, album.UID)
	if err != nil {
		t.Fatalf("GetAlbumByUID: %v", err)
	}
	if got.FeaturedUntil != nil {
		t.Errorf("expired featured_until reads %v, want nil", got.FeaturedUntil)
	}

	// Featuring it again starts a fresh feature rather than inheriting the old
	// featured_at, so it lands at the front of the strip.
	if _, err := store.FeatureAlbumAudited(ctx, album.UID, time.Now().Add(time.Hour), "",
		audit.Entry{Action: audit.ActionAlbumFeature, TargetType: "albums"}); err != nil {
		t.Fatalf("re-featuring: %v", err)
	}
	list, err = store.ListFeaturedAlbums(ctx, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListFeaturedAlbums = %v (err %v), want one", featuredUIDs(list), err)
	}
	if time.Since(list[0].FeaturedAt) > time.Minute {
		t.Errorf("re-featured featured_at = %v, want ~now", list[0].FeaturedAt)
	}
}

// TestListFeaturedAlbums_privateAndOrder checks that a private album is listed
// only when the caller may see it, and that the newest feature comes first.
func TestListFeaturedAlbums_privateAndOrder(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	older, err := store.CreateAlbum(ctx, organize.Album{Title: "Older"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	newer, err := store.CreateAlbum(ctx, organize.Album{Title: "Newer"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	secret, err := store.CreateAlbum(ctx, organize.Album{Title: "Secret", Private: true})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	for i, uid := range []string{older.UID, newer.UID, secret.UID} {
		if _, err := db.Pool().Exec(ctx,
			`UPDATE albums SET featured_until = now() + interval '1 day',
			        featured_at = now() - make_interval(hours => $2) WHERE uid = $1`,
			uid, 10-i); err != nil {
			t.Fatalf("featuring %s: %v", uid, err)
		}
	}

	public, err := store.ListFeaturedAlbums(ctx, false)
	if err != nil {
		t.Fatalf("ListFeaturedAlbums(false): %v", err)
	}
	if got := featuredUIDs(public); len(got) != 2 || got[0] != newer.UID || got[1] != older.UID {
		t.Errorf("public featured = %v, want [%s %s]", got, newer.UID, older.UID)
	}
	everything, err := store.ListFeaturedAlbums(ctx, true)
	if err != nil {
		t.Fatalf("ListFeaturedAlbums(true): %v", err)
	}
	if got := featuredUIDs(everything); len(got) != 3 || got[0] != secret.UID {
		t.Errorf("curator featured = %v, want the private one first of three", got)
	}
}

// TestFeatureAlbum_rejectsNonAlbumAndMissing refuses a machine-made album and a
// missing one, writing no audit row for either.
func TestFeatureAlbum_rejectsNonAlbumAndMissing(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	month, err := store.CreateAlbum(ctx, organize.Album{Title: "January 2026", Type: organize.AlbumMonth})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	entry := audit.Entry{Action: audit.ActionAlbumFeature, TargetType: "albums"}
	until := time.Now().Add(time.Hour)
	if _, err := store.FeatureAlbumAudited(ctx, month.UID, until, "", entry); !errors.Is(err, organize.ErrNotFeaturable) {
		t.Errorf("featuring a month album: err = %v, want ErrNotFeaturable", err)
	}
	if _, err := store.FeatureAlbumAudited(ctx, "alnope", until, "", entry); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("featuring a missing album: err = %v, want ErrAlbumNotFound", err)
	}
	if _, err := store.UnfeatureAlbumAudited(ctx, "alnope", entry); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("unfeaturing a missing album: err = %v, want ErrAlbumNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionAlbumFeature)

	// The database holds the same line for a write that skips the store.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE albums SET featured_until = now() + interval '1 day' WHERE uid = $1`, month.UID); err == nil {
		t.Error("the CHECK let a month album be featured")
	}
}
