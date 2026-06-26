//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// similarResp mirrors the similar endpoint's JSON body.
type similarResp struct {
	Similar []struct {
		photos.Photo
		Distance float64 `json:"distance"`
	} `json:"similar"`
}

// imageVecAt builds an ImageDim vector with the given index→value overrides.
func imageVecAt(set map[int]float32) []float32 {
	v := make([]float32, vectors.ImageDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// getSimilar fetches the similar endpoint for uid and decodes the body, failing
// on a non-200 status.
func getSimilar(t *testing.T, client *http.Client, base, uid, query string) similarResp {
	t.Helper()
	url := base + "/api/v1/photos/" + uid + "/similar"
	if query != "" {
		url += "?" + query
	}
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("similar status = %d, want 200", resp.StatusCode)
	}
	var out similarResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode similar: %v", err)
	}
	return out
}

// TestSimilar_ordersAndExcludesSource verifies the similar endpoint returns
// photos ordered by ascending cosine distance and excludes the source itself.
func TestSimilar_ordersAndExcludesSource(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	src := env.seedPhoto(t, photos.Photo{}, "src", 10, 0, 0)
	near := env.seedPhoto(t, photos.Photo{}, "near", 20, 0, 0)
	mid := env.seedPhoto(t, photos.Photo{}, "mid", 30, 0, 0)
	far := env.seedPhoto(t, photos.Photo{}, "far", 40, 0, 0)

	// Source points along axis 0; near matches it, mid is between axes, far is
	// orthogonal — so the distance order is near < mid < far.
	saveVec(t, env, src.UID, imageVecAt(map[int]float32{0: 1}))
	saveVec(t, env, near.UID, imageVecAt(map[int]float32{0: 1, 1: 0.05}))
	saveVec(t, env, mid.UID, imageVecAt(map[int]float32{0: 1, 1: 1}))
	saveVec(t, env, far.UID, imageVecAt(map[int]float32{1: 1}))

	got := getSimilar(t, client, base, src.UID, "")
	order := make([]string, len(got.Similar))
	for i, s := range got.Similar {
		order[i] = s.UID
	}
	want := []string{near.UID, mid.UID, far.UID}
	if len(order) != len(want) {
		t.Fatalf("similar returned %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("similar order = %v, want %v", order, want)
		}
	}
	for _, s := range got.Similar {
		if s.UID == src.UID {
			t.Fatal("similar result included the source photo")
		}
	}
	// Distances are populated and ascending.
	for i := 1; i < len(got.Similar); i++ {
		if got.Similar[i].Distance < got.Similar[i-1].Distance {
			t.Errorf("distances not ascending: %+v", got.Similar)
		}
	}
}

// TestSimilar_limit caps the number of returned neighbours.
func TestSimilar_limit(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	src := env.seedPhoto(t, photos.Photo{}, "src", 11, 0, 0)
	saveVec(t, env, src.UID, imageVecAt(map[int]float32{0: 1}))
	for i := range 5 {
		p := env.seedPhoto(t, photos.Photo{}, "n"+string(rune('a'+i)), uint8(50+i), 0, 0)
		saveVec(t, env, p.UID, imageVecAt(map[int]float32{0: 1, 1: float32(i+1) * 0.1}))
	}

	got := getSimilar(t, client, base, src.UID, "limit=2")
	if len(got.Similar) != 2 {
		t.Errorf("similar with limit=2 returned %d, want 2", len(got.Similar))
	}
}

// TestSimilar_noEmbeddingIsEmpty verifies a photo without an embedding yields an
// empty (200) result, and a missing photo yields 404.
func TestSimilar_noEmbeddingIsEmpty(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	src := env.seedPhoto(t, photos.Photo{}, "noemb", 12, 0, 0)
	got := getSimilar(t, client, base, src.UID, "")
	if len(got.Similar) != 0 {
		t.Errorf("similar for un-embedded photo = %+v, want empty", got.Similar)
	}

	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/ph_missing/similar", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("similar for missing photo = %d, want 404", resp.StatusCode)
	}
}

// saveVec stores an image embedding for uid via the env's vector store.
func saveVec(t *testing.T, e *env, uid string, vec []float32) {
	t.Helper()
	if _, err := e.vectors.SaveEmbedding(t.Context(), vectors.Embedding{PhotoUID: uid, Vector: vec}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", uid, err)
	}
}
