package photos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	// SortByTitle orders by the photo title (alphabetical).
	SortByTitle SortField = "title"
	// SortBySize orders by the original file size in bytes.
	SortBySize SortField = "file_size"
)

// sortColumns maps each accepted SortField to its physical column. Lookups
// against this allow-list keep ORDER BY free of caller-controlled SQL.
var sortColumns = map[SortField]string{
	SortByTakenAt:   "taken_at",
	SortByCreatedAt: "created_at",
	SortByUID:       "uid",
	SortByTitle:     "title",
	SortBySize:      "file_size",
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

// ListParams is the filtering, sorting and pagination scaffold for List and
// Count. All values are bound as query parameters; the sort column is chosen
// from an allow-list, so no field can inject SQL.
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
	// TakenAfter, when non-nil, keeps photos whose taken_at is at or after it.
	// Photos with an unknown capture time (NULL taken_at) are excluded.
	TakenAfter *time.Time
	// TakenBefore, when non-nil, keeps photos whose taken_at is at or before it.
	// Photos with an unknown capture time (NULL taken_at) are excluded.
	TakenBefore *time.Time
	// HasGPS, when non-nil, keeps photos that have (true) or lack (false) both a
	// latitude and a longitude.
	HasGPS *bool
	// Camera, when non-empty, keeps photos whose make or model contains it
	// (case-insensitive substring match).
	Camera string
	// Lens, when non-empty, keeps photos whose lens model contains it
	// (case-insensitive substring match).
	Lens string
	// Search, when non-empty, keeps photos whose title, description or notes
	// contain it (case-insensitive substring match).
	Search string
	// FullText, when non-empty, keeps photos whose search vector (title,
	// description, notes, normalised file_name) matches it as a Czech-aware,
	// diacritics-insensitive full-text query. It is used by Search, where it also
	// drives the ts_rank ordering; List and Count treat it as a plain filter.
	FullText string
	// AlbumUID, when non-empty, restricts the result to photos that are members of
	// the album with that UID. It scopes the shared list/search path to an album so
	// every other filter, the sort and pagination apply unchanged.
	AlbumUID string
	// LabelUID, when non-empty, restricts the result to photos that carry the label
	// with that UID. It scopes the shared list/search path to a label so every
	// other filter, the sort and pagination apply unchanged.
	LabelUID string
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

// Search returns the photos whose search vector matches params.FullText,
// ordered by full-text relevance (ts_rank, which weights title > description >
// notes > file_name) with the UID as a stable tiebreaker. It honours every List
// filter (date range, GPS, private, …) and the same limit/offset pagination, so
// a search can be scoped exactly like a browse. params.FullText must be
// non-empty; an empty query yields ErrEmptySearch rather than every photo. Pair
// it with Count (which shares the filters) for the total. The slice is empty
// (not nil) when nothing matches.
func (s *Store) Search(ctx context.Context, params ListParams) ([]Photo, error) {
	if params.FullText == "" {
		return nil, ErrEmptySearch
	}
	query, args := buildSearchQuery(params)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: searching photos: %w", err)
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
		return nil, fmt.Errorf("photos: iterating search results: %w", err)
	}
	return photos, nil
}

// FilterUIDs returns the photos among uids that pass params' structural filters
// (archive state, private, uploader, date range, GPS, camera, lens, substring
// search), as a slice in unspecified order. It is the structural-filter
// companion to a vector similarity search: the caller holds an ordered set of
// candidate uids from the embeddings index and uses this to drop the ones a
// browse filter would hide, then reorders the survivors by similarity itself.
// params' ordering, pagination and FullText query are ignored — semantic
// relevance, not full-text rank, drives a similarity search. An empty input
// returns an empty slice without querying.
func (s *Store) FilterUIDs(ctx context.Context, uids []string, params ListParams) ([]Photo, error) {
	if len(uids) == 0 {
		return []Photo{}, nil
	}
	// FullText would add a tsquery filter; a semantic search must not require a
	// full-text match, so clear it before building the WHERE clause.
	params.FullText = ""
	where, args := buildWhere(params)
	args = append(args, uids)
	where = append(where, "uid = ANY($"+strconv.Itoa(len(args))+")")
	query := "SELECT " + photoColumns + " FROM photos WHERE " + strings.Join(where, " AND ")

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: filtering uids: %w", err)
	}
	defer rows.Close()

	out := make([]Photo, 0, len(uids))
	for rows.Next() {
		photo, scanErr := scanPhoto(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating filtered uids: %w", err)
	}
	return out, nil
}

// Count returns the number of photos matching params' filters, ignoring its
// limit, offset and ordering. It powers the total used by paginated listings.
func (s *Store) Count(ctx context.Context, params ListParams) (int, error) {
	query, args := buildCountQuery(params)
	var total int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("photos: counting photos: %w", err)
	}
	return total, nil
}

// buildWhere assembles the parameterised WHERE filters shared by List and
// Count. It returns the filter clauses (to be joined with AND) and the bound
// argument values in matching positional order, starting at $1. The bind closure
// appends a value and yields its placeholder so every caller value is bound, not
// interpolated.
func buildWhere(params ListParams) (where []string, args []any) {
	bind := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	return whereClauses(params, bind), args
}

// whereClauses returns every WHERE filter implied by params, binding each value
// through bind. It is shared by buildWhere (which owns the bind closure) and by
// buildSearchQuery (which needs to interleave its own binds for the ts_rank
// ordering), so the filter set stays identical across list, count and search.
func whereClauses(params ListParams, bind func(any) string) []string {
	where := archivedClauses(params)
	where = append(where, scalarClauses(params, bind)...)
	where = append(where, gpsClauses(params)...)
	where = append(where, textClauses(params, bind)...)
	where = append(where, ftsClauses(params, bind)...)
	where = append(where, membershipClauses(params, bind)...)
	return where
}

// membershipClauses returns the album/label scoping filters as correlated EXISTS
// subqueries, binding each UID through bind. They keep an album- or label-scoped
// listing on the shared List/Count/Search path, so the standard filters, the
// chosen ordering and pagination all apply on top of the scope. The outer photo
// reference is qualified (photos.uid) to disambiguate it from the join table's
// photo_uid inside the subquery.
func membershipClauses(params ListParams, bind func(any) string) []string {
	var where []string
	if params.AlbumUID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM album_photos ap "+
			"WHERE ap.photo_uid = photos.uid AND ap.album_uid = "+bind(params.AlbumUID)+")")
	}
	if params.LabelUID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM photo_labels pl "+
			"WHERE pl.photo_uid = photos.uid AND pl.label_uid = "+bind(params.LabelUID)+")")
	}
	return where
}

// archivedClauses returns the archive-state filter: live-only by default,
// archived-only when requested (which takes precedence), or none when archived
// photos are explicitly included.
func archivedClauses(params ListParams) []string {
	switch {
	case params.OnlyArchived:
		return []string{"archived_at IS NOT NULL"}
	case !params.IncludeArchived:
		return []string{"archived_at IS NULL"}
	default:
		return nil
	}
}

// scalarClauses returns the equality and range filters (private, uploader, date
// range), binding each value through bind.
func scalarClauses(params ListParams, bind func(any) string) []string {
	var where []string
	if params.Private != nil {
		where = append(where, "private = "+bind(*params.Private))
	}
	if params.UploadedBy != "" {
		where = append(where, "uploaded_by = "+bind(params.UploadedBy))
	}
	if params.TakenAfter != nil {
		where = append(where, "taken_at >= "+bind(*params.TakenAfter))
	}
	if params.TakenBefore != nil {
		where = append(where, "taken_at <= "+bind(*params.TakenBefore))
	}
	return where
}

// gpsClauses returns the has-GPS filter, which needs no bound parameter: present
// requires both coordinates, absent requires at least one to be missing.
func gpsClauses(params ListParams) []string {
	if params.HasGPS == nil {
		return nil
	}
	if *params.HasGPS {
		return []string{"lat IS NOT NULL AND lng IS NOT NULL"}
	}
	return []string{"(lat IS NULL OR lng IS NULL)"}
}

// textClauses returns the case-insensitive substring filters (camera, lens,
// free-text search), binding each wildcard pattern through bind.
func textClauses(params ListParams, bind func(any) string) []string {
	var where []string
	if params.Camera != "" {
		p := bind("%" + params.Camera + "%")
		where = append(where, "(camera_make ILIKE "+p+" OR camera_model ILIKE "+p+")")
	}
	if params.Lens != "" {
		where = append(where, "lens_model ILIKE "+bind("%"+params.Lens+"%"))
	}
	if params.Search != "" {
		p := bind("%" + params.Search + "%")
		where = append(where, "(title ILIKE "+p+" OR description ILIKE "+p+" OR notes ILIKE "+p+")")
	}
	return where
}

// ftsClauses returns the full-text match filter, binding the raw query string so
// it is parsed by the same unaccented tsquery expression that Search ranks on.
// It returns nil when no full-text query is set.
func ftsClauses(params ListParams, bind func(any) string) []string {
	if params.FullText == "" {
		return nil
	}
	return []string{"fts @@ " + tsQueryExpr(bind(params.FullText))}
}

// tsQueryExpr returns the SQL that turns the query string at the given bound
// placeholder into a tsquery, mirroring the generated fts column: the `simple`
// dictionary (no stemming) wrapped in immutable_unaccent for diacritics-
// insensitive matching. websearch_to_tsquery is used because it never errors on
// arbitrary user input and supports quoted phrases and "-" exclusions.
func tsQueryExpr(placeholder string) string {
	return "websearch_to_tsquery('simple', immutable_unaccent(" + placeholder + "))"
}

// buildListQuery assembles the parameterised SELECT for List: the WHERE filters,
// the validated ORDER BY, and the LIMIT/OFFSET. All caller values are bound as
// parameters; ordering is chosen from an allow-list, never interpolated raw.
func buildListQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)

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

// buildCountQuery assembles the parameterised SELECT count(*) for Count, reusing
// List's WHERE filters but omitting ordering and pagination.
func buildCountQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)
	query := "SELECT count(*) FROM photos"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return query, args
}

// buildSearchQuery assembles the parameterised SELECT for Search: the WHERE
// filters (which include the full-text match via ftsClauses), an ORDER BY that
// ranks rows with ts_rank over the same unaccented tsquery, and LIMIT/OFFSET.
// The query string is bound a second time for the rank expression rather than
// reusing the WHERE placeholder, keeping each builder self-contained; the bound
// value is identical, so the planner still sees one query. All caller values are
// bound as parameters.
func buildSearchQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)
	query := "SELECT " + photoColumns + " FROM photos WHERE " + strings.Join(where, " AND ")

	args = append(args, params.FullText)
	rank := "ts_rank(fts, " + tsQueryExpr("$"+strconv.Itoa(len(args))) + ")"
	query += " ORDER BY " + rank + " DESC, uid DESC"

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
