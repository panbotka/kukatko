package photoapi

import (
	"net/url"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// parse is a test helper turning a raw query string into list parameters.
func parse(t *testing.T, query string) (photos.ListParams, error) {
	t.Helper()
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", query, err)
	}
	return parseListParams(q)
}

// TestParseListParams_valid verifies that each recognised filter, sort and
// pagination value maps onto the expected ListParams field.
func TestParseListParams_valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		check func(t *testing.T, p photos.ListParams)
	}{
		{
			name:  "empty query yields defaults",
			query: "",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Limit != 0 || p.Offset != 0 || p.IncludeArchived || p.OnlyArchived {
					t.Errorf("unexpected defaults: %+v", p)
				}
			},
		},
		{
			name:  "limit and offset",
			query: "limit=20&offset=40",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Limit != 20 || p.Offset != 40 {
					t.Errorf("limit/offset = %d/%d, want 20/40", p.Limit, p.Offset)
				}
			},
		},
		{
			name:  "limit clamped to max",
			query: "limit=99999",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Limit != maxListLimit {
					t.Errorf("limit = %d, want clamp to %d", p.Limit, maxListLimit)
				}
			},
		},
		{
			name:  "sort newest",
			query: "sort=newest",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Sort != photos.SortByTakenAt || p.Order != photos.OrderDesc {
					t.Errorf("sort = %v/%v, want taken_at/desc", p.Sort, p.Order)
				}
			},
		},
		{
			name:  "sort oldest",
			query: "sort=oldest",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Sort != photos.SortByTakenAt || p.Order != photos.OrderAsc {
					t.Errorf("sort = %v/%v, want taken_at/asc", p.Sort, p.Order)
				}
			},
		},
		{
			name:  "sort title with order override",
			query: "sort=title&order=desc",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Sort != photos.SortByTitle || p.Order != photos.OrderDesc {
					t.Errorf("sort = %v/%v, want title/desc", p.Sort, p.Order)
				}
			},
		},
		{
			name:  "sort size and added",
			query: "sort=size",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Sort != photos.SortBySize || p.Order != photos.OrderDesc {
					t.Errorf("sort = %v/%v, want file_size/desc", p.Sort, p.Order)
				}
			},
		},
		{
			name:  "archived true",
			query: "archived=true",
			check: func(t *testing.T, p photos.ListParams) {
				if !p.IncludeArchived || p.OnlyArchived {
					t.Errorf("archived flags = %v/%v, want include only", p.IncludeArchived, p.OnlyArchived)
				}
			},
		},
		{
			name:  "archived only",
			query: "archived=only",
			check: func(t *testing.T, p photos.ListParams) {
				if !p.OnlyArchived {
					t.Errorf("OnlyArchived = false, want true")
				}
			},
		},
		{
			name:  "private and has_gps booleans",
			query: "private=true&has_gps=false",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Private == nil || !*p.Private {
					t.Errorf("Private = %v, want true", p.Private)
				}
				if p.HasGPS == nil || *p.HasGPS {
					t.Errorf("HasGPS = %v, want false", p.HasGPS)
				}
			},
		},
		{
			name:  "date range rfc3339 and date-only",
			query: "taken_after=2023-01-02T03:04:05Z&taken_before=2023-12-31",
			check: func(t *testing.T, p photos.ListParams) {
				wantAfter := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
				wantBefore := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
				if p.TakenAfter == nil || !p.TakenAfter.Equal(wantAfter) {
					t.Errorf("TakenAfter = %v, want %v", p.TakenAfter, wantAfter)
				}
				if p.TakenBefore == nil || !p.TakenBefore.Equal(wantBefore) {
					t.Errorf("TakenBefore = %v, want %v", p.TakenBefore, wantBefore)
				}
			},
		},
		{
			name:  "text filters",
			query: "camera=Canon&lens=RF&uploader=us123&q=beach",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Camera != "Canon" || p.Lens != "RF" || p.UploadedBy != "us123" || p.Search != "beach" {
					t.Errorf("text filters mismapped: %+v", p)
				}
			},
		},
		{
			name:  "album and label scope",
			query: "album=al_1&label=lb_2",
			check: func(t *testing.T, p photos.ListParams) {
				if p.AlbumUID != "al_1" || p.LabelUID != "lb_2" {
					t.Errorf("scope filters mismapped: album=%q label=%q", p.AlbumUID, p.LabelUID)
				}
			},
		},
		{
			name:  "country and city place scope",
			query: "country=Czechia&city=Praha",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Country != "Czechia" || p.City != "Praha" {
					t.Errorf("place filters mismapped: country=%q city=%q", p.Country, p.City)
				}
			},
		},
		{
			name:  "sort rating",
			query: "sort=rating",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Sort != photos.SortByRating || p.Order != photos.OrderDesc {
					t.Errorf("sort = %v/%v, want rating/desc", p.Sort, p.Order)
				}
			},
		},
		{
			name:  "min_rating and flag filters",
			query: "min_rating=3&flag=pick",
			check: func(t *testing.T, p photos.ListParams) {
				if p.MinRating == nil || *p.MinRating != 3 {
					t.Errorf("MinRating = %v, want 3", p.MinRating)
				}
				if p.Flag == nil || *p.Flag != "pick" {
					t.Errorf("Flag = %v, want pick", p.Flag)
				}
			},
		},
		{
			name:  "flag reject",
			query: "flag=reject",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Flag == nil || *p.Flag != "reject" {
					t.Errorf("Flag = %v, want reject", p.Flag)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := parse(t, tt.query)
			if err != nil {
				t.Fatalf("parseListParams(%q) error: %v", tt.query, err)
			}
			tt.check(t, p)
		})
	}
}

// TestParseListParams_invalid verifies that malformed values are rejected so the
// handler can answer 400.
func TestParseListParams_invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{name: "non-integer limit", query: "limit=lots"},
		{name: "negative limit", query: "limit=-1"},
		{name: "negative offset", query: "offset=-5"},
		{name: "unknown sort", query: "sort=color"},
		{name: "unknown order", query: "order=sideways"},
		{name: "unknown archived", query: "archived=maybe"},
		{name: "non-bool private", query: "private=sometimes"},
		{name: "non-bool has_gps", query: "has_gps=42"},
		{name: "bad taken_after", query: "taken_after=yesterday"},
		{name: "bad taken_before", query: "taken_before=2023/01/01"},
		{name: "non-integer min_rating", query: "min_rating=many"},
		{name: "unknown flag", query: "flag=star"},
		{name: "flag none rejected as filter", query: "flag=none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parse(t, tt.query); err == nil {
				t.Errorf("parseListParams(%q) = nil error, want validation error", tt.query)
			}
		})
	}
}
