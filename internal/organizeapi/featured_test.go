package organizeapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
)

// fakeFeatured is an in-memory FeaturedStore recording what it was asked.
type fakeFeatured struct {
	list       []organize.FeaturedAlbum
	featureErr error

	lastUntil          time.Time
	lastEntry          audit.Entry
	lastIncludePrivate bool
	unfeaturedUID      string
}

func (f *fakeFeatured) FeatureAlbumAudited(
	_ context.Context, _ string, uid string, until time.Time, _ string, entry audit.Entry,
) (organize.Album, error) {
	f.lastUntil, f.lastEntry = until, entry
	if f.featureErr != nil {
		return organize.Album{}, f.featureErr
	}
	return organize.Album{UID: uid, FeaturedUntil: &until}, nil
}

func (f *fakeFeatured) UnfeatureAlbumAudited(
	_ context.Context, _ string, uid string, entry audit.Entry,
) (organize.Album, error) {
	f.unfeaturedUID, f.lastEntry = uid, entry
	return organize.Album{UID: uid}, nil
}

func (f *fakeFeatured) ListFeaturedAlbums(_ context.Context, _ string, includePrivate bool) ([]organize.FeaturedAlbum, error) {
	f.lastIncludePrivate = includePrivate
	return f.list, nil
}

// featuredNow is the fixed clock the featured handler tests run on.
var featuredNow = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// newFeaturedServer mounts an API with a featured store behind pass-through
// guards, on a fixed clock and the given default length.
func newFeaturedServer(albums *fakeAlbums, featured *fakeFeatured, defaultDays int) http.Handler {
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:              albums,
		Labels:              &fakeLabels{},
		Featured:            featured,
		FeaturedDefaultDays: defaultDays,
		Now:                 func() time.Time { return featuredNow },
		RequireAuth:         passThrough,
		RequireCurator:      passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// TestAlbumFeature_defaultsUntil features for the configured default length
// when the body is empty or names no end, and audits the change.
func TestAlbumFeature_defaultsUntil(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "{}", "  "} {
		featured := &fakeFeatured{}
		albums := &fakeAlbums{album: organize.Album{UID: "al_1", Title: "Pouť"}}
		rec := do(t, newFeaturedServer(albums, featured, 61), http.MethodPut, "/albums/al_1/featured", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("body %q: status = %d, want 200 (%s)", body, rec.Code, rec.Body)
		}
		if want := featuredNow.Add(61 * 24 * time.Hour); !featured.lastUntil.Equal(want) {
			t.Errorf("body %q: until = %v, want %v", body, featured.lastUntil, want)
		}
		if featured.lastEntry.Action != audit.ActionAlbumFeature || featured.lastEntry.TargetUID != "al_1" {
			t.Errorf("audit entry = %+v, want album.feature on al_1", featured.lastEntry)
		}
	}
}

// TestAlbumFeature_fallbackDefault replaces an unusable configured length with
// the two-month default.
func TestAlbumFeature_fallbackDefault(t *testing.T) {
	t.Parallel()
	featured := &fakeFeatured{}
	albums := &fakeAlbums{album: organize.Album{UID: "al_1"}}
	rec := do(t, newFeaturedServer(albums, featured, 5000), http.MethodPut, "/albums/al_1/featured", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if want := featuredNow.Add(61 * 24 * time.Hour); !featured.lastUntil.Equal(want) {
		t.Errorf("until = %v, want %v", featured.lastUntil, want)
	}
}

// TestAlbumFeature_validatesUntil accepts an end inside the year and refuses one
// in the past or beyond MaxFeaturedDays, plus a malformed body.
func TestAlbumFeature_validatesUntil(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want int
	}{
		{"tomorrow", `{"until":"2026-10-12T21:59:59Z"}`, http.StatusOK},
		{"last day", `{"until":"2027-10-11T21:59:59Z"}`, http.StatusOK},
		{"past", `{"until":"2026-10-10T12:00:00Z"}`, http.StatusBadRequest},
		{"now", `{"until":"2026-10-11T12:00:00Z"}`, http.StatusBadRequest},
		{"too far", `{"until":"2027-10-13T12:00:00Z"}`, http.StatusBadRequest},
		{"garbage", `{"until":"soon"}`, http.StatusBadRequest},
		{"unknown field", `{"forever":true}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		featured := &fakeFeatured{}
		albums := &fakeAlbums{album: organize.Album{UID: "al_1"}}
		rec := do(t, newFeaturedServer(albums, featured, 61), http.MethodPut, "/albums/al_1/featured", tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body)
		}
	}
}

// TestAlbumFeature_errors maps a non-album type to 400 and a missing album to
// 404.
func TestAlbumFeature_errors(t *testing.T) {
	t.Parallel()
	featured := &fakeFeatured{featureErr: organize.ErrNotFeaturable}
	albums := &fakeAlbums{album: organize.Album{UID: "al_1"}}
	rec := do(t, newFeaturedServer(albums, featured, 61), http.MethodPut, "/albums/al_1/featured", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-album: status = %d, want 400", rec.Code)
	}

	missing := &fakeAlbums{getErr: organize.ErrAlbumNotFound}
	rec = do(t, newFeaturedServer(missing, &fakeFeatured{}, 61), http.MethodPut, "/albums/al_x/featured", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing PUT: status = %d, want 404", rec.Code)
	}
	rec = do(t, newFeaturedServer(missing, &fakeFeatured{}, 61), http.MethodDelete, "/albums/al_x/featured", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing DELETE: status = %d, want 404", rec.Code)
	}
}

// TestAlbumUnfeature_ok takes the album off the homepage and audits it.
func TestAlbumUnfeature_ok(t *testing.T) {
	t.Parallel()
	featured := &fakeFeatured{}
	albums := &fakeAlbums{album: organize.Album{UID: "al_1", Title: "Pouť"}}
	rec := do(t, newFeaturedServer(albums, featured, 61), http.MethodDelete, "/albums/al_1/featured", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if featured.unfeaturedUID != "al_1" || featured.lastEntry.Action != audit.ActionAlbumUnfeature {
		t.Errorf("unfeatured %q with %+v", featured.unfeaturedUID, featured.lastEntry)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if v, ok := body["featured_until"]; !ok || v != nil {
		t.Errorf("featured_until = %v (present %v), want an explicit null", v, ok)
	}
}

// TestFeaturedList_ok lists the featured albums with a thumbnail URL for each
// cover, the bounds the dialog needs, and no private albums for a caller with no
// curator role (here: no principal at all).
func TestFeaturedList_ok(t *testing.T) {
	t.Parallel()
	cover := "ph_cover"
	until := featuredNow.Add(time.Hour)
	featured := &fakeFeatured{list: []organize.FeaturedAlbum{
		{
			Album:      organize.Album{UID: "al_1", Title: "Pouť", FeaturedUntil: &until},
			PhotoCount: 3, CoverUID: &cover, CoverFileHash: "abc",
			CoverUIDs: []string{"ph_cover", "ph_b", "ph_c", "ph_d", "ph_e"},
		},
		{Album: organize.Album{UID: "al_2", Title: "Empty", FeaturedUntil: &until}},
	}}
	rec := do(t, newFeaturedServer(&fakeAlbums{}, featured, 30), http.MethodGet, "/albums/featured", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if featured.lastIncludePrivate {
		t.Error("an anonymous caller was shown private albums")
	}
	var body struct {
		Albums []struct {
			UID       string   `json:"uid"`
			ThumbURL  string   `json:"thumb_url"`
			CoverHash string   `json:"cover_file_hash"`
			CoverUIDs []string `json:"cover_uids"`
			Count     int      `json:"photo_count"`
		} `json:"albums"`
		DefaultDays int `json:"default_days"`
		MaxDays     int `json:"max_days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Albums) != 2 || body.DefaultDays != 30 || body.MaxDays != organizeapi.MaxFeaturedDays {
		t.Fatalf("body = %+v", body)
	}
	if !strings.HasSuffix(body.Albums[0].ThumbURL, "/photos/ph_cover/thumb/tile_500") {
		t.Errorf("thumb_url = %q, want the cover's grid thumbnail", body.Albums[0].ThumbURL)
	}
	// The collage's candidates travel as the album index sends them; an album
	// with none omits the list.
	if want := []string{"ph_cover", "ph_b", "ph_c", "ph_d", "ph_e"}; !slices.Equal(body.Albums[0].CoverUIDs, want) {
		t.Errorf("cover_uids = %v, want %v", body.Albums[0].CoverUIDs, want)
	}
	if body.Albums[1].CoverUIDs != nil {
		t.Errorf("empty album cover_uids = %v, want absent", body.Albums[1].CoverUIDs)
	}
	if body.Albums[0].Count != 3 || body.Albums[1].ThumbURL != "" {
		t.Errorf("albums = %+v", body.Albums)
	}
	if strings.Contains(rec.Body.String(), "abc") {
		t.Error("the cover's file hash leaked into the payload")
	}
}

// TestFeaturedRoutes_absentWithoutStore leaves the featured routes unmounted
// when no featured store is configured.
func TestFeaturedRoutes_absentWithoutStore(t *testing.T) {
	t.Parallel()
	albums := &fakeAlbums{album: organize.Album{UID: "al_1"}}
	rec := do(t, newServer(albums, &fakeLabels{}), http.MethodPut, "/albums/al_1/featured", "")
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404/405 with no featured store", rec.Code)
	}
}
