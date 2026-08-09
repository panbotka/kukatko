//go:build integration

package bulkapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// setTakenAtBody builds a bulk request that sets the capture date of the given
// photos at one grain.
func setTakenAtBody(precision, value string, uids ...string) []byte {
	body, _ := json.Marshal(map[string]any{
		"photo_uids": uids,
		"operations": map[string]any{
			"set_taken_at": map[string]string{"precision": precision, "value": value},
		},
	})
	return body
}

// TestBulk_setTakenAtEachPrecision walks the four grains a date can be stated
// at, asserting for each that the stored anchor is the first instant of the
// period in UTC, that the precision is recorded beside it, and that only a grain
// coarser than a day is marked an estimate.
func TestBulk_setTakenAtEachPrecision(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	tests := []struct {
		name          string
		precision     string
		value         string
		wantAt        time.Time
		wantEstimated bool
	}{
		{
			name:      "exact date",
			precision: "day",
			value:     "1974-06-14",
			wantAt:    time.Date(1974, time.June, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "month and year",
			precision:     "month",
			value:         "1974-06",
			wantAt:        time.Date(1974, time.June, 1, 0, 0, 0, 0, time.UTC),
			wantEstimated: true,
		},
		{
			name:          "year only",
			precision:     "year",
			value:         "1974",
			wantAt:        time.Date(1974, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantEstimated: true,
		},
		{
			name:          "decade",
			precision:     "decade",
			value:         "1970",
			wantAt:        time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantEstimated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid := env.seedPhoto(t, "scan-"+tt.precision)
			resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk",
				setTakenAtBody(tt.precision, tt.value, uid))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
			}
			var result bulk.Result
			decodeBody(t, resp, &result)
			if result.Counts.Updated != 1 {
				t.Fatalf("counts = %+v, want updated=1", result.Counts)
			}

			photo, err := env.photos.GetByUID(t.Context(), uid)
			if err != nil {
				t.Fatalf("get %s: %v", uid, err)
			}
			if photo.TakenAt == nil || !photo.TakenAt.Equal(tt.wantAt) {
				t.Errorf("TakenAt = %v, want %s", photo.TakenAt, tt.wantAt)
			}
			if photo.TakenAtPrecision != tt.precision {
				t.Errorf("TakenAtPrecision = %q, want %q", photo.TakenAtPrecision, tt.precision)
			}
			// A date the user typed is theirs, whatever its grain: a later metadata
			// pass must not recompute it from the file.
			if photo.TakenAtSource != photos.TakenAtSourceManual {
				t.Errorf("TakenAtSource = %q, want %q", photo.TakenAtSource, photos.TakenAtSourceManual)
			}
			if photo.TakenAtEstimated != tt.wantEstimated {
				t.Errorf("TakenAtEstimated = %v, want %v", photo.TakenAtEstimated, tt.wantEstimated)
			}
		})
	}
}

// TestBulk_setTakenAtExactDateDropsTheEstimate verifies the other direction:
// stating an exact date on photos that were "somewhere in the seventies" lowers
// the estimate flag and drops the dating note with it, keeping the invariant that
// a note only lives beside a date flagged as a guess.
func TestBulk_setTakenAtExactDateDropsTheEstimate(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	uid := env.seedPhoto(t, "guessed")
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk",
		setTakenAtBody("decade", "1970", uid))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first bulk status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// A note is what a user would have added beside the guess.
	current, err := env.photos.GetByUID(ctx, uid)
	if err != nil {
		t.Fatalf("get %s: %v", uid, err)
	}
	update := photos.MetadataUpdate{
		TakenAt: current.TakenAt, TakenAtSource: current.TakenAtSource,
		TakenAtEstimated: true, TakenAtNote: "podle babičky",
		TakenAtPrecision: current.TakenAtPrecision,
	}
	if _, err := env.photos.UpdateMetadata(ctx, uid, update); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk",
		setTakenAtBody("day", "1974-06-14", uid))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second bulk status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	photo, err := env.photos.GetByUID(ctx, uid)
	if err != nil {
		t.Fatalf("get %s: %v", uid, err)
	}
	if photo.TakenAtPrecision != photos.TakenAtPrecisionDay {
		t.Errorf("TakenAtPrecision = %q, want day", photo.TakenAtPrecision)
	}
	if photo.TakenAtEstimated {
		t.Error("TakenAtEstimated = true, want false after an exact date")
	}
	if photo.TakenAtNote != "" {
		t.Errorf("TakenAtNote = %q, want empty once the date is a fact", photo.TakenAtNote)
	}
}

// TestBulk_setTakenAtIsTransactional verifies the whole batch commits or not at
// all: a request that also references a missing album is rejected before
// anything is written, so no photo comes out of it half-dated.
func TestBulk_setTakenAtIsTransactional(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	p1 := env.seedPhoto(t, "batch1")
	p2 := env.seedPhoto(t, "batch2")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{p1, p2},
		"operations": map[string]any{
			"set_taken_at":  map[string]string{"precision": "year", "value": "1974"},
			"add_to_albums": []string{"missing-album-uid"},
		},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bulk status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	for _, uid := range []string{p1, p2} {
		photo, err := env.photos.GetByUID(t.Context(), uid)
		if err != nil {
			t.Fatalf("get %s: %v", uid, err)
		}
		if photo.TakenAt != nil {
			t.Errorf("%s TakenAt = %v, want nil (the batch rolled back)", uid, photo.TakenAt)
		}
		if photo.TakenAtPrecision != photos.TakenAtPrecisionDay {
			t.Errorf("%s TakenAtPrecision = %q, want the untouched default",
				uid, photo.TakenAtPrecision)
		}
	}
}

// TestBulk_setTakenAtRejectsABadDate verifies a value that does not match the
// grain it was stated at is refused with a 400 and changes nothing — the anchor
// must never be guessed at from a half-parsed value.
func TestBulk_setTakenAtRejectsABadDate(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	uid := env.seedPhoto(t, "badvalue")
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk",
		setTakenAtBody("year", "1974-06-14", uid))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bulk status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	photo, err := env.photos.GetByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("get %s: %v", uid, err)
	}
	if photo.TakenAt != nil {
		t.Errorf("TakenAt = %v, want nil", photo.TakenAt)
	}
}

// TestBulk_setTakenAtViewerForbidden verifies the operation is an editor's: a
// viewer cannot re-date the library.
func TestBulk_setTakenAtViewerForbidden(t *testing.T) {
	env := newEnv(t, 1000)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)

	uid := env.seedPhoto(t, "viewerdate")
	resp := env.mustDo(t, viewer, http.MethodPost, "/api/v1/photos/bulk",
		setTakenAtBody("year", "1974", uid))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bulk status = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	photo, err := env.photos.GetByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("get %s: %v", uid, err)
	}
	if photo.TakenAt != nil {
		t.Errorf("TakenAt = %v, want nil", photo.TakenAt)
	}
}

// TestBulk_setTakenAtLandsInTheRightPeriod is the point of the whole feature:
// the bulk-dated photos must sort and filter into their period like any other.
// It checks the three readers of the time axis at once — the year facets, the
// timeline buckets and the query language's year: filter — plus the period
// bounds the library's picker sends.
func TestBulk_setTakenAtLandsInTheRightPeriod(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	scanned := []string{env.seedPhoto(t, "sc1"), env.seedPhoto(t, "sc2")}
	// One photo dated properly, in another year, so a facet that counted
	// everything would be indistinguishable from one that counted the right rows.
	other := env.seedPhoto(t, "other")
	otherTaken := time.Date(2019, time.August, 3, 12, 0, 0, 0, time.UTC)
	if _, err := env.photos.UpdateMetadata(ctx, other, photos.MetadataUpdate{
		TakenAt: &otherTaken, TakenAtSource: "exif", TakenAtPrecision: photos.TakenAtPrecisionDay,
	}); err != nil {
		t.Fatalf("date the control photo: %v", err)
	}

	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk",
		setTakenAtBody("year", "1974", scanned...))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	years, err := env.photos.YearBuckets(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("YearBuckets: %v", err)
	}
	if got := yearCount(years, 1974); got != len(scanned) {
		t.Errorf("year facet 1974 = %d, want %d", got, len(scanned))
	}
	if got := yearCount(years, 2019); got != 1 {
		t.Errorf("year facet 2019 = %d, want 1", got)
	}

	// The timeline groups by the same anchor, so the two scans have to sit in
	// January 1974 rather than in whatever month the library was imported in.
	buckets, err := env.photos.TimelineBuckets(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("TimelineBuckets: %v", err)
	}
	if got := bucketCount(buckets, 1974, 1); got != len(scanned) {
		t.Errorf("timeline bucket 1974-01 = %d, want %d", got, len(scanned))
	}

	// The year filter the facets link to, and the same year written as a query
	// token, must both return exactly those photos.
	year := 1974
	assertListedUIDs(t, ctx, env, photos.ListParams{Year: &year}, scanned)
	assertListedUIDs(t, ctx, env, photos.ListParams{
		QueryFilters: query.Parse("year:1974").Filters,
	}, scanned)

	// The period picker sends inclusive day bounds; a photo dated "1974" has to
	// fall inside the whole of 1974 and inside the seventies.
	from := time.Date(1974, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(1974, time.December, 31, 23, 59, 59, 999999000, time.UTC)
	assertListedUIDs(t, ctx, env, photos.ListParams{TakenAfter: &from, TakenBefore: &to}, scanned)
}

// yearCount returns the photo count the year facets report for a calendar year,
// or 0 when the year holds none.
func yearCount(years photos.Years, year int) int {
	for _, bucket := range years.Years {
		if bucket.Year == year {
			return bucket.Count
		}
	}
	return 0
}

// bucketCount returns the photo count of one timeline month, or 0 when absent.
func bucketCount(buckets photos.Timeline, year, month int) int {
	for _, b := range buckets.Buckets {
		if b.Year == year && b.Month == month {
			return b.Count
		}
	}
	return 0
}

// assertListedUIDs fails unless listing with params returns exactly want, in any
// order.
func assertListedUIDs(
	t *testing.T, ctx context.Context, env *env, params photos.ListParams, want []string,
) {
	t.Helper()
	got, err := env.photos.List(ctx, params)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seen := make(map[string]bool, len(got))
	for _, p := range got {
		seen[p.UID] = true
	}
	if len(got) != len(want) {
		t.Fatalf("List returned %d photos, want %d", len(got), len(want))
	}
	for _, uid := range want {
		if !seen[uid] {
			t.Errorf("List is missing %s", uid)
		}
	}
}
