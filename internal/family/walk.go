package family

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// descendantsSQL walks down from a root and hydrates what it found.
//
// The recursive term is the design's §4 verbatim, and `UNION` rather than
// `UNION ALL` is load-bearing: when cousins marry — which in a village they do —
// the same person is reachable by two paths, and UNION ALL would both duplicate
// the rows and, with the depth guard alone, explode combinatorially. The guard
// itself (depth < MaxDepth, inlined because a recursive term takes no parameter
// there) bounds a cycle that somehow reached the tables despite wouldCycle.
//
// `picked` collapses the walk to one row per person at their shortest depth: UNION
// dedupes identical (uid, depth) pairs, but a diamond can still reach one person
// at two different depths, and a tree draws each person once.
//
// `partners` is the non-recursive second step — everybody who shares a family
// with a descendant — and it is the "plus their partners" half of what "the
// Nečas family" means. It is filtered by $2 rather than by building a second
// statement, so the two variants cannot drift apart; the CTE itself is cheap.
const descendantsSQL = `
WITH RECURSIVE descendants(uid, depth) AS (
        SELECT $1::varchar, 0
    UNION
        SELECT c.child_uid, d.depth + 1
        FROM descendants d
        JOIN subject_families f
          ON f.partner_a_uid = d.uid OR f.partner_b_uid = d.uid
        JOIN subject_family_children c ON c.family_uid = f.uid
        WHERE d.depth < 20
),
picked AS (
    SELECT uid, MIN(depth) AS depth FROM descendants GROUP BY uid
),
partners AS (
    SELECT CASE WHEN f.partner_a_uid = d.uid THEN f.partner_b_uid
                ELSE f.partner_a_uid END AS uid,
           MIN(d.depth) AS depth
    FROM picked d
    JOIN subject_families f
      ON f.partner_a_uid = d.uid OR f.partner_b_uid = d.uid
    GROUP BY 1
),
members AS (
    SELECT uid, depth, FALSE AS partner FROM picked
    UNION ALL
    SELECT p.uid, p.depth, TRUE FROM partners p
    WHERE $2::boolean
      AND p.uid IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM picked k WHERE k.uid = p.uid)
)
SELECT ` + relativeColumns + `, m.depth, m.partner
FROM members m
JOIN subjects s ON s.uid = m.uid
ORDER BY m.depth, s.birth_year NULLS LAST, s.name, s.uid`

// ancestorsSQL walks up from a subject and hydrates what it found. A pedigree is
// bounded by construction (2, 4, 8, 16 …), so it takes the generation limit as a
// parameter and needs no dedup subtlety beyond UNION — which is still not
// optional, because two of somebody's ancestors marrying makes the same person
// reachable twice.
const ancestorsSQL = `
WITH RECURSIVE ancestors(uid, depth) AS (
        SELECT $1::varchar, 0
    UNION
        SELECT s.uid, a.depth + 1
        FROM ancestors a
        JOIN subject_family_children c ON c.child_uid = a.uid
        JOIN subject_families f ON f.uid = c.family_uid
        JOIN subjects s ON s.uid IN (f.partner_a_uid, f.partner_b_uid)
        WHERE a.depth < $2
),
picked AS (
    SELECT uid, MIN(depth) AS depth FROM ancestors GROUP BY uid
)
SELECT ` + relativeColumns + `, m.depth, FALSE
FROM picked m
JOIN subjects s ON s.uid = m.uid
ORDER BY m.depth, s.birth_year NULLS LAST, s.name, s.uid`

// treeFamiliesSQL reads every family a member of the walked set is a partner in,
// with the children of it that the walk also reached. A child outside the set is
// left out deliberately: the renderer must not be handed an edge to a person it
// was given no node for, which is exactly what a pedigree's uncles would be.
const treeFamiliesSQL = `
SELECT f.uid, f.partner_a_uid, f.partner_b_uid, f.kind, f.from_year, f.to_year,
       f.note, f.created_at, f.updated_at,
       COALESCE(ARRAY_AGG(c.child_uid ORDER BY c.child_uid)
                FILTER (WHERE c.child_uid = ANY($1)), '{}') AS child_uids
FROM subject_families f
LEFT JOIN subject_family_children c ON c.family_uid = f.uid
WHERE f.partner_a_uid = ANY($1) OR f.partner_b_uid = ANY($1)
GROUP BY f.uid
ORDER BY f.from_year NULLS LAST, f.created_at, f.uid`

// Descendants returns the subject and everyone who descends from them, each with
// the number of generations that separate them from the root (which is itself at
// depth 0). With opts.WithPartners the people who married into the family come
// too, carrying the depth of the descendant they are partnered with.
//
// It returns ErrSubjectNotFound when the root does not exist, so an empty result
// always means "nobody is recorded below this person" rather than "no such
// person".
func (s *Store) Descendants(ctx context.Context, rootUID string, opts DescendantOptions) ([]Member, error) {
	if _, err := getRelative(ctx, s.pool, rootUID); err != nil {
		return nil, err
	}
	return queryMembers(ctx, s.pool, "descendants", descendantsSQL, rootUID, opts.WithPartners)
}

// Ancestors returns the subject and their pedigree up to generations levels
// above them (1 = parents, 2 = grandparents), the root itself at depth 0.
// generations is clamped into 1..MaxDepth, so a caller cannot ask for an
// unbounded walk.
//
// It returns ErrSubjectNotFound when the subject does not exist.
func (s *Store) Ancestors(ctx context.Context, subjectUID string, generations int) ([]Member, error) {
	if _, err := getRelative(ctx, s.pool, subjectUID); err != nil {
		return nil, err
	}
	return queryMembers(ctx, s.pool, "ancestors", ancestorsSQL, subjectUID, clampGenerations(generations))
}

// clampGenerations bounds a requested generation count into 1..MaxDepth. Zero or
// negative means "the default", which is the whole bounded walk: asking for a
// pedigree and getting only the person back would be a confusing answer to a
// missing parameter.
func clampGenerations(generations int) int {
	if generations <= 0 || generations > MaxDepth {
		return MaxDepth
	}
	return generations
}

// Tree returns the layout-ready payload of one family tree: the root, everybody
// the walk in the given direction reached, and every family box tying them
// together. Descendants are walked with their partners, because a couple is one
// box in the drawing; ancestors are a binary pedigree and have no partners to
// add — both sides of every family are already in the set.
//
// An unrecognised direction returns ErrInvalidKind; a missing root returns
// ErrSubjectNotFound.
func (s *Store) Tree(ctx context.Context, rootUID string, direction Direction) (Tree, error) {
	if !direction.valid() {
		return Tree{}, fmt.Errorf("%w: direction %q", ErrInvalidKind, direction)
	}
	root, err := getRelative(ctx, s.pool, rootUID)
	if err != nil {
		return Tree{}, err
	}
	var members []Member
	if direction == DirectionDescendants {
		members, err = queryMembers(ctx, s.pool, "descendants",
			descendantsSQL, rootUID, true)
	} else {
		members, err = queryMembers(ctx, s.pool, "ancestors",
			ancestorsSQL, rootUID, MaxDepth)
	}
	if err != nil {
		return Tree{}, err
	}
	families, err := s.treeFamilies(ctx, memberUIDs(members))
	if err != nil {
		return Tree{}, err
	}
	return Tree{Root: root, Direction: direction, Members: members, Families: families}, nil
}

// memberUIDs returns the UIDs of the walked set, the argument the family lookup
// is scoped by.
func memberUIDs(members []Member) []string {
	uids := make([]string, 0, len(members))
	for _, m := range members {
		uids = append(uids, m.UID)
	}
	return uids
}

// treeFamilies reads the family boxes of a walked set. An empty set has no
// families, and saying so without a query keeps `= ANY('{}')` out of the plan.
func (s *Store) treeFamilies(ctx context.Context, uids []string) ([]TreeFamily, error) {
	out := []TreeFamily{}
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, treeFamiliesSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("family: reading tree families: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fam TreeFamily
		if err := rows.Scan(
			&fam.UID, &fam.PartnerA, &fam.PartnerB, &fam.Kind, &fam.FromYear, &fam.ToYear,
			&fam.Note, &fam.CreatedAt, &fam.UpdatedAt, &fam.ChildUIDs,
		); err != nil {
			return nil, fmt.Errorf("family: scanning tree family: %w", err)
		}
		out = append(out, fam)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: reading tree families: %w", err)
	}
	return out, nil
}

// queryMembers runs one of the two walks and collects its rows, which carry the
// relative columns followed by the walk's depth and its partner flag.
func queryMembers(ctx context.Context, q querier, what, sql string, args ...any) ([]Member, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("family: walking %s: %w", what, err)
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		member, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: walking %s: %w", what, err)
	}
	return out, nil
}

// scanMember reads one walk row: the relative columns, then the depth the walk
// found the person at and whether they are in the set only as a partner.
func scanMember(row pgx.Row) (Member, error) {
	var member Member
	if err := row.Scan(
		&member.UID, &member.Slug, &member.Name, &member.Type, &member.BirthYear,
		&member.DeathYear, &member.CoverPhotoUID, &member.PhotoCount,
		&member.Depth, &member.Partner,
	); err != nil {
		return Member{}, fmt.Errorf("family: scanning tree member: %w", err)
	}
	return member, nil
}
