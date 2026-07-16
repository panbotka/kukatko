package photos

import (
	"math"
	"strings"

	"github.com/panbotka/kukatko/internal/query"
)

// defaultNearRadiusKm is the km radius the near: filter uses when the query
// carries no dist: value.
const defaultNearRadiusKm = 5.0

// condEnv carries the per-query context a condition builder needs: the shared
// bind closure, the caller's UID for the per-user filters (favorite:, rating:,
// flag:) and the km radius for near:.
type condEnv struct {
	bind     func(any) string
	ratedBy  *string
	radiusKm float64
}

// queryClauses returns the WHERE filters implied by the parsed search query
// language: the '-term' free-text exclusions plus one AND-ed clause per
// key:value filter. Every value is bound through bind — the AST is compiled,
// never string-concatenated from user input.
func queryClauses(params ListParams, bind func(any) string) []string {
	where := searchNotClauses(params, bind)
	env := condEnv{bind: bind, ratedBy: params.RatedBy, radiusKm: queryRadiusKm(params.QueryFilters)}
	for _, f := range params.QueryFilters {
		if clause, ok := queryFilterClause(f, env); ok {
			where = append(where, clause)
		}
	}
	return where
}

// searchNotClauses compiles the negated free-text terms into NOT ILIKE filters
// over the same columns the positive substring search matches. NULL columns
// count as empty so a photo without a description still passes "-word".
func searchNotClauses(params ListParams, bind func(any) string) []string {
	where := make([]string, 0, len(params.SearchNot))
	for _, term := range params.SearchNot {
		p := bind("%" + likeEscape(term) + "%")
		where = append(where, "(COALESCE(title, '') NOT ILIKE "+p+
			" AND COALESCE(description, '') NOT ILIKE "+p+
			" AND COALESCE(notes, '') NOT ILIKE "+p+")")
	}
	return where
}

// queryRadiusKm returns the km radius for the near: filter: the query's first
// dist: value, or the default.
func queryRadiusKm(filters []query.Filter) float64 {
	for _, f := range filters {
		if f.Key == query.KeyDist && len(f.Values) > 0 && f.Values[0].Min != nil {
			return *f.Values[0].Min
		}
	}
	return defaultNearRadiusKm
}

// queryHasFilter reports whether the parsed filters include the given key.
func queryHasFilter(filters []query.Filter, key query.Key) bool {
	for _, f := range filters {
		if f.Key == key {
			return true
		}
	}
	return false
}

// queryFilterClause compiles one filter into a single WHERE clause: its
// OR-alternatives each become a condition (negated alternatives are wrapped
// NULL-safely) and are joined with OR. ok is false when the filter yields no
// condition — dist: (a near: parameter, not a filter) and the per-user filters
// without a caller.
func queryFilterClause(f query.Filter, env condEnv) (string, bool) {
	build, known := queryCondBuilders[f.Key]
	if !known {
		return "", false
	}
	conds := make([]string, 0, len(f.Values))
	for _, v := range f.Values {
		cond, ok := build(v, env)
		if !ok {
			continue
		}
		if v.Not {
			cond = negateCond(cond)
		}
		conds = append(conds, cond)
	}
	if len(conds) == 0 {
		return "", false
	}
	return "(" + strings.Join(conds, " OR ") + ")", true
}

// negateCond wraps a condition so its negation also matches rows where the
// condition is NULL: an unknown value is "not a match", so its negation is.
func negateCond(cond string) string {
	return "NOT COALESCE((" + cond + "), FALSE)"
}

// likeEscaper escapes the LIKE metacharacters so a filter value matches them
// literally; '*' is left alone because likePattern turns it into the wildcard.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likeEscape returns text with the LIKE metacharacters escaped.
func likeEscape(text string) string {
	return likeEscaper.Replace(text)
}

// likePattern converts a filter's text value into an ILIKE pattern: '*' is the
// user's wildcard (the pattern is then anchored to the value's shape), and a
// value without any wildcard matches as a plain substring.
func likePattern(text string) string {
	escaped := likeEscape(text)
	if !strings.Contains(escaped, "*") {
		return "%" + escaped + "%"
	}
	return strings.ReplaceAll(escaped, "*", "%")
}

// Effective display dimensions: EXIF orientations 5–8 rotate the frame by 90°,
// so the stored width and height swap for the orientation filters.
const (
	effWidthExpr  = "(CASE WHEN file_orientation BETWEEN 5 AND 8 THEN file_height ELSE file_width END)"
	effHeightExpr = "(CASE WHEN file_orientation BETWEEN 5 AND 8 THEN file_width ELSE file_height END)"
)

// condBuilder compiles one value alternative of a filter into a boolean SQL
// condition, binding every user value through the environment's bind closure.
// ok is false when the alternative yields no condition for this key.
type condBuilder func(v query.Value, env condEnv) (string, bool)

// queryCondBuilders maps each canonical filter key to its condition builder.
// query.KeyDist is deliberately absent: dist: only parameterises near:.
var queryCondBuilders = map[query.Key]condBuilder{
	query.KeyTitle:       likeCond("title"),
	query.KeyDescription: likeCond("description"),
	query.KeyNotes:       likeCond("notes"),
	query.KeyFilename:    likeCond("file_name"),
	query.KeyKeywords:    likeCond("keywords"),
	query.KeyLens:        likeCond("lens_model"),
	query.KeyCamera:      cameraCond,
	query.KeyCodec:       codecCond,
	query.KeyAlbum:       albumCond,
	query.KeyLabel:       labelCond,
	query.KeyPerson:      personCond,
	query.KeyCountry:     placeCond("country"),
	query.KeyCity:        placeCond("city"),
	query.KeyGeo:         geoCond,
	query.KeyAlt:         numberCond("altitude"),
	query.KeyISO:         numberCond("iso"),
	query.KeyAperture:    numberCond("aperture"),
	query.KeyFocalLength: numberCond("focal_length"),
	query.KeyMegapixels:  numberCond("((file_width::bigint * file_height)::double precision / 1000000.0)"),
	query.KeyYear:        yearCond,
	query.KeyMonth:       numberCond("EXTRACT(MONTH FROM taken_at)"),
	query.KeyDay:         numberCond("EXTRACT(DAY FROM taken_at)"),
	query.KeyTaken:       dateCond("taken_at"),
	query.KeyAdded:       dateCond("created_at"),
	query.KeyBefore:      beforeCond,
	query.KeyAfter:       afterCond,
	query.KeyType:        typeCond,
	query.KeyPortrait:    orientationCond(effHeightExpr + " > " + effWidthExpr),
	query.KeyLandscape:   orientationCond(effWidthExpr + " > " + effHeightExpr),
	query.KeySquare:      orientationCond(effWidthExpr + " = " + effHeightExpr),
	query.KeyPanorama:    orientationCond(effWidthExpr + " >= " + effHeightExpr + " * 1.9"),
	query.KeyFavorite:    favoriteCond,
	query.KeyPrivate:     privateCond,
	query.KeyArchived:    archivedCond,
	query.KeyRating:      ratingCond,
	query.KeyFlag:        flagCond,
	query.KeyFaces:       facesCond,
	query.KeyFace:        faceNewCond,
	query.KeyNear:        nearCond,
}

// likeCond builds a case-insensitive pattern match against one column.
func likeCond(column string) condBuilder {
	return func(v query.Value, env condEnv) (string, bool) {
		return column + " ILIKE " + env.bind(likePattern(v.Text)), true
	}
}

// cameraCond matches the camera make or model, mirroring the camera= filter.
func cameraCond(v query.Value, env condEnv) (string, bool) {
	p := env.bind(likePattern(v.Text))
	return "(camera_make ILIKE " + p + " OR camera_model ILIKE " + p + ")", true
}

// codecCond matches the still-image or video codec.
func codecCond(v query.Value, env condEnv) (string, bool) {
	p := env.bind(likePattern(v.Text))
	return "(image_codec ILIKE " + p + " OR video_codec ILIKE " + p + ")", true
}

// albumCond matches membership in an album by title pattern or exact UID.
func albumCond(v query.Value, env condEnv) (string, bool) {
	p := env.bind(likePattern(v.Text))
	uid := env.bind(v.Text)
	return "EXISTS (SELECT 1 FROM album_photos ap JOIN albums a ON a.uid = ap.album_uid " +
		"WHERE ap.photo_uid = photos.uid AND (a.title ILIKE " + p + " OR a.uid = " + uid + "))", true
}

// labelCond matches a carried label by name pattern or exact UID.
func labelCond(v query.Value, env condEnv) (string, bool) {
	p := env.bind(likePattern(v.Text))
	uid := env.bind(v.Text)
	return "EXISTS (SELECT 1 FROM photo_labels pl JOIN labels l ON l.uid = pl.label_uid " +
		"WHERE pl.photo_uid = photos.uid AND (l.name ILIKE " + p + " OR l.uid = " + uid + "))", true
}

// personCond matches a contained subject by name pattern or exact UID via a
// non-invalid marker, the same linkage the person= scope uses.
func personCond(v query.Value, env condEnv) (string, bool) {
	p := env.bind(likePattern(v.Text))
	uid := env.bind(v.Text)
	return "EXISTS (SELECT 1 FROM markers m JOIN subjects s ON s.uid = m.subject_uid " +
		"WHERE m.photo_uid = photos.uid AND m.invalid = FALSE " +
		"AND (s.name ILIKE " + p + " OR s.uid = " + uid + "))", true
}

// placeCond builds a match against one photo_places column (country or city).
func placeCond(column string) condBuilder {
	return func(v query.Value, env condEnv) (string, bool) {
		return "EXISTS (SELECT 1 FROM photo_places pp WHERE pp.photo_uid = photos.uid " +
			"AND pp." + column + " ILIKE " + env.bind(likePattern(v.Text)) + ")", true
	}
}

// geoCond keeps photos with (yes) or without (no) both GPS coordinates.
func geoCond(v query.Value, _ condEnv) (string, bool) {
	if v.Bool == nil {
		return "", false
	}
	if *v.Bool {
		return "(lat IS NOT NULL AND lng IS NOT NULL)", true
	}
	return "(lat IS NULL OR lng IS NULL)", true
}

// numberCond builds a numeric range condition over the given SQL expression,
// binding whichever bounds the value carries.
func numberCond(expr string) condBuilder {
	return func(v query.Value, env condEnv) (string, bool) {
		return boundsCond(expr, v, env)
	}
}

// floatMatchEpsilon widens an exact fractional match (f:1.8) into a hair of a
// range: single-precision EXIF columns store 1.8 as 1.79999995…, which a bound
// of exactly 1.8 would miss.
const floatMatchEpsilon = 0.005

// boundsCond renders expr constrained to the value's numeric bounds; ok is
// false when the value carries no bound at all. An exact fractional match is
// widened by floatMatchEpsilon so it survives float rounding.
func boundsCond(expr string, v query.Value, env condEnv) (string, bool) {
	lo, hi := v.Min, v.Max
	if lo != nil && hi != nil && *lo == *hi && *lo != math.Trunc(*lo) {
		wlo, whi := *lo-floatMatchEpsilon, *hi+floatMatchEpsilon
		lo, hi = &wlo, &whi
	}
	var conds []string
	if lo != nil {
		conds = append(conds, expr+" >= "+env.bind(*lo))
	}
	if hi != nil {
		conds = append(conds, expr+" <= "+env.bind(*hi))
	}
	if len(conds) == 0 {
		return "", false
	}
	return "(" + strings.Join(conds, " AND ") + ")", true
}

// yearCond compiles a capture-year range the same way the year= filter does:
// as a half-open taken_at range so idx_photos_taken_at stays usable. The
// explicit ::int casts pin make_timestamptz's integer signature.
func yearCond(v query.Value, env condEnv) (string, bool) {
	var conds []string
	if v.Min != nil {
		conds = append(conds, "taken_at >= make_timestamptz(("+env.bind(int(*v.Min))+")::int, 1, 1, 0, 0, 0)")
	}
	if v.Max != nil {
		conds = append(conds, "taken_at < make_timestamptz(("+env.bind(int(*v.Max)+1)+")::int, 1, 1, 0, 0, 0)")
	}
	if len(conds) == 0 {
		return "", false
	}
	return "(" + strings.Join(conds, " AND ") + ")", true
}

// dateCond builds a half-open calendar range condition over the given
// timestamp column, used by taken: (capture time) and added: (catalogue time).
func dateCond(column string) condBuilder {
	return func(v query.Value, env condEnv) (string, bool) {
		if v.From == nil || v.Until == nil {
			return "", false
		}
		return "(" + column + " >= " + env.bind(*v.From) +
			" AND " + column + " < " + env.bind(*v.Until) + ")", true
	}
}

// beforeCond keeps photos taken strictly before the value's start.
func beforeCond(v query.Value, env condEnv) (string, bool) {
	if v.From == nil {
		return "", false
	}
	return "taken_at < " + env.bind(*v.From), true
}

// afterCond keeps photos taken on or after the value's start.
func afterCond(v query.Value, env condEnv) (string, bool) {
	if v.From == nil {
		return "", false
	}
	return "taken_at >= " + env.bind(*v.From), true
}

// typeCond matches the media type (image, video, live).
func typeCond(v query.Value, env condEnv) (string, bool) {
	return "media_type = " + env.bind(v.Text), true
}

// orientationCond builds a yes/no condition over an aspect comparison of the
// effective (orientation-corrected) dimensions. Photos with unknown dimensions
// match neither the positive nor the negative form's comparison, so "no"
// negates NULL-safely.
func orientationCond(compare string) condBuilder {
	cond := "(file_width IS NOT NULL AND file_height IS NOT NULL AND " + compare + ")"
	return func(v query.Value, _ condEnv) (string, bool) {
		if v.Bool == nil {
			return "", false
		}
		if *v.Bool {
			return cond, true
		}
		return negateCond(cond), true
	}
}

// favoriteCond keeps (yes) or drops (no) the caller's favorites. It needs the
// caller's UID; without one the filter is inert.
func favoriteCond(v query.Value, env condEnv) (string, bool) {
	if v.Bool == nil || env.ratedBy == nil {
		return "", false
	}
	cond := "EXISTS (SELECT 1 FROM user_favorites uf " +
		"WHERE uf.photo_uid = photos.uid AND uf.user_uid = " + env.bind(*env.ratedBy) + ")"
	if !*v.Bool {
		cond = negateCond(cond)
	}
	return cond, true
}

// privateCond keeps (yes) or drops (no) private photos.
func privateCond(v query.Value, _ condEnv) (string, bool) {
	if v.Bool == nil {
		return "", false
	}
	if *v.Bool {
		return "private", true
	}
	return "NOT private", true
}

// archivedCond keeps archived (yes) or live (no) photos. The store's default
// live-only clause yields to it (see archivedClauses), so archived:yes can
// actually match.
func archivedCond(v query.Value, _ condEnv) (string, bool) {
	if v.Bool == nil {
		return "", false
	}
	if *v.Bool {
		return "archived_at IS NOT NULL", true
	}
	return "archived_at IS NULL", true
}

// ratingCond compiles a rating range over the caller's per-user rating; a
// photo without a rating row counts as 0, so rating:0 finds the unrated.
func ratingCond(v query.Value, env condEnv) (string, bool) {
	if env.ratedBy == nil {
		return "", false
	}
	expr := "COALESCE((SELECT ur.rating FROM user_ratings ur " +
		"WHERE ur.photo_uid = photos.uid AND ur.user_uid = " + env.bind(*env.ratedBy) + "), 0)"
	return boundsCond(expr, v, env)
}

// flagCond keeps photos the caller flagged with the given pick/reject/eye word.
func flagCond(v query.Value, env condEnv) (string, bool) {
	if env.ratedBy == nil {
		return "", false
	}
	return "EXISTS (SELECT 1 FROM user_ratings ur " +
		"WHERE ur.photo_uid = photos.uid AND ur.user_uid = " + env.bind(*env.ratedBy) +
		" AND ur.flag = " + env.bind(v.Text) + ")", true
}

// faceMarkersExpr is the correlated face set of a photo: its non-invalid face
// markers, the same linkage the person galleries count.
const faceMarkersExpr = "FROM markers m WHERE m.photo_uid = photos.uid AND m.type = 'face' AND m.invalid = FALSE"

// facesCond compiles the face-count filter: yes/no for any/none, a bare
// number as a minimum, a range for both bounds.
func facesCond(v query.Value, env condEnv) (string, bool) {
	if v.Bool != nil {
		cond := "EXISTS (SELECT 1 " + faceMarkersExpr + ")"
		if !*v.Bool {
			cond = negateCond(cond)
		}
		return cond, true
	}
	return boundsCond("(SELECT count(*) "+faceMarkersExpr+")", v, env)
}

// faceNewCond keeps photos with at least one detected face that has no
// assigned subject yet — the "someone to name here" probe.
func faceNewCond(_ query.Value, _ condEnv) (string, bool) {
	return "EXISTS (SELECT 1 FROM faces f WHERE f.photo_uid = photos.uid AND f.subject_uid IS NULL)", true
}

// nearCond keeps photos within the query's km radius of the given photo's
// coordinates, using the spherical law of cosines (clamped into acos's domain)
// on an Earth radius of 6371 km. The reference photo itself matches (it is
// zero km away). Photos or references without coordinates never match.
func nearCond(v query.Value, env condEnv) (string, bool) {
	uid := env.bind(v.Text)
	radius := env.bind(env.radiusKm)
	return "(photos.lat IS NOT NULL AND photos.lng IS NOT NULL AND EXISTS (" +
		"SELECT 1 FROM photos ref WHERE ref.uid = " + uid +
		" AND ref.lat IS NOT NULL AND ref.lng IS NOT NULL" +
		" AND 6371.0 * acos(least(1.0, greatest(-1.0," +
		" sin(radians(ref.lat)) * sin(radians(photos.lat)) +" +
		" cos(radians(ref.lat)) * cos(radians(photos.lat)) * cos(radians(photos.lng - ref.lng))" +
		"))) <= " + radius + "))", true
}
