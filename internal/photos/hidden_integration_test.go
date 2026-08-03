//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// hiddenLibrary is the fixture the visibility tests share: one ordinary photo
// and one scan hidden from the library, the scan filed in an album, on a label
// and in the user's favorites — the three places it must still be visible.
type hiddenLibrary struct {
	store   *photos.Store
	db      *database.DB
	visible photos.Photo
	scan    photos.Photo
	album   organize.Album
	label   organize.Label
	user    string
}

// newHiddenLibrary builds the shared fixture against the real test database.
func newHiddenLibrary(t *testing.T) hiddenLibrary {
	t.Helper()
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()
	taken := time.Date(2024, 3, 7, 9, 0, 0, 0, time.UTC)

	lib := hiddenLibrary{store: store, db: db, user: "u_hidden"}
	lib.visible = mustCreate(t, store, photos.Photo{
		FileHash: "h-visible", FilePath: "2024/03/v.jpg", FileName: "v.jpg", FileMime: "image/jpeg",
		Title: "sunset", TakenAt: &taken, TakenAtSource: "exif",
	})
	lib.scan = mustCreate(t, store, photos.Photo{
		FileHash: "h-scan", FilePath: "2024/03/s.jpg", FileName: "s.jpg", FileMime: "image/jpeg",
		Title: "tiraz", TakenAt: &taken, TakenAtSource: "exif", HiddenFromLibrary: true,
	})

	var err error
	if lib.album, err = org.CreateAlbum(ctx, organize.Album{Title: "Documents"}); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if err = org.AddPhoto(ctx, lib.album.UID, lib.scan.UID); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}
	if lib.label, err = org.CreateLabel(ctx, organize.Label{Name: "Scans"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err = org.AttachLabel(ctx, lib.scan.UID, lib.label.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}
	mustCreateUser(t, db, lib.user)
	if err = org.AddFavorite(ctx, lib.user, lib.scan.UID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	return lib
}

// mustCreateUser inserts the account the per-user favorites scope needs.
func mustCreateUser(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	if _, err := db.Pool().Exec(t.Context(),
		`INSERT INTO users (uid, username, password_hash, role) VALUES ($1, $2, 'x', 'editor')`,
		uid, uid); err != nil {
		t.Fatalf("insert user %s: %v", uid, err)
	}
}

// TestHiddenFromLibrary_absentFromTheLibrary verifies a hidden photo is missing
// from every listing the library firehose is made of: the grid, its count, the
// full-text search and both bucket queries. The buckets matter as much as the
// grid — a year that claims photos the grid will not show is a scrollbar that
// lies.
func TestHiddenFromLibrary_absentFromTheLibrary(t *testing.T) {
	lib := newHiddenLibrary(t)
	ctx := t.Context()

	list, err := lib.store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if set := uidSet(list); len(set) != 1 || !set[lib.visible.UID] {
		t.Errorf("List = %v, want only the visible photo", set)
	}

	total, err := lib.store.Count(ctx, photos.ListParams{})
	if err != nil || total != 1 {
		t.Errorf("Count = %d, %v, want 1", total, err)
	}

	found, err := lib.store.Search(ctx, photos.ListParams{FullText: "tiraz"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("Search(tiraz) = %v, want no hidden photo", uidSet(found))
	}
	// The visible photo's title is indexed the same way, so this is what keeps the
	// assertion above from passing for the wrong reason (a search that matches
	// nothing at all).
	found, err = lib.store.Search(ctx, photos.ListParams{FullText: "sunset"})
	if err != nil {
		t.Fatalf("Search(sunset): %v", err)
	}
	if set := uidSet(found); len(set) != 1 || !set[lib.visible.UID] {
		t.Errorf("Search(sunset) = %v, want the visible photo", set)
	}

	years, err := lib.store.YearBuckets(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("YearBuckets: %v", err)
	}
	if years.Total != 1 || len(years.Years) != 1 || years.Years[0].Count != 1 {
		t.Errorf("YearBuckets = %+v, want one year holding one photo", years)
	}

	timeline, err := lib.store.TimelineBuckets(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("TimelineBuckets: %v", err)
	}
	if timeline.Total != 1 || len(timeline.Buckets) != 1 || timeline.Buckets[0].Count != 1 {
		t.Errorf("TimelineBuckets = %+v, want one month holding one photo", timeline)
	}
}

// TestHiddenFromLibrary_visibleWhereItWasFiled verifies the automatic lift: a
// listing scoped to an album, a label or the caller's favorites shows the hidden
// photo, because filing it there was a deliberate act. It also pins that the
// scope does not become a back door into the whole library.
func TestHiddenFromLibrary_visibleWhereItWasFiled(t *testing.T) {
	lib := newHiddenLibrary(t)
	ctx := t.Context()

	tests := []struct {
		name   string
		params photos.ListParams
	}{
		{name: "album", params: photos.ListParams{AlbumUIDs: []string{lib.album.UID}}},
		{name: "label", params: photos.ListParams{LabelUIDs: []string{lib.label.UID}}},
		{name: "favorites", params: photos.ListParams{FavoriteOf: lib.user}},
		{name: "explicit include", params: photos.ListParams{IncludeHidden: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := lib.store.List(ctx, tt.params)
			if err != nil {
				t.Fatalf("List(%s): %v", tt.name, err)
			}
			if !uidSet(list)[lib.scan.UID] {
				t.Errorf("List(%s) = %v, want the hidden photo present", tt.name, uidSet(list))
			}
			total, err := lib.store.Count(ctx, tt.params)
			if err != nil {
				t.Fatalf("Count(%s): %v", tt.name, err)
			}
			if total != len(list) {
				t.Errorf("Count(%s) = %d, want %d to match the page", tt.name, total, len(list))
			}
		})
	}
}

// TestHiddenFromLibrary_reachableByUID verifies the direct link keeps working: a
// hidden photo is not a deleted photo, and the URL the user pasted must still
// open it.
func TestHiddenFromLibrary_reachableByUID(t *testing.T) {
	lib := newHiddenLibrary(t)

	got, err := lib.store.GetByUID(t.Context(), lib.scan.UID)
	if err != nil {
		t.Fatalf("GetByUID(hidden): %v", err)
	}
	if !got.HiddenFromLibrary {
		t.Error("GetByUID(hidden).HiddenFromLibrary = false, want true")
	}
}

// TestHiddenFromLibrary_queryLanguage verifies hidden:yes finds the hidden photo
// and nothing else, and hidden:no the visible one — the documented way back, and
// the reason the default filter has to yield to an explicit hidden:.
func TestHiddenFromLibrary_queryLanguage(t *testing.T) {
	lib := newHiddenLibrary(t)
	ctx := t.Context()

	tests := []struct {
		input string
		want  string
	}{
		{input: "hidden:yes", want: lib.scan.UID},
		{input: "hidden:no", want: lib.visible.UID},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := query.Parse(tt.input)
			list, err := lib.store.List(ctx, photos.ListParams{QueryFilters: parsed.Filters})
			if err != nil {
				t.Fatalf("List(%s): %v", tt.input, err)
			}
			set := uidSet(list)
			if len(set) != 1 || !set[tt.want] {
				t.Errorf("List(%s) = %v, want exactly %s", tt.input, set, tt.want)
			}
		})
	}
}

// TestHiddenFromLibrary_placesExcludesHidden verifies the places hierarchy drops
// hidden photos too. It builds its WHERE clause by hand rather than going
// through buildListQuery, so it is the one that silently regresses.
func TestHiddenFromLibrary_placesExcludesHidden(t *testing.T) {
	lib := newHiddenLibrary(t)
	ctx := t.Context()

	for _, uid := range []string{lib.visible.UID, lib.scan.UID} {
		if _, err := lib.db.Pool().Exec(ctx,
			`INSERT INTO photo_places (photo_uid, country, city) VALUES ($1, 'Czechia', 'Praha')`,
			uid); err != nil {
			t.Fatalf("insert place for %s: %v", uid, err)
		}
	}

	places, err := lib.store.AggregatePlaces(ctx, "")
	if err != nil {
		t.Fatalf("AggregatePlaces: %v", err)
	}
	if len(places) != 1 || places[0].Count != 1 {
		t.Fatalf("AggregatePlaces = %+v, want one country holding one photo", places)
	}
}

// TestHiddenFromLibrary_setToggles verifies SetHiddenFromLibrary flips the flag
// both ways and that the photo re-enters the library when it is cleared.
func TestHiddenFromLibrary_setToggles(t *testing.T) {
	lib := newHiddenLibrary(t)
	ctx := t.Context()

	shown, err := lib.store.SetHiddenFromLibrary(ctx, lib.scan.UID, false)
	if err != nil {
		t.Fatalf("SetHiddenFromLibrary(false): %v", err)
	}
	if shown.HiddenFromLibrary {
		t.Error("SetHiddenFromLibrary(false) left the photo hidden")
	}
	total, err := lib.store.Count(ctx, photos.ListParams{})
	if err != nil || total != 2 {
		t.Errorf("Count after unhide = %d, %v, want 2", total, err)
	}

	if _, err = lib.store.SetHiddenFromLibrary(ctx, lib.visible.UID, true); err != nil {
		t.Fatalf("SetHiddenFromLibrary(true): %v", err)
	}
	total, err = lib.store.Count(ctx, photos.ListParams{})
	if err != nil || total != 1 {
		t.Errorf("Count after hide = %d, %v, want 1", total, err)
	}

	if _, err = lib.store.SetHiddenFromLibrary(ctx, "nope", true); err == nil {
		t.Error("SetHiddenFromLibrary on a missing photo returned no error")
	}
}
