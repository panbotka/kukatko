package photos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/query"
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
	// SortByRating orders by the RatedBy user's star rating (unrated photos last).
	// Unlike the others it is not a plain column on photos; it resolves to a
	// correlated subquery over user_ratings and is honoured only when RatedBy is
	// set, since a rating is always scoped to the current caller.
	SortByRating SortField = "rating"
	// SortByChronology orders by capture time with the upload (catalogue
	// insertion) time standing in for photos whose capture time is unknown, so
	// the ordering is total and stable rather than pushing undated photos to an
	// arbitrary end. It backs the album view, which is always presented oldest
	// first; it is not offered as a public sort alias.
	SortByChronology SortField = "chronology"
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
	// IncludeStackMembers returns the non-primary members of a stack alongside the
	// primaries when true. By default (false) only a stack's primary is returned,
	// so the several files of one shot occupy a single tile in every listing.
	IncludeStackMembers bool
	// UploadedBy, when non-empty, restricts the result to photos uploaded by the
	// given user UID.
	UploadedBy string
	// TakenAfter, when non-nil, keeps photos whose taken_at is at or after it.
	// Photos with an unknown capture time (NULL taken_at) are excluded.
	TakenAfter *time.Time
	// TakenBefore, when non-nil, keeps photos whose taken_at is at or before it.
	// Photos with an unknown capture time (NULL taken_at) are excluded.
	TakenBefore *time.Time
	// Year, when non-nil, keeps photos captured in that calendar year. Photos with
	// an unknown capture time (NULL taken_at) are excluded. The year is derived the
	// same way YearBuckets derives it, so selecting a bucket returns exactly the
	// photos that bucket counted.
	Year *int
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
	// SearchNot excludes photos whose title, description or notes contain any
	// of the given substrings — the '-term' free-text negations of the search
	// query language on the substring path. The full-text path does not use
	// it; websearch_to_tsquery handles '-' natively there.
	SearchNot []string
	// QueryFilters are the structured key:value conditions parsed from the
	// search query language (internal/query); each compiles to one AND-ed,
	// fully parameterised WHERE clause. The per-user filters among them
	// (favorite:, rating:, flag:) are scoped to RatedBy and stay inert when it
	// is nil.
	QueryFilters []query.Filter
	// FullText, when non-empty, keeps photos whose search vector (title,
	// description, notes, normalised file_name) matches it as a Czech-aware,
	// diacritics-insensitive full-text query. It is used by Search, where it also
	// drives the ts_rank ordering; List and Count treat it as a plain filter.
	FullText string
	// AlbumUIDs, when non-empty, restricts the result to photos that are members of
	// every listed album (AND): each UID contributes its own correlated EXISTS over
	// album_photos, so a photo must belong to all of them to match. It scopes the
	// shared list/search path to one or more albums so every other filter, the sort
	// and pagination apply unchanged.
	AlbumUIDs []string
	// LabelUIDs, when non-empty, restricts the result to photos that carry every
	// listed label (AND): each UID contributes its own correlated EXISTS over
	// photo_labels, so a photo must carry all of them to match. It scopes the shared
	// list/search path to one or more labels so every other filter, the sort and
	// pagination apply unchanged.
	LabelUIDs []string
	// SubjectUIDs, when non-empty, restricts the result to photos that contain every
	// listed subject (person/pet/other) — AND semantics like the album/label scopes:
	// each UID contributes its own correlated EXISTS over markers, so a photo must
	// carry a non-invalid marker for all of them to match. A subject is linked to a
	// photo by a marker (a named face/region); rejected markers (invalid = TRUE) do
	// not count, matching the subject photo gallery. It keeps a person-scoped listing
	// on the shared list/search path so every other filter, the sort and pagination
	// apply unchanged.
	SubjectUIDs []string
	// Country, when non-empty, restricts the result to photos whose cached place
	// has exactly that country. It scopes the shared list/search path to a place
	// (via the photo_places side table) so every other filter, the sort and
	// pagination apply unchanged.
	Country string
	// City, when non-empty, restricts the result to photos whose cached place has
	// exactly that city. Like Country it scopes the shared list/search path via the
	// photo_places side table.
	City string
	// FavoriteOf, when non-empty, restricts the result to photos the user with that
	// UID has favorited. Favorites are per-user, so this scope is always bound to
	// the current caller; it keeps the favorites grid on the shared list/search
	// path so every other filter, the sort and pagination apply unchanged.
	FavoriteOf string
	// RatedBy, when non-nil, is the current caller's user UID and scopes the
	// per-user rating annotation, filters (MinRating, Flag) and the rating sort to
	// that user. Ratings are per-user, so they are always bound to the caller;
	// MinRating, Flag and SortByRating have no effect when RatedBy is nil.
	RatedBy *string
	// MinRating, when non-nil and positive, keeps photos the RatedBy user has
	// rated at or above the given star value (correlated EXISTS over user_ratings).
	// A photo with no rating row counts as rating 0, so a positive minimum
	// excludes it; a value <= 0 matches every photo and adds no filter.
	MinRating *int
	// Flag, when non-nil and one of "pick"/"reject", keeps photos the RatedBy user
	// has marked with that flag (correlated EXISTS over user_ratings). A photo with
	// no rating row counts as flag "none", so it is excluded.
	Flag *string
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
// filter (date range, GPS, …) and the same limit/offset pagination, so
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
// (archive state, uploader, date range, GPS, camera, lens, substring
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
	where = append(where, stackClauses(params)...)
	where = append(where, scalarClauses(params, bind)...)
	where = append(where, yearClauses(params, bind)...)
	where = append(where, gpsClauses(params)...)
	where = append(where, textClauses(params, bind)...)
	where = append(where, ftsClauses(params, bind)...)
	where = append(where, membershipClauses(params, bind)...)
	where = append(where, subjectClauses(params, bind)...)
	where = append(where, placeClauses(params, bind)...)
	where = append(where, favoriteClauses(params, bind)...)
	where = append(where, ratingClauses(params, bind)...)
	where = append(where, queryClauses(params, bind)...)
	return where
}

// ratingClauses returns the per-user rating filters (minimum star rating and
// personal-marking flag) as correlated EXISTS subqueries over user_ratings, binding
// each value through bind. Both are scoped to params.RatedBy (the current
// caller), so they apply only when a rating user is set. A photo with no rating
// row counts as rating 0 / flag "none", so a positive MinRating or a pick/reject/eye
// flag filter excludes it; a MinRating <= 0 matches every photo and adds no
// clause. The outer photo reference is qualified (photos.uid) to disambiguate it
// from the join table's photo_uid inside the subquery.
func ratingClauses(params ListParams, bind func(any) string) []string {
	if params.RatedBy == nil {
		return nil
	}
	var where []string
	if params.MinRating != nil && *params.MinRating > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM user_ratings ur "+
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = "+bind(*params.RatedBy)+
			" AND ur.rating >= "+bind(*params.MinRating)+")")
	}
	if params.Flag != nil && (*params.Flag == "pick" || *params.Flag == "reject" || *params.Flag == "eye") {
		where = append(where, "EXISTS (SELECT 1 FROM user_ratings ur "+
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = "+bind(*params.RatedBy)+
			" AND ur.flag = "+bind(*params.Flag)+")")
	}
	return where
}

// favoriteClauses returns the per-user favorites scoping filter as a correlated
// EXISTS subquery, binding the user UID through bind. Like the album/label
// scopes it keeps a favorites-only listing on the shared List/Count/Search path,
// so the standard filters, the chosen ordering and pagination all apply on top.
// The outer photo reference is qualified (photos.uid) to disambiguate it from the
// join table's photo_uid inside the subquery.
func favoriteClauses(params ListParams, bind func(any) string) []string {
	if params.FavoriteOf == "" {
		return nil
	}
	return []string{"EXISTS (SELECT 1 FROM user_favorites uf " +
		"WHERE uf.photo_uid = photos.uid AND uf.user_uid = " + bind(params.FavoriteOf) + ")"}
}

// membershipClauses returns the album/label scoping filters as correlated EXISTS
// subqueries, binding each UID through bind. It emits one EXISTS per selected
// album UID and one per selected label UID; because buildWhere joins every clause
// with AND, a photo must be a member of every listed album and carry every listed
// label to match ("in album A and album B, with label X and label Y"). The clauses
// keep an album- or label-scoped listing on the shared List/Count/Search path, so
// the standard filters, the chosen ordering and pagination all apply on top of the
// scope. The outer photo reference is qualified (photos.uid) to disambiguate it
// from the join table's photo_uid inside the subquery.
func membershipClauses(params ListParams, bind func(any) string) []string {
	where := make([]string, 0, len(params.AlbumUIDs)+len(params.LabelUIDs))
	for _, albumUID := range params.AlbumUIDs {
		where = append(where, "EXISTS (SELECT 1 FROM album_photos ap "+
			"WHERE ap.photo_uid = photos.uid AND ap.album_uid = "+bind(albumUID)+")")
	}
	for _, labelUID := range params.LabelUIDs {
		where = append(where, "EXISTS (SELECT 1 FROM photo_labels pl "+
			"WHERE pl.photo_uid = photos.uid AND pl.label_uid = "+bind(labelUID)+")")
	}
	return where
}

// subjectClauses returns the person/subject scoping filters as correlated EXISTS
// subqueries over the markers table, binding each subject UID through bind. It
// emits one EXISTS per selected subject UID; because buildWhere joins every clause
// with AND, a photo must contain every listed subject to match ("with person A and
// person B"). A subject is on a photo when a non-invalid marker (a named
// face/region) links them, so rejected markers (invalid = TRUE) are ignored —
// matching the subject photo gallery. The clauses keep a person-scoped listing on
// the shared List/Count/Search path, so the standard filters, the chosen ordering
// and pagination all apply on top of the scope. The outer photo reference is
// qualified (photos.uid) to disambiguate it from the markers table's photo_uid
// inside the subquery.
func subjectClauses(params ListParams, bind func(any) string) []string {
	where := make([]string, 0, len(params.SubjectUIDs))
	for _, subjectUID := range params.SubjectUIDs {
		where = append(where, "EXISTS (SELECT 1 FROM markers m "+
			"WHERE m.photo_uid = photos.uid AND m.subject_uid = "+bind(subjectUID)+
			" AND m.invalid = FALSE)")
	}
	return where
}

// placeClauses returns the country/city scoping filter as a single correlated
// EXISTS subquery over the photo_places side table, binding each value through
// bind. Place data lives in photo_places (one row per geotagged photo), not on
// the photos row, so the scope is expressed as EXISTS rather than a plain column
// comparison. Both values, when set, are ANDed inside one subquery; an empty
// Country and City add no clause. The outer photo reference is qualified
// (photos.uid) to disambiguate it from the join table's photo_uid.
func placeClauses(params ListParams, bind func(any) string) []string {
	var conds []string
	if params.Country != "" {
		conds = append(conds, "pp.country = "+bind(params.Country))
	}
	if params.City != "" {
		conds = append(conds, "pp.city = "+bind(params.City))
	}
	if len(conds) == 0 {
		return nil
	}
	return []string{"EXISTS (SELECT 1 FROM photo_places pp " +
		"WHERE pp.photo_uid = photos.uid AND " + strings.Join(conds, " AND ") + ")"}
}

// archivedClauses returns the archive-state filter: live-only by default,
// archived-only when requested (which takes precedence), or none when archived
// photos are explicitly included. When the search query language carries its
// own archived: condition, the default yields to it — otherwise archived:yes
// would fight the live-only clause and never match anything.
func archivedClauses(params ListParams) []string {
	switch {
	case params.OnlyArchived:
		return []string{"archived_at IS NOT NULL"}
	case queryHasFilter(params.QueryFilters, query.KeyArchived):
		return nil
	case !params.IncludeArchived:
		return []string{"archived_at IS NULL"}
	default:
		return nil
	}
}

// stackClauses returns the stack-visibility filter that hides the non-primary
// members of a stack from the default views: a photo is shown when it is not
// stacked (stack_uid IS NULL) or it is the stack's primary. This is what gets
// the RAW+JPEG duplicates out of the grid while keeping every member's row. It
// is a pure photos.* predicate (no bind), the natural sibling of archivedClauses,
// so adding it here propagates to List, Count, Search, FilterUIDs, YearBuckets
// and TimelineBuckets at once. IncludeStackMembers lifts it for the callers that
// deliberately want every member (e.g. listing a single stack's variants).
func stackClauses(params ListParams) []string {
	if params.IncludeStackMembers {
		return nil
	}
	return []string{"(stack_uid IS NULL OR stack_primary)"}
}

// scalarClauses returns the equality and range filters (uploader, date range),
// binding each value through bind.
func scalarClauses(params ListParams, bind func(any) string) []string {
	var where []string
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

// yearClauses returns the capture-year filter, binding the year through bind. It
// is expressed as a half-open taken_at range rather than the
// date_part('year', taken_at) = $n it is equivalent to, so the planner can use
// idx_photos_taken_at instead of scanning every row. The two forms select the
// same photos: make_timestamptz resolves the year boundaries in the session time
// zone, which is the very zone date_part reads the year in, and YearBuckets
// groups by that same date_part — so filtering on a bucket's year returns exactly
// the photos the bucket counted. NULL taken_at fails both comparisons, so undated
// photos are excluded, as in the buckets. The explicit ::int casts pin the
// parameters to make_timestamptz's integer signature (pgx binds a Go int as
// bigint, which the function would not resolve).
func yearClauses(params ListParams, bind func(any) string) []string {
	if params.Year == nil {
		return nil
	}
	from := "make_timestamptz((" + bind(*params.Year) + ")::int, 1, 1, 0, 0, 0)"
	until := "make_timestamptz((" + bind(*params.Year+1) + ")::int, 1, 1, 0, 0, 0)"
	return []string{"taken_at >= " + from + " AND taken_at < " + until}
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
	// Continue binding from where buildWhere left off so the rating sort's
	// correlated subquery can bind the RatedBy user after the filter parameters.
	bind := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	query := "SELECT " + photoColumns + " FROM photos"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY " + orderClause(params, bind)

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

// orderClause returns the validated ORDER BY body for params, defaulting to
// taken_at and descending. NULLS LAST keeps photos with an unknown sort value at
// the end of an ordering; UID is appended as a tiebreaker for a stable, total
// order across pages. The chronology sort orders by capture time with the
// upload time standing in for photos that have none. The rating sort orders by
// the RatedBy user's star rating via a correlated subquery over user_ratings
// (unrated photos sort last), binding the user UID through bind; it falls back
// to the default when RatedBy is nil, since a rating is always scoped to the
// current caller.
func orderClause(params ListParams, bind func(any) string) string {
	direction := "DESC"
	if params.Order == OrderAsc {
		direction = "ASC"
	}
	if params.Sort == SortByChronology {
		// COALESCE never yields NULL here (created_at is NOT NULL), so no NULLS
		// LAST is needed; the uid tiebreaker keeps the order total across pages.
		return "COALESCE(taken_at, created_at) " + direction + ", uid " + direction
	}
	if params.Sort == SortByRating && params.RatedBy != nil {
		sub := "(SELECT ur.rating FROM user_ratings ur " +
			"WHERE ur.photo_uid = photos.uid AND ur.user_uid = " + bind(*params.RatedBy) + ")"
		return sub + " " + direction + " NULLS LAST, uid " + direction
	}
	column, ok := sortColumns[params.Sort]
	if !ok {
		column = "taken_at"
	}
	clause := column + " " + direction + " NULLS LAST"
	if column != "uid" {
		clause += ", uid " + direction
	}
	return clause
}
