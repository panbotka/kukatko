package photos

import (
	"strconv"
	"strings"
	"testing"
	"time"
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

	t.Run("private and uploader filters bind params", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{Private: &yes, UploadedBy: "us123", Limit: 10, Offset: 5})
		if !strings.Contains(query, "private = $1") || !strings.Contains(query, "uploaded_by = $2") {
			t.Errorf("query missing bound filters: %q", query)
		}
		if len(args) != 4 || args[0] != true || args[1] != "us123" || args[2] != 10 || args[3] != 5 {
			t.Errorf("args = %v, want [true us123 10 5]", args)
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

// TestBuildListQuery_membershipScope verifies the album/label scope filters add
// correlated EXISTS subqueries that bind the UID and apply alongside the standard
// filters and pagination.
func TestBuildListQuery_membershipScope(t *testing.T) {
	t.Parallel()

	t.Run("album scope binds the uid", func(t *testing.T) {
		t.Parallel()
		query, args := buildListQuery(ListParams{AlbumUID: "al_1"})
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
		query, args := buildListQuery(ListParams{LabelUID: "lb_1"})
		want := "EXISTS (SELECT 1 FROM photo_labels pl " +
			"WHERE pl.photo_uid = photos.uid AND pl.label_uid = $1)"
		if !strings.Contains(query, want) {
			t.Errorf("query missing label scope %q: %q", want, query)
		}
		if len(args) != 3 || args[0] != "lb_1" {
			t.Errorf("args = %v, want [lb_1 limit offset]", args)
		}
	})

	t.Run("scope applies after the other filters and keeps the archive guard", func(t *testing.T) {
		t.Parallel()
		yes := true
		query, args := buildListQuery(ListParams{AlbumUID: "al_2", Private: &yes})
		if !strings.Contains(query, "private = $1") {
			t.Errorf("query missing private filter: %q", query)
		}
		if !strings.Contains(query, "ap.album_uid = $2") {
			t.Errorf("query missing bound album scope after filters: %q", query)
		}
		if !strings.Contains(query, "archived_at IS NULL") {
			t.Errorf("query dropped the live-only guard: %q", query)
		}
		// private + album uid + limit + offset.
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
		yes := true
		query, args := buildSearchQuery(ListParams{FullText: "beach", Private: &yes})
		if !strings.Contains(query, "private = $1") {
			t.Errorf("query missing private filter: %q", query)
		}
		if !strings.Contains(query, "fts @@ websearch_to_tsquery('simple', immutable_unaccent($2))") {
			t.Errorf("query missing bound full-text match after filters: %q", query)
		}
		// private + fts query (WHERE) + fts query (rank) + limit + offset.
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
}

// TestBuildCountQuery verifies the count query reuses the same filters as the
// list query but omits ordering and pagination.
func TestBuildCountQuery(t *testing.T) {
	t.Parallel()

	yes := true
	query, args := buildCountQuery(ListParams{Private: &yes, Limit: 10, Offset: 5})
	if !strings.HasPrefix(query, "SELECT count(*) FROM photos") {
		t.Errorf("count query has wrong prefix: %q", query)
	}
	if strings.Contains(query, "ORDER BY") || strings.Contains(query, "LIMIT") || strings.Contains(query, "OFFSET") {
		t.Errorf("count query must not order or paginate: %q", query)
	}
	if !strings.Contains(query, "private = $1") {
		t.Errorf("count query missing filter: %q", query)
	}
	// Only the filter arg is bound; limit/offset are ignored by Count.
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}
