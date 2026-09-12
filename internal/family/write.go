package family

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// pairIndexName is the unique index that makes one couple exactly one family. A
// violation of it is the one unique violation these paths can provoke that is not
// a programming error, so it is recognised by name and reported as
// ErrFamilyConflict.
const pairIndexName = "idx_subject_families_pair"

// childIndexName is the unique index that makes a person a child in at most one
// family. A violation means somebody attached a second parentage between this
// path's check and its write.
const childIndexName = "idx_subject_family_children_child"

// insertFamilySQL creates a family from an already normalised pair.
const insertFamilySQL = `
INSERT INTO subject_families (uid, partner_a_uid, partner_b_uid, kind)
VALUES ($1, $2, $3, $4)
RETURNING ` + familyColumns

// setFamilyPartnersSQL rewrites a family's pair, which the store only ever does
// with an already normalised one.
const setFamilyPartnersSQL = `
UPDATE subject_families SET partner_a_uid = $2, partner_b_uid = $3, updated_at = now()
WHERE uid = $1
RETURNING ` + familyColumns

// updateFamilySQL rewrites a family's editable fields.
const updateFamilySQL = `
UPDATE subject_families SET kind = $2, from_year = $3, to_year = $4, note = $5, updated_at = now()
WHERE uid = $1
RETURNING ` + familyColumns

// deleteFamilySQL removes a family row; its children rows cascade.
const deleteFamilySQL = "DELETE FROM subject_families WHERE uid = $1"

// insertChildSQL records a child's membership of a family.
const insertChildSQL = "INSERT INTO subject_family_children (family_uid, child_uid, kind) VALUES ($1, $2, $3)"

// setChildKindSQL rewrites how an existing child belongs to their family.
const setChildKindSQL = "UPDATE subject_family_children SET kind = $3 WHERE family_uid = $1 AND child_uid = $2"

// deleteChildSQL removes a child's membership of a family.
const deleteChildSQL = "DELETE FROM subject_family_children WHERE family_uid = $1 AND child_uid = $2"

// countChildrenSQL counts a family's children, which decides whether a family
// that just lost its last child is still worth keeping.
const countChildrenSQL = "SELECT COUNT(*) FROM subject_family_children WHERE family_uid = $1"

// ancestorUIDsSQL walks up from the given subjects and returns them together with
// everybody above them. It is the cycle check's evidence: if the prospective
// child is in this set, attaching them would make somebody their own ancestor.
// The depth guard bounds the walk even when the tables already contain a cycle —
// which is the case this check exists to prevent, so it cannot assume its own
// success.
const ancestorUIDsSQL = `
WITH RECURSIVE ancestors(uid, depth) AS (
        SELECT u.uid, 0 FROM unnest($1::varchar[]) AS u(uid)
    UNION
        SELECT s.uid, a.depth + 1
        FROM ancestors a
        JOIN subject_family_children c ON c.child_uid = a.uid
        JOIN subject_families f ON f.uid = c.family_uid
        JOIN subjects s ON s.uid IN (f.partner_a_uid, f.partner_b_uid)
        WHERE a.depth < 20
)
SELECT DISTINCT uid FROM ancestors`

// AddParentAudited records parentUID as a parent of subjectUID and writes entry in
// the same transaction, so the relation and the record of who added it commit
// atomically. entry's TargetUID defaults to subjectUID; it is stamped before the
// transaction opens, because mutateAudited copies the entry and a stamp made
// inside would be lost.
//
// kind says how the child belongs to the family (empty defaults to ChildBirth).
// The family the pair end up in is returned, whether it was created, joined or
// completed by this call. See attachChild for what each of those means and which
// sentinel each refusal carries.
func (s *Store) AddParentAudited(
	ctx context.Context, subjectUID, parentUID string, kind ChildKind, entry audit.Entry,
) (Family, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = subjectUID
	}
	return s.addChildRelation(ctx, subjectUID, parentUID, kind, entry)
}

// AddChildAudited records childUID as a child of subjectUID and writes entry in
// the same transaction. It is AddParentAudited with the roles swapped — the same
// family row results either way, which is the point of the family being the node
// — and differs only in that entry's TargetUID defaults to subjectUID, the person
// whose page the relation was added from.
func (s *Store) AddChildAudited(
	ctx context.Context, subjectUID, childUID string, kind ChildKind, entry audit.Entry,
) (Family, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = subjectUID
	}
	return s.addChildRelation(ctx, childUID, subjectUID, kind, entry)
}

// addChildRelation is the shared body of AddParentAudited and AddChildAudited:
// it validates the kind, then runs attachChild in an audited transaction.
func (s *Store) addChildRelation(
	ctx context.Context, childUID, parentUID string, kind ChildKind, entry audit.Entry,
) (Family, error) {
	childKind, err := checkChildKind(kind)
	if err != nil {
		return Family{}, err
	}
	if err := checkPair(childUID, parentUID); err != nil {
		return Family{}, err
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Family, error) {
		return attachChild(ctx, tx, childUID, parentUID, childKind)
	})
}

// AddPartnerAudited records subjectUID and partnerUID as a couple and writes entry
// in the same transaction, returning the family — the one that already existed if
// the pair had one, since one couple is exactly one family. entry's TargetUID
// defaults to subjectUID, stamped before the transaction opens.
//
// A childless union is a legitimate row here: a marriage nobody has children from
// is still a fact about the two people, and the couple's box is what the tree
// draws. It returns ErrSelfRelation for a subject partnered with itself and
// ErrSubjectNotFound when either side is missing.
func (s *Store) AddPartnerAudited(
	ctx context.Context, subjectUID, partnerUID string, entry audit.Entry,
) (Family, error) {
	if err := checkPair(subjectUID, partnerUID); err != nil {
		return Family{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = subjectUID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Family, error) {
		if err := requireSubjects(ctx, tx, subjectUID, partnerUID); err != nil {
			return Family{}, err
		}
		first, second := normalisePair(subjectUID, partnerUID)
		return findOrCreateFamily(ctx, tx, first, second)
	})
}

// UpdateFamilyAudited rewrites a family's editable fields — kind, years, note —
// and writes entry in the same transaction. entry's TargetUID defaults to the
// family UID. The update rewrites the whole editable set, so a nil year clears
// the column. It returns ErrFamilyNotFound for a missing family, ErrInvalidKind
// for an unrecognised kind and ErrInvalidYears for impossible years.
func (s *Store) UpdateFamilyAudited(
	ctx context.Context, familyUID string, upd Update, entry audit.Entry,
) (Family, error) {
	checked, err := checkUpdate(upd)
	if err != nil {
		return Family{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = familyUID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Family, error) {
		fam, err := scanFamily(tx.QueryRow(ctx, updateFamilySQL,
			familyUID, checked.Kind, checked.FromYear, checked.ToYear, checked.Note))
		if errors.Is(err, pgx.ErrNoRows) {
			return Family{}, ErrFamilyNotFound
		}
		return fam, err
	})
}

// RemoveRelationAudited removes whatever relation ties subjectUID and otherUID
// together and writes entry in the same transaction. entry's TargetUID defaults
// to subjectUID.
//
// Which relation that is follows from the rows, not from an argument: one is the
// other's parent, or the other's child, or their partner. It returns
// ErrRelationNotFound when the two are not related at all, so a caller can answer
// 404 rather than pretend it removed something.
func (s *Store) RemoveRelationAudited(
	ctx context.Context, subjectUID, otherUID string, entry audit.Entry,
) error {
	if err := checkPair(subjectUID, otherUID); err != nil {
		return err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = subjectUID
	}
	_, err := mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, removeRelation(ctx, tx, subjectUID, otherUID)
	})
	return err
}

// attachChild makes parentUID a parent of childUID, in the family that already
// holds the child's parentage where there is one:
//
//   - the child has no family yet — the parent's lone-parent family is found or
//     created and the child joins it;
//   - the child's family already lists this parent — only the membership kind is
//     rewritten, so attaching twice is idempotent rather than an error;
//   - the child's family has one parent and this is the second — the family's pair
//     is completed, unless a family for that exact couple already exists, in which
//     case the child moves into it and the emptied lone-parent family is pruned.
//     That second step is what makes "add the mother, then add the father" work
//     for a couple the library already knew about;
//   - the child's family already has two other parents — ErrAlreadyChild, because
//     a person is a child in at most one family and this model will not guess
//     which parentage was meant to win.
//
// It refuses with ErrCycle when the child is already an ancestor of the
// prospective parent, and with ErrSubjectNotFound when either side is missing.
func attachChild(ctx context.Context, tx pgx.Tx, childUID, parentUID string, kind ChildKind) (Family, error) {
	if err := requireSubjects(ctx, tx, childUID, parentUID); err != nil {
		return Family{}, err
	}
	if err := checkNoCycle(ctx, tx, childUID, parentUID); err != nil {
		return Family{}, err
	}
	current, currentKind, err := childFamily(ctx, tx, childUID)
	if errors.Is(err, ErrFamilyNotFound) {
		return attachToNewFamily(ctx, tx, childUID, parentUID, childKindOrDefault(kind))
	}
	if err != nil {
		return Family{}, err
	}
	// An unspecified kind leaves an existing membership as it was found.
	if kind == "" {
		kind = currentKind
	}
	if slices.Contains(current.partnerUIDs(), parentUID) {
		return current, setChildKind(ctx, tx, current.UID, childUID, kind)
	}
	if len(current.partnerUIDs()) > 1 {
		return Family{}, fmt.Errorf("%w: %s is a child of family %s", ErrAlreadyChild, childUID, current.UID)
	}
	return completeParentage(ctx, tx, current, childUID, parentUID, kind)
}

// attachToNewFamily joins a child with no recorded parentage to the parent's
// lone-parent family, creating that family when the parent has none.
func attachToNewFamily(
	ctx context.Context, tx pgx.Tx, childUID, parentUID string, kind ChildKind,
) (Family, error) {
	first, second := normalisePair(parentUID, "")
	fam, err := findOrCreateFamily(ctx, tx, first, second)
	if err != nil {
		return Family{}, err
	}
	if err := insertChild(ctx, tx, fam.UID, childUID, kind); err != nil {
		return Family{}, err
	}
	return fam, nil
}

// completeParentage adds a second parent to a child whose parentage is currently
// the lone-parent family current.
//
// Completing that family's pair in place is only right when this child is the
// only one hanging off it: a lone parent's *other* children are theirs alone, and
// rewriting the pair would silently hand all of them a second parent they were
// never said to have. So the child moves into the couple's family instead —
// the one they already have, or a fresh one — and the lone-parent family is left
// behind for its remaining children, or pruned when it has none.
func completeParentage(
	ctx context.Context, tx pgx.Tx, current Family, childUID, parentUID string, kind ChildKind,
) (Family, error) {
	first, second := normalisePair(current.partnerUIDs()[0], parentUID)
	couple, err := findFamilyByPair(ctx, tx, first, second)
	if err != nil && !errors.Is(err, ErrFamilyNotFound) {
		return Family{}, err
	}
	if errors.Is(err, ErrFamilyNotFound) {
		alone, err := isOnlyChild(ctx, tx, current.UID)
		if err != nil {
			return Family{}, err
		}
		if alone {
			return completeInPlace(ctx, tx, current, childUID, first, second, kind)
		}
		if couple, err = findOrCreateFamily(ctx, tx, first, second); err != nil {
			return Family{}, err
		}
	}
	if err := moveChild(ctx, tx, current.UID, couple.UID, childUID, kind); err != nil {
		return Family{}, err
	}
	return couple, pruneFamily(ctx, tx, current)
}

// completeInPlace turns a lone-parent family into the couple's family by writing
// the normalised pair onto it, which keeps the family UID — and everything that
// already refers to it — intact.
func completeInPlace(
	ctx context.Context, tx pgx.Tx, current Family, childUID string, first, second *string, kind ChildKind,
) (Family, error) {
	fam, err := scanFamily(tx.QueryRow(ctx, setFamilyPartnersSQL, current.UID, first, second))
	if err != nil {
		return Family{}, translateWriteError(err)
	}
	return fam, setChildKind(ctx, tx, fam.UID, childUID, kind)
}

// isOnlyChild reports whether the family has at most the one child the caller is
// about to move out of it.
func isOnlyChild(ctx context.Context, q querier, familyUID string) (bool, error) {
	children, err := countChildren(ctx, q, familyUID)
	if err != nil {
		return false, err
	}
	return children <= 1, nil
}

// checkNoCycle refuses with ErrCycle when childUID is already an ancestor of
// parentUID, which is the one contradiction the schema cannot refuse on its own:
// the one-family-per-child index stops a second parentage, not a circular one.
func checkNoCycle(ctx context.Context, tx pgx.Tx, childUID, parentUID string) error {
	ancestors, err := ancestorUIDs(ctx, tx, parentUID)
	if err != nil {
		return err
	}
	if wouldCycle(childUID, ancestors) {
		return fmt.Errorf("%w: %s is already an ancestor of %s", ErrCycle, childUID, parentUID)
	}
	return nil
}

// ancestorUIDs returns the given subjects and everybody above them, bounded by
// the walk's depth guard.
func ancestorUIDs(ctx context.Context, q querier, uids ...string) ([]string, error) {
	rows, err := q.Query(ctx, ancestorUIDsSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("family: walking ancestors of %s: %w", strings.Join(uids, ","), err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("family: scanning ancestor uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("family: walking ancestors: %w", err)
	}
	return out, nil
}

// findOrCreateFamily returns the family stored for the already normalised pair,
// creating it when the couple (or lone parent) has none. The pair is looked up
// rather than inserted blind because one couple is exactly one family: a second
// row for them is not a new fact, it is the same one written twice.
func findOrCreateFamily(ctx context.Context, tx pgx.Tx, first, second *string) (Family, error) {
	fam, err := findFamilyByPair(ctx, tx, first, second)
	if err == nil {
		return fam, nil
	}
	if !errors.Is(err, ErrFamilyNotFound) {
		return Family{}, err
	}
	uid, err := newFamilyUID()
	if err != nil {
		return Family{}, err
	}
	fam, err = scanFamily(tx.QueryRow(ctx, insertFamilySQL, uid, first, second, KindPartnership))
	if err != nil {
		return Family{}, translateWriteError(err)
	}
	return fam, nil
}

// removeRelation deletes whatever ties the two subjects together, looking for it
// in the order a page offers it: the other is the subject's child, the other is
// the subject's parent, the two are partners.
func removeRelation(ctx context.Context, tx pgx.Tx, subjectUID, otherUID string) error {
	done, err := detachParentage(ctx, tx, otherUID, subjectUID)
	if err != nil || done {
		return err
	}
	done, err = detachParentage(ctx, tx, subjectUID, otherUID)
	if err != nil || done {
		return err
	}
	first, second := normalisePair(subjectUID, otherUID)
	fam, err := findFamilyByPair(ctx, tx, first, second)
	if errors.Is(err, ErrFamilyNotFound) {
		return fmt.Errorf("%w: %s and %s", ErrRelationNotFound, subjectUID, otherUID)
	}
	if err != nil {
		return err
	}
	return detachPartner(ctx, tx, fam, otherUID)
}

// detachParentage removes parentUID from childUID's parentage when that is
// indeed how the two are related, reporting whether it found anything to do. It
// is called once for each direction, because "remove the relation between these
// two" does not say which of them is the parent.
func detachParentage(ctx context.Context, tx pgx.Tx, childUID, parentUID string) (bool, error) {
	fam, kind, err := childFamily(ctx, tx, childUID)
	if errors.Is(err, ErrFamilyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !slices.Contains(fam.partnerUIDs(), parentUID) {
		return false, nil
	}
	return true, detachChild(ctx, tx, fam, childUID, parentUID, kind)
}

// detachChild removes one parent from a child's parentage. When the family has a
// second parent the child keeps them — the child row moves to that parent's
// lone-parent family — because "X is no longer Y's father" must not quietly take
// Y's mother away too. When there was no second parent the membership is simply
// deleted, and the emptied family is pruned.
func detachChild(
	ctx context.Context, tx pgx.Tx, fam Family, childUID, parentUID string, kind ChildKind,
) error {
	remaining := fam.other(parentUID)
	if remaining == nil {
		if err := deleteChild(ctx, tx, fam.UID, childUID); err != nil {
			return err
		}
		return pruneFamily(ctx, tx, fam)
	}
	first, second := normalisePair(*remaining, "")
	target, err := findOrCreateFamily(ctx, tx, first, second)
	if err != nil {
		return err
	}
	if err := moveChild(ctx, tx, fam.UID, target.UID, childUID, kind); err != nil {
		return err
	}
	return pruneFamily(ctx, tx, fam)
}

// detachPartner removes one partner from a family. A family with children keeps
// its row and becomes the remaining partner's lone-parent family, because the
// children's parentage is hanging off it; a childless union has nothing left to
// record and its row goes.
//
// The survivor is renormalised into the first column, which is what keeps "one
// lone parent, one family" true — the unique pair index treats (X, NULL) and
// (NULL, X) as different rows. If the survivor already has a lone-parent family
// of their own the renormalisation collides with it, and that is reported as
// ErrFamilyConflict rather than resolved by guessing which children were meant
// to move.
func detachPartner(ctx context.Context, tx pgx.Tx, fam Family, partnerUID string) error {
	children, err := countChildren(ctx, tx, fam.UID)
	if err != nil {
		return err
	}
	if children == 0 {
		if _, err := tx.Exec(ctx, deleteFamilySQL, fam.UID); err != nil {
			return fmt.Errorf("family: deleting family %s: %w", fam.UID, err)
		}
		return nil
	}
	remaining := fam.other(partnerUID)
	if remaining == nil {
		return fmt.Errorf("%w: %s and family %s", ErrRelationNotFound, partnerUID, fam.UID)
	}
	first, second := normalisePair(*remaining, "")
	if _, err := scanFamily(tx.QueryRow(ctx, setFamilyPartnersSQL, fam.UID, first, second)); err != nil {
		return translateWriteError(err)
	}
	return nil
}

// pruneFamily deletes a lone-parent family that has no children left. A childless
// *couple* survives: a marriage nobody had children from is a fact worth keeping,
// while a lone parent with no children records nothing at all.
func pruneFamily(ctx context.Context, tx pgx.Tx, fam Family) error {
	if len(fam.partnerUIDs()) > 1 {
		return nil
	}
	children, err := countChildren(ctx, tx, fam.UID)
	if err != nil {
		return err
	}
	if children > 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, deleteFamilySQL, fam.UID); err != nil {
		return fmt.Errorf("family: deleting family %s: %w", fam.UID, err)
	}
	return nil
}

// countChildren returns how many children a family has.
func countChildren(ctx context.Context, q querier, familyUID string) (int, error) {
	var n int
	if err := q.QueryRow(ctx, countChildrenSQL, familyUID).Scan(&n); err != nil {
		return 0, fmt.Errorf("family: counting children of %s: %w", familyUID, err)
	}
	return n, nil
}

// insertChild records a child's membership of a family.
func insertChild(ctx context.Context, tx pgx.Tx, familyUID, childUID string, kind ChildKind) error {
	if _, err := tx.Exec(ctx, insertChildSQL, familyUID, childUID, kind); err != nil {
		return translateWriteError(err)
	}
	return nil
}

// setChildKind rewrites how an existing child belongs to their family.
func setChildKind(ctx context.Context, tx pgx.Tx, familyUID, childUID string, kind ChildKind) error {
	if _, err := tx.Exec(ctx, setChildKindSQL, familyUID, childUID, kind); err != nil {
		return fmt.Errorf("family: updating child %s of family %s: %w", childUID, familyUID, err)
	}
	return nil
}

// deleteChild removes a child's membership of a family.
func deleteChild(ctx context.Context, tx pgx.Tx, familyUID, childUID string) error {
	if _, err := tx.Exec(ctx, deleteChildSQL, familyUID, childUID); err != nil {
		return fmt.Errorf("family: detaching child %s from family %s: %w", childUID, familyUID, err)
	}
	return nil
}

// moveChild moves a child's membership from one family to another, keeping how
// they belong. The delete has to precede the insert: a person is a child in at
// most one family, and the unique index says so.
func moveChild(ctx context.Context, tx pgx.Tx, fromUID, toUID, childUID string, kind ChildKind) error {
	if err := deleteChild(ctx, tx, fromUID, childUID); err != nil {
		return err
	}
	return insertChild(ctx, tx, toUID, childUID, kind)
}

// translateWriteError maps the constraint violations these paths can provoke onto
// the package's sentinels: the pair index onto ErrFamilyConflict, the child index
// onto ErrAlreadyChild, a foreign key onto ErrSubjectNotFound. Anything else is
// wrapped as it came.
func translateWriteError(err error) error {
	if err == nil {
		return nil
	}
	if name, ok := constraintOf(err); ok {
		switch name {
		case pairIndexName:
			return fmt.Errorf("%w: %w", ErrFamilyConflict, err)
		case childIndexName, "subject_family_children_pkey":
			return fmt.Errorf("%w: %w", ErrAlreadyChild, err)
		}
	}
	if isMissingSubject(err) {
		return fmt.Errorf("%w: %w", ErrSubjectNotFound, err)
	}
	return fmt.Errorf("family: writing family row: %w", err)
}
