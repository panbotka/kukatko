package photos

import (
	"strings"
	"testing"
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
			name:   "unknown field falls back to taken_at",
			params: ListParams{Sort: SortField("evil; DROP TABLE photos")},
			want:   "taken_at DESC NULLS LAST, uid DESC",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := orderClause(tt.params); got != tt.want {
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
}
