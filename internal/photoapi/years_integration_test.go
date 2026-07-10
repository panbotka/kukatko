//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// yearsResp mirrors the years endpoint's JSON body.
type yearsResp struct {
	Years []struct {
		Year  int `json:"year"`
		Count int `json:"count"`
	} `json:"years"`
	Total int `json:"total"`
}

// getYears fetches the years endpoint with the given query and decodes the body,
// failing the test on a non-200 status.
func getYears(t *testing.T, client *http.Client, base, query string) yearsResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/years?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("years status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out yearsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode years: %v", err)
	}
	return out
}

// yearCounts flattens the buckets into a year -> count map for order-independent
// assertions; the slice order is asserted separately.
func yearCounts(resp yearsResp) map[int]int {
	out := make(map[int]int, len(resp.Years))
	for _, b := range resp.Years {
		out[b.Year] = b.Count
	}
	return out
}

// TestYears exercises the year-facet endpoint against a real database: the
// default buckets (archived hidden, undated photos counted in the total but in no
// bucket), the caller's visibility filters narrowing the counts, the year filter
// being excluded from its own facet, and the counts agreeing exactly with the
// same-filtered list.
func TestYears(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	// Mid-year, mid-day capture times, so the bucket a photo lands in does not
	// depend on the database session's time zone.
	y2021 := time.Date(2021, 3, 10, 12, 0, 0, 0, time.UTC)
	jun2023 := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	dec2023 := time.Date(2023, 12, 20, 12, 0, 0, 0, time.UTC)
	y2019 := time.Date(2019, 5, 5, 12, 0, 0, 0, time.UTC)

	old := env.seedPhoto(t, photos.Photo{Title: "Old", TakenAt: ptrTime(y2021), TakenAtSource: "exif"},
		"old.jpg", 200, 10, 10)
	priv := env.seedPhoto(t,
		photos.Photo{Title: "Jun", TakenAt: ptrTime(jun2023), TakenAtSource: "exif", Private: true},
		"jun.jpg", 10, 200, 10)
	env.seedPhoto(t, photos.Photo{Title: "Dec A", TakenAt: ptrTime(dec2023), TakenAtSource: "exif"},
		"deca.jpg", 10, 10, 200)
	env.seedPhoto(t, photos.Photo{Title: "Dec B", TakenAt: ptrTime(dec2023), TakenAtSource: "exif"},
		"decb.jpg", 60, 60, 60)
	// No capture time: counted in the total, member of no year.
	env.seedPhoto(t, photos.Photo{Title: "Undated"}, "undated.jpg", 90, 90, 90)
	archived := env.seedPhoto(t, photos.Photo{Title: "Gone", TakenAt: ptrTime(y2019), TakenAtSource: "exif"},
		"gone.jpg", 120, 120, 120)
	if _, err := env.store.Archive(t.Context(), archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	t.Run("default buckets are newest first and exclude archived", func(t *testing.T) {
		got := getYears(t, client, base, "")
		if got.Total != 5 {
			t.Fatalf("total = %d, want 5 (archived excluded, undated counted)", got.Total)
		}
		if len(got.Years) != 2 {
			t.Fatalf("years = %+v, want 2 buckets (2023, 2021)", got.Years)
		}
		if got.Years[0].Year != 2023 || got.Years[0].Count != 3 {
			t.Errorf("years[0] = %+v, want 2023 count 3", got.Years[0])
		}
		if got.Years[1].Year != 2021 || got.Years[1].Count != 1 {
			t.Errorf("years[1] = %+v, want 2021 count 1", got.Years[1])
		}
		if _, ok := yearCounts(got)[2019]; ok {
			t.Error("archived photo's year 2019 leaked into the default facet")
		}
	})

	t.Run("bucket count equals the same-filtered list", func(t *testing.T) {
		got := getYears(t, client, base, "")
		for _, bucket := range got.Years {
			list := getList(t, client, base, "year="+strconv.Itoa(bucket.Year))
			if list.Total != bucket.Count {
				t.Errorf("year %d: list total = %d, bucket count = %d", bucket.Year, list.Total, bucket.Count)
			}
		}
		// The undated photo belongs to no year, so no bucket claims it.
		list := getList(t, client, base, "year=2021")
		if len(list.Photos) != 1 || list.Photos[0].UID != old.UID {
			t.Errorf("year=2021 photos = %v, want [%s]", uids(list.Photos), old.UID)
		}
	})

	t.Run("other filters narrow the counts", func(t *testing.T) {
		got := getYears(t, client, base, "private=true")
		if got.Total != 1 || len(got.Years) != 1 {
			t.Fatalf("private years = %+v total=%d, want the single 2023 photo", got.Years, got.Total)
		}
		if got.Years[0].Year != 2023 || got.Years[0].Count != 1 {
			t.Errorf("bucket = %+v, want 2023 count 1", got.Years[0])
		}
		list := getList(t, client, base, "private=true&year=2023")
		if list.Total != 1 || len(list.Photos) != 1 || list.Photos[0].UID != priv.UID {
			t.Errorf("list total=%d photos=%v, want 1/[%s]", list.Total, uids(list.Photos), priv.UID)
		}
	})

	t.Run("archived visibility follows the caller", func(t *testing.T) {
		got := getYears(t, client, base, "archived=only")
		if counts := yearCounts(got); counts[2019] != 1 || len(counts) != 1 {
			t.Errorf("archived-only years = %+v, want only 2019 count 1", got.Years)
		}
	})

	t.Run("the year filter does not narrow its own facet", func(t *testing.T) {
		got := getYears(t, client, base, "year=2023")
		counts := yearCounts(got)
		if len(counts) != 2 || counts[2023] != 3 || counts[2021] != 1 {
			t.Errorf("years = %+v, want every year offered regardless of the selected one", got.Years)
		}
		if got.Total != 5 {
			t.Errorf("total = %d, want 5 (the selected year must not narrow the total)", got.Total)
		}
	})

	t.Run("a year with no photos is absent", func(t *testing.T) {
		list := getList(t, client, base, "year=2020")
		if list.Total != 0 || len(list.Photos) != 0 {
			t.Errorf("year=2020 total=%d photos=%v, want empty", list.Total, uids(list.Photos))
		}
		if _, ok := yearCounts(getYears(t, client, base, ""))[2020]; ok {
			t.Error("empty year 2020 offered as a facet option")
		}
	})

	t.Run("invalid year is 400", func(t *testing.T) {
		for _, query := range []string{"year=nineteen", "year=42", "archived=maybe"} {
			resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/years?"+query, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("years?%s status = %d, want 400", query, resp.StatusCode)
			}
			resp = mustDo(t, client, http.MethodGet, base+"/api/v1/photos?"+query, nil)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("photos?%s status = %d, want 400", query, resp.StatusCode)
			}
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		resp := mustDo(t, &http.Client{}, http.MethodGet, base+"/api/v1/photos/years", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
		}
	})
}
