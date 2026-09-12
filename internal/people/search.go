package people

import (
	"context"
	"fmt"
	"strings"
)

// defaultSearchLimit caps a search group when the caller passes a non-positive
// limit, so a mis-wired caller cannot request an unbounded scan.
const defaultSearchLimit = 8

// subjectMatchCond is the predicate that decides whether a subject answers to a
// typed query: its name or its nickname contains the pattern, both folded through
// immutable_unaccent on each side so neither case nor diacritics matter. A
// nickname is the handle somebody naming faces actually remembers, so it has to
// match on the same terms as the name.
//
// Both arms bind the SAME placeholder: they are text comparisons of one value,
// and a second bind of it would only be waste. An empty nickname cannot widen a
// search, because the pattern is a "contains" one — '%x%' never matches ” — and
// the one query that does match everything ('%%') already matched every row
// through the name arm.
const subjectMatchCond = "(immutable_unaccent(name) ILIKE immutable_unaccent($1) " +
	"OR immutable_unaccent(nickname) ILIKE immutable_unaccent($1))"

// searchSubjectsSQL matches subjects by name or nickname, case- and
// accent-insensitively (immutable_unaccent + ILIKE), ordered by name then uid for
// a stable result, capped at the bound limit. The pattern in $1 is a pre-escaped
// "contains" ILIKE pattern.
const searchSubjectsSQL = "SELECT " + subjectColumns + " FROM subjects " +
	"WHERE " + subjectMatchCond + " " +
	"ORDER BY name, uid LIMIT $2"

// SearchSubjects returns up to limit subjects whose name or nickname contains q,
// matched case- and accent-insensitively, ordered by name. A non-positive limit
// falls back to defaultSearchLimit. It backs the grouped global-search endpoint.
// The result is empty (not nil) when nothing matches.
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
