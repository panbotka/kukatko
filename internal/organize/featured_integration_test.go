//go:build integration

package organize_test

import (
	"errors"
	"slices"
	"strconv"
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

// featuredByUID returns the listed featured album with the given uid, failing
// the test when the strip does not carry it.
func featuredByUID(t *testing.T, list []organize.FeaturedAlbum, uid string) organize.FeaturedAlbum {
	t.Helper()
	for _, fa := range list {
		if fa.UID == uid {
			return fa
		}
	}
	t.Fatalf("album %q missing from the featured strip", uid)
	return organize.FeaturedAlbum{}
}

// TestListFeaturedAlbums_coverCandidates pins the cover candidates the strip
// draws its collage from: the album index's own list for the same album — same
// order, same cap — so a featured card shows the photos its tile on /albums
// shows; a hand-picked cover left out of the list; and a photo hidden from the
// library never among them, nor the derived cover.
func TestListFeaturedAlbums_coverCandidates(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	// Ten photos an hour apart, so the expected order involves no tie-break.
	base := time.Date(2016, 6, 1, 8, 0, 0, 0, time.UTC)
	newestFirst := make([]string, 0, 10)
	for i := range 10 {
		newestFirst = append(newestFirst,
			makePhotoAt(t, photoStore, "featcand"+strconv.Itoa(i), base.Add(time.Duration(i)*time.Hour)))
	}
	slices.Reverse(newestFirst)
	// The newest photo of all, but hidden from the library: the homepage never
	// headlines it.
	hidden := makePhotoAt(t, photoStore, "featcandHidden", base.Add(100*time.Hour))
	if _, err := photoStore.SetHiddenFromLibrary(ctx, hidden, true); err != nil {
		t.Fatalf("SetHiddenFromLibrary: %v", err)
	}

	full, _ := store.CreateAlbum(ctx, organize.Album{Title: "A Full"})
	pinned, _ := store.CreateAlbum(ctx, organize.Album{Title: "B Pinned"})
	small, _ := store.CreateAlbum(ctx, organize.Album{Title: "C Small"})
	hiddenOnly, _ := store.CreateAlbum(ctx, organize.Album{Title: "D Hidden"})
	emptyAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "E Empty"})
	addPhotos(t, store, full.UID, newestFirst...)
	addPhotos(t, store, pinned.UID, append(slices.Clone(newestFirst), hidden)...)
	addPhotos(t, store, small.UID, newestFirst[0], newestFirst[1])
	addPhotos(t, store, hiddenOnly.UID, hidden)
	oldest := newestFirst[len(newestFirst)-1]
	if _, err := store.SetCover(ctx, pinned.UID, &oldest); err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	until := time.Now().Add(48 * time.Hour)
	for _, uid := range []string{full.UID, pinned.UID, small.UID, hiddenOnly.UID, emptyAlbum.UID} {
		if _, err := store.FeatureAlbumAudited(ctx, uid, until, "",
			actorEntry("", audit.ActionAlbumFeature, "albums", uid, nil)); err != nil {
			t.Fatalf("FeatureAlbumAudited %s: %v", uid, err)
		}
	}

	strip, err := store.ListFeaturedAlbums(ctx, false)
	if err != nil {
		t.Fatalf("ListFeaturedAlbums: %v", err)
	}
	index, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}

	// A Full: the cap's worth of newest photos, exactly as the index lists them,
	// with the single cover the head of that list.
	want := newestFirst[:organize.CoverCandidates]
	fullGot := featuredByUID(t, strip, full.UID)
	if !slices.Equal(fullGot.CoverUIDs, want) {
		t.Errorf("full candidates = %v, want %v", fullGot.CoverUIDs, want)
	}
	if indexed := albumByUID(t, index, full.UID).CoverUIDs; !slices.Equal(fullGot.CoverUIDs, indexed) {
		t.Errorf("strip candidates %v differ from the album index's %v", fullGot.CoverUIDs, indexed)
	}
	if fullGot.CoverUID == nil || *fullGot.CoverUID != want[0] || fullGot.CoverFileHash != "featcand9" {
		t.Errorf("full cover = %v (%q), want the first candidate %q", fullGot.CoverUID, fullGot.CoverFileHash, want[0])
	}

	// B Pinned: the hand-picked cover stands for the album, stays out of the
	// candidates, and the hidden member is in neither.
	pinnedGot := featuredByUID(t, strip, pinned.UID)
	if pinnedGot.CoverUID == nil || *pinnedGot.CoverUID != oldest {
		t.Errorf("pinned cover = %v, want hand-picked %q", pinnedGot.CoverUID, oldest)
	}
	if !slices.Equal(pinnedGot.CoverUIDs, want) {
		t.Errorf("pinned candidates = %v, want %v (no hidden photo)", pinnedGot.CoverUIDs, want)
	}

	// C Small: fewer photos than the cap, fewer candidates.
	if got := featuredByUID(t, strip, small.UID); !slices.Equal(got.CoverUIDs, newestFirst[:2]) {
		t.Errorf("small candidates = %v, want %v", got.CoverUIDs, newestFirst[:2])
	}

	// D Hidden and E Empty: nothing the homepage may show — no candidates, no
	// cover — although the hidden one still counts its photo, as the index does.
	for _, uid := range []string{hiddenOnly.UID, emptyAlbum.UID} {
		got := featuredByUID(t, strip, uid)
		if len(got.CoverUIDs) != 0 || got.CoverUID != nil {
			t.Errorf("album %s candidates/cover = %v/%v, want none", got.Title, got.CoverUIDs, got.CoverUID)
		}
	}
	if got := featuredByUID(t, strip, hiddenOnly.UID).PhotoCount; got != 1 {
		t.Errorf("hidden-only photo count = %d, want 1", got)
	}
}
