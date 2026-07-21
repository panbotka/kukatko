package photoapi

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// parse is a test helper turning a raw query string into list parameters,
// dropping the unknown-token report (parseUnknown covers it).
func parse(t *testing.T, query string) (photos.ListParams, error) {
	t.Helper()
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", query, err)
	}
	params, _, err := parseListParams(q)
	return params, err
}

// parseUnknown is a test helper returning the unknown q-token report of
// parseListParams for a raw query string.
func parseUnknown(t *testing.T, query string) []string {
	t.Helper()
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", query, err)
	}
	_, unknown, err := parseListParams(q)
	if err != nil {
		t.Fatalf("parseListParams(%q) error: %v", query, err)
	}
	return unknown
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
			name:  "has_gps boolean",
			query: "has_gps=false",
			check: func(t *testing.T, p photos.ListParams) {
				if p.HasGPS == nil || *p.HasGPS {
					t.Errorf("HasGPS = %v, want false", p.HasGPS)
				}
			},
		},
		{
			// The private filter is gone; a stale param from a bookmarked URL is
			// just an unknown key and must be ignored, not rejected.
			name:  "stale private param is ignored",
			query: "private=true&has_gps=true",
			check: func(t *testing.T, p photos.ListParams) {
				if p.HasGPS == nil || !*p.HasGPS {
					t.Errorf("HasGPS = %v, want true", p.HasGPS)
				}
			},
		},
		{
			name:  "year facet",
			query: "year=2023",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Year == nil || *p.Year != 2023 {
					t.Errorf("Year = %v, want 2023", p.Year)
				}
			},
		},
		{
			name:  "absent year is no filter",
			query: "camera=leica",
			check: func(t *testing.T, p photos.ListParams) {
				if p.Year != nil {
					t.Errorf("Year = %v, want nil", p.Year)
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
				if len(p.AlbumUIDs) != 1 || p.AlbumUIDs[0] != "al_1" {
					t.Errorf("album scope mismapped: %v", p.AlbumUIDs)
				}
				if len(p.LabelUIDs) != 1 || p.LabelUIDs[0] != "lb_2" {
					t.Errorf("label scope mismapped: %v", p.LabelUIDs)
				}
			},
		},
		{
			name:  "repeated album and label params select several (AND)",
			query: "album=al_1&album=al_2&label=lb_1&label=lb_2",
			check: func(t *testing.T, p photos.ListParams) {
				if len(p.AlbumUIDs) != 2 || p.AlbumUIDs[0] != "al_1" || p.AlbumUIDs[1] != "al_2" {
					t.Errorf("repeated album params mismapped: %v", p.AlbumUIDs)
				}
				if len(p.LabelUIDs) != 2 || p.LabelUIDs[0] != "lb_1" || p.LabelUIDs[1] != "lb_2" {
					t.Errorf("repeated label params mismapped: %v", p.LabelUIDs)
				}
			},
		},
		{
			name:  "person scope maps repeated params (AND)",
			query: "person=su_1&person=su_2",
			check: func(t *testing.T, p photos.ListParams) {
				if len(p.SubjectUIDs) != 2 || p.SubjectUIDs[0] != "su_1" || p.SubjectUIDs[1] != "su_2" {
					t.Errorf("person scope mismapped: %v", p.SubjectUIDs)
				}
			},
		},
		{
			name:  "empty person value adds no scope",
			query: "person=",
			check: func(t *testing.T, p photos.ListParams) {
				if len(p.SubjectUIDs) != 0 {
					t.Errorf("empty person value should add no scope, got %v", p.SubjectUIDs)
				}
			},
		},
		{
			name:  "empty album value adds no scope",
			query: "album=&label=lb_1",
			check: func(t *testing.T, p photos.ListParams) {
				if len(p.AlbumUIDs) != 0 {
					t.Errorf("empty album value should add no scope, got %v", p.AlbumUIDs)
				}
				if len(p.LabelUIDs) != 1 || p.LabelUIDs[0] != "lb_1" {
					t.Errorf("label scope mismapped: %v", p.LabelUIDs)
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

// TestParseListParams_albumForcesChronology verifies that an album scope pins
// the ordering to oldest-first chronology whatever sort and order the query
// carries, while a query without the scope keeps its requested sort.
func TestParseListParams_albumForcesChronology(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{name: "bare album scope", query: "album=al1"},
		{name: "sort ignored", query: "album=al1&sort=newest"},
		{name: "sort and order ignored", query: "album=al1&sort=title&order=desc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := parse(t, tt.query)
			if err != nil {
				t.Fatalf("parseListParams(%q) error: %v", tt.query, err)
			}
			if p.Sort != photos.SortByChronology || p.Order != photos.OrderAsc {
				t.Errorf("parseListParams(%q) sort = %s/%s, want chronology/asc",
					tt.query, p.Sort, p.Order)
			}
		})
	}

	t.Run("no album scope keeps the requested sort", func(t *testing.T) {
		t.Parallel()
		p, err := parse(t, "sort=newest")
		if err != nil {
			t.Fatalf("parseListParams error: %v", err)
		}
		if p.Sort != photos.SortByTakenAt || p.Order != photos.OrderDesc {
			t.Errorf("sort = %s/%s, want taken_at/desc", p.Sort, p.Order)
		}
	})
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
		{name: "non-bool has_gps", query: "has_gps=42"},
		{name: "non-integer year", query: "year=nineteen"},
		{name: "year below the four-digit range", query: "year=42"},
		{name: "year above the four-digit range", query: "year=12023"},
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

// TestParseListParams_queryLanguage verifies that the free-form q parameter is
// parsed through the search query language: filters become structured
// conditions, the residual free text keeps the substring behaviour, negations
// become exclusions, and tokens that were not understood are reported.
func TestParseListParams_queryLanguage(t *testing.T) {
	t.Parallel()

	t.Run("filters and free text split", func(t *testing.T) {
		t.Parallel()
		p, err := parse(t, "q="+url.QueryEscape(`beach iso:100-400 label:cat|dog -blurry`))
		if err != nil {
			t.Fatalf("parseListParams error: %v", err)
		}
		if p.Search != "beach" {
			t.Errorf("Search = %q, want %q", p.Search, "beach")
		}
		if len(p.SearchNot) != 1 || p.SearchNot[0] != "blurry" {
			t.Errorf("SearchNot = %v, want [blurry]", p.SearchNot)
		}
		if len(p.QueryFilters) != 2 {
			t.Fatalf("QueryFilters = %v, want iso and label", p.QueryFilters)
		}
		if p.QueryFilters[0].Key != query.KeyISO || p.QueryFilters[1].Key != query.KeyLabel {
			t.Errorf("QueryFilters keys = %s/%s, want iso/label",
				p.QueryFilters[0].Key, p.QueryFilters[1].Key)
		}
		if len(p.QueryFilters[1].Values) != 2 {
			t.Errorf("label alternatives = %v, want two", p.QueryFilters[1].Values)
		}
	})

	t.Run("plain q keeps the substring behaviour", func(t *testing.T) {
		t.Parallel()
		p, err := parse(t, "q=red+car")
		if err != nil {
			t.Fatalf("parseListParams error: %v", err)
		}
		if p.Search != "red car" {
			t.Errorf("Search = %q, want %q", p.Search, "red car")
		}
		if len(p.QueryFilters) != 0 || len(p.SearchNot) != 0 {
			t.Errorf("plain text produced filters: %+v", p)
		}
	})

	t.Run("unknown token degrades to free text and is reported", func(t *testing.T) {
		t.Parallel()
		p, err := parse(t, "q="+url.QueryEscape("cat color:red"))
		if err != nil {
			t.Fatalf("parseListParams error: %v", err)
		}
		if p.Search != "cat color:red" {
			t.Errorf("Search = %q, want the token kept as free text", p.Search)
		}
		unknown := parseUnknown(t, "q="+url.QueryEscape("cat color:red"))
		if len(unknown) != 1 || unknown[0] != "color:red" {
			t.Errorf("unknown = %v, want [color:red]", unknown)
		}
	})

	t.Run("no q reports nothing", func(t *testing.T) {
		t.Parallel()
		if unknown := parseUnknown(t, "camera=Canon"); len(unknown) != 0 {
			t.Errorf("unknown = %v, want empty", unknown)
		}
	})
}

// TestParseListParams_complexityCap verifies the search-complexity guard: a q
// packing more '|'-alternatives than query.MaxComplexity, or a q longer than
// query.MaxLength, or more scope UIDs than maxScopeFilters, is rejected with an
// error the handler answers as 400 — while a query at the cap and a normal
// query pass untouched. This is the fix for the authenticated slow-query DoS.
func TestParseListParams_complexityCap(t *testing.T) {
	t.Parallel()

	// strings.Repeat("a|", n) + "a" yields exactly n+1 pipe-separated alternatives.
	overAlternatives := "title:" + strings.Repeat("a|", query.MaxComplexity) + "a" // MaxComplexity+1
	atAlternatives := "title:" + strings.Repeat("a|", query.MaxComplexity-1) + "a" // exactly MaxComplexity
	overLength := strings.Repeat("a", query.MaxLength+1)                           // one token, over the byte cap
	overScope := make([]string, maxScopeFilters+1)
	for i := range overScope {
		overScope[i] = "al"
	}

	tests := []struct {
		name    string
		values  url.Values
		wantErr bool
	}{
		{name: "alternatives over the cap rejected", values: url.Values{"q": {overAlternatives}}, wantErr: true},
		{name: "alternatives at the cap accepted", values: url.Values{"q": {atAlternatives}}, wantErr: false},
		{name: "over-long q rejected", values: url.Values{"q": {overLength}}, wantErr: true},
		{name: "normal query accepted", values: url.Values{"q": {"beach label:cat|dog iso:100-400"}}, wantErr: false},
		{name: "too many scope filters rejected", values: url.Values{"album": overScope}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseListParams(tt.values)
			if tt.wantErr && err == nil {
				t.Errorf("parseListParams(%s) = nil error, want a validation error", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("parseListParams(%s) error = %v, want nil", tt.name, err)
			}
		})
	}
}
