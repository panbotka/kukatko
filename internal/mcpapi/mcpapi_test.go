package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// passthrough is a guard that authorises everything, for the tests that are not
// about authorisation.
func passthrough(next http.Handler) http.Handler { return next }

// callerCtx returns a context carrying a caller with the given role, as
// withCaller would have built it.
func callerCtx(role auth.Role) context.Context {
	return context.WithValue(context.Background(), callerKey{},
		caller{user: auth.User{UID: "user-1", Role: role}})
}

// TestRegisterRoutesDisabledMountsNothing pins the config switch's contract: off
// means the route does not exist, not that it exists and refuses. A 403 would
// still tell an attacker the endpoint is there.
//
// The enabled case answers 401 rather than 200 because the guard here is a
// passthrough that identifies nobody, and an unidentified caller must never reach
// a tool. That is the point: 401 proves the route is mounted *and* fails closed,
// where 404 proves it was never mounted.
//
// The 404 is chi's own, because this router has no SPA fallback. The full server
// does (server.routes mounts web.Handler on NotFound), so there a disabled path
// falls through to index.html like any unknown URL — the assertion here is the
// clean signal that nothing was registered on the router at all.
func TestRegisterRoutesDisabledMountsNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    int
	}{
		{name: "disabled", enabled: false, want: http.StatusNotFound},
		{name: "enabled", enabled: true, want: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := NewAPI(Config{Enabled: tc.enabled, RequireAuth: passthrough})
			r := chi.NewRouter()
			r.Route("/api/v1", api.RegisterRoutes)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/mcp",
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("POST /api/v1/mcp status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// TestNewAPIDisabledBuildsNoServer checks that a disabled server is not merely
// unmounted but never built, so an instance that has not asked for MCP carries
// none of it.
func TestNewAPIDisabledBuildsNoServer(t *testing.T) {
	if api := NewAPI(Config{Enabled: false, RequireAuth: passthrough}); api.handler != nil {
		t.Fatal("disabled API built an MCP handler")
	}
}

// TestWriterFromContextEnforcesRole pins the second lock on the write door: even
// if a write tool were reachable, a read-only role must not get through it.
func TestWriterFromContextEnforcesRole(t *testing.T) {
	for _, tc := range []struct {
		role    auth.Role
		wantErr bool
	}{
		{role: auth.RoleViewer, wantErr: true},
		{role: auth.RoleEditor, wantErr: false},
		{role: auth.RoleAdmin, wantErr: false},
		{role: auth.RoleMaintainer, wantErr: false},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			_, err := writerFromContext(callerCtx(tc.role))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("writerFromContext(%s) = nil error, want refusal", tc.role)
				}
				return
			}
			if err != nil {
				t.Fatalf("writerFromContext(%s) = %v, want nil", tc.role, err)
			}
		})
	}
}

// TestCallerFromContextWithoutCaller checks the tools fail closed rather than
// attributing a change to nobody.
func TestCallerFromContextWithoutCaller(t *testing.T) {
	if _, err := callerFromContext(context.Background()); err == nil {
		t.Fatal("callerFromContext on a bare context = nil error, want a refusal")
	}
	if _, err := writerFromContext(context.Background()); err == nil {
		t.Fatal("writerFromContext on a bare context = nil error, want a refusal")
	}
}

// TestPhotoDetailNeverCarriesExif is the spec's compactness rule as a test: the
// raw EXIF blob must not reach an agent from any tool, so a photo carrying one
// must still serialise without it.
func TestPhotoDetailNeverCarriesExif(t *testing.T) {
	taken := time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC)
	photo := photos.Photo{
		UID:      "p1",
		Title:    "babička",
		TakenAt:  &taken,
		FileName: "IMG_1.jpg",
		Exif:     json.RawMessage(`{"Make":"Zorki","secret":"do-not-leak"}`),
	}
	for name, payload := range map[string]any{
		"detail":  toPhotoDetail(photo),
		"summary": (&API{}).summarize([]photos.Photo{photo}),
	} {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		for _, leak := range []string{"exif", "Zorki", "do-not-leak"} {
			if strings.Contains(string(encoded), leak) {
				t.Errorf("%s payload leaked %q: %s", name, leak, encoded)
			}
		}
	}
}

// TestSummarizeShape checks a list row carries the four useful fields and stops.
func TestSummarizeShape(t *testing.T) {
	taken := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	got := (&API{}).summarize([]photos.Photo{{
		UID: "p1", Title: "t", TakenAt: &taken, MediaType: photos.MediaImage,
	}})
	if len(got) != 1 {
		t.Fatalf("summarize returned %d rows, want 1", len(got))
	}
	if got[0].TakenAt != "2001-02-03T04:05:06Z" {
		t.Errorf("TakenAt = %q, want RFC 3339", got[0].TakenAt)
	}
	if got[0].UID != "p1" || got[0].Title != "t" {
		t.Errorf("summary = %+v, want uid p1 title t", got[0])
	}
}

// TestPageCounters pins the "say how many more there are" rule, including the
// clamp that keeps a stale count from reporting a negative remainder.
func TestPageCounters(t *testing.T) {
	rows := []photoSummary{{UID: "a"}, {UID: "b"}}
	for _, tc := range []struct {
		name          string
		total, offset int
		wantRemaining int
	}{
		{name: "more to come", total: 10, offset: 0, wantRemaining: 8},
		{name: "last page", total: 2, offset: 0, wantRemaining: 0},
		{name: "middle page", total: 5, offset: 2, wantRemaining: 1},
		{name: "count shrank under us", total: 1, offset: 0, wantRemaining: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := page(rows, tc.total, tc.offset)
			if got.Remaining != tc.wantRemaining {
				t.Errorf("Remaining = %d, want %d", got.Remaining, tc.wantRemaining)
			}
			if got.Total != tc.total || got.Offset != tc.offset {
				t.Errorf("page = %+v, want total %d offset %d", got, tc.total, tc.offset)
			}
		})
	}
}

// TestClampLimit checks the page-size bounds: unset falls back, oversized is
// trimmed rather than refused.
func TestClampLimit(t *testing.T) {
	api := &API{pageSize: 25, maxPageSize: 100}
	for _, tc := range []struct{ in, want int }{
		{in: 0, want: 25},
		{in: -5, want: 25},
		{in: 10, want: 10},
		{in: 1000, want: 100},
	} {
		if got := api.clampLimit(tc.in); got != tc.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestPositiveOr checks a misconfigured bound degrades to the package default.
func TestPositiveOr(t *testing.T) {
	for _, tc := range []struct{ in, fallback, want int }{
		{in: 0, fallback: 25, want: 25},
		{in: -1, fallback: 25, want: 25},
		{in: 7, fallback: 25, want: 7},
	} {
		if got := positiveOr(tc.in, tc.fallback); got != tc.want {
			t.Errorf("positiveOr(%d, %d) = %d, want %d", tc.in, tc.fallback, got, tc.want)
		}
	}
}

// TestSearchParamsUsesQueryLanguage checks the search tool is wired to the real
// query parser, scopes the per-user filters to the caller, and only takes the
// ranked path when the caller has not asked for an order of their own.
func TestSearchParamsUsesQueryLanguage(t *testing.T) {
	api := &API{pageSize: 25, maxPageSize: 100}
	c := caller{user: auth.User{UID: "user-1", Role: auth.RoleViewer}}

	params, ranked, err := api.searchParams(c, searchPhotosIn{Query: "babicka year:1965"})
	if err != nil {
		t.Fatalf("searchParams: %v", err)
	}
	if !ranked {
		t.Error("free text with no explicit sort should take the ranked path")
	}
	if params.FullText != "babicka" {
		t.Errorf("FullText = %q, want the free text only", params.FullText)
	}
	if !hasFilter(params.QueryFilters, query.KeyYear) {
		t.Errorf("QueryFilters = %+v, want the year: filter parsed", params.QueryFilters)
	}
	if params.RatedBy == nil || *params.RatedBy != "user-1" {
		t.Errorf("RatedBy = %v, want the caller's uid so favorite:/rating: mean them", params.RatedBy)
	}

	// An explicit sort must win over relevance ranking.
	params, ranked, err = api.searchParams(c, searchPhotosIn{Query: "babicka", Sort: "taken_at"})
	if err != nil {
		t.Fatalf("searchParams with sort: %v", err)
	}
	if ranked {
		t.Error("an explicit sort should take the list path, not the ranked one")
	}
	if params.Sort != photos.SortByTakenAt {
		t.Errorf("Sort = %v, want SortByTakenAt", params.Sort)
	}
}

// TestSearchParamsScopes checks the uid scopes reach the store's filters, which
// is what makes "an album's photos" a search rather than a separate tool.
func TestSearchParamsScopes(t *testing.T) {
	api := &API{pageSize: 25, maxPageSize: 100}
	c := caller{user: auth.User{UID: "user-1", Role: auth.RoleViewer}}
	params, _, err := api.searchParams(c, searchPhotosIn{
		AlbumUID: "a1", LabelUID: " l1 ", PersonUID: "s1", Offset: -3,
	})
	if err != nil {
		t.Fatalf("searchParams: %v", err)
	}
	if len(params.AlbumUIDs) != 1 || params.AlbumUIDs[0] != "a1" {
		t.Errorf("AlbumUIDs = %v, want [a1]", params.AlbumUIDs)
	}
	if len(params.LabelUIDs) != 1 || params.LabelUIDs[0] != "l1" {
		t.Errorf("LabelUIDs = %v, want [l1] (trimmed)", params.LabelUIDs)
	}
	if len(params.SubjectUIDs) != 1 || params.SubjectUIDs[0] != "s1" {
		t.Errorf("SubjectUIDs = %v, want [s1]", params.SubjectUIDs)
	}
	if params.Offset != 0 {
		t.Errorf("Offset = %d, want a negative offset clamped to 0", params.Offset)
	}
}

// TestSearchParamsCapsComplexity checks the MCP search tool enforces the same
// query-complexity cap as the HTTP endpoints, so this equivalent path cannot be
// used to force an authenticated slow-query DoS. A query packing more
// '|'-alternatives than query.MaxComplexity is rejected; a normal query is not.
func TestSearchParamsCapsComplexity(t *testing.T) {
	api := &API{pageSize: 25, maxPageSize: 100}
	c := caller{user: auth.User{UID: "user-1", Role: auth.RoleViewer}}

	overComplex := "title:" + strings.Repeat("a|", query.MaxComplexity) + "a"
	if _, _, err := api.searchParams(c, searchPhotosIn{Query: overComplex}); err == nil {
		t.Error("searchParams accepted an over-complex query, want a cap error")
	}

	overLong := strings.Repeat("a", query.MaxLength+1)
	if _, _, err := api.searchParams(c, searchPhotosIn{Query: overLong}); err == nil {
		t.Error("searchParams accepted an over-long query, want a cap error")
	}

	if _, _, err := api.searchParams(c, searchPhotosIn{Query: "babicka label:cat|dog"}); err != nil {
		t.Errorf("searchParams rejected a normal query: %v", err)
	}
}

// TestApplySortRejectsUnknown checks a mistyped sort is an error naming the
// alternatives, not a silent fallback that answers the wrong question.
func TestApplySortRejectsUnknown(t *testing.T) {
	var params photos.ListParams
	err := applySort(&params, "newest", "")
	if err == nil {
		t.Fatal("applySort(newest) = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "taken_at") {
		t.Errorf("error %q should name the valid sorts", err)
	}
	if err := applySort(&params, "", "sideways"); err == nil {
		t.Fatal("applySort with a bad order = nil error, want a refusal")
	}
	if err := applySort(&params, "title", "asc"); err != nil {
		t.Fatalf("applySort(title, asc) = %v, want nil", err)
	}
	if params.Sort != photos.SortByTitle || params.Order != photos.OrderAsc {
		t.Errorf("params = %+v, want title/asc applied", params)
	}
}

// TestMatchesName checks the listing filter is case-insensitive and that an
// empty filter keeps everything.
func TestMatchesName(t *testing.T) {
	for _, tc := range []struct {
		name, filter string
		want         bool
	}{
		{name: "Dovolená 2019", filter: "", want: true},
		{name: "Dovolená 2019", filter: "dovolená", want: true},
		{name: "Dovolená 2019", filter: "DOVOLENÁ", want: true},
		{name: "Dovolená 2019", filter: "  ", want: true},
		{name: "Dovolená 2019", filter: "vánoce", want: false},
	} {
		if got := matchesName(tc.name, tc.filter); got != tc.want {
			t.Errorf("matchesName(%q, %q) = %v, want %v", tc.name, tc.filter, got, tc.want)
		}
	}
}

// TestCleanUIDs checks the uid lists are trimmed and emptied entries dropped, so
// a sloppy agent argument cannot reach the store as a blank uid.
func TestCleanUIDs(t *testing.T) {
	got := cleanUIDs([]string{" a ", "", "  ", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("cleanUIDs = %v, want [a b]", got)
	}
	if got := cleanUIDs(nil); len(got) != 0 {
		t.Errorf("cleanUIDs(nil) = %v, want empty", got)
	}
}

// TestMetadataOfPreservesEverything is the guard against the read-modify-write
// trap: the store's update is a whole-record replace, so a partial edit that did
// not carry a field would silently blank it.
func TestMetadataOfPreservesEverything(t *testing.T) {
	taken := time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC)
	lat, lng := 50.0, 14.0
	photo := photos.Photo{
		Title: "t", Description: "d", Notes: "n", AiNote: "ai", Subject: "s",
		Keywords: "k", Artist: "a", Copyright: "c", License: "l", Scan: true,
		TakenAt: &taken, TakenAtSource: "exif", TakenAtEstimated: true, TakenAtNote: "za války",
		Lat: &lat, Lng: &lng, LocationSource: "manual", Private: true,
	}
	upd := metadataOf(photo)

	// Only the three text fields a tool may touch; everything else must survive.
	newTitle := "new"
	applyString(&upd.Title, &newTitle)
	applyString(&upd.Description, nil)

	if upd.Title != "new" {
		t.Errorf("Title = %q, want the new value", upd.Title)
	}
	if upd.Description != "d" || upd.Notes != "n" {
		t.Errorf("an omitted field was blanked: %+v", upd)
	}
	if upd.Keywords != "k" || upd.Artist != "a" || upd.License != "l" || !upd.Scan {
		t.Errorf("an untouched column was dropped: %+v", upd)
	}
	if upd.TakenAt == nil || !upd.TakenAtEstimated || upd.TakenAtNote != "za války" {
		t.Errorf("the capture date was dropped: %+v", upd)
	}
	if upd.Lat == nil || upd.Lng == nil || upd.LocationSource != "manual" || !upd.Private {
		t.Errorf("the location or privacy was dropped: %+v", upd)
	}
}

// TestChangedFields checks the audit details name what actually changed.
func TestChangedFields(t *testing.T) {
	v := "x"
	got := changedFields(setMetadataIn{Title: &v, Notes: &v})
	if len(got) != 2 || got[0] != "title" || got[1] != "notes" {
		t.Errorf("changedFields = %v, want [title notes]", got)
	}
	if got := changedFields(setMetadataIn{}); got != nil {
		t.Errorf("changedFields on an empty edit = %v, want nil", got)
	}
}

// TestFormatTime checks an unknown capture time is absent rather than a zero date.
func TestFormatTime(t *testing.T) {
	if got := formatTime(nil); got != "" {
		t.Errorf("formatTime(nil) = %q, want empty", got)
	}
	at := time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC)
	if got := formatTime(&at); got != "1965-06-01T12:00:00Z" {
		t.Errorf("formatTime = %q, want RFC 3339", got)
	}
}

// TestDescribeKey checks a not-found error repeats back the key that was used.
func TestDescribeKey(t *testing.T) {
	if got := describeKey("u1", ""); !strings.Contains(got, "uid") {
		t.Errorf("describeKey = %q, want it to name the uid", got)
	}
	if got := describeKey("", "s1"); !strings.Contains(got, "slug") {
		t.Errorf("describeKey = %q, want it to name the slug", got)
	}
}

// TestTypeFilterMatchesQueryLanguage checks the stats counter and a type:video
// search agree by construction rather than by coincidence.
func TestTypeFilterMatchesQueryLanguage(t *testing.T) {
	got := typeFilter("video")
	want := query.Parse("type:video").Filters
	if len(got) != 1 || len(want) != 1 {
		t.Fatalf("typeFilter = %+v, parsed = %+v, want one filter each", got, want)
	}
	if got[0].Key != want[0].Key || got[0].Values[0].Text != want[0].Values[0].Text {
		t.Errorf("typeFilter = %+v, want it to equal the parsed %+v", got[0], want[0])
	}
}

// hasFilter reports whether the parsed filters carry the given key.
func hasFilter(filters []query.Filter, key query.Key) bool {
	for _, f := range filters {
		if f.Key == key {
			return true
		}
	}
	return false
}
