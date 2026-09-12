// Package family is Kukátko's genealogy over subjects: who is whose parent, whose
// partner and whose child. It reads subjects without owning them — internal/people
// owns those — and adds two tables of its own, subject_families and
// subject_family_children (migration 0073).
//
// The family is the node, not the edge. A family is a couple (or a lone parent)
// plus their children, which is the shape genealogy software settled on decades
// ago and the reason the derived relations here cannot contradict each other:
// siblings are the other children of the family a person is a child in, a partner
// is the other partner of a family they are a partner in, half-siblings are the
// children of another family one of their parents is a partner in, and a second
// marriage is simply a second family.
//
// Two database constraints carry the invariants this package relies on. The
// unique pair index (NULLS NOT DISTINCT) makes one couple exactly one family and
// one lone parent exactly one family; the unique index on child_uid makes a
// person a child in at most one family, which is what keeps the descendant walk a
// tree rather than a general graph. Cycles are the one thing SQL cannot refuse on
// its own — A the child of B's family while B is the child of A's — so every
// attachment walks the prospective parents' ancestors first (see wouldCycle), and
// every recursive walk carries a depth guard so a cycle that somehow reached the
// table still cannot hang a request.
package family

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Sentinel errors returned by the store so callers (handlers, the CLI, tests) can
// branch with errors.Is.
var (
	// ErrSubjectNotFound indicates one of the named subjects does not exist.
	ErrSubjectNotFound = errors.New("family: subject not found")
	// ErrFamilyNotFound indicates no family matched the given UID.
	ErrFamilyNotFound = errors.New("family: family not found")
	// ErrRelationNotFound indicates the two subjects are not related in any way
	// this package records, so there is nothing to remove.
	ErrRelationNotFound = errors.New("family: relation not found")
	// ErrCycle indicates the requested parentage would make somebody their own
	// ancestor, which the walks could not terminate on.
	ErrCycle = errors.New("family: relation would create a cycle")
	// ErrAlreadyChild indicates the subject is already a child in another family
	// with both parents recorded, and a person is a child in at most one family.
	ErrAlreadyChild = errors.New("family: subject is already a child in another family")
	// ErrSelfRelation indicates a subject was named as their own parent, child or
	// partner.
	ErrSelfRelation = errors.New("family: a subject cannot be related to itself")
	// ErrFamilyConflict indicates the change would produce a second family for a
	// couple (or a second lone-parent family for one person), which the unique
	// pair index forbids.
	ErrFamilyConflict = errors.New("family: a family for this couple already exists")
	// ErrInvalidKind indicates a family or child kind outside the allowed set.
	ErrInvalidKind = errors.New("family: invalid kind")
	// ErrInvalidYears indicates a from/to year outside the accepted range or a
	// partnership that ends before it begins.
	ErrInvalidYears = errors.New("family: invalid from or to year")
)

const (
	// MinYear is the earliest year a partnership may carry. It sits well below
	// photography itself, so a mistyped year (198, 19) is rejected while every
	// family a photo archive can hold still fits. Mirrored by the SQL CHECK
	// constraint of migration 0073.
	MinYear = 1800
	// MaxDepth bounds every recursive walk. A family archive is nowhere near
	// twenty generations deep, so the guard costs nothing real; what it buys is
	// that a cycle which somehow reached the tables — despite wouldCycle — ends
	// the query instead of the request.
	MaxDepth = 20
)

// Kind classifies what ties a family's partners together, mirrored by the SQL
// CHECK constraint on subject_families.kind.
type Kind string

// The recognised family kinds.
const (
	// KindMarriage is a married couple.
	KindMarriage Kind = "marriage"
	// KindPartnership is an unmarried couple, and the default: it is what the
	// archive can honestly claim about most of the pairs in it.
	KindPartnership Kind = "partnership"
	// KindUnknown admits that nobody knows which of the two it was.
	KindUnknown Kind = "unknown"
)

// valid reports whether k is one of the recognised family kinds.
func (k Kind) valid() bool {
	switch k {
	case KindMarriage, KindPartnership, KindUnknown:
		return true
	default:
		return false
	}
}

// ChildKind records how a child belongs to their family, mirrored by the SQL
// CHECK constraint on subject_family_children.kind.
type ChildKind string

// The recognised child kinds.
const (
	// ChildBirth is a child born to the family, and the default.
	ChildBirth ChildKind = "birth"
	// ChildAdopted is a child adopted into the family.
	ChildAdopted ChildKind = "adopted"
	// ChildStep is a step-child brought into the family by one partner.
	ChildStep ChildKind = "step"
)

// valid reports whether k is one of the recognised child kinds.
func (k ChildKind) valid() bool {
	switch k {
	case ChildBirth, ChildAdopted, ChildStep:
		return true
	default:
		return false
	}
}

// Direction says which way a tree is walked from its root.
type Direction string

// The recognised walk directions.
const (
	// DirectionDescendants walks down: the root's children, their children, and
	// the partners who married in.
	DirectionDescendants Direction = "descendants"
	// DirectionAncestors walks up: parents, grandparents, a binary pedigree
	// bounded by generation.
	DirectionAncestors Direction = "ancestors"
)

// valid reports whether d is one of the recognised directions.
func (d Direction) valid() bool {
	switch d {
	case DirectionDescendants, DirectionAncestors:
		return true
	default:
		return false
	}
}

// Family is one couple (or lone parent) with the metadata of their union. Either
// partner may be nil — a lone parent is a family, because the
// great-grandmother whose husband nobody remembers still has children — but not
// both, and when both are set they are stored in byte order (see normalisePair),
// which is what makes the unique pair index mean "one couple, one family".
type Family struct {
	UID string `json:"uid"`
	// PartnerA and PartnerB are the two sides of the union, nil when unrecorded.
	// The two columns carry no role: which of them is the mother is not something
	// this model claims to know.
	PartnerA  *string   `json:"partner_a_uid"`
	PartnerB  *string   `json:"partner_b_uid"`
	Kind      Kind      `json:"kind"`
	FromYear  *int      `json:"from_year"`
	ToYear    *int      `json:"to_year"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// partnerUIDs returns the family's partners that are actually recorded, in
// stored order: two UIDs for a couple, one for a lone parent.
func (f Family) partnerUIDs() []string {
	uids := make([]string, 0, 2)
	for _, uid := range []*string{f.PartnerA, f.PartnerB} {
		if uid != nil {
			uids = append(uids, *uid)
		}
	}
	return uids
}

// other returns the partner of the family that is not uid, or nil when the
// family has no second partner.
func (f Family) other(uid string) *string {
	if f.PartnerA != nil && *f.PartnerA == uid {
		return f.PartnerB
	}
	if f.PartnerB != nil && *f.PartnerB == uid {
		return f.PartnerA
	}
	return nil
}

// Update carries the editable fields of a family, applied by
// Store.UpdateFamilyAudited. It rewrites the whole editable set, so a nil year
// means "unknown" and clears the column.
type Update struct {
	Kind     Kind   `json:"kind"`
	FromYear *int   `json:"from_year"`
	ToYear   *int   `json:"to_year"`
	Note     string `json:"note"`
}

// Relative is one related subject in the shape the family strip and the tree
// render it: enough of the subject to draw a chip — name, life years, the cover
// it illustrates itself with — plus how many photos it appears on, so the strip
// renders from one response instead of a request per person. A subject with no
// photos is an ordinary relative here, not an anomaly: a great-grandmother nobody
// photographed is exactly the kind of person a tree is drawn for.
type Relative struct {
	UID           string  `json:"uid"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	BirthYear     *int    `json:"birth_year"`
	DeathYear     *int    `json:"death_year"`
	CoverPhotoUID *string `json:"cover_photo_uid,omitempty"`
	// PhotoCount is how many visible photos the subject appears on, counted the
	// way the people index counts them (non-invalid markers, one per photo).
	PhotoCount int `json:"photo_count"`
	// FamilyUID names the family row this relation is recorded in: the family the
	// pair are children of for a parent or a sibling, the family the subject is a
	// partner in for a child. It is empty only where no single family applies.
	FamilyUID string `json:"family_uid,omitempty"`
	// ChildKind is the kind of the child row this relation rides on — birth,
	// adopted or step. For a child or a sibling it is how *they* belong to
	// FamilyUID; for a parent it is how the subject asked about belongs to that
	// parent's family, since that is the membership the relation is read from. It
	// is empty for a partner, who is not a child of the family at all.
	ChildKind ChildKind `json:"child_kind,omitempty"`
}

// Partnership is one family a subject is a partner in, together with the person
// on the other side. Partner is nil for a lone-parent family, which is a family
// all the same: it is where that person's children hang.
type Partnership struct {
	Family  Family    `json:"family"`
	Partner *Relative `json:"partner"`
}

// Relations is the derived view of one subject's immediate family — the four
// lists the strip on the subject page draws. Every one of them is derived from
// the family rows rather than stored, which is why they cannot disagree with each
// other.
type Relations struct {
	Parents  []Relative    `json:"parents"`
	Siblings []Relative    `json:"siblings"`
	Partners []Partnership `json:"partners"`
	Children []Relative    `json:"children"`
}

// Member is one person in a walked tree: the relative plus where the walk found
// them.
type Member struct {
	Relative
	// Depth is how many generations separate this person from the root, which is
	// itself at depth 0. When two paths reach the same person — which happens as
	// soon as cousins marry — the shortest one wins.
	Depth int `json:"depth"`
	// Partner reports that this person is in the set only because they are
	// partnered with a descendant, not because they descend from the root. It is
	// the "plus their partners" half of what "the Nečas family" means.
	Partner bool `json:"partner"`
}

// TreeFamily is one family box of a drawn tree: the family itself plus the
// children of it that the walk actually reached, so the renderer draws no edge
// to a person it was not given.
type TreeFamily struct {
	Family
	ChildUIDs []string `json:"child_uids"`
}

// Tree is the layout-ready payload of one family tree: every person in it, every
// family box tying them together, and which way it was walked. The layout itself
// is a pure function in the frontend; this is only its input.
type Tree struct {
	Root      Relative     `json:"root"`
	Direction Direction    `json:"direction"`
	Members   []Member     `json:"members"`
	Families  []TreeFamily `json:"families"`
}

// DescendantOptions tunes the descendant walk.
type DescendantOptions struct {
	// WithPartners adds every subject who shares a family with a descendant — the
	// people who married into the family. They carry the depth of the descendant
	// they are partnered with and Member.Partner true.
	WithPartners bool
}

// normalisePair orders a couple the way the database stores it: by byte order,
// which is what Go's `<` on strings does and what the COLLATE "C" CHECK of
// migration 0073 enforces. A lone parent — one UID and one empty string — is
// normalised into the first column with the second left nil, so that the unique
// pair index (NULLS NOT DISTINCT) sees one row per lone parent rather than one
// per column they happened to be written into.
//
// Both empty yields (nil, nil), which no caller may store: a family with neither
// partner is refused by subject_families_has_partner.
func normalisePair(a, b string) (*string, *string) {
	switch {
	case a == "" && b == "":
		return nil, nil
	case b == "":
		return &a, nil
	case a == "":
		return &b, nil
	case a < b:
		return &a, &b
	default:
		return &b, &a
	}
}

// wouldCycle reports whether making childUID a child of a family whose ancestry
// is ancestorUIDs would make somebody their own ancestor. The set is the
// prospective parents *and* everyone above them, so the check catches both the
// direct case (a person named as their own parent's parent) and the distant one
// (a great-grandchild adopted as their great-grandparent's parent).
//
// It is a pure function over a set the store reads in SQL, so the rule can be
// tested without a database.
func wouldCycle(childUID string, ancestorUIDs []string) bool {
	return slices.Contains(ancestorUIDs, childUID)
}

// checkPair validates the two ends of a relation: both must be named, and they
// must be different people. It returns ErrSubjectNotFound for an empty UID —
// there is no subject with no UID — and ErrSelfRelation for a subject named on
// both sides.
func checkPair(a, b string) error {
	if a == "" || b == "" {
		return fmt.Errorf("%w: empty subject uid", ErrSubjectNotFound)
	}
	if a == b {
		return fmt.Errorf("%w: %s", ErrSelfRelation, a)
	}
	return nil
}

// checkYears validates a partnership's optional years against nowYear as
// "today": each lies within [MinYear, nowYear], because a year in the future is
// a typo rather than a fact, and an end does not precede a beginning. A nil year
// is unknown and always allowed. It mirrors the SQL CHECK constraints of
// migration 0073, so a caller gets ErrInvalidYears instead of a constraint
// violation.
//
// The upper bound is passed in rather than read from the clock so the rule can be
// tested at a fixed "now"; callers go through checkUpdate.
func checkYears(from, to *int, nowYear int) error {
	for _, y := range []struct {
		name  string
		value *int
	}{{name: "from_year", value: from}, {name: "to_year", value: to}} {
		if y.value == nil {
			continue
		}
		if *y.value < MinYear || *y.value > nowYear {
			return fmt.Errorf("%w: %s %d is outside %d..%d", ErrInvalidYears, y.name, *y.value, MinYear, nowYear)
		}
	}
	if from != nil && to != nil && *to < *from {
		return fmt.Errorf("%w: to_year %d precedes from_year %d", ErrInvalidYears, *to, *from)
	}
	return nil
}

// checkUpdate defaults and validates upd, returning the update the store
// may write. An empty kind becomes KindPartnership; an unrecognised one returns
// ErrInvalidKind, and impossible years return ErrInvalidYears.
func checkUpdate(upd Update) (Update, error) {
	if upd.Kind == "" {
		upd.Kind = KindPartnership
	}
	if !upd.Kind.valid() {
		return Update{}, fmt.Errorf("%w: family kind %q", ErrInvalidKind, upd.Kind)
	}
	if err := checkYears(upd.FromYear, upd.ToYear, time.Now().Year()); err != nil {
		return Update{}, err
	}
	return upd, nil
}

// checkChildKind validates an explicitly requested child kind, returning
// ErrInvalidKind for an unrecognised one. An empty kind is legal and is returned
// as it came, because it means two different things depending on where it lands:
// the default for a membership being created, and "leave it as it is" for one
// that already exists. Adding the second parent of an adopted child must not
// quietly reclassify the adoption as a birth.
func checkChildKind(kind ChildKind) (ChildKind, error) {
	if kind == "" {
		return "", nil
	}
	if !kind.valid() {
		return "", fmt.Errorf("%w: child kind %q", ErrInvalidKind, kind)
	}
	return kind, nil
}

// childKindOrDefault resolves an empty child kind to ChildBirth, which is what a
// new membership gets when the caller did not say.
func childKindOrDefault(kind ChildKind) ChildKind {
	if kind == "" {
		return ChildBirth
	}
	return kind
}
