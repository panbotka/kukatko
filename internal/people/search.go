package people

import (
	"context"
	"fmt"
	"strings"
)

// defaultSearchLimit caps a search group when the caller passes a non-positive
// limit, so a mis-wired caller cannot request an unbounded scan.
const defaultSearchLimit = 8

// searchSubjectsSQL matches subjects by name, case- and accent-insensitively
// (immutable_unaccent + ILIKE), ordered by name then uid for a stable result,
// capped at the bound limit. The pattern in $1 is a pre-escaped "contains" ILIKE
// pattern.
const searchSubjectsSQL = "SELECT " + subjectColumns + " FROM subjects " +
	"WHERE immutable_unaccent(name) ILIKE immutable_unaccent($1) " +
	"ORDER BY name, uid LIMIT $2"

// SearchSubjects returns up to limit subjects whose name contains q, matched
// case- and accent-insensitively, ordered by name. A non-positive limit falls
// back to defaultSearchLimit. It backs the grouped global-search endpoint. The
// result is empty (not nil) when nothing matches.
func (s *Store) SearchSubjects(ctx context.Context, q string, limit int) ([]Subject, error) {
	rows, err := s.pool.Query(ctx, searchSubjectsSQL, likePattern(q), clampSearchLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("people: searching subjects: %w", err)
	}
	defer rows.Close()

	out := make([]Subject, 0)
	for rows.Next() {
		subj, err := scanSubject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, subj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating subject search: %w", err)
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
