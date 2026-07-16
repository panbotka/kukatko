//go:build integration

package photoapi_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// intPtr returns a pointer to n for the photo fixtures below.
func intPtr(n int) *int {
	return &n
}

// TestSearch_filterOnlyQuerySkipsSidecar verifies that a q holding only
// filters (no free text) runs the plain list path: the response reports mode
// "filter", the parsed filters constrain the result, and the embedding sidecar
// is never consulted — even in the default hybrid mode.
func TestSearch_filterOnlyQuerySkipsSidecar(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "ql-filter", auth.RoleViewer)
	base := env.server.URL

	low := env.seedPhoto(t, photos.Photo{Title: "low iso", ISO: intPtr(100)}, "low.jpg", 10, 0, 0)
	env.seedPhoto(t, photos.Photo{Title: "high iso", ISO: intPtr(3200)}, "high.jpg", 20, 0, 0)

	got := getSearch(t, client, base, "q="+url.QueryEscape("iso:100-400"))
	if got.Mode != "filter" || got.Degraded {
		t.Fatalf("mode=%q degraded=%v, want filter/false", got.Mode, got.Degraded)
	}
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != low.UID {
		t.Fatalf("filter-only result = %v (total %d), want just the low-ISO photo", uids(got.Photos), got.Total)
	}
	if env.embedder.calls != 0 {
		t.Fatalf("embedder calls = %d, want 0 — a pure filter query must not touch the sidecar", env.embedder.calls)
	}
}

// TestSearch_queryLanguageConstrainsFreeText verifies that filters parsed out
// of q constrain the ranked free-text search: the same word matches both
// photos, the iso: filter keeps one.
func TestSearch_queryLanguageConstrainsFreeText(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "ql-mixed", auth.RoleViewer)
	base := env.server.URL

	sharp := env.seedPhoto(t, photos.Photo{Title: "sunset pier", ISO: intPtr(100)}, "a.jpg", 10, 0, 0)
	env.seedPhoto(t, photos.Photo{Title: "sunset hill", ISO: intPtr(3200)}, "b.jpg", 20, 0, 0)

	got := getSearch(t, client, base, "q="+url.QueryEscape("sunset iso:-400")+"&mode=fulltext")
	if got.Mode != "fulltext" {
		t.Fatalf("mode = %q, want fulltext", got.Mode)
	}
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != sharp.UID {
		t.Fatalf("constrained search = %v (total %d), want just the low-ISO sunset", uids(got.Photos), got.Total)
	}
}

// TestSearch_unknownTokenDegradesAndReports verifies the contract for a token
// the language does not understand: it is reported in unknown_tokens, and the
// search still runs with the token as free text (finding a caption that
// carries it verbatim) instead of erroring or returning nothing.
func TestSearch_unknownTokenDegradesAndReports(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "ql-unknown", auth.RoleViewer)
	base := env.server.URL

	tagged := env.seedPhoto(t, photos.Photo{Title: "wall", Notes: "sprayed color:red all over"},
		"wall.jpg", 10, 0, 0)

	got := getSearch(t, client, base, "q="+url.QueryEscape("color:red")+"&mode=fulltext")
	if len(got.UnknownTokens) != 1 || got.UnknownTokens[0] != "color:red" {
		t.Fatalf("unknown_tokens = %v, want [color:red]", got.UnknownTokens)
	}
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != tagged.UID {
		t.Fatalf("degraded search = %v (total %d), want the caption match", uids(got.Photos), got.Total)
	}
}

// TestList_queryLanguageApplies verifies GET /photos parses q through the same
// language: filters constrain the browse grid and unknown tokens are reported
// on the list response too.
func TestList_queryLanguageApplies(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "ql-list", auth.RoleViewer)
	base := env.server.URL

	y2024 := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	y2022 := time.Date(2022, 5, 1, 12, 0, 0, 0, time.UTC)
	recent := env.seedPhoto(t, photos.Photo{Title: "recent", TakenAt: &y2024, TakenAtSource: "exif"},
		"r.jpg", 10, 0, 0)
	env.seedPhoto(t, photos.Photo{Title: "older", TakenAt: &y2022, TakenAtSource: "exif"},
		"o.jpg", 20, 0, 0)

	got := getList(t, client, base, "q="+url.QueryEscape("year:2024"))
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != recent.UID {
		t.Fatalf("list filter = %v (total %d), want just the 2024 photo", uids(got.Photos), got.Total)
	}

	got = getList(t, client, base, "q="+url.QueryEscape("foo:bar"))
	if len(got.UnknownTokens) != 1 || got.UnknownTokens[0] != "foo:bar" {
		t.Fatalf("list unknown_tokens = %v, want [foo:bar]", got.UnknownTokens)
	}
}
