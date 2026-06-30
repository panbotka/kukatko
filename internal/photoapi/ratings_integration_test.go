//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// ratedPhoto is the per-photo shape the rating tests decode: the UID plus the
// current user's star rating and pick/reject flag carried alongside every field.
type ratedPhoto struct {
	UID    string `json:"uid"`
	Rating int    `json:"rating"`
	Flag   string `json:"flag"`
}

// ratingURL builds the per-photo rating endpoint URL.
func ratingURL(base, uid string) string {
	return base + "/api/v1/photos/" + uid + "/rating"
}

// mustStatusBody performs a request with a JSON body and asserts the response
// status, closing the body.
func mustStatusBody(t *testing.T, client *http.Client, method, urlStr, body string, want int) {
	t.Helper()
	resp := mustDo(t, client, method, urlStr, []byte(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("%s %s status = %d, want %d", method, urlStr, resp.StatusCode, want)
	}
}

// getRating fetches a photo's detail and returns its current-user rating/flag.
func getRating(t *testing.T, client *http.Client, base, uid string) ratedPhoto {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var out ratedPhoto
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return out
}

// TestRatingsAPI exercises the per-user rating/flag endpoints end to end:
// setting a rating and a flag independently, the rating/flag annotation in detail
// and list, clearing, validation and missing-photo errors, and per-user isolation.
func TestRatingsAPI(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "rat_alice", auth.RoleViewer)
	bob, _ := e.login(t, "rat_bob", auth.RoleEditor)

	p1 := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 11, 21, 31)
	p2 := e.seedPhoto(t, photos.Photo{Title: "two"}, "2.jpg", 41, 51, 61)

	t.Run("set rating then flag independently and reflect in detail", func(t *testing.T) {
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{"rating":4}`, http.StatusNoContent)
		if got := getRating(t, alice, e.server.URL, p1.UID); got.Rating != 4 || got.Flag != "none" {
			t.Fatalf("after set rating = %+v, want {4 none}", got)
		}
		// Setting the flag preserves the existing rating (independent columns).
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{"flag":"pick"}`, http.StatusNoContent)
		if got := getRating(t, alice, e.server.URL, p1.UID); got.Rating != 4 || got.Flag != "pick" {
			t.Fatalf("after set flag = %+v, want {4 pick}", got)
		}
		// An unrated photo reports the defaults.
		if got := getRating(t, alice, e.server.URL, p2.UID); got.Rating != 0 || got.Flag != "none" {
			t.Fatalf("unrated p2 = %+v, want {0 none}", got)
		}
	})

	t.Run("list response carries the rating/flag annotation", func(t *testing.T) {
		got := getList(t, alice, e.server.URL, "sort=title")
		byUID := map[string]photos.Photo{}
		for _, p := range got.Photos {
			byUID[p.UID] = p
		}
		if p := byUID[p1.UID]; p.Rating != 4 || p.Flag != "pick" {
			t.Errorf("p1 in list = {%d %q}, want {4 pick}", p.Rating, p.Flag)
		}
		if p := byUID[p2.UID]; p.Rating != 0 || p.Flag != "none" {
			t.Errorf("p2 in list = {%d %q}, want {0 none}", p.Rating, p.Flag)
		}
	})

	t.Run("clear removes both rating and flag", func(t *testing.T) {
		mustStatusBody(t, alice, http.MethodDelete, ratingURL(e.server.URL, p1.UID), "", http.StatusNoContent)
		if got := getRating(t, alice, e.server.URL, p1.UID); got.Rating != 0 || got.Flag != "none" {
			t.Fatalf("after clear = %+v, want {0 none}", got)
		}
		// Clearing again still succeeds (idempotent).
		mustStatusBody(t, alice, http.MethodDelete, ratingURL(e.server.URL, p1.UID), "", http.StatusNoContent)
	})

	t.Run("validation and missing photo", func(t *testing.T) {
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{"rating":9}`, http.StatusBadRequest)
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{"flag":"star"}`, http.StatusBadRequest)
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{}`, http.StatusBadRequest)
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, "ph_missing"), `{"rating":3}`, http.StatusNotFound)
	})

	t.Run("per-user isolation", func(t *testing.T) {
		mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p2.UID), `{"rating":5}`, http.StatusNoContent)
		mustStatusBody(t, bob, http.MethodPut, ratingURL(e.server.URL, p2.UID), `{"flag":"reject"}`, http.StatusNoContent)
		if got := getRating(t, alice, e.server.URL, p2.UID); got.Rating != 5 || got.Flag != "none" {
			t.Errorf("alice p2 = %+v, want {5 none}", got)
		}
		if got := getRating(t, bob, e.server.URL, p2.UID); got.Rating != 0 || got.Flag != "reject" {
			t.Errorf("bob p2 = %+v, want {0 reject} (isolation)", got)
		}
	})
}

// TestRatingsFilterAndSort exercises the min_rating and flag list filters and the
// rating sort, all scoped to the requesting user.
func TestRatingsFilterAndSort(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "ratf_alice", auth.RoleViewer)

	p1 := e.seedPhoto(t, photos.Photo{Title: "alpha"}, "f1.jpg", 12, 22, 32)
	p2 := e.seedPhoto(t, photos.Photo{Title: "bravo"}, "f2.jpg", 42, 52, 62)
	p3 := e.seedPhoto(t, photos.Photo{Title: "charlie"}, "f3.jpg", 72, 82, 92)

	mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p1.UID), `{"rating":5,"flag":"pick"}`, http.StatusNoContent)
	mustStatusBody(t, alice, http.MethodPut, ratingURL(e.server.URL, p2.UID), `{"rating":3}`, http.StatusNoContent)
	// p3 stays unrated (rating 0 / flag none).

	t.Run("min_rating keeps high-rated photos", func(t *testing.T) {
		got := getList(t, alice, e.server.URL, "min_rating=4")
		ids := listUIDs(got)
		if got.Total != 1 || !ids[p1.UID] {
			t.Fatalf("min_rating=4 = %v (total %d), want {p1}", ids, got.Total)
		}
	})

	t.Run("flag keeps picked photos", func(t *testing.T) {
		got := getList(t, alice, e.server.URL, "flag=pick")
		ids := listUIDs(got)
		if got.Total != 1 || !ids[p1.UID] {
			t.Fatalf("flag=pick = %v (total %d), want {p1}", ids, got.Total)
		}
	})

	t.Run("sort by rating puts the highest first and the unrated last", func(t *testing.T) {
		got := getList(t, alice, e.server.URL, "sort=rating")
		if len(got.Photos) != 3 {
			t.Fatalf("sort=rating returned %d photos, want 3", len(got.Photos))
		}
		order := []string{got.Photos[0].UID, got.Photos[1].UID, got.Photos[2].UID}
		want := []string{p1.UID, p2.UID, p3.UID}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("sort=rating order = %v, want %v", order, want)
			}
		}
	})
}

// listUIDs collects the UIDs of a list response into a set.
func listUIDs(list listResp) map[string]bool {
	set := make(map[string]bool, len(list.Photos))
	for _, p := range list.Photos {
		set[p.UID] = true
	}
	return set
}
