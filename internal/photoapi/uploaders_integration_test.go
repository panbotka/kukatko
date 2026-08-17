//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// uploadersResp mirrors the uploader facet's JSON body.
type uploadersResp struct {
	Uploaders []struct {
		UID   string `json:"uid"`
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"uploaders"`
}

// getUploaders fetches the uploader facet with the given query and decodes the
// body, failing the test on a non-200 status.
func getUploaders(t *testing.T, client *http.Client, base, query string) uploadersResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/uploaders?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uploaders status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out uploadersResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode uploaders: %v", err)
	}
	return out
}

// uploaderCounts flattens the facet into a uid -> count map for
// order-independent assertions; the slice order is asserted separately.
func uploaderCounts(resp uploadersResp) map[string]int {
	out := make(map[string]int, len(resp.Uploaders))
	for _, bucket := range resp.Uploaders {
		out[bucket.UID] = bucket.Count
	}
	return out
}

// seedUploader creates an account with a display name and returns its UID, so a
// test can attribute uploads to it and then find it in the facet by name.
func seedUploader(t *testing.T, e *env, username, displayName string) string {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, DisplayName: displayName, Password: testPassword, Role: auth.RoleEditor,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return user.UID
}

// TestUploaders exercises the uploader facet and the filters it feeds against a
// real database: who contributed to the current view, the imported photos as a
// group of their own, the album scope that is the whole point of the feature,
// and the facet refusing to narrow its own option list.
func TestUploaders(t *testing.T) {
	env := newEnv(t)
	// The caller is an uploader too, so `uploader:me` has something to find.
	annaUID := seedUploader(t, env, "anna", "Anna Nováková")
	client, _ := env.loginAs(t, "anna")
	tomasUID := seedUploader(t, env, "tomas", "Tomáš Novák")
	base := env.server.URL

	// Two contributors to one event, plus an imported photo nobody uploaded.
	annaWedding := env.seedPhoto(t, photos.Photo{Title: "Anna 1", UploadedBy: &annaUID},
		"anna1.jpg", 10, 20, 30)
	tomasWedding := env.seedPhoto(t, photos.Photo{Title: "Tomáš 1", UploadedBy: &tomasUID},
		"tomas1.jpg", 40, 50, 60)
	tomasOther := env.seedPhoto(t, photos.Photo{Title: "Tomáš 2", UploadedBy: &tomasUID},
		"tomas2.jpg", 70, 80, 90)
	tomasThird := env.seedPhoto(t, photos.Photo{Title: "Tomáš 3", UploadedBy: &tomasUID},
		"tomas3.jpg", 100, 110, 120)
	imported := env.seedPhoto(t, photos.Photo{Title: "Imported"}, "imported.jpg", 130, 140, 150)

	album, err := env.organize.CreateAlbum(t.Context(), organize.Album{Title: "Svatba"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	for _, uid := range []string{annaWedding.UID, tomasWedding.UID, imported.UID} {
		if err := env.organize.AddPhoto(t.Context(), album.UID, uid); err != nil {
			t.Fatalf("AddPhoto(%s): %v", uid, err)
		}
	}

	t.Run("every contributor is offered, largest first, imported included", func(t *testing.T) {
		got := getUploaders(t, client, base, "")
		if len(got.Uploaders) != 3 {
			t.Fatalf("uploaders = %+v, want 3 buckets (tomas, anna, imported)", got.Uploaders)
		}
		if got.Uploaders[0].UID != tomasUID || got.Uploaders[0].Count != 3 {
			t.Errorf("uploaders[0] = %+v, want %s count 3", got.Uploaders[0], tomasUID)
		}
		if got.Uploaders[0].Name != "Tomáš Novák" {
			t.Errorf("uploader name = %q, want the display name", got.Uploaders[0].Name)
		}
		counts := uploaderCounts(got)
		if counts[annaUID] != 1 {
			t.Errorf("anna's count = %d, want 1", counts[annaUID])
		}
		// The photos nobody uploaded are their own entry, so the buckets add up
		// to what the grid shows.
		if counts[""] != 1 {
			t.Errorf("imported count = %d, want 1", counts[""])
		}
		total := 0
		for _, count := range counts {
			total += count
		}
		if list := getList(t, client, base, ""); list.Total != total {
			t.Errorf("buckets sum to %d, list total = %d", total, list.Total)
		}
	})

	t.Run("a bucket's count equals the same-filtered list", func(t *testing.T) {
		for _, bucket := range getUploaders(t, client, base, "").Uploaders {
			uploader := bucket.UID
			if uploader == "" {
				uploader = "none"
			}
			list := getList(t, client, base, "uploader="+uploader)
			if list.Total != bucket.Count {
				t.Errorf("uploader %q: list total = %d, bucket count = %d", uploader, list.Total, bucket.Count)
			}
		}
	})

	t.Run("an album lists that event's contributors only", func(t *testing.T) {
		got := getUploaders(t, client, base, "album="+album.UID)
		counts := uploaderCounts(got)
		if len(counts) != 3 || counts[tomasUID] != 1 || counts[annaUID] != 1 || counts[""] != 1 {
			t.Fatalf("album facet = %+v, want one photo each from tomas, anna and the import", got.Uploaders)
		}
		// Album + uploader is the case the feature exists for: the two compose
		// with no special-casing.
		list := getList(t, client, base, "album="+album.UID+"&uploader="+tomasUID)
		if len(list.Photos) != 1 || list.Photos[0].UID != tomasWedding.UID {
			t.Errorf("album+uploader photos = %v, want [%s]", uids(list.Photos), tomasWedding.UID)
		}
	})

	t.Run("the uploader filter does not narrow its own facet", func(t *testing.T) {
		got := getUploaders(t, client, base, "uploader="+annaUID)
		if counts := uploaderCounts(got); len(counts) != 3 || counts[tomasUID] != 3 {
			t.Errorf("facet under a selected uploader = %+v, want every uploader still offered", got.Uploaders)
		}
		got = getUploaders(t, client, base, "uploader=none")
		if counts := uploaderCounts(got); len(counts) != 3 || counts[tomasUID] != 3 {
			t.Errorf("facet under the imported group = %+v, want every uploader still offered", got.Uploaders)
		}
	})

	t.Run("other filters narrow the counts", func(t *testing.T) {
		got := getUploaders(t, client, base, "q="+url.QueryEscape("title:Tomáš"))
		counts := uploaderCounts(got)
		if len(counts) != 1 || counts[tomasUID] != 3 {
			t.Errorf("query-scoped facet = %+v, want only tomas with 3", got.Uploaders)
		}
	})

	t.Run("archived photos follow the caller's visibility", func(t *testing.T) {
		if _, err := env.store.Archive(t.Context(), tomasThird.UID); err != nil {
			t.Fatalf("Archive: %v", err)
		}
		if counts := uploaderCounts(getUploaders(t, client, base, "")); counts[tomasUID] != 2 {
			t.Errorf("tomas count = %d, want 2 with the archived photo hidden", counts[tomasUID])
		}
		if counts := uploaderCounts(getUploaders(t, client, base, "archived=only")); len(counts) != 1 ||
			counts[tomasUID] != 1 {
			t.Errorf("archived-only facet = %v, want only tomas with 1", counts)
		}
		// Restored here rather than in a cleanup: the subtests below share this
		// library, and a photo left archived would quietly shrink their counts.
		if _, err := env.store.Unarchive(t.Context(), tomasThird.UID); err != nil {
			t.Fatalf("Unarchive: %v", err)
		}
	})

	t.Run("the uploader param scopes the grid", func(t *testing.T) {
		list := getList(t, client, base, "uploader="+tomasUID)
		if len(list.Photos) != 3 {
			t.Fatalf("uploader=%s photos = %v, want three", tomasUID, uids(list.Photos))
		}
		list = getList(t, client, base, "uploader=none")
		if len(list.Photos) != 1 || list.Photos[0].UID != imported.UID {
			t.Errorf("uploader=none photos = %v, want [%s]", uids(list.Photos), imported.UID)
		}
	})

	t.Run("the query language matches by name, uid, me and none", func(t *testing.T) {
		tests := []struct {
			name string
			q    string
			want []string
		}{
			// Diacritics-insensitive, like every name a person types from memory.
			{"display name without diacritics", `uploader:"tomas novak"`,
				[]string{tomasWedding.UID, tomasOther.UID, tomasThird.UID}},
			{"username", "uploader:tomas", []string{tomasWedding.UID, tomasOther.UID, tomasThird.UID}},
			{"wildcard", "uploader:tom*", []string{tomasWedding.UID, tomasOther.UID, tomasThird.UID}},
			{"uid", "uploader:" + annaUID, []string{annaWedding.UID}},
			{"me is the caller", "uploader:me", []string{annaWedding.UID}},
			{"none is the imported photos", "uploader:none", []string{imported.UID}},
			{"not none is everything somebody uploaded", "uploader:!none",
				[]string{tomasWedding.UID, tomasOther.UID, tomasThird.UID, annaWedding.UID}},
			{"an unknown name matches nothing", "uploader:nobody", nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				list := getList(t, client, base, "q="+url.QueryEscape(tt.q))
				got := make(map[string]bool, len(list.Photos))
				for _, photo := range list.Photos {
					got[photo.UID] = true
				}
				if len(got) != len(tt.want) {
					t.Fatalf("q=%q photos = %v, want %d", tt.q, uids(list.Photos), len(tt.want))
				}
				for _, uid := range tt.want {
					if !got[uid] {
						t.Errorf("q=%q photos = %v, missing %s", tt.q, uids(list.Photos), uid)
					}
				}
				if len(list.UnknownTokens) != 0 {
					t.Errorf("q=%q unknown tokens = %v, want none", tt.q, list.UnknownTokens)
				}
			})
		}
	})

	t.Run("invalid filter is 400", func(t *testing.T) {
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/uploaders?archived=maybe", nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		resp := mustDo(t, &http.Client{}, http.MethodGet, base+"/api/v1/photos/uploaders", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
		}
	})
}
