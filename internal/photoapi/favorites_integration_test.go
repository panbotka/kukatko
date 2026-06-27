//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// favPhoto is the per-photo shape the favorites tests decode: just the UID and the
// current user's is_favorite flag carried alongside every photo field.
type favPhoto struct {
	UID        string `json:"uid"`
	IsFavorite bool   `json:"is_favorite"`
}

// favListResp mirrors the list/favorites endpoints for the favorites tests,
// decoding only the fields the assertions need.
type favListResp struct {
	Photos []favPhoto `json:"photos"`
	Total  int        `json:"total"`
}

// favoriteURL builds the per-photo favorite endpoint URL.
func favoriteURL(base, uid string) string {
	return base + "/api/v1/photos/" + uid + "/favorite"
}

// getFavList fetches a list-shaped endpoint (the path includes the query) and
// decodes it into favListResp, failing on a non-200 status.
func getFavList(t *testing.T, client *http.Client, urlStr string) favListResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, urlStr, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", urlStr, resp.StatusCode)
	}
	var out favListResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list %s: %v", urlStr, err)
	}
	return out
}

// getFavFlag fetches a photo's detail and returns its is_favorite flag.
func getFavFlag(t *testing.T, client *http.Client, base, uid string) bool {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var out favPhoto
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return out.IsFavorite
}

// mustStatus performs a request and asserts the response status, closing the body.
func mustStatus(t *testing.T, client *http.Client, method, urlStr string, want int) {
	t.Helper()
	resp := mustDo(t, client, method, urlStr, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("%s %s status = %d, want %d", method, urlStr, resp.StatusCode, want)
	}
}

// favUIDs collects the UIDs of a favorites list into a set.
func favUIDs(list favListResp) map[string]bool {
	set := make(map[string]bool, len(list.Photos))
	for _, p := range list.Photos {
		set[p.UID] = true
	}
	return set
}

// TestFavoritesAPI exercises the per-user favorites endpoints end to end:
// idempotent favorite/unfavorite, the is_favorite flag in list and detail, the
// favorite=true filter, the /favorites listing, and per-user isolation.
func TestFavoritesAPI(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "fav_alice", auth.RoleViewer)
	bob, _ := e.login(t, "fav_bob", auth.RoleEditor)

	p1 := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)
	p2 := e.seedPhoto(t, photos.Photo{Title: "two"}, "2.jpg", 40, 50, 60)
	p3 := e.seedPhoto(t, photos.Photo{Title: "three"}, "3.jpg", 70, 80, 90)

	t.Run("favorite is idempotent and reflected in detail", func(t *testing.T) {
		mustStatus(t, alice, http.MethodPut, favoriteURL(e.server.URL, p1.UID), http.StatusNoContent)
		// Favoriting again still succeeds (idempotent).
		mustStatus(t, alice, http.MethodPut, favoriteURL(e.server.URL, p1.UID), http.StatusNoContent)
		if !getFavFlag(t, alice, e.server.URL, p1.UID) {
			t.Error("p1 is_favorite = false for alice after favorite, want true")
		}
		if getFavFlag(t, alice, e.server.URL, p2.UID) {
			t.Error("p2 is_favorite = true for alice, want false")
		}
	})

	t.Run("favorite=true filter scopes to the caller", func(t *testing.T) {
		mustStatus(t, alice, http.MethodPut, favoriteURL(e.server.URL, p3.UID), http.StatusNoContent)
		got := getFavList(t, alice, e.server.URL+"/api/v1/photos?favorite=true")
		set := favUIDs(got)
		if got.Total != 2 || !set[p1.UID] || !set[p3.UID] || set[p2.UID] {
			t.Fatalf("favorite filter = %v (total %d), want {p1,p3}", set, got.Total)
		}
		for _, p := range got.Photos {
			if !p.IsFavorite {
				t.Errorf("photo %s in favorite filter has is_favorite false", p.UID)
			}
		}
	})

	t.Run("favorites listing matches the filter and honours pagination", func(t *testing.T) {
		got := getFavList(t, alice, e.server.URL+"/api/v1/favorites?limit=1")
		if got.Total != 2 || len(got.Photos) != 1 {
			t.Fatalf("/favorites?limit=1 = %d photos (total %d), want 1 of 2", len(got.Photos), got.Total)
		}
	})

	t.Run("per-user isolation", func(t *testing.T) {
		// Bob has favorited nothing; alice's favorites are invisible to him.
		bobList := getFavList(t, bob, e.server.URL+"/api/v1/favorites")
		if bobList.Total != 0 || len(bobList.Photos) != 0 {
			t.Fatalf("bob favorites = %d, want 0 (isolation)", bobList.Total)
		}
		if getFavFlag(t, bob, e.server.URL, p1.UID) {
			t.Error("p1 is_favorite = true for bob, want false (isolation)")
		}
		// Bob favorites p2; that must not leak into alice's view.
		mustStatus(t, bob, http.MethodPut, favoriteURL(e.server.URL, p2.UID), http.StatusNoContent)
		if getFavFlag(t, alice, e.server.URL, p2.UID) {
			t.Error("p2 is_favorite = true for alice after bob favorited it, want false")
		}
	})

	t.Run("unfavorite is idempotent and clears the flag", func(t *testing.T) {
		mustStatus(t, alice, http.MethodDelete, favoriteURL(e.server.URL, p1.UID), http.StatusNoContent)
		// Removing again still succeeds (idempotent).
		mustStatus(t, alice, http.MethodDelete, favoriteURL(e.server.URL, p1.UID), http.StatusNoContent)
		if getFavFlag(t, alice, e.server.URL, p1.UID) {
			t.Error("p1 is_favorite = true for alice after unfavorite, want false")
		}
		got := getFavList(t, alice, e.server.URL+"/api/v1/favorites")
		if set := favUIDs(got); got.Total != 1 || !set[p3.UID] || set[p1.UID] {
			t.Fatalf("alice favorites after unfavorite = %v (total %d), want {p3}", set, got.Total)
		}
	})

	t.Run("favoriting a missing photo is 404", func(t *testing.T) {
		mustStatus(t, alice, http.MethodPut, favoriteURL(e.server.URL, "ph_missing"), http.StatusNotFound)
	})
}
