package family

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
)

// ErrInvalidRole indicates a relation role outside the recognised set.
var ErrInvalidRole = fmt.Errorf("%w: unknown relation role", ErrInvalidKind)

// ErrAmbiguousRelation indicates a request that named both an existing subject
// and a new one to create, or neither. Exactly one of the two is a relation this
// package can record.
var ErrAmbiguousRelation = errors.New(
	"family: name either an existing subject or a new one, not both and not neither")

// Role says which side of a relation the other person occupies, seen from the
// subject the relation is recorded on. There is no sibling role: siblings are
// derived from a shared family, so the way to record one is to give the two
// children the same parent.
type Role string

// The recognised relation roles.
const (
	// RoleParent makes the other person a parent of the subject.
	RoleParent Role = "parent"
	// RoleChild makes the other person a child of the subject.
	RoleChild Role = "child"
	// RolePartner makes the other person the subject's partner.
	RolePartner Role = "partner"
)

// valid reports whether r is one of the recognised relation roles.
func (r Role) valid() bool {
	switch r {
	case RoleParent, RoleChild, RolePartner:
		return true
	default:
		return false
	}
}

// NewPerson describes a subject to create as part of recording a relation to
// them. It is the affordance that makes filling a tree bearable: a
// great-grandmother nobody photographed is otherwise a trip to another screen and
// back, once per person, for every generation nobody wrote down.
//
// Only the fields a tree needs are offered. Everything else a subject can carry
// is edited afterwards on the subject itself, which is where that belongs.
type NewPerson struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	BirthYear *int   `json:"birth_year,omitempty"`
	DeathYear *int   `json:"death_year,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// AddRelation is one request to record a relation: which role the other person
// takes, and who they are — an existing subject (SubjectUID) or one created by
// this very call (New). Exactly one of the two must be given.
type AddRelation struct {
	// Role is the other person's side of the relation.
	Role Role `json:"role"`
	// SubjectUID names an existing subject to relate to.
	SubjectUID string `json:"subject_uid,omitempty"`
	// New describes a subject to create and relate to in the same transaction.
	New *NewPerson `json:"new_subject,omitempty"`
	// ChildKind is how the child of the pair belongs to their family — birth,
	// adopted or step. Empty means birth for a new membership and leaves an
	// existing one as it was. It is ignored for RolePartner, who is not a child
	// of the family at all.
	ChildKind ChildKind `json:"child_kind,omitempty"`
}

// AddResult is what recording a relation produced: the family the relation ended
// up in, the person on the other side as the strip renders them, and whether that
// person was created by this call.
type AddResult struct {
	Family   Family   `json:"family"`
	Relative Relative `json:"relative"`
	Created  bool     `json:"created"`
}

// AddRelationAudited records one relation on subjectUID and writes entry in the
// same transaction. When rel.New is set the subject is created in that same
// transaction first, so a refused relation — a cycle, a second parentage, a
// subject that vanished meanwhile — leaves no orphan person behind. That
// atomicity is the whole reason this path exists rather than two API calls.
//
// entry's TargetUID defaults to subjectUID and is stamped before the transaction
// opens, because mutateAudited copies the entry. Its Details map, on the other
// hand, is shared with the copy, and the facts only the mutation knows — which
// family the relation landed in, who the other person turned out to be — are
// stamped into it from inside; a map written there does reach the audit row.
//
// It returns ErrInvalidRole for an unrecognised role, ErrAmbiguousRelation when
// the request names both an existing and a new subject or neither,
// ErrSubjectNotFound when a named subject does not exist, and whatever the
// attachment refuses with: ErrSelfRelation, ErrCycle, ErrAlreadyChild.
func (s *Store) AddRelationAudited(
	ctx context.Context, subjectUID string, rel AddRelation, entry audit.Entry,
) (AddResult, error) {
	if !rel.Role.valid() {
		return AddResult{}, fmt.Errorf("%w: %q", ErrInvalidRole, rel.Role)
	}
	if err := checkRelationTarget(rel); err != nil {
		return AddResult{}, err
	}
	kind, err := checkChildKind(rel.ChildKind)
	if err != nil {
		return AddResult{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = subjectUID
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (AddResult, error) {
		return addRelation(ctx, tx, subjectUID, rel, kind, entry.Details)
	})
}

// checkRelationTarget refuses a request that does not name exactly one other
// person: both an existing subject and a new one is a contradiction, neither is
// not a relation at all. A new subject with a name that identifies nobody —
// blank, or punctuation alone — is refused here too, because it would be stored
// under the shared fallback slug and read as unnamed everywhere afterwards.
func checkRelationTarget(rel AddRelation) error {
	named := strings.TrimSpace(rel.SubjectUID) != ""
	if named == (rel.New != nil) {
		return ErrAmbiguousRelation
	}
	if rel.New != nil && people.NameSlug(rel.New.Name) == "" {
		return fmt.Errorf("%w: a new subject needs a name", ErrSubjectNotFound)
	}
	return nil
}

// addRelation is the body of AddRelationAudited, inside the transaction: resolve
// the other person (creating them when asked), attach them in the requested role,
// and report what the audit trail and the caller should see.
func addRelation(
	ctx context.Context, tx pgx.Tx, subjectUID string, rel AddRelation,
	kind ChildKind, details map[string]any,
) (AddResult, error) {
	if err := requireSubjects(ctx, tx, subjectUID); err != nil {
		return AddResult{}, err
	}
	otherUID, created, err := resolveOther(ctx, tx, rel)
	if err != nil {
		return AddResult{}, err
	}
	if err := checkPair(subjectUID, otherUID); err != nil {
		return AddResult{}, err
	}
	fam, err := attachInRole(ctx, tx, subjectUID, otherUID, rel.Role, kind)
	if err != nil {
		return AddResult{}, err
	}
	relative, err := getRelative(ctx, tx, otherUID)
	if err != nil {
		return AddResult{}, err
	}
	details["role"] = string(rel.Role)
	details["other_uid"] = otherUID
	details["other_name"] = relative.Name
	details["family_uid"] = fam.UID
	details["created_subject"] = created
	return AddResult{Family: fam, Relative: relative, Created: created}, nil
}

// resolveOther returns the UID of the person on the other side of the relation,
// creating the subject first when the request described one instead of naming it,
// and reporting which of the two happened.
func resolveOther(ctx context.Context, tx pgx.Tx, rel AddRelation) (string, bool, error) {
	if rel.New == nil {
		if err := requireSubjects(ctx, tx, rel.SubjectUID); err != nil {
			return "", false, err
		}
		return rel.SubjectUID, false, nil
	}
	subj, err := people.CreateSubjectTx(ctx, tx, people.Subject{
		Name:      strings.TrimSpace(rel.New.Name),
		Type:      people.SubjectType(rel.New.Type),
		Notes:     rel.New.Notes,
		BirthYear: rel.New.BirthYear,
		DeathYear: rel.New.DeathYear,
	})
	if err != nil {
		return "", false, fmt.Errorf("family: creating subject for a new relation: %w", err)
	}
	return subj.UID, true, nil
}

// attachInRole records the relation itself, in the role the request asked for:
// the other person as the subject's parent, as their child, or as their partner.
// The first two are the same attachment with the arguments swapped — the family
// is the node, so which of the pair the page was opened from is presentation, not
// data.
func attachInRole(
	ctx context.Context, tx pgx.Tx, subjectUID, otherUID string, role Role, kind ChildKind,
) (Family, error) {
	switch role {
	case RoleParent:
		return attachChild(ctx, tx, subjectUID, otherUID, kind)
	case RoleChild:
		return attachChild(ctx, tx, otherUID, subjectUID, kind)
	case RolePartner:
		first, second := normalisePair(subjectUID, otherUID)
		return findOrCreateFamily(ctx, tx, first, second)
	default:
		return Family{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
}
