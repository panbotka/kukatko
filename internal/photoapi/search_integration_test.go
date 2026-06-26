//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// getSearch fetches the search endpoint with the given query string and decodes
// the body, asserting a 200.
func getSearch(t *testing.T, client *http.Client, base, query string) listResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/search?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out listResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	return out
}

// TestSearch_endpoint exercises the full-text search endpoint: diacritics-
// insensitive matching, ranking by relevance, filter combination and the
// required-query and pagination behaviour.
func TestSearch_endpoint(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	// The `simple` dictionary does no stemming, so the matching token must appear
	// verbatim (after unaccent): "tomas" matches "Tomáš", not "Tomášem".
	titleHit := env.seedPhoto(t, photos.Photo{Title: "Tomáš na výletě"}, "trip.jpg", 200, 10, 10)
	notesHit := env.seedPhoto(t, photos.Photo{Title: "Výlet", Notes: "v lese byl Tomáš"}, "forest.jpg", 10, 200, 10)
	env.seedPhoto(t, photos.Photo{Title: "Praha"}, "prague.jpg", 10, 10, 200)

	t.Run("diacritics-insensitive and ranked", func(t *testing.T) {
		got := getSearch(t, client, base, "q=tomas")
		if got.Total != 2 || len(got.Photos) != 2 {
			t.Fatalf("search total=%d len=%d, want 2/2", got.Total, len(got.Photos))
		}
		// Title hit (weight A) ranks above the notes hit (weight C).
		if got.Photos[0].UID != titleHit.UID || got.Photos[1].UID != notesHit.UID {
			t.Fatalf("rank order = %v, want title %s before notes %s",
				uids(got.Photos), titleHit.UID, notesHit.UID)
		}
	})

	t.Run("file_name token is searchable", func(t *testing.T) {
		got := getSearch(t, client, base, "q=prague")
		if got.Total != 1 {
			t.Fatalf("search(prague) total=%d, want 1", got.Total)
		}
	})

	t.Run("missing query is 400", func(t *testing.T) {
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/search", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("search without q status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("blank query is 400", func(t *testing.T) {
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/search?q=%20%20", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("search with blank q status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("paginates with next_offset", func(t *testing.T) {
		page := getSearch(t, client, base, "q=tomas&limit=1")
		if page.Total != 2 || len(page.Photos) != 1 {
			t.Fatalf("page total=%d len=%d, want 2/1", page.Total, len(page.Photos))
		}
		if page.NextOffset == nil || *page.NextOffset != 1 {
			t.Fatalf("next_offset = %v, want 1", page.NextOffset)
		}
		last := getSearch(t, client, base, "q=tomas&limit=1&offset=1")
		if len(last.Photos) != 1 || last.NextOffset != nil {
			t.Fatalf("last page len=%d next=%v, want 1/nil", len(last.Photos), last.NextOffset)
		}
	})
}

// TestSearch_combinedFilter verifies the search endpoint applies list filters
// alongside the full-text query.
func TestSearch_combinedFilter(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer2", auth.RoleViewer)
	base := env.server.URL

	publicMatch := env.seedPhoto(t, photos.Photo{Title: "beach holiday"}, "pub.jpg", 200, 10, 10)
	private := env.seedPhoto(t, photos.Photo{Title: "beach sunset", Private: true}, "priv.jpg", 10, 200, 10)

	pubOnly := getSearch(t, client, base, "q=beach&private=false")
	if pubOnly.Total != 1 || len(pubOnly.Photos) != 1 || pubOnly.Photos[0].UID != publicMatch.UID {
		t.Fatalf("search(beach, private=false) = %v, want [%s]", uids(pubOnly.Photos), publicMatch.UID)
	}
	privOnly := getSearch(t, client, base, "q=beach&private=true")
	if privOnly.Total != 1 || len(privOnly.Photos) != 1 || privOnly.Photos[0].UID != private.UID {
		t.Fatalf("search(beach, private=true) = %v, want [%s]", uids(privOnly.Photos), private.UID)
	}
}
