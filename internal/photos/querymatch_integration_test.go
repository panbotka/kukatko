//go:build integration

package photos_test

import (
	"sort"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the matching edge cases where two
// ways of expressing the same filter used to disagree: LIKE metacharacters in a
// term versus its negation, an escaped wildcard, and the year filters versus
// taken: at a New Year boundary.

// matchLibrary is a small purpose-built library plus the name→UID mapping its
// expectations use; unlike the broad query fixture it holds titles chosen to
// contain LIKE metacharacters.
type matchLibrary struct {
	store *photos.Store
	names map[string]string
}

// seedMatchLibrary creates the photos the matching tests search over and
// returns them indexed by fixture name.
func seedMatchLibrary(t *testing.T, fixtures map[string]photos.Photo) matchLibrary {
	t.Helper()
	store, _ := newStore(t)
	lib := matchLibrary{store: store, names: map[string]string{}}
	for name, p := range fixtures {
		created := mustCreate(t, store, p)
		lib.names[created.UID] = name
	}
	return lib
}

// search runs input through the query language onto ListParams exactly like the
// API layer does and returns the sorted fixture names it matched.
func (lib matchLibrary) search(t *testing.T, input string) []string {
	t.Helper()
	parsed := query.Parse(input)
	list, err := lib.store.List(t.Context(), photos.ListParams{
		Search:       parsed.PlainText(),
		SearchNot:    parsed.NotTerms(),
		QueryFilters: parsed.Filters,
	})
	if err != nil {
		t.Fatalf("List(%q): %v", input, err)
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, lib.names[p.UID])
	}
	sort.Strings(names)
	return names
}

// assertNames fails the test unless got holds exactly the wanted fixture names.
func assertNames(t *testing.T, input string, got, want []string) {
	t.Helper()
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if len(got) != len(sorted) {
		t.Fatalf("query %q = %v, want %v", input, got, sorted)
	}
	for i := range sorted {
		if got[i] != sorted[i] {
			t.Fatalf("query %q = %v, want %v", input, got, sorted)
		}
	}
}

// matchPhoto builds a minimal image fixture carrying the given title.
func matchPhoto(hash, title string) photos.Photo {
	return photos.Photo{
		FileHash: hash, FilePath: "m/" + hash + ".jpg", FileName: hash + ".jpg",
		FileMime: "image/jpeg", FileWidth: 100, FileHeight: 100, Title: title,
	}
}

// TestQueryMatching_likeMetacharacters verifies a free-text term and its '-'
// negation read '_' and '%' the same way — literally. They used to disagree:
// the positive path bound the term unescaped, so 'a_b' also matched 'axb'
// while '-a_b' excluded only the literal one.
func TestQueryMatching_likeMetacharacters(t *testing.T) {
	lib := seedMatchLibrary(t, map[string]photos.Photo{
		"underscore": matchPhoto("qm-1", "a_b"),
		"wildcarded": matchPhoto("qm-2", "axb"),
		"percent":    matchPhoto("qm-3", "100% sharp"),
		"plain":      matchPhoto("qm-4", "100 percent sharp"),
	})

	tests := []struct {
		input string
		want  []string
	}{
		{"a_b", []string{"underscore"}},
		{"-a_b", []string{"percent", "plain", "wildcarded"}},
		{"100%", []string{"percent"}},
		{"-100%", []string{"plain", "underscore", "wildcarded"}},
		// The same value through the query language's text filters.
		{"title:a_b", []string{"underscore"}},
		{"camera:a_b", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assertNames(t, tt.input, lib.search(t, tt.input), tt.want)
		})
	}
}

// TestQueryMatching_escapedWildcard verifies that only an unescaped '*' acts as
// the wildcard, so a literal star is searchable at all — an escaped or quoted
// one used to be turned into '%' just the same.
func TestQueryMatching_escapedWildcard(t *testing.T) {
	lib := seedMatchLibrary(t, map[string]photos.Photo{
		"star":  matchPhoto("qm-5", "foo*bar"),
		"other": matchPhoto("qm-6", "fooXbar"),
	})

	tests := []struct {
		input string
		want  []string
	}{
		{`title:foo\*bar`, []string{"star"}},
		{`title:"foo*bar"`, []string{"star"}},
		{"title:foo*bar", []string{"other", "star"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assertNames(t, tt.input, lib.search(t, tt.input), tt.want)
		})
	}
}

// TestQueryMatching_yearBoundaryAgreesWithTaken verifies that a photo taken
// minutes either side of New Year lands in the same calendar year whichever way
// the year is asked for: the query language's year:, the ?year= list parameter,
// taken:, and the year histogram. They all resolve in the pooled session's time
// zone, which internal/database pins to UTC — the zone the parser builds taken:
// in — so the four cannot drift apart.
func TestQueryMatching_yearBoundaryAgreesWithTaken(t *testing.T) {
	lib := seedMatchLibrary(t, map[string]photos.Photo{
		"newyearseve": withTakenAt(matchPhoto("qm-7", "fireworks"),
			time.Date(2019, 12, 31, 23, 30, 0, 0, time.UTC)),
		"newyearsday": withTakenAt(matchPhoto("qm-8", "hangover"),
			time.Date(2020, 1, 1, 0, 30, 0, 0, time.UTC)),
	})

	for _, tt := range []struct {
		input string
		want  []string
	}{
		{"year:2019", []string{"newyearseve"}},
		{"year:2020", []string{"newyearsday"}},
		{"taken:2019", []string{"newyearseve"}},
		{"taken:2020", []string{"newyearsday"}},
	} {
		t.Run(tt.input, func(t *testing.T) {
			assertNames(t, tt.input, lib.search(t, tt.input), tt.want)
		})
	}

	t.Run("year param", func(t *testing.T) {
		year := 2019
		list, err := lib.store.List(t.Context(), photos.ListParams{Year: &year})
		if err != nil {
			t.Fatalf("List(year=2019): %v", err)
		}
		if len(list) != 1 || lib.names[list[0].UID] != "newyearseve" {
			t.Fatalf("?year=2019 matched %d photos, want only newyearseve", len(list))
		}
	})

	t.Run("year buckets", func(t *testing.T) {
		years, err := lib.store.YearBuckets(t.Context(), photos.ListParams{})
		if err != nil {
			t.Fatalf("YearBuckets: %v", err)
		}
		counts := map[int]int{}
		for _, b := range years.Years {
			counts[b.Year] = b.Count
		}
		if counts[2019] != 1 || counts[2020] != 1 {
			t.Fatalf("year buckets = %v, want one photo in 2019 and one in 2020", counts)
		}
	})
}

// withTakenAt stamps a capture time onto a fixture photo.
func withTakenAt(p photos.Photo, at time.Time) photos.Photo {
	p.TakenAt = &at
	p.TakenAtSource = "exif"
	return p
}
