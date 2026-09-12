package family

import (
	"context"
	"fmt"
)

// ChildMembership is one row of subject_family_children: which family a child
// belongs to and how. It is the shape the genealogy is exported in, where a
// membership has to stand on its own rather than hang off a walked tree.
type ChildMembership struct {
	// FamilyUID is the family the child belongs to.
	FamilyUID string
	// ChildUID is the subject who belongs to it.
	ChildUID string
	// Kind records how they belong — born to it, adopted into it, or a
	// step-child brought in by a partner.
	Kind ChildKind
}

// ExportSubject is a person as the genealogy export records them: enough to
// re-create the subject row a family points at, and nothing about the photos
// they appear on.
//
// It is a separate type from people.Subject on purpose. This package must not
// depend on internal/people to answer a question about its own tables, and the
// export deliberately carries only the identity a tree needs — a subject's
// favourite flag, notes and cover are facts about a person in the library, not
// about their place in a family.
type ExportSubject struct {
	UID       string
	Slug      string
	Name      string
	Nickname  string
	Type      string
	BirthYear *int
	DeathYear *int
}

// Export is the whole genealogy in one read: every family, every child
// membership, and every subject either of them names.
//
// It is closed over its own uids — a subject referenced by a family is always
// present in Subjects — which is what lets it be written to a file that means
// something on its own, with no catalogue and no database to look anything up
// in.
type Export struct {
	// Families is every family row, ordered by uid.
	Families []Family
	// Children is every child membership, ordered by family then child.
	Children []ChildMembership
	// Subjects is every subject that appears in a family, ordered by uid. A
	// subject in no family is not here: they have no place in a tree, and what is
	// known about them travels in the sidecars of the photos they appear on.
	Subjects []ExportSubject
}

// Empty reports whether the export holds no genealogy at all — no family, and so
// no membership and no subject either. A library nobody has recorded a relation
// in is empty, which is a legitimate state and not an error.
func (e Export) Empty() bool {
	return len(e.Families) == 0 && len(e.Children) == 0 && len(e.Subjects) == 0
}

// exportFamiliesSQL reads every family, oldest uid first. The order is by uid
// rather than by creation so two runs over an unchanged database produce byte-
// identical output; a file that reshuffles itself on every write is a file whose
// diffs say nothing.
const exportFamiliesSQL = "SELECT " + familyColumns + " FROM subject_families ORDER BY uid"

// exportChildrenSQL reads every child membership, in a stable order for the same
// reason.
const exportChildrenSQL = `
SELECT family_uid, child_uid, kind
FROM subject_family_children
ORDER BY family_uid, child_uid`

// exportSubjectsSQL reads every subject that appears in a family, whether as a
// partner or as a child. The three arms are unioned rather than joined so a
// subject who is both — the usual case, everybody is somebody's child — is read
// once.
const exportSubjectsSQL = `
SELECT s.uid, s.slug, s.name, s.nickname, s.type, s.birth_year, s.death_year
FROM subjects s
WHERE s.uid IN (
    SELECT partner_a_uid FROM subject_families WHERE partner_a_uid IS NOT NULL
    UNION
    SELECT partner_b_uid FROM subject_families WHERE partner_b_uid IS NOT NULL
    UNION
    SELECT child_uid FROM subject_family_children
)
ORDER BY s.uid`

// Export returns the whole genealogy: every family, every child membership and
// every subject either names, each in a deterministic order.
//
// It is read in three statements rather than one join because that is what keeps
// the result a set of rows instead of a cross product to be de-duplicated in Go,
// and because the whole genealogy of a family archive is small — the library it
// was written for holds 118 people. The three reads are not in one transaction:
// the export is rewritten whenever a relation changes, so a run that catches a
// concurrent edit half-applied is superseded by the run that edit itself
// schedules.
func (s *Store) Export(ctx context.Context) (Export, error) {
	families, err := s.exportFamilies(ctx)
	if err != nil {
		return Export{}, err
	}
	children, err := s.exportChildren(ctx)
	if err != nil {
		return Export{}, err
	}
	subjects, err := s.exportSubjects(ctx)
	if err != nil {
		return Export{}, err
	}
	return Export{Families: families, Children: children, Subjects: subjects}, nil
}

// exportFamilies reads every family row.
func (s *Store) exportFamilies(ctx context.Context) ([]Family, error) {
	rows, err := s.pool.Query(ctx, exportFamiliesSQL)
	if err != nil {
		return nil, fmt.Errorf("family: listing families for export: %w", err)
	}
	defer rows.Close()

	var out []Family
	for rows.Next() {
		fam, scanErr := scanFamily(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, fam)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: iterating families for export: %w", err)
	}
	return out, nil
}

// exportChildren reads every child membership.
func (s *Store) exportChildren(ctx context.Context) ([]ChildMembership, error) {
	rows, err := s.pool.Query(ctx, exportChildrenSQL)
	if err != nil {
		return nil, fmt.Errorf("family: listing child memberships for export: %w", err)
	}
	defer rows.Close()

	var out []ChildMembership
	for rows.Next() {
		var membership ChildMembership
		if err := rows.Scan(&membership.FamilyUID, &membership.ChildUID, &membership.Kind); err != nil {
			return nil, fmt.Errorf("family: scanning child membership: %w", err)
		}
		out = append(out, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: iterating child memberships for export: %w", err)
	}
	return out, nil
}

// exportSubjects reads every subject that appears in a family.
func (s *Store) exportSubjects(ctx context.Context) ([]ExportSubject, error) {
	rows, err := s.pool.Query(ctx, exportSubjectsSQL)
	if err != nil {
		return nil, fmt.Errorf("family: listing subjects for export: %w", err)
	}
	defer rows.Close()

	var out []ExportSubject
	for rows.Next() {
		var subj ExportSubject
		if err := rows.Scan(
			&subj.UID, &subj.Slug, &subj.Name, &subj.Nickname, &subj.Type,
			&subj.BirthYear, &subj.DeathYear,
		); err != nil {
			return nil, fmt.Errorf("family: scanning subject for export: %w", err)
		}
		out = append(out, subj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: iterating subjects for export: %w", err)
	}
	return out, nil
}
