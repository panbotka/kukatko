package organize

import (
	"context"
	"fmt"
	"strings"
)

// defaultSearchLimit caps a search group when the caller passes a non-positive
// limit, so a mis-wired caller cannot request an unbounded scan.
const defaultSearchLimit = 8

// searchAlbumsSQL matches albums by title or description, case- and
// accent-insensitively (immutable_unaccent + ILIKE), returning each match with
// its photo count, ordered by title then uid for a stable result, capped at the
// bound limit. The pattern in $1 is a pre-escaped "contains" ILIKE pattern.
const searchAlbumsSQL = `
SELECT a.uid, a.slug, a.title, a.description, a.type, a.cover_photo_uid,
       a.private, a.order_by, a.created_by, a.created_at, a.updated_at,
       COUNT(ap.photo_uid) AS photo_count
FROM albums a
LEFT JOIN album_photos ap ON ap.album_uid = a.uid
WHERE immutable_unaccent(a.title) ILIKE immutable_unaccent($1)
   OR immutable_unaccent(a.description) ILIKE immutable_unaccent($1)
GROUP BY a.uid
ORDER BY a.title, a.uid
LIMIT $2`

// SearchAlbums returns up to limit albums whose title or description contains q,
// matched case- and accent-insensitively, each paired with its photo count and
// ordered by title. A non-positive limit falls back to defaultSearchLimit. It
// backs the grouped global-search endpoint. The result is empty (not nil) when
// nothing matches.
func (s *Store) SearchAlbums(ctx context.Context, q string, limit int) ([]AlbumCount, error) {
	rows, err := s.pool.Query(ctx, searchAlbumsSQL, likePattern(q), clampSearchLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("organize: searching albums: %w", err)
	}
	defer rows.Close()

	out := make([]AlbumCount, 0)
	for rows.Next() {
		ac, err := scanAlbumCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ac)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating album search: %w", err)
	}
	return out, nil
}

// searchLabelsSQL matches labels by name, case- and accent-insensitively
// (immutable_unaccent + ILIKE), returning each match with its photo count,
// ordered by priority (highest first) then name then uid, capped at the bound
// limit. The pattern in $1 is a pre-escaped "contains" ILIKE pattern.
const searchLabelsSQL = `
SELECT l.uid, l.slug, l.name, l.priority, l.created_at, l.updated_at,
       COUNT(pl.photo_uid) AS photo_count
FROM labels l
LEFT JOIN photo_labels pl ON pl.label_uid = l.uid
WHERE immutable_unaccent(l.name) ILIKE immutable_unaccent($1)
GROUP BY l.uid
ORDER BY l.priority DESC, l.name, l.uid
LIMIT $2`

// SearchLabels returns up to limit labels whose name contains q, matched case-
// and accent-insensitively, each paired with its photo count and ordered by
// priority then name. A non-positive limit falls back to defaultSearchLimit. It
// backs the grouped global-search endpoint. The result is empty (not nil) when
// nothing matches.
func (s *Store) SearchLabels(ctx context.Context, q string, limit int) ([]LabelCount, error) {
	rows, err := s.pool.Query(ctx, searchLabelsSQL, likePattern(q), clampSearchLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("organize: searching labels: %w", err)
	}
	defer rows.Close()

	out := make([]LabelCount, 0)
	for rows.Next() {
		lc, err := scanLabelCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating label search: %w", err)
	}
	return out, nil
}

// likePattern wraps q as a case-insensitive "contains" ILIKE pattern, escaping
// the LIKE metacharacters (backslash, %, _) in q so they match literally instead
// of acting as wildcards. The result is meant to be fed through immutable_unaccent
// in the query for diacritics-insensitive matching.
func likePattern(q string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + escaped + "%"
}

// clampSearchLimit returns limit when positive, or defaultSearchLimit for a
// non-positive limit, bounding a search result set.
func clampSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	return limit
}
