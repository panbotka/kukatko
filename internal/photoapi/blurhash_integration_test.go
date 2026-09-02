//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// blurhashOf returns the placeholder a JSON object carries, and whether the key
// was there at all.
func blurhashOf(t *testing.T, row map[string]json.RawMessage) (string, bool) {
	t.Helper()
	raw, ok := row["blurhash"]
	if !ok {
		return "", false
	}
	var hash string
	if err := json.Unmarshal(raw, &hash); err != nil {
		t.Fatalf("blurhash %s is not a string: %v", raw, err)
	}
	return hash, true
}

// fetchObject GETs path and decodes the response body as a JSON object.
func fetchObject(t *testing.T, client *http.Client, url string) map[string]json.RawMessage {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return body
}

// TestBlurhash_servedInListAndDetail verifies the placeholder reaches both the
// list rows and the detail response. The list is the one that matters: the whole
// point is that a grid can paint something the moment its page of rows arrives,
// which a value served only on the detail could never do.
func TestBlurhash_servedInListAndDetail(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	photo := env.seedPhoto(t, photos.Photo{Title: "Beach"}, "beach.jpg", 10, 20, 30)
	const hash = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"
	if err := env.store.SaveBlurhash(t.Context(), photo.UID, hash); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}

	detail := fetchObject(t, client, env.server.URL+"/api/v1/photos/"+photo.UID)
	got, present := blurhashOf(t, detail)
	if !present {
		t.Error("the detail carries no blurhash")
	} else if got != hash {
		t.Errorf("detail blurhash = %q, want %q", got, hash)
	}

	for _, path := range []string{"/api/v1/photos", "/api/v1/search?q=Beach"} {
		resp := mustDo(t, client, http.MethodGet, env.server.URL+path, nil)
		var page struct {
			Photos []map[string]json.RawMessage `json:"photos"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if len(page.Photos) == 0 {
			t.Fatalf("%s returned no photos", path)
		}
		row, ok := blurhashOf(t, page.Photos[0])
		if !ok {
			t.Errorf("%s carries no blurhash on its rows", path)
			continue
		}
		if row != hash {
			t.Errorf("%s row blurhash = %q, want %q", path, row, hash)
		}
	}
}

// TestBlurhash_omittedWhenNotComputed verifies a photo whose placeholder has not
// been computed yet simply has no key, so a client can ask whether the field is
// there rather than having to tell an empty string from a real value.
func TestBlurhash_omittedWhenNotComputed(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	photo := env.seedPhoto(t, photos.Photo{Title: "Unhashed"}, "unhashed.jpg", 40, 50, 60)
	detail := fetchObject(t, client, env.server.URL+"/api/v1/photos/"+photo.UID)
	if _, present := blurhashOf(t, detail); present {
		t.Error("blurhash is present for a photo whose placeholder was never computed")
	}
}
