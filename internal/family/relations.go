package family

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// relativeColumns is the canonical, ordered column list a Relative is read from,
// written against the alias `s` for the subject. The photo count is a correlated
// subquery rather than a join, so a person appearing on no photo still produces a
// row: in a tree that is an ordinary case, not an anomaly.
//
// It counts what the people index counts — non-invalid markers on visible photos,
// once per photo — so the number on a chip in the family strip is the number the
// person's own page will show.
const relativeColumns = `s.uid, s.slug, s.name, s.type, s.birth_year, s.death_year, s.cover_photo_uid,
       (SELECT COUNT(DISTINCT m.photo_uid)
          FROM markers m
          JOIN photos p ON p.uid = m.photo_uid
         WHERE m.subject_uid = s.uid
           AND m.invalid = FALSE
           AND p.archived_at IS NULL
           AND (p.stack_uid IS NULL OR p.stack_primary))`

// relativeOrder orders a list of relatives the way a family reads: oldest first
// where the years are known, then by name, then by uid so the order is stable
// across requests.
const relativeOrder = " ORDER BY s.birth_year NULLS LAST, s.name, s.uid"

// scanRelative reads one relative row: relativeColumns followed by the family the
// relation is recorded in and the child kind, both of which may be NULL for a
// relation that is not a membership (a partner is not a child of the family).
func scanRelative(row pgx.Row) (Relative, error) {
	var rel Relative
	var familyUID *string
	var childKind *string
	if err := row.Scan(
		&rel.UID, &rel.Slug, &rel.Name, &rel.Type, &rel.BirthYear, &rel.DeathYear,
		&rel.CoverPhotoUID, &rel.PhotoCount, &familyUID, &childKind,
	); err != nil {
		return Relative{}, fmt.Errorf("family: scanning relative: %w", err)
	}
	if familyUID != nil {
		rel.FamilyUID = *familyUID
	}
	if childKind != nil {
		rel.ChildKind = ChildKind(*childKind)
	}
	return rel, nil
}

// queryRelatives runs a relative query and collects its rows, wrapping any error
// with what was being read.
func queryRelatives(ctx context.Context, q querier, what, sql string, args ...any) ([]Relative, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("family: reading %s: %w", what, err)
	}
	defer rows.Close()

	out := []Relative{}
	for rows.Next() {
		rel, err := scanRelative(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: reading %s: %w", what, err)
	}
	return out, nil
}

// getRelativeSQL reads one subject as a relative, with no family attached.
const getRelativeSQL = "SELECT " + relativeColumns + `, NULL::varchar, NULL::text
FROM subjects s WHERE s.uid = $1`

// getRelative reads one subject as a Relative, returning ErrSubjectNotFound when
// there is no such subject.
func getRelative(ctx context.Context, q querier, uid string) (Relative, error) {
	rel, err := scanRelative(q.QueryRow(ctx, getRelativeSQL, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Relative{}, fmt.Errorf("%w: %s", ErrSubjectNotFound, uid)
		}
		return Relative{}, err
	}
	return rel, nil
}

// parentsSQL reads the partners of the family the subject is a child in — one
// row per recorded parent, so a lone parent yields one and a couple two. The
// child kind travels with them, because how somebody belongs to the family (born,
// adopted, step) is a fact about that membership and not about either parent.
const parentsSQL = "SELECT " + relativeColumns + `, f.uid, c.kind
FROM subject_family_children c
JOIN subject_families f ON f.uid = c.family_uid
JOIN subjects s ON s.uid IN (f.partner_a_uid, f.partner_b_uid)
WHERE c.child_uid = $1` + relativeOrder

// siblingsSQL reads the other children of the family the subject is a child in.
// Siblings are derived rather than stored, which is the whole point of the family
// being the node: a sibling edge could contradict the parents, and this cannot.
const siblingsSQL = "SELECT " + relativeColumns + `, f.uid, c2.kind
FROM subject_family_children c
JOIN subject_families f ON f.uid = c.family_uid
JOIN subject_family_children c2 ON c2.family_uid = f.uid AND c2.child_uid <> c.child_uid
JOIN subjects s ON s.uid = c2.child_uid
WHERE c.child_uid = $1` + relativeOrder

// childrenSQL reads the children of every family the subject is a partner in, so
// the children of a second marriage appear beside those of the first, each
// carrying the family they belong to.
const childrenSQL = "SELECT " + relativeColumns + `, f.uid, c.kind
FROM subject_families f
JOIN subject_family_children c ON c.family_uid = f.uid
JOIN subjects s ON s.uid = c.child_uid
WHERE f.partner_a_uid = $1 OR f.partner_b_uid = $1` + relativeOrder

// partnersSQL reads every family the subject is a partner in together with the
// person on the other side. The join is a LEFT JOIN because a lone-parent family
// has no other side and must still be returned: it is where that person's
// children hang.
const partnersSQL = `
SELECT f.uid, f.partner_a_uid, f.partner_b_uid, f.kind, f.from_year, f.to_year,
       f.note, f.created_at, f.updated_at, ` + relativeColumns + `
FROM subject_families f
LEFT JOIN subjects s ON s.uid = CASE WHEN f.partner_a_uid = $1 THEN f.partner_b_uid
                                     ELSE f.partner_a_uid END
WHERE f.partner_a_uid = $1 OR f.partner_b_uid = $1
ORDER BY f.from_year NULLS LAST, f.created_at, f.uid`

// Relations returns the four derived lists of one subject's immediate family:
// parents, siblings, partners and children. Every list is derived from the family
// rows, so they cannot disagree with one another; a subject with no recorded
// relations gets four empty lists rather than an error, because "nobody has
// filled this in yet" is the normal state of a new library.
//
// It returns ErrSubjectNotFound if there is no such subject at all.
func (s *Store) Relations(ctx context.Context, subjectUID string) (Relations, error) {
	if _, err := getRelative(ctx, s.pool, subjectUID); err != nil {
		return Relations{}, err
	}
	parents, err := queryRelatives(ctx, s.pool, "parents", parentsSQL, subjectUID)
	if err != nil {
		return Relations{}, err
	}
	siblings, err := queryRelatives(ctx, s.pool, "siblings", siblingsSQL, subjectUID)
	if err != nil {
		return Relations{}, err
	}
	children, err := queryRelatives(ctx, s.pool, "children", childrenSQL, subjectUID)
	if err != nil {
		return Relations{}, err
	}
	partners, err := s.partnerships(ctx, subjectUID)
	if err != nil {
		return Relations{}, err
	}
	return Relations{Parents: parents, Siblings: siblings, Partners: partners, Children: children}, nil
}

// partnerships reads every family the subject is a partner in, each with the
// person on the other side (nil for a lone-parent family).
func (s *Store) partnerships(ctx context.Context, subjectUID string) ([]Partnership, error) {
	rows, err := s.pool.Query(ctx, partnersSQL, subjectUID)
	if err != nil {
		return nil, fmt.Errorf("family: reading partnerships: %w", err)
	}
	defer rows.Close()

	out := []Partnership{}
	for rows.Next() {
		part, err := scanPartnership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: reading partnerships: %w", err)
	}
	return out, nil
}

// scanPartnership reads one partnersSQL row: the family, then the other partner's
// relative columns, which arrive all-NULL for a lone-parent family and then yield
// a nil Partner rather than a zeroed one.
func scanPartnership(row pgx.Row) (Partnership, error) {
	var fam Family
	var rel Relative
	var uid, slug, name, subjectType, cover *string
	var birth, death, photoCount *int
	if err := row.Scan(
		&fam.UID, &fam.PartnerA, &fam.PartnerB, &fam.Kind, &fam.FromYear, &fam.ToYear,
		&fam.Note, &fam.CreatedAt, &fam.UpdatedAt,
		&uid, &slug, &name, &subjectType, &birth, &death, &cover, &photoCount,
	); err != nil {
		return Partnership{}, fmt.Errorf("family: scanning partnership: %w", err)
	}
	if uid == nil {
		return Partnership{Family: fam}, nil
	}
	rel = Relative{
		UID: *uid, Slug: derefString(slug), Name: derefString(name), Type: derefString(subjectType),
		BirthYear: birth, DeathYear: death, CoverPhotoUID: cover,
		PhotoCount: derefInt(photoCount), FamilyUID: fam.UID,
	}
	return Partnership{Family: fam, Partner: &rel}, nil
}

// derefString returns the string ptr points at, or "" when it is nil. The
// partner's columns are NOT NULL in subjects and only become nullable by riding
// on a LEFT JOIN, so a nil here means "no partner row" — which the caller has
// already decided by looking at the uid.
func derefString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

// derefInt returns the int ptr points at, or 0 when it is nil, for the same
// reason as derefString.
func derefInt(ptr *int) int {
	if ptr == nil {
		return 0
	}
	return *ptr
}
