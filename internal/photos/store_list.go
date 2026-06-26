package photos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SortField names a column the photo list may be ordered by. Only the values
// below are accepted; List falls back to the default for anything else, so a
// caller can never inject an arbitrary column name.
type SortField string

// The sortable fields for List.
const (
	// SortByTakenAt orders by capture time (the default timeline order).
	SortByTakenAt SortField = "taken_at"
	// SortByCreatedAt orders by catalogue insertion time.
	SortByCreatedAt SortField = "created_at"
	// SortByUID orders by UID (a stable, total order).
	SortByUID SortField = "uid"
)

// sortColumns maps each accepted SortField to its physical column. Lookups
// against this allow-list keep ORDER BY free of caller-controlled SQL.
var sortColumns = map[SortField]string{
	SortByTakenAt:   "taken_at",
	SortByCreatedAt: "created_at",
	SortByUID:       "uid",
}

// SortOrder is the direction of a List ordering.
type SortOrder string

// The ordering directions for List.
const (
	// OrderAsc sorts ascending.
	OrderAsc SortOrder = "asc"
	// OrderDesc sorts descending (the default).
	OrderDesc SortOrder = "desc"
)

// defaultListLimit caps a page when ListParams.Limit is unset (<= 0).
const defaultListLimit = 100

// ListParams is the filtering, sorting and pagination scaffold for List. The
// full filter set (text search, date ranges, people, albums, …) is added by the
// CRUD task; this carries the fields the catalogue needs immediately.
type ListParams struct {
	// IncludeArchived returns archived photos alongside live ones when true. By
	// default (false) only live photos (archived_at IS NULL) are returned.
	IncludeArchived bool
	// OnlyArchived restricts the result to archived photos. It takes precedence
	// over IncludeArchived.
	OnlyArchived bool
	// Private, when non-nil, restricts the result to photos with the given
	// private flag.
	Private *bool
	// UploadedBy, when non-empty, restricts the result to photos uploaded by the
	// given user UID.
	UploadedBy string
	// Sort selects the ordering column; an unknown value falls back to
	// SortByTakenAt.
	Sort SortField
	// Order selects the ordering direction; anything other than OrderAsc means
	// descending.
	Order SortOrder
	// Limit caps the number of rows; values <= 0 use defaultListLimit.
	Limit int
	// Offset skips the given number of rows for pagination.
	Offset int
}

// List returns photos matching params, ordered and paginated as requested. The
// slice is empty (not nil) when nothing matches.
func (s *Store) List(ctx context.Context, params ListParams) ([]Photo, error) {
	query, args := buildListQuery(params)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: querying photos: %w", err)
	}
	defer rows.Close()

	photos := make([]Photo, 0)
	for rows.Next() {
		photo, scanErr := scanPhoto(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating photos: %w", err)
	}
	return photos, nil
}

// buildListQuery assembles the parameterised SELECT for List: the WHERE filters,
// the validated ORDER BY, and the LIMIT/OFFSET. All caller values are bound as
// parameters; ordering is chosen from an allow-list, never interpolated raw.
func buildListQuery(params ListParams) (string, []any) {
	var where []string
	var args []any

	switch {
	case params.OnlyArchived:
		where = append(where, "archived_at IS NOT NULL")
	case !params.IncludeArchived:
		where = append(where, "archived_at IS NULL")
	}
	if params.Private != nil {
		args = append(args, *params.Private)
		where = append(where, "private = $"+strconv.Itoa(len(args)))
	}
	if params.UploadedBy != "" {
		args = append(args, params.UploadedBy)
		where = append(where, "uploaded_by = $"+strconv.Itoa(len(args)))
	}

	query := "SELECT " + photoColumns + " FROM photos"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY " + orderClause(params)

	limit := params.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	args = append(args, limit)
	query += " LIMIT $" + strconv.Itoa(len(args))
	args = append(args, params.Offset)
	query += " OFFSET $" + strconv.Itoa(len(args))

	return query, args
}

// orderClause returns the validated "column DIR" ORDER BY body for params,
// defaulting to taken_at and descending. NULLS LAST keeps photos with an unknown
// capture time at the end of timeline orderings. UID is appended as a tiebreaker
// for a stable, total order across pages.
func orderClause(params ListParams) string {
	column, ok := sortColumns[params.Sort]
	if !ok {
		column = "taken_at"
	}
	direction := "DESC"
	if params.Order == OrderAsc {
		direction = "ASC"
	}
	clause := column + " " + direction + " NULLS LAST"
	if column != "uid" {
		clause += ", uid " + direction
	}
	return clause
}
