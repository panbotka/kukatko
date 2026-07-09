package ctl

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// TestListOptions_query verifies each filter maps onto the query parameter the
// API actually understands, and that omitted filters are omitted entirely so the
// server's own defaults apply.
func TestListOptions_query(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts ListOptions
		want url.Values
	}{
		{name: "zero value sends nothing", opts: ListOptions{}, want: url.Values{}},
		{
			name: "paging",
			opts: ListOptions{Limit: 50, Offset: 100},
			want: url.Values{"limit": {"50"}, "offset": {"100"}},
		},
		{
			name: "sort and order",
			opts: ListOptions{Sort: "title", Order: "asc"},
			want: url.Values{"sort": {"title"}, "order": {"asc"}},
		},
		{
			name: "year becomes an inclusive taken_at range",
			opts: ListOptions{Year: 2024},
			want: url.Values{
				"taken_after":  {"2024-01-01T00:00:00Z"},
				"taken_before": {"2024-12-31T23:59:59.999999999Z"},
			},
		},
		{
			name: "album and label scopes",
			opts: ListOptions{Album: "alb1", Label: "lbl1"},
			want: url.Values{"album": {"alb1"}, "label": {"lbl1"}},
		},
		{name: "favorite", opts: ListOptions{Favorite: true}, want: url.Values{"favorite": {"true"}}},
		{name: "archived only", opts: ListOptions{Archived: "only"}, want: url.Values{"archived": {"only"}}},
		{name: "archived true", opts: ListOptions{Archived: "true"}, want: url.Values{"archived": {"true"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.opts.query()
			if err != nil {
				t.Fatalf("query() returned %v", err)
			}
			if got.Encode() != tt.want.Encode() {
				t.Errorf("query() = %q, want %q", got.Encode(), tt.want.Encode())
			}
		})
	}
}

// TestListOptions_query_invalid verifies obviously bad input is caught before a
// round trip is spent on it.
func TestListOptions_query_invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    ListOptions
		wantErr error
	}{
		{name: "negative limit", opts: ListOptions{Limit: -1}, wantErr: ErrInvalidPaging},
		{name: "negative offset", opts: ListOptions{Offset: -5}, wantErr: ErrInvalidPaging},
		{name: "year too small", opts: ListOptions{Year: 1799}, wantErr: ErrInvalidYear},
		{name: "year too large", opts: ListOptions{Year: 10000}, wantErr: ErrInvalidYear},
		{name: "unknown archived", opts: ListOptions{Archived: "yes"}, wantErr: ErrInvalidArchived},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tt.opts.query(); !errors.Is(err, tt.wantErr) {
				t.Errorf("query() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestYearBounds verifies the range brackets the whole calendar year, including
// a leap day, with an upper bound the API's taken_at <= comparison accepts.
func TestYearBounds(t *testing.T) {
	t.Parallel()

	after, before := yearBounds(2024)
	if after != "2024-01-01T00:00:00Z" {
		t.Errorf("after = %q, want the first instant of the year", after)
	}
	if before != "2024-12-31T23:59:59.999999999Z" {
		t.Errorf("before = %q, want the last instant of the year", before)
	}
}

// TestSearchOptions_query verifies the query text, the mode and the shared list
// filters all reach the API.
func TestSearchOptions_query(t *testing.T) {
	t.Parallel()

	got, err := SearchOptions{
		Query: "sunset over the lake",
		Mode:  SearchSemantic,
		List:  ListOptions{Limit: 10, Year: 2024},
	}.query()
	if err != nil {
		t.Fatalf("query() returned %v", err)
	}
	if got.Get("q") != "sunset over the lake" {
		t.Errorf("q = %q, want the search text", got.Get("q"))
	}
	if got.Get("mode") != SearchSemantic {
		t.Errorf("mode = %q, want %q", got.Get("mode"), SearchSemantic)
	}
	if got.Get("limit") != "10" || got.Get("taken_after") == "" {
		t.Errorf("query() = %q, want the list filters merged in", got.Encode())
	}
}

// TestSearchOptions_query_dropsFavorite pins the one filter GET /search does not
// implement. handleSearch never calls favoriteRequested, so forwarding the
// parameter would promise a filter the server silently ignores.
func TestSearchOptions_query_dropsFavorite(t *testing.T) {
	t.Parallel()

	got, err := SearchOptions{
		Query: "lake",
		List:  ListOptions{Favorite: true, Album: "alb1"},
	}.query()
	if err != nil {
		t.Fatalf("query() returned %v", err)
	}
	if got.Has("favorite") {
		t.Errorf("query() = %q, want no favorite parameter", got.Encode())
	}
	if got.Get("album") != "alb1" {
		t.Errorf("album = %q, want the filters search does honour to survive", got.Get("album"))
	}
}

// TestSearchOptions_query_invalid verifies a blank query and an unknown mode are
// rejected client-side, and that an empty mode simply defers to the API default.
func TestSearchOptions_query_invalid(t *testing.T) {
	t.Parallel()

	if _, err := (SearchOptions{Query: ""}).query(); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("blank query error = %v, want ErrEmptyQuery", err)
	}
	if _, err := (SearchOptions{Query: "x", Mode: "magic"}).query(); !errors.Is(err, ErrInvalidSearchMode) {
		t.Errorf("unknown mode error = %v, want ErrInvalidSearchMode", err)
	}
	got, err := (SearchOptions{Query: "x"}).query()
	if err != nil {
		t.Fatalf("empty mode returned %v", err)
	}
	if got.Has("mode") {
		t.Errorf("empty mode sent %q, want the parameter omitted", got.Get("mode"))
	}
	if _, err := (SearchOptions{Query: "x", List: ListOptions{Limit: -1}}).query(); !errors.Is(err, ErrInvalidPaging) {
		t.Errorf("bad paging error = %v, want ErrInvalidPaging", err)
	}
}

// listBody is a realistic /photos envelope: rows plus the paging fields. It
// deliberately mirrors the API's shape, which no other resource shares.
const listBody = `{"photos":[
	{"uid":"pht01","file_name":"a.jpg","file_size":2097152,"media_type":"image",
	 "taken_at":"2024-05-01T10:22:33Z","title":"Lake","is_favorite":true,"rating":4,"flag":"pick"},
	{"uid":"pht02","file_name":"b.mp4","file_size":10485760,"media_type":"video","title":""}
],"total":42,"limit":2,"offset":0,"next_offset":2}`

// TestClient_ListPhotos verifies the filters reach the wire and the envelope
// decodes, raw bytes and all.
func TestClient_ListPhotos(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(listBody))
	})

	raw, err := client.ListPhotos(t.Context(), ListOptions{Limit: 2, Year: 2024, Favorite: true})
	if err != nil {
		t.Fatalf("ListPhotos returned %v", err)
	}
	if gotQuery.Get("limit") != "2" || gotQuery.Get("favorite") != "true" ||
		gotQuery.Get("taken_after") != "2024-01-01T00:00:00Z" {
		t.Errorf("query = %v, want limit, favorite and the year range", gotQuery)
	}
	if !json.Valid(raw) {
		t.Fatal("ListPhotos returned invalid JSON")
	}

	page, err := DecodePhotoPage(raw)
	if err != nil {
		t.Fatalf("DecodePhotoPage returned %v", err)
	}
	if len(page.Photos) != 2 || page.Total != 42 || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatalf("page = %+v, want 2 rows of 42 with next_offset 2", page)
	}
	first := page.Photos[0]
	if first.UID != "pht01" || first.Title != "Lake" || !first.IsFavorite || first.Rating != 4 {
		t.Errorf("first photo = %+v, want the decoded row", first)
	}
	if first.TakenAt == nil || first.TakenAt.Year() != 2024 {
		t.Errorf("taken_at = %v, want a 2024 timestamp", first.TakenAt)
	}
}

// TestClient_ListPhotos_empty verifies an empty result set decodes to an empty
// page rather than an error.
func TestClient_ListPhotos_empty(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	})

	raw, err := client.ListPhotos(t.Context(), ListOptions{})
	if err != nil {
		t.Fatalf("ListPhotos returned %v", err)
	}
	page, err := DecodePhotoPage(raw)
	if err != nil {
		t.Fatalf("DecodePhotoPage returned %v", err)
	}
	if len(page.Photos) != 0 || page.Total != 0 || page.NextOffset != nil {
		t.Errorf("page = %+v, want an empty page", page)
	}
}

// TestClient_ListPhotos_invalidOptions verifies a bad option never reaches the
// network.
func TestClient_ListPhotos_invalidOptions(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid options")
	})
	if _, err := client.ListPhotos(t.Context(), ListOptions{Year: 1}); !errors.Is(err, ErrInvalidYear) {
		t.Errorf("ListPhotos error = %v, want ErrInvalidYear", err)
	}
}

// TestClient_GetPhoto verifies the uid is escaped into the path and the detail
// envelope decodes, memberships and files included.
func TestClient_GetPhoto(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"uid":"pht01","title":"Lake","file_name":"a.jpg","file_size":1024,
			"file_width":800,"file_height":600,"camera_make":"Canon","camera_model":"R6",
			"lat":50.1,"lng":14.4,"is_favorite":true,
			"files":[{"file_path":"2024/05/abc.jpg","is_primary":true,"role":"primary"}],
			"albums":[{"uid":"alb1","title":"Trip"}],"labels":[{"uid":"lbl1","name":"lake"}]}`))
	})

	raw, err := client.GetPhoto(t.Context(), "pht 01")
	if err != nil {
		t.Fatalf("GetPhoto returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht 01" {
		t.Errorf("path = %q, want the escaped uid", gotPath)
	}
	detail, err := DecodePhotoDetail(raw)
	if err != nil {
		t.Fatalf("DecodePhotoDetail returned %v", err)
	}
	if detail.UID != "pht01" || detail.CameraModel != "R6" || len(detail.Files) != 1 {
		t.Fatalf("detail = %+v, want the decoded photo", detail)
	}
	if len(detail.Albums) != 1 || detail.Albums[0].Label() != "Trip" {
		t.Errorf("albums = %+v, want the Trip album", detail.Albums)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].Label() != "lake" {
		t.Errorf("labels = %+v, want the lake label", detail.Labels)
	}
	if detail.Lat == nil || detail.Lng == nil {
		t.Error("GPS coordinates did not decode")
	}
}

// TestClient_GetPhoto_emptyUID verifies a blank uid is rejected client-side, so
// it can never be mistaken for a list request.
func TestClient_GetPhoto_emptyUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank uid")
	})
	if _, err := client.GetPhoto(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("GetPhoto(\"\") error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_SearchPhotos verifies the search endpoint is used and that the
// degraded flag — set when the embeddings sidecar is offline — survives decoding.
func TestClient_SearchPhotos(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null,
			"mode":"fulltext","degraded":true}`))
	})

	raw, err := client.SearchPhotos(t.Context(), SearchOptions{Query: "lake", Mode: SearchHybrid})
	if err != nil {
		t.Fatalf("SearchPhotos returned %v", err)
	}
	if gotPath != "/api/v1/search" {
		t.Errorf("path = %q, want /api/v1/search", gotPath)
	}
	page, err := DecodePhotoPage(raw)
	if err != nil {
		t.Fatalf("DecodePhotoPage returned %v", err)
	}
	if page.Mode != SearchFulltext || !page.Degraded {
		t.Errorf("page = %+v, want a degraded fulltext result", page)
	}
}

// TestDecodePhotoPage_invalid verifies malformed JSON surfaces as an error rather
// than a silently empty page.
func TestDecodePhotoPage_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodePhotoPage([]byte(`{"photos":`)); err == nil {
		t.Error("DecodePhotoPage of malformed JSON returned no error")
	}
	if _, err := DecodePhotoDetail([]byte(`not json`)); err == nil {
		t.Error("DecodePhotoDetail of malformed JSON returned no error")
	}
}

// TestNamedRef_Label verifies albums render their title and labels their name.
func TestNamedRef_Label(t *testing.T) {
	t.Parallel()

	if got := (NamedRef{UID: "a", Title: "Trip"}).Label(); got != "Trip" {
		t.Errorf("album Label() = %q, want Trip", got)
	}
	if got := (NamedRef{UID: "l", Name: "lake"}).Label(); got != "lake" {
		t.Errorf("label Label() = %q, want lake", got)
	}
	if got := (NamedRef{UID: "x"}).Label(); got != "" {
		t.Errorf("bare Label() = %q, want empty", got)
	}
}
