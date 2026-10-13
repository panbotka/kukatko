//go:build integration

package organizeapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
)

// featuredList is the decoded body of GET /albums/featured.
type featuredList struct {
	Albums []struct {
		UID           string     `json:"uid"`
		Title         string     `json:"title"`
		PhotoCount    int        `json:"photo_count"`
		FeaturedUntil *time.Time `json:"featured_until"`
		ThumbURL      string     `json:"thumb_url"`
	} `json:"albums"`
	DefaultDays int `json:"default_days"`
	MaxDays     int `json:"max_days"`
}

// listFeatured fetches the homepage's featured albums as c sees them.
func (e *env) listFeatured(t *testing.T, c *http.Client) featuredList {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, apiV+"/albums/featured", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /albums/featured = %d, want 200", resp.StatusCode)
	}
	var out featuredList
	decodeBody(t, resp, &out)
	return out
}

// createAlbum creates an album through the API as c and returns it.
func (e *env) createAlbum(t *testing.T, c *http.Client, body string) organize.Album {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodPost, apiV+"/albums", []byte(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /albums %s = %d, want 201", body, resp.StatusCode)
	}
	var album organize.Album
	decodeBody(t, resp, &album)
	return album
}

// status issues a request and returns only its status code.
func (e *env) status(t *testing.T, c *http.Client, method, path, body string) int {
	t.Helper()
	resp := e.mustDo(t, c, method, path, bodyOf(body))
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestFeatured_overHTTP walks the whole surface through the real auth stack: a
// viewer may read the strip but not feature; a curator features, extends and
// unfeatures, each audited; a private album shows on the strip only for those
// who curate; a machine-made album and a date beyond the year are refused.
func TestFeatured_overHTTP(t *testing.T) {
	env := newEnv(t)
	curator := env.login(t, "curator", auth.AccountCurator)
	viewer := env.login(t, "viewer", auth.AccountViewer)
	photo := env.seedPhoto(t, "feat1")

	album := env.createAlbum(t, curator, `{"title":"Pouť 2026"}`)
	secret := env.createAlbum(t, curator, `{"title":"Rodinné","private":true}`)
	month := env.createAlbum(t, curator, `{"title":"January 2026","type":"month"}`)
	if got := env.status(t, curator, http.MethodPost, apiV+"/albums/"+album.UID+"/photos",
		`{"photo_uids":["`+photo+`"]}`); got != http.StatusOK {
		t.Fatalf("adding the photo = %d", got)
	}

	path := apiV + "/albums/" + album.UID + "/featured"
	if got := env.status(t, viewer, http.MethodPut, path, ""); got != http.StatusForbidden {
		t.Errorf("viewer PUT featured = %d, want 403", got)
	}
	if got := env.status(t, curator, http.MethodPut, path, ""); got != http.StatusOK {
		t.Fatalf("curator PUT featured = %d, want 200", got)
	}
	if got := env.status(t, curator, http.MethodPut, apiV+"/albums/"+secret.UID+"/featured", ""); got != http.StatusOK {
		t.Fatalf("curator PUT featured (private) = %d, want 200", got)
	}

	// The viewer sees the public album only; the curator sees both.
	seen := env.listFeatured(t, viewer)
	if len(seen.Albums) != 1 || seen.Albums[0].UID != album.UID {
		t.Fatalf("viewer featured = %+v, want only %s", seen.Albums, album.UID)
	}
	first := seen.Albums[0]
	if first.PhotoCount != 1 || first.ThumbURL == "" || first.FeaturedUntil == nil {
		t.Errorf("featured album = %+v, want count 1, a thumb and an end", first)
	}
	if seen.DefaultDays != 61 || seen.MaxDays != 366 {
		t.Errorf("bounds = %d/%d, want 61/366", seen.DefaultDays, seen.MaxDays)
	}
	if wantAbout := time.Now().Add(61 * 24 * time.Hour); first.FeaturedUntil.Sub(wantAbout).Abs() > time.Minute {
		t.Errorf("default end = %v, want ~%v", first.FeaturedUntil, wantAbout)
	}
	if got := env.listFeatured(t, curator); len(got.Albums) != 2 {
		t.Errorf("curator featured = %+v, want both", got.Albums)
	}

	// Extending re-dates it; the album payload carries the new end.
	until := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	resp := env.mustDo(t, curator, http.MethodPut, path, []byte(`{"until":"`+until.Format(time.RFC3339)+`"}`))
	var extended organize.Album
	decodeBody(t, resp, &extended)
	if extended.FeaturedUntil == nil || !extended.FeaturedUntil.Equal(until) {
		t.Errorf("extended featured_until = %v, want %v", extended.FeaturedUntil, until)
	}

	tooFar := time.Now().Add(400 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if got := env.status(t, curator, http.MethodPut, path, `{"until":"`+tooFar+`"}`); got != http.StatusBadRequest {
		t.Errorf("PUT a date beyond a year = %d, want 400", got)
	}
	if got := env.status(t, curator, http.MethodPut, apiV+"/albums/"+month.UID+"/featured", ""); got != http.StatusBadRequest {
		t.Errorf("PUT featured on a month album = %d, want 400", got)
	}

	if got := env.status(t, viewer, http.MethodDelete, path, ""); got != http.StatusForbidden {
		t.Errorf("viewer DELETE featured = %d, want 403", got)
	}
	if got := env.status(t, curator, http.MethodDelete, path, ""); got != http.StatusOK {
		t.Errorf("curator DELETE featured = %d, want 200", got)
	}
	if got := env.listFeatured(t, viewer); len(got.Albums) != 0 {
		t.Errorf("viewer featured after unfeature = %+v, want none", got.Albums)
	}

	// Three successful features (public, private, the extension) and one removal
	// are on the trail; the refused requests left nothing.
	assertAuditCount(t, env, audit.ActionAlbumFeature, 3)
	assertAuditCount(t, env, audit.ActionAlbumUnfeature, 1)
}

// assertAuditCount fails unless the trail holds exactly want rows for action.
func assertAuditCount(t *testing.T, e *env, action string, want int) {
	t.Helper()
	recs, err := e.audit.List(t.Context(), audit.Filter{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit %s: %v", action, err)
	}
	if len(recs) != want {
		t.Errorf("audit %s rows = %d, want %d", action, len(recs), want)
	}
}
