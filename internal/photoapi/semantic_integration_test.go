//go:build integration

package photoapi_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestSearch_semanticOrdersByVector verifies semantic mode embeds the query and
// returns photos ordered by ascending cosine distance, excluding photos that have
// not been embedded yet.
func TestSearch_semanticOrdersByVector(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "sem-viewer", auth.RoleViewer)
	base := env.server.URL

	near := env.seedPhoto(t, photos.Photo{Title: "alpha"}, "near.jpg", 10, 0, 0)
	mid := env.seedPhoto(t, photos.Photo{Title: "beta"}, "mid.jpg", 20, 0, 0)
	far := env.seedPhoto(t, photos.Photo{Title: "gamma"}, "far.jpg", 30, 0, 0)
	noEmb := env.seedPhoto(t, photos.Photo{Title: "delta"}, "noemb.jpg", 40, 0, 0)

	// The query points along axis 0; near matches it, mid is between axes, far is
	// orthogonal, so distance order is near < mid < far. noEmb has no embedding.
	env.embedder.byQuery["sunset"] = imageVecAt(map[int]float32{0: 1})
	saveVec(t, env, near.UID, imageVecAt(map[int]float32{0: 1, 1: 0.05}))
	saveVec(t, env, mid.UID, imageVecAt(map[int]float32{0: 1, 1: 1}))
	saveVec(t, env, far.UID, imageVecAt(map[int]float32{1: 1}))

	got := getSearch(t, client, base, "q=sunset&mode=semantic")
	if got.Mode != "semantic" || got.Degraded {
		t.Fatalf("mode=%q degraded=%v, want semantic/false", got.Mode, got.Degraded)
	}
	if got.Total != 3 {
		t.Fatalf("semantic total = %d, want 3 (un-embedded photo excluded)", got.Total)
	}
	want := []string{near.UID, mid.UID, far.UID}
	if order := uids(got.Photos); !equalStrings(order, want) {
		t.Fatalf("semantic order = %v, want %v", order, want)
	}
	for _, p := range got.Photos {
		if p.UID == noEmb.UID {
			t.Fatal("semantic result included the un-embedded photo")
		}
	}
}

// TestSearch_semanticAppliesFilters verifies semantic mode honours the standard
// list filters (here the private flag) alongside vector ranking.
func TestSearch_semanticAppliesFilters(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "sem-filter", auth.RoleViewer)
	base := env.server.URL

	pub := env.seedPhoto(t, photos.Photo{Title: "public"}, "pub.jpg", 12, 0, 0)
	priv := env.seedPhoto(t, photos.Photo{Title: "private", Private: true}, "priv.jpg", 24, 0, 0)

	env.embedder.byQuery["holiday"] = imageVecAt(map[int]float32{0: 1})
	saveVec(t, env, pub.UID, imageVecAt(map[int]float32{0: 1}))
	saveVec(t, env, priv.UID, imageVecAt(map[int]float32{0: 1, 1: 0.02}))

	pubOnly := getSearch(t, client, base, "q=holiday&mode=semantic&private=false")
	if pubOnly.Total != 1 || len(pubOnly.Photos) != 1 || pubOnly.Photos[0].UID != pub.UID {
		t.Fatalf("semantic(private=false) = %v, want [%s]", uids(pubOnly.Photos), pub.UID)
	}
	privOnly := getSearch(t, client, base, "q=holiday&mode=semantic&private=true")
	if privOnly.Total != 1 || len(privOnly.Photos) != 1 || privOnly.Photos[0].UID != priv.UID {
		t.Fatalf("semantic(private=true) = %v, want [%s]", uids(privOnly.Photos), priv.UID)
	}
}

// TestSearch_hybridFusesAndDedups verifies hybrid mode fuses the full-text and
// semantic rankings: a photo ranked highly in both leads, a semantic-only photo
// is still included, and the union is de-duplicated.
func TestSearch_hybridFusesAndDedups(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "hyb-viewer", auth.RoleViewer)
	base := env.server.URL

	// both: matches the full-text query and is the nearest vector → ranks high in
	// both lists. textOnly: full-text match but a distant vector. vecOnly: nearest
	// vector but no full-text match. miss: neither.
	both := env.seedPhoto(t, photos.Photo{Title: "beach sunset"}, "both.jpg", 10, 0, 0)
	textOnly := env.seedPhoto(t, photos.Photo{Title: "beach party"}, "text.jpg", 20, 0, 0)
	vecOnly := env.seedPhoto(t, photos.Photo{Title: "mountain ridge"}, "vec.jpg", 30, 0, 0)
	miss := env.seedPhoto(t, photos.Photo{Title: "forest path"}, "miss.jpg", 40, 0, 0)

	env.embedder.byQuery["beach"] = imageVecAt(map[int]float32{0: 1})
	saveVec(t, env, both.UID, imageVecAt(map[int]float32{0: 1}))
	saveVec(t, env, vecOnly.UID, imageVecAt(map[int]float32{0: 1, 1: 0.05}))
	saveVec(t, env, textOnly.UID, imageVecAt(map[int]float32{1: 1}))

	got := getSearch(t, client, base, "q=beach&mode=hybrid")
	if got.Mode != "hybrid" || got.Degraded {
		t.Fatalf("mode=%q degraded=%v, want hybrid/false", got.Mode, got.Degraded)
	}
	// Union of full-text {both, textOnly} and semantic {both, vecOnly, textOnly}.
	if got.Total != 3 {
		t.Fatalf("hybrid total = %d, want 3", got.Total)
	}
	if len(got.Photos) == 0 || got.Photos[0].UID != both.UID {
		t.Fatalf("hybrid order = %v, want %s first (high in both rankings)", uids(got.Photos), both.UID)
	}
	seen := map[string]int{}
	for _, p := range got.Photos {
		seen[p.UID]++
		if p.UID == miss.UID {
			t.Fatal("hybrid result included the non-matching photo")
		}
	}
	for uid, n := range seen {
		if n != 1 {
			t.Fatalf("hybrid result duplicated %s (%d times)", uid, n)
		}
	}
	if !containsAll(seen, both.UID, textOnly.UID, vecOnly.UID) {
		t.Fatalf("hybrid result %v missing an expected uid", uids(got.Photos))
	}
}

// TestSearch_defaultsToHybrid verifies omitting the mode parameter selects hybrid.
func TestSearch_defaultsToHybrid(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "default-mode", auth.RoleViewer)
	base := env.server.URL

	p := env.seedPhoto(t, photos.Photo{Title: "harbour"}, "harbour.jpg", 15, 0, 0)
	env.embedder.byQuery["harbour"] = imageVecAt(map[int]float32{0: 1})
	saveVec(t, env, p.UID, imageVecAt(map[int]float32{0: 1}))

	got := getSearch(t, client, base, "q=harbour")
	if got.Mode != "hybrid" {
		t.Fatalf("default mode = %q, want hybrid", got.Mode)
	}
}

// TestSearch_degradedFallback verifies that when the sidecar is offline, semantic
// and hybrid modes fall back to full-text search and flag the response degraded.
func TestSearch_degradedFallback(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "degraded", auth.RoleViewer)
	base := env.server.URL

	match := env.seedPhoto(t, photos.Photo{Title: "ocean wave"}, "ocean.jpg", 11, 0, 0)
	env.seedPhoto(t, photos.Photo{Title: "forest trail"}, "forest.jpg", 22, 0, 0)
	// An embedding exists, but the sidecar that embeds the *query* is offline, so
	// semantic ranking cannot run regardless.
	saveVec(t, env, match.UID, imageVecAt(map[int]float32{0: 1}))
	env.embedder.unavailable = true

	for _, mode := range []string{"semantic", "hybrid"} {
		got := getSearch(t, client, base, "q=ocean&mode="+mode)
		if !got.Degraded {
			t.Fatalf("%s with offline sidecar: degraded = false, want true", mode)
		}
		if got.Mode != mode {
			t.Fatalf("%s degraded response mode = %q, want %q", mode, got.Mode, mode)
		}
		if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != match.UID {
			t.Fatalf("%s fallback = %v, want full-text [%s]", mode, uids(got.Photos), match.UID)
		}
	}
}

// containsAll reports whether seen holds every uid.
func containsAll(seen map[string]int, uids ...string) bool {
	for _, uid := range uids {
		if seen[uid] == 0 {
			return false
		}
	}
	return true
}
