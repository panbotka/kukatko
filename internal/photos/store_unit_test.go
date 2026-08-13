package photos

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/query"
)

// TestPlaceholders verifies the positional-parameter list for representative
// counts and the n <= 0 edge cases.
func TestPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		want string
	}{
		{name: "zero", n: 0, want: ""},
		{name: "negative", n: -3, want: ""},
		{name: "one", n: 1, want: "$1"},
		{name: "three", n: 3, want: "$1, $2, $3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := placeholders(tt.n); got != tt.want {
				t.Errorf("placeholders(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// TestInsertPhotoSQL_consistency verifies the generated INSERT has one
// placeholder per insert column and returns the full read column set, guarding
// against drift between the column slice, the VALUES list and scanPhoto.
func TestInsertPhotoSQL_consistency(t *testing.T) {
	t.Parallel()

	wantPlaceholder := placeholders(len(photoInsertColumns))
	if !strings.Contains(insertPhotoSQL, "VALUES ("+wantPlaceholder+")") {
		t.Errorf("insertPhotoSQL missing VALUES (%s); got %q", wantPlaceholder, insertPhotoSQL)
	}
	if !strings.HasSuffix(insertPhotoSQL, "RETURNING "+photoColumns) {
		t.Errorf("insertPhotoSQL does not return photoColumns; got %q", insertPhotoSQL)
	}
	// photoColumns adds created_at and updated_at to the insert columns.
	if got, want := strings.Count(photoColumns, ",")+1, len(photoInsertColumns)+2; got != want {
		t.Errorf("photoColumns has %d columns, want %d", got, want)
	}
}

// TestOrderClause verifies the ORDER BY body for each sort field/direction,
// including the fallback for an unknown sort field.
func TestOrderClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params ListParams
		want   string
	}{
		{
			name:   "default is taken_at desc",
			params: ListParams{},
			want:   "taken_at DESC NULLS LAST, uid DESC",
		},
		{
			name:   "created_at ascending",
			params: ListParams{Sort: SortByCreatedAt, Order: OrderAsc},
			want:   "created_at ASC NULLS LAST, uid ASC",
		},
		{
			name:   "uid has no tiebreaker",
			params: ListParams{Sort: SortByUID, Order: OrderDesc},
			want:   "uid DESC NULLS LAST",
		},
		{
			name:   "title ascending",
			params: ListParams{Sort: SortByTitle, Order: OrderAsc},
			want:   "title ASC NULLS LAST, uid ASC",
		},
		{
			name:   "size descending",
			params: ListParams{Sort: SortBySize, Order: OrderDesc},
			want:   "file_size DESC NULLS LAST, uid DESC",
		},
		{
			name:   "unknown field falls back to taken_at",
			params: ListParams{Sort: SortField("evil; DROP TABLE photos")},
			want:   "taken_at DESC NULLS LAST, uid DESC",
		},
		{
			name:   "rating sort uses a correlated subquery bound to the user",
			params: ListParams{Sort: SortByRating, RatedBy: new("us_1"), Order: OrderDesc},
			want: "(SELECT ur.rating FROM user_ratings ur " +
				"WHERE ur.photo_uid = photos.uid AND ur.user_uid = $1) DESC NULLS LAST, uid DESC",
		},
		{
			name:   "rating sort ascending keeps NULLS LAST",
			params: ListParams{Sort: SortByRating, RatedBy: new("us_1"), Order: OrderAsc},
			want: "(SELECT ur.rating FROM user_ratings ur " +
				"WHERE ur.photo_uid = photos.uid AND ur.user_uid = $1) ASC NULLS LAST, uid ASC",
		},
		{
			name:   "rating sort without a user falls back to taken_at",
			params: ListParams{Sort: SortByRating},
			want:   "taken_at DESC NULLS LAST, uid DESC",
		},
		{
			name:   "random sort orders on a digest of the uid and the bound seed",
			params: ListParams{Sort: SortByRandom, Seed: "s7"},
			want:   "md5(uid || $1), uid",
		},
		{
			name:   "a random order has no direction to reverse",
			params: ListParams{Sort: SortByRandom, Seed: "s7", Order: OrderAsc},
			want:   "md5(uid || $1), uid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var args []any
			bind := func(value any) string {
				args = append(args, value)
				return "$" + strconv.Itoa(len(args))
			}
			if got := orderClause(tt.params, bind); got != tt.want {
				t.Errorf("orderClause(%+v) = %q, want %q", tt.params, got, tt.want)
			}
		})
	}
}

// TestBuildListQuery verifies the WHERE filters, parameter binding and
// pagination of the list query builder.
func TestBuildListQuery(t *testing.T) {
	t.Parallel()

	yes := true

	t.Run("default excludes archived and paginates", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{})
		if !strings.Contains(query, "WHERE archived_at IS NULL") {
			t.Errorf("query missing live-only filter: %q", query)
		}
		// Only LIMIT and OFFSET are bound by default.
		if len(args) != 2 {
			t.Fatalf("args = %v, want [limit offset]", args)
		}
		if args[0] != defaultListLimit || args[1] != 0 {
			t.Errorf("args = %v, want [%d 0]", args, defaultListLimit)
		}
	})

	t.Run("only-archived overrides include", func(t *testing.T) {
		t.Parallel()
		query, _ := buildListQuery(ListParams{OnlyArchived: true, IncludeArchived: false})
		if !strings.Contains(query, "archived_at IS NOT NULL") {
			t.Errorf("query missing archived-only filter: %q", query)
		}
	})

	t.Run("include-archived adds no archive filter", func(t *testing.T) {
		t.Parallel()
		query, _ := buildListQuery(ListParams{IncludeArchived: true})
		// archived_at appears in the SELECT column list; assert only that it is
		// not used as a filter.
		if strings.Contains(query, "archived_at IS") {
			t.Errorf("query should not filter on archived_at: %q", query)
		}
	})

	t.Run("uploader filter binds params", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{UploadedBy: "us123", Limit: 10, Offset: 5})
		if !strings.Contains(query, "uploaded_by = $1") {
			t.Errorf("query missing bound filters: %q", query)
		}
		if len(args) != 3 || args[0] != "us123" || args[1] != 10 || args[2] != 5 {
			t.Errorf("args = %v, want [us123 10 5]", args)
		}
	})

	t.Run("date range, gps, camera, lens and search bind params", func(t *testing.T) {
		t.Parallel()
		after := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		before := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
		query, args := buildListQuery(ListParams{
			TakenAfter:  &after,
			TakenBefore: &before,
			HasGPS:      &yes,
			Camera:      "Canon",
			Lens:        "RF 50",
			Search:      "beach",
		})
		for _, want := range []string{
			"taken_at >= $1", "taken_at <= $2",
			"lat IS NOT NULL AND lng IS NOT NULL",
			"camera_make ILIKE $3 OR camera_model ILIKE $3",
			"lens_model ILIKE $4",
			"title ILIKE $5 OR description ILIKE $5 OR notes ILIKE $5",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("query missing %q: %q", want, query)
			}
		}
		// 5 bound filter args (has-gps is inline SQL) + LIMIT + OFFSET.
		if len(args) != 7 {
			t.Fatalf("args = %v, want 7 entries", args)
		}
		if args[2] != "%Canon%" || args[3] != "%RF 50%" || args[4] != "%beach%" {
			t.Errorf("substring filters not wrapped in wildcards: %v", args)
		}
	})

	t.Run("has-gps false matches missing coordinates", func(t *testing.T) {
		t.Parallel()
		no := false
		query, _ := buildListQuery(ListParams{HasGPS: &no})
		if !strings.Contains(query, "(lat IS NULL OR lng IS NULL)") {
			t.Errorf("query missing absent-gps filter: %q", query)
		}
	})
}

// TestHiddenClauses verifies the library-visibility filter: on by default,
// lifted for the scopes a photo is only reachable through because the user put
// it there (album, label, favorites), lifted by the explicit IncludeHidden
// escape hatch, and lifted by an explicit hidden: in the query language so
// hidden:yes can actually match.
func TestHiddenClauses(t *testing.T) {
	t.Parallel()

	yes, no := true, false

	tests := []struct {
		name   string
		params ListParams
		want   []string
	}{
		{name: "default hides", params: ListParams{}, want: []string{"NOT hidden_from_library"}},
		{name: "include-hidden lifts", params: ListParams{IncludeHidden: true}},
		{name: "album scope lifts", params: ListParams{AlbumUIDs: []string{"al_1"}}},
		{name: "label scope lifts", params: ListParams{LabelUIDs: []string{"lb_1"}}},
		{name: "favorites scope lifts", params: ListParams{FavoriteOf: "us_1"}},
		{
			name:   "subject scope still hides",
			params: ListParams{SubjectUIDs: []string{"sub_1"}},
			want:   []string{"NOT hidden_from_library"},
		},
		{
			name:   "explicit hidden:yes lifts the default",
			params: ListParams{QueryFilters: boolFilters(query.KeyHidden, yes)},
		},
		{
			name:   "explicit hidden:no lifts the default too",
			params: ListParams{QueryFilters: boolFilters(query.KeyHidden, no)},
		},
		{
			name:   "an unrelated filter does not lift it",
			params: ListParams{QueryFilters: boolFilters(query.KeyArchived, yes)},
			want:   []string{"NOT hidden_from_library"},
		},
		{
			name:   "a uid: lookup lifts it",
			params: ListParams{QueryFilters: uidFilters("ph_1")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hiddenClauses(tt.params); !slices.Equal(got, tt.want) {
				t.Errorf("hiddenClauses(%+v) = %v, want %v", tt.params, got, tt.want)
			}
		})
	}
}

// TestBuildListQuery_hiddenFromLibrary verifies the visibility filter reaches the
// assembled list query — the point of writing it as a bare photos.* predicate is
// that it rides along with every listing.
func TestBuildListQuery_hiddenFromLibrary(t *testing.T) {
	t.Parallel()

	got, _ := buildListQuery(ListParams{})
	if !strings.Contains(got, "NOT hidden_from_library") {
		t.Errorf("default list query missing the visibility filter: %q", got)
	}
	scoped, _ := buildListQuery(ListParams{AlbumUIDs: []string{"al_1"}})
	if strings.Contains(scoped, "NOT hidden_from_library") {
		t.Errorf("album-scoped list query must not hide: %q", scoped)
	}
}

// TestBuildListQuery_hiddenQueryFilterCompiles verifies hidden:yes compiles to
// the positive predicate, so the documented way back to a hidden photo returns
// the hidden ones rather than nothing.
func TestBuildListQuery_hiddenQueryFilterCompiles(t *testing.T) {
	t.Parallel()

	got, _ := buildListQuery(ListParams{QueryFilters: boolFilters(query.KeyHidden, true)})
	if !strings.Contains(got, "(hidden_from_library)") {
		t.Errorf("query missing the hidden:yes predicate: %q", got)
	}
	if strings.Contains(got, "NOT hidden_from_library") {
		t.Errorf("hidden:yes must not be ANDed with the default visible-only filter: %q", got)
	}
}

// boolFilters builds the parsed-filter slice for one yes/no key, the shape
// internal/query hands the store.
func boolFilters(key query.Key, value bool) []query.Filter {
	return []query.Filter{{Key: key, Values: []query.Value{{Bool: &value}}}}
}

// uidFilters returns a parsed uid: filter naming one photo, the shape the
// visibility scopes yield to.
func uidFilters(uid string) []query.Filter {
	return []query.Filter{{Key: query.KeyUID, Values: []query.Value{{Text: uid}}}}
}

// TestUIDLookupLiftsScopes verifies a uid: filter lifts every default scope that
// would otherwise hide the photo it names: the live-only archive filter, the
// visible-only library filter and the stack-primary-only filter. Naming an id is
// explicit intent, and reporting nothing about a photo that exists is the one
// useless answer.
func TestUIDLookupLiftsScopes(t *testing.T) {
	t.Parallel()

	params := ListParams{QueryFilters: uidFilters("ph_1")}
	if got := archivedClauses(params); len(got) != 0 {
		t.Errorf("archivedClauses = %v, want none", got)
	}
	if got := hiddenClauses(params); len(got) != 0 {
		t.Errorf("hiddenClauses = %v, want none", got)
	}
	if got := stackClauses(params); len(got) != 0 {
		t.Errorf("stackClauses = %v, want none", got)
	}
	// OnlyArchived is a caller decision, not a default, so it still wins.
	onlyArchived := ListParams{OnlyArchived: true, QueryFilters: uidFilters("ph_1")}
	if got := archivedClauses(onlyArchived); !slices.Equal(got, []string{"archived_at IS NOT NULL"}) {
		t.Errorf("archivedClauses(OnlyArchived) = %v, want the archived-only clause", got)
	}
}

// TestUIDCond verifies the uid: filter compiles to the three exact, indexed
// lookups it promises — the photo's own uid, the PhotoPrism uid it was imported
// under, and the alias of a source photo that collapsed onto this row — with the
// value bound once rather than interpolated.
func TestUIDCond(t *testing.T) {
	t.Parallel()

	q, args := buildListQuery(ListParams{QueryFilters: uidFilters("ph_1")})
	for _, want := range []string{
		"photos.uid = $", "photos.photoprism_uid = $", "FROM photoprism_aliases pa",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q: %s", want, q)
		}
	}
	if !slices.Contains(args, any("ph_1")) {
		t.Errorf("args = %v, want the bound uid", args)
	}
}

// TestBuildListQuery_membershipScope verifies the album/label scope filters add
// correlated EXISTS subqueries that bind the UID and apply alongside the standard
// filters and pagination.
func TestBuildListQuery_membershipScope(t *testing.T) {
	t.Parallel()

	t.Run("album scope binds the uid", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{AlbumUIDs: []string{"al_1"}})
		want := "EXISTS (SELECT 1 FROM album_photos ap " +
			"WHERE ap.photo_uid = photos.uid AND ap.album_uid = $1)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing album scope %q: %q", want, query)
		}
		if len(args) != 3 || args[0] != "al_1" {
			t.Errorf("args = %v, want [al_1 limit offset]", args)
		}
	})

	t.Run("label scope binds the uid", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{LabelUIDs: []string{"lb_1"}})
		want := "EXISTS (SELECT 1 FROM photo_labels pl " +
			"WHERE pl.photo_uid = photos.uid AND pl.label_uid = $1)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing label scope %q: %q", want, query)
		}
		if len(args) != 3 || args[0] != "lb_1" {
			t.Errorf("args = %v, want [lb_1 limit offset]", args)
		}
	})

	t.Run("several albums and labels each emit an EXISTS bound in order", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{
			AlbumUIDs: []string{"al_1", "al_2"},
			LabelUIDs: []string{"lb_1", "lb_2"},
		})
		// Two AND-ed album memberships and two AND-ed label memberships, so a photo
		// must be in both albums and carry both labels.
		if strings.Count(query, "FROM album_photos ap") != 2 {
			t.Errorf("query missing one EXISTS per album: %q", query)
		}
		if strings.Count(query, "FROM photo_labels pl") != 2 {
			t.Errorf("query missing one EXISTS per label: %q", query)
		}
		if !strings.Contains(query, "ap.album_uid = $1") || !strings.Contains(query, "ap.album_uid = $2") {
			t.Errorf("album uids not bound in order: %q", query)
		}
		if !strings.Contains(query, "pl.label_uid = $3") || !strings.Contains(query, "pl.label_uid = $4") {
			t.Errorf("label uids not bound in order: %q", query)
		}
		// four scope uids + limit + offset.
		if len(args) != 6 || args[0] != "al_1" || args[1] != "al_2" || args[2] != "lb_1" || args[3] != "lb_2" {
			t.Fatalf("args = %v, want [al_1 al_2 lb_1 lb_2 limit offset]", args)
		}
	})

	t.Run("scope applies after the other filters and keeps the archive guard", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{AlbumUIDs: []string{"al_2"}, UploadedBy: "us123"})
		if !strings.Contains(query, "uploaded_by = $1") {
			t.Errorf("query missing uploader filter: %q", query)
		}
		if !strings.Contains(query, "ap.album_uid = $2") {
			t.Errorf("query missing bound album scope after filters: %q", query)
		}
		if !strings.Contains(query, "archived_at IS NULL") {
			t.Errorf("query dropped the live-only guard: %q", query)
		}
		// uploader + album uid + limit + offset.
		if len(args) != 4 {
			t.Fatalf("args = %v, want 4 entries", args)
		}
	})

	t.Run("favorite scope binds the user uid", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{FavoriteOf: "us_1"})
		want := "EXISTS (SELECT 1 FROM user_favorites uf " +
			"WHERE uf.photo_uid = photos.uid AND uf.user_uid = $1)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing favorite scope %q: %q", want, query)
		}
		if len(args) != 3 || args[0] != "us_1" {
			t.Errorf("args = %v, want [us_1 limit offset]", args)
		}
	})

	t.Run("subject scope binds the uid and guards invalid markers", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{SubjectUIDs: []string{"su_1"}})
		want := "EXISTS (SELECT 1 FROM markers m " +
			"WHERE m.photo_uid = photos.uid AND m.subject_uid = $1 AND m.invalid = FALSE)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing subject scope %q: %q", want, query)
		}
		if len(args) != 3 || args[0] != "su_1" {
			t.Errorf("args = %v, want [su_1 limit offset]", args)
		}
	})

	t.Run("several subjects each emit an EXISTS bound in order (AND)", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{SubjectUIDs: []string{"su_1", "su_2"}})
		if strings.Count(query, "FROM markers m") != 2 {
			t.Errorf("query missing one EXISTS per subject: %q", query)
		}
		if !strings.Contains(query, "m.subject_uid = $1") || !strings.Contains(query, "m.subject_uid = $2") {
			t.Errorf("subject uids not bound in order: %q", query)
		}
		if len(args) != 4 || args[0] != "su_1" || args[1] != "su_2" {
			t.Fatalf("args = %v, want [su_1 su_2 limit offset]", args)
		}
	})
}

// TestBuildListQuery_ratingFilters verifies the per-user rating filters add
// correlated EXISTS subqueries that bind the user UID and the value, apply only
// when RatedBy is set, and that the rating sort binds its user after the filters.
func TestBuildListQuery_ratingFilters(t *testing.T) {
	t.Parallel()

	t.Run("min rating binds the user and the threshold", func(t *testing.T) {
		t.Parallel()
		three := 3
		query, args := buildListQuery(ListParams{RatedBy: new("us_1"), MinRating: &three})
		want := "EXISTS (SELECT 1 FROM user_ratings ur " +
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = $1 AND ur.rating >= $2)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing min-rating filter %q: %q", want, query)
		}
		if len(args) != 4 || args[0] != "us_1" || args[1] != 3 {
			t.Errorf("args = %v, want [us_1 3 limit offset]", args)
		}
	})

	t.Run("non-positive min rating adds no filter", func(t *testing.T) {
		t.Parallel()
		zero := 0
		query, args := buildListQuery(ListParams{RatedBy: new("us_1"), MinRating: &zero})
		if strings.Contains(query, "user_ratings") {
			t.Errorf("query should not filter on user_ratings for min rating 0: %q", query)
		}
		if len(args) != 2 {
			t.Errorf("args = %v, want [limit offset]", args)
		}
	})

	t.Run("flag filter binds pick", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{RatedBy: new("us_1"), Flag: new("pick")})
		want := "EXISTS (SELECT 1 FROM user_ratings ur " +
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = $1 AND ur.flag = $2)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing flag filter %q: %q", want, query)
		}
		if len(args) != 4 || args[0] != "us_1" || args[1] != "pick" {
			t.Errorf("args = %v, want [us_1 pick limit offset]", args)
		}
	})

	t.Run("flag none adds no filter", func(t *testing.T) {
		t.Parallel()
		query, _ := buildListQuery(ListParams{RatedBy: new("us_1"), Flag: new("none")})
		if strings.Contains(query, "user_ratings") {
			t.Errorf("query should not filter on user_ratings for flag none: %q", query)
		}
	})

	t.Run("rating filters need a rated-by user", func(t *testing.T) {
		t.Parallel()
		five := 5
		query, _ := buildListQuery(ListParams{MinRating: &five, Flag: new("pick")})
		if strings.Contains(query, "user_ratings") {
			t.Errorf("query should not filter on user_ratings without RatedBy: %q", query)
		}
	})

	t.Run("rating sort binds the user after the filters", func(t *testing.T) {
		t.Parallel()
		two := 2
		query, args := buildListQuery(ListParams{
			RatedBy: new("us_1"), MinRating: &two, Sort: SortByRating,
		})
		if !strings.Contains(query, "ur.rating >= $2") {
			t.Errorf("query missing bound min-rating filter: %q", query)
		}
		// $1 user (filter) + $2 threshold + $3 user (sort) + limit + offset.
		if !strings.Contains(query, "ORDER BY (SELECT ur.rating FROM user_ratings ur "+
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = $3) DESC NULLS LAST, uid DESC") {
			t.Errorf("query missing rating sort bound after filters: %q", query)
		}
		if len(args) != 5 || args[2] != "us_1" {
			t.Errorf("args = %v, want [us_1 2 us_1 limit offset]", args)
		}
	})
}

// TestBuildSearchQuery verifies the search query binds the full-text query,
// ranks by ts_rank, keeps the list filters and paginates.
func TestBuildSearchQuery(t *testing.T) {
	t.Parallel()

	t.Run("ranks by ts_rank and binds the query", func(t *testing.T) {
		t.Parallel()
		query, args := buildSearchQuery(ListParams{FullText: "tomas", Limit: 20, Offset: 40})
		for _, want := range []string{
			"fts @@ websearch_to_tsquery('simple', immutable_unaccent($1))",
			"ORDER BY ts_rank(fts, websearch_to_tsquery('simple', immutable_unaccent($2))) DESC, uid DESC",
			"LIMIT $3",
			"OFFSET $4",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("query missing %q: %q", want, query)
			}
		}
		// The query string is bound twice (WHERE match + rank), then limit/offset.
		if len(args) != 4 {
			t.Fatalf("args = %v, want 4 entries", args)
		}
		if args[0] != "tomas" || args[1] != "tomas" || args[2] != 20 || args[3] != 40 {
			t.Errorf("args = %v, want [tomas tomas 20 40]", args)
		}
	})

	t.Run("keeps list filters alongside the full-text match", func(t *testing.T) {
		t.Parallel()
		query, args := buildSearchQuery(ListParams{FullText: "beach", UploadedBy: "us123"})
		if !strings.Contains(query, "uploaded_by = $1") {
			t.Errorf("query missing uploader filter: %q", query)
		}
		if !strings.Contains(query, "fts @@ websearch_to_tsquery('simple', immutable_unaccent($2))") {
			t.Errorf("query missing bound full-text match after filters: %q", query)
		}
		// uploader + fts query (WHERE) + fts query (rank) + limit + offset.
		if len(args) != 5 {
			t.Fatalf("args = %v, want 5 entries", args)
		}
	})

	t.Run("defaults the limit when unset", func(t *testing.T) {
		t.Parallel()
		_, args := buildSearchQuery(ListParams{FullText: "x"})
		// fts(WHERE) + fts(rank) + limit + offset, with the default limit applied.
		if len(args) != 4 || args[2] != defaultListLimit {
			t.Errorf("args = %v, want default limit %d at index 2", args, defaultListLimit)
		}
	})

	// A shuffled slideshow replaying a search wants the random order, not the
	// relevance ranking — and the same random order on every page.
	t.Run("the random sort replaces the relevance ranking", func(t *testing.T) {
		t.Parallel()
		query, args := buildSearchQuery(ListParams{FullText: "x", Sort: SortByRandom, Seed: "s7"})
		if !strings.Contains(query, "ORDER BY md5(uid || $2), uid") {
			t.Errorf("query missing the seeded random order: %q", query)
		}
		if strings.Contains(query, "ts_rank") {
			t.Errorf("a shuffled search must not rank by relevance: %q", query)
		}
		// fts(WHERE) + seed + limit + offset; the query is bound once now.
		if len(args) != 4 || args[1] != "s7" {
			t.Errorf("args = %v, want the seed bound at index 1", args)
		}
	})
}

// TestBuildCountQuery verifies the count query reuses the same filters as the
// list query but omits ordering and pagination.
func TestBuildCountQuery(t *testing.T) {
	t.Parallel()

	query, args := buildCountQuery(ListParams{UploadedBy: "us123", Limit: 10, Offset: 5})
	if !strings.HasPrefix(query, "SELECT count(*) FROM photos") {
		t.Errorf("count query has wrong prefix: %q", query)
	}
	if strings.Contains(query, "ORDER BY") || strings.Contains(query, "LIMIT") || strings.Contains(query, "OFFSET") {
		t.Errorf("count query must not order or paginate: %q", query)
	}
	if !strings.Contains(query, "uploaded_by = $1") {
		t.Errorf("count query missing filter: %q", query)
	}
	// Only the filter arg is bound; limit/offset are ignored by Count.
	if len(args) != 1 || args[0] != "us123" {
		t.Errorf("args = %v, want [us123]", args)
	}
}

// TestMatchNone verifies that an unsatisfiable scope compiles to a query that
// matches nothing, on every path, and that it drops the other filters rather
// than AND-ing a contradiction onto them.
func TestMatchNone(t *testing.T) {
	t.Parallel()

	t.Run("the list matches nothing", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{MatchNone: true, UploadedBy: "us123"})
		if !strings.Contains(query, "WHERE FALSE") {
			t.Errorf("query missing the impossible filter: %q", query)
		}
		// uploaded_by is also a SELECT column, so look for it as a filter.
		if strings.Contains(query, "uploaded_by = $") {
			t.Errorf("an impossible query should bind nothing else: %q", query)
		}
		// Only LIMIT and OFFSET stay bound.
		if len(args) != 2 {
			t.Errorf("args = %v, want [limit offset]", args)
		}
	})

	t.Run("the count matches nothing", func(t *testing.T) {
		t.Parallel()
		query, args := buildCountQuery(ListParams{MatchNone: true})
		if !strings.Contains(query, "WHERE FALSE") {
			t.Errorf("count query missing the impossible filter: %q", query)
		}
		if len(args) != 0 {
			t.Errorf("args = %v, want none", args)
		}
	})
}
