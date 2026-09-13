package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors raised by the family commands, checked client-side so an
// obvious mistake costs no round trip.
var (
	// ErrInvalidRole indicates a relation role the API does not recognise.
	ErrInvalidRole = errors.New(`ctl: a relation role is "parent", "child" or "partner"`)
	// ErrInvalidChildKind indicates a child kind outside the recognised set.
	ErrInvalidChildKind = errors.New(`ctl: a child kind is "birth", "adopted" or "step"`)
	// ErrInvalidFamilyKind indicates a family kind outside the recognised set.
	ErrInvalidFamilyKind = errors.New(`ctl: a family kind is "marriage", "partnership" or "unknown"`)
	// ErrInvalidFamilyYear indicates a year outside the accepted range, or a union
	// that ends before it begins.
	ErrInvalidFamilyYear = errors.New("ctl: a family year lies between " +
		strconv.Itoa(FamilyMinYear) + " and this year, and an end does not precede its beginning")
	// ErrNoFamilyEdits indicates an edit that would change nothing.
	ErrNoFamilyEdits = errors.New("ctl: name at least one of --kind, --from-year, --to-year and --note")
	// ErrRelationSelf indicates a subject named on both sides of a relation.
	ErrRelationSelf = errors.New("ctl: a subject cannot be related to itself")
	// ErrNotRelated indicates the two subjects share no relation this package
	// records, so there is nothing to remove.
	ErrNotRelated = errors.New("ctl: these two are not related")
	// ErrSiblingsDerived indicates an attempt to remove a sibling relation. There
	// is no such row: siblings are the other children of a shared family.
	ErrSiblingsDerived = errors.New(
		"ctl: siblings are derived from a shared parent, so there is no sibling relation to remove")
)

// The relation roles POST /subjects/{uid}/relations accepts. They are the other
// person's side of the relation, seen from the subject it is recorded on.
const (
	// RoleParent makes the other person a parent of the subject.
	RoleParent = "parent"
	// RoleChild makes the other person a child of the subject.
	RoleChild = "child"
	// RolePartner makes the other person the subject's partner.
	RolePartner = "partner"
	// RoleSibling is how Relations.Role reports a sibling, and never a role a
	// request may carry: siblings are derived from a shared family, so the way to
	// record one is to give the two children the same parent — and there is no
	// row between them to remove.
	RoleSibling = "sibling"
	// RoleLoneParent labels the row of a family that has no second partner, and
	// is never a role a request may carry either. It is not a relation to anybody:
	// it is the family the subject is a lone parent in, printed so the uid its
	// children hang on stays visible.
	RoleLoneParent = "lone-parent"
)

// The child kinds, saying how a child belongs to their family.
const (
	// ChildBirth is a child born to the family, and the server's default.
	ChildBirth = "birth"
	// ChildAdopted is a child adopted into the family.
	ChildAdopted = "adopted"
	// ChildStep is a step-child brought into the family by one partner.
	ChildStep = "step"
)

// The family kinds, saying what ties a family's partners together.
const (
	// FamilyMarriage is a married couple.
	FamilyMarriage = "marriage"
	// FamilyPartnership is an unmarried couple, and the server's default: it is
	// what the archive can honestly claim about most of the pairs in it.
	FamilyPartnership = "partnership"
	// FamilyUnknown admits that nobody knows which of the two it was.
	FamilyUnknown = "unknown"
)

// FamilyMinYear mirrors family.MinYear, the earliest year a union may carry. It
// is duplicated rather than imported so `ctl` stays a client of the HTTP surface
// and links none of the server's domain packages; the server enforces it either
// way, and so does the SQL CHECK behind it.
const FamilyMinYear = 1800

// Relative is one related person as the family API renders them: enough of the
// subject to name them, their life years, and how many photos they appear on. A
// relative with no photos at all is ordinary here rather than an anomaly — a
// great-grandmother nobody photographed is exactly who a tree is drawn for.
type Relative struct {
	UID           string  `json:"uid"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	BirthYear     *int    `json:"birth_year"`
	DeathYear     *int    `json:"death_year"`
	CoverPhotoUID *string `json:"cover_photo_uid,omitempty"`
	PhotoCount    int     `json:"photo_count"`
	// FamilyUID names the family row the relation is recorded in.
	FamilyUID string `json:"family_uid,omitempty"`
	// ChildKind is how the child row this relation rides on belongs to that
	// family. It is empty for a partner, who is not a child of it at all.
	ChildKind string `json:"child_kind,omitempty"`
}

// Family is one couple — or one lone parent, which is a family all the same,
// since it is where that person's children hang — with the metadata of their
// union. Either partner may be absent; the two columns carry no role, so which of
// them is the mother is not something this record claims to know.
type Family struct {
	UID       string    `json:"uid"`
	PartnerA  *string   `json:"partner_a_uid"`
	PartnerB  *string   `json:"partner_b_uid"`
	Kind      string    `json:"kind"`
	FromYear  *int      `json:"from_year"`
	ToYear    *int      `json:"to_year"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Partners lists the family's recorded partners, in stored order.
func (f Family) Partners() []string {
	uids := make([]string, 0, 2)
	for _, uid := range []*string{f.PartnerA, f.PartnerB} {
		if uid != nil && *uid != "" {
			uids = append(uids, *uid)
		}
	}
	return uids
}

// Partnership is one family a subject is a partner in, with the person on the
// other side. Partner is nil for a lone-parent family.
type Partnership struct {
	Family  Family    `json:"family"`
	Partner *Relative `json:"partner"`
}

// LoneParent reports whether this family records no second partner. Such a
// family is created whenever a child is recorded before a partner is, and it is
// stored as a partnership because the family is the node a child hangs on — but
// there is nobody on the other side, so it is neither presented nor counted as a
// partner.
func (p Partnership) LoneParent() bool {
	return p.Partner == nil
}

// Relations is the derived view of one subject's immediate family, as GET
// /subjects/{uid}/relations answers it. Every list is derived from the family
// rows rather than stored, which is why they cannot disagree with each other.
type Relations struct {
	Parents  []Relative    `json:"parents"`
	Siblings []Relative    `json:"siblings"`
	Partners []Partnership `json:"partners"`
	Children []Relative    `json:"children"`
}

// Role reports how uid is related to the subject these relations belong to:
// RoleParent, RoleChild, RolePartner, RoleSibling, or "" when the two are not
// related at all. It is what turns "remove the relation between these two" into a
// sentence naming what would go, before anything is written.
func (r Relations) Role(uid string) string {
	if relativeByUID(r.Parents, uid) != nil {
		return RoleParent
	}
	if relativeByUID(r.Children, uid) != nil {
		return RoleChild
	}
	for _, partnership := range r.Partners {
		if partnership.Partner != nil && partnership.Partner.UID == uid {
			return RolePartner
		}
	}
	if relativeByUID(r.Siblings, uid) != nil {
		return RoleSibling
	}
	return ""
}

// Find returns the related person with this uid, whichever list they are in, or
// nil when nobody in the family carries it.
func (r Relations) Find(uid string) *Relative {
	for _, list := range [][]Relative{r.Parents, r.Children, r.Siblings} {
		if found := relativeByUID(list, uid); found != nil {
			return found
		}
	}
	for _, partnership := range r.Partners {
		if partnership.Partner != nil && partnership.Partner.UID == uid {
			return partnership.Partner
		}
	}
	return nil
}

// relativeByUID returns the relative with this uid, or nil.
func relativeByUID(list []Relative, uid string) *Relative {
	for i, relative := range list {
		if relative.UID == uid {
			return &list[i]
		}
	}
	return nil
}

// NewPerson describes a person to create while recording a relation to them. It
// is the affordance that makes filling a tree bearable: a great-grandmother
// nobody photographed is otherwise a trip to another screen and back, once per
// person, for every generation nobody wrote down. Only the fields a tree needs
// are offered; the rest is edited on the subject afterwards.
type NewPerson struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	BirthYear *int   `json:"birth_year,omitempty"`
	DeathYear *int   `json:"death_year,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// RelationInput is the body of POST /subjects/{uid}/relations: which role the
// other person takes, and who they are — an existing subject or one this very
// call creates. Exactly one of the two must be given, and the created half
// commits in the same transaction as the relation, so a refused relation leaves
// no orphan person behind.
type RelationInput struct {
	Role       string     `json:"role"`
	SubjectUID string     `json:"subject_uid,omitempty"`
	New        *NewPerson `json:"new_subject,omitempty"`
	ChildKind  string     `json:"child_kind,omitempty"`
}

// validate range-checks what the CLI can reject without a round trip.
func (in RelationInput) validate() error {
	switch in.Role {
	case RoleParent, RoleChild, RolePartner:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidRole, in.Role)
	}
	switch in.ChildKind {
	case "", ChildBirth, ChildAdopted, ChildStep:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidChildKind, in.ChildKind)
	}
	switch {
	case in.SubjectUID == "" && in.New == nil:
		return ErrSubjectRequired
	case in.SubjectUID != "" && in.New != nil:
		return ErrSubjectAmbiguous
	case in.New == nil:
		return nil
	}
	return SubjectInput{Name: in.New.Name, Type: in.New.Type}.validate()
}

// RelationResult is what recording a relation produced, as the API answers it:
// the family the relation landed in, the person on the other side, and whether
// this call created them.
type RelationResult struct {
	Family   Family   `json:"family"`
	Relative Relative `json:"relative"`
	Created  bool     `json:"created"`
}

// RelationReport is a recorded relation with both people named and the role
// spelled out. Like MergeReport it is synthesized rather than passed through:
// the response knows neither the role that was asked for nor the name of the
// subject the relation was recorded on, and "created: true" without saying who
// became whose parent is unreadable — by a person and by an agent alike.
type RelationReport struct {
	RelationResult
	Role        string `json:"role"`
	SubjectUID  string `json:"subject_uid"`
	SubjectName string `json:"subject_name,omitempty"`
}

// FamilyUpdate is the body of PATCH /families/{uid}. It rewrites the whole
// editable set rather than patching it — an omitted year clears a stored one —
// which is what makes ErrNoFamilyEdits worth raising: an edit that names nothing
// is not a no-op, it is an erasure.
type FamilyUpdate struct {
	Kind     string `json:"kind"`
	FromYear *int   `json:"from_year"`
	ToYear   *int   `json:"to_year"`
	Note     string `json:"note"`
}

// validate range-checks the family record against the same rules the SQL CHECKs
// of migration 0073 enforce, so a mistyped year is refused before it is sent.
func (in FamilyUpdate) validate() error {
	switch in.Kind {
	case "", FamilyMarriage, FamilyPartnership, FamilyUnknown:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidFamilyKind, in.Kind)
	}
	thisYear := time.Now().Year()
	for _, year := range []*int{in.FromYear, in.ToYear} {
		if year != nil && (*year < FamilyMinYear || *year > thisYear) {
			return fmt.Errorf("%w: %d", ErrInvalidFamilyYear, *year)
		}
	}
	if in.FromYear != nil && in.ToYear != nil && *in.ToYear < *in.FromYear {
		return fmt.Errorf("%w: %d precedes %d", ErrInvalidFamilyYear, *in.ToYear, *in.FromYear)
	}
	return nil
}

// GetRelations fetches GET /subjects/{uid}/relations and returns the raw JSON
// body: the four derived lists, each entry carrying the person and their photo
// count. Every signed-in role may read them. A missing subject yields a
// *StatusError with status 404; a subject with nothing filled in gets four empty
// lists. Decode it with DecodeRelations.
func (c *Client) GetRelations(ctx context.Context, subjectUID string) (json.RawMessage, error) {
	if err := requireUID("subject", subjectUID); err != nil {
		return nil, err
	}
	return c.get(ctx, relationsPath(subjectUID), nil)
}

// FetchRelations reads one subject's relations and decodes them, for the commands
// that need the record rather than its raw bytes.
func (c *Client) FetchRelations(ctx context.Context, subjectUID string) (Relations, error) {
	raw, err := c.GetRelations(ctx, subjectUID)
	if err != nil {
		return Relations{}, err
	}
	return DecodeRelations(raw)
}

// AddRelation records one relation on subjectUID via POST
// /subjects/{uid}/relations and returns the raw result. It needs the editor or
// admin role. A refusal about the *state* of the tree — a cycle, a second
// parentage, a second family for one couple — is a 409, not a 400: the request
// was well formed, the tree is what stood in its way.
func (c *Client) AddRelation(
	ctx context.Context, subjectUID string, in RelationInput,
) (json.RawMessage, error) {
	if err := requireUID("subject", subjectUID); err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	if in.SubjectUID != "" && in.SubjectUID == subjectUID {
		return nil, fmt.Errorf("%w: %s", ErrRelationSelf, subjectUID)
	}
	return c.send(ctx, http.MethodPost, relationsPath(subjectUID), in)
}

// RemoveRelation removes whatever ties the two subjects together via DELETE
// /subjects/{uid}/relations/{uid2}, which answers 204. Which relation that is
// follows from the rows rather than from the request, and two subjects that are
// not related answer 404 — so a repeated call is never reported as a removal.
func (c *Client) RemoveRelation(ctx context.Context, subjectUID, otherUID string) error {
	if err := requireUID("subject", subjectUID); err != nil {
		return err
	}
	if err := requireUID("related subject", otherUID); err != nil {
		return err
	}
	if subjectUID == otherUID {
		return fmt.Errorf("%w: %s", ErrRelationSelf, subjectUID)
	}
	_, err := c.send(ctx, http.MethodDelete, relationsPath(subjectUID)+"/"+url.PathEscape(otherUID), nil)
	return err
}

// UpdateFamily rewrites a family's own record — what tied the pair together and
// when — via PATCH /families/{uid}, and returns the refreshed family. Who is *in*
// the family is not edited here: that is what the relation commands are for.
func (c *Client) UpdateFamily(ctx context.Context, familyUID string, in FamilyUpdate) (json.RawMessage, error) {
	if err := requireUID("family", familyUID); err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPatch, "/families/"+url.PathEscape(familyUID), in)
}

// relationsPath renders the relations path of one subject.
func relationsPath(subjectUID string) string {
	return "/subjects/" + url.PathEscape(subjectUID) + "/relations"
}

// FindSubjectByName looks a person up among GET /subjects by the name that was
// typed, matching it case-insensitively against the stored name or slug. It
// reports whether anybody matched, so the caller can create the person instead —
// which is what makes `--name` usable for a great-grandmother the library has
// never heard of.
//
// The match is client-side because the family API, unlike the face assignment,
// has no find-or-create by name: its inline half *always* creates, and handing it
// a name that already exists would quietly split one person into two. Several
// people carrying the same name yield ErrSubjectAmbiguous naming them, since
// guessing which great-grandmother was meant is exactly the wrong thing to do.
func (c *Client) FindSubjectByName(ctx context.Context, name string) (Subject, bool, error) {
	wanted := strings.TrimSpace(name)
	if wanted == "" {
		return Subject{}, false, ErrEmptySubjectName
	}
	raw, err := c.ListSubjects(ctx)
	if err != nil {
		return Subject{}, false, err
	}
	subjects, err := DecodeSubjects(raw)
	if err != nil {
		return Subject{}, false, err
	}
	matches := make([]Subject, 0, 1)
	for _, subject := range subjects {
		if strings.EqualFold(subject.Name, wanted) || strings.EqualFold(subject.Slug, wanted) {
			matches = append(matches, subject)
		}
	}
	switch len(matches) {
	case 0:
		return Subject{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return Subject{}, false, fmt.Errorf("%w: %q is %s — name them by uid",
			ErrSubjectAmbiguous, wanted, joinSubjectLabels(matches))
	}
}

// joinSubjectLabels names every candidate of an ambiguous lookup, so the operator
// can copy the uid of the one they meant straight out of the error.
func joinSubjectLabels(subjects []Subject) string {
	labels := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		labels = append(labels, SubjectLabel(subject.Name, subject.UID))
	}
	return strings.Join(labels, ", ")
}

// DecodeRelations decodes the body of GET /subjects/{uid}/relations.
func DecodeRelations(raw json.RawMessage) (Relations, error) {
	var relations Relations
	if err := json.Unmarshal(raw, &relations); err != nil {
		return Relations{}, fmt.Errorf("decoding the relations: %w", err)
	}
	return relations, nil
}

// DecodeRelationResult decodes the body of POST /subjects/{uid}/relations.
func DecodeRelationResult(raw json.RawMessage) (RelationResult, error) {
	var result RelationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return RelationResult{}, fmt.Errorf("decoding the relation: %w", err)
	}
	return result, nil
}

// DecodeFamily decodes one family, as PATCH /families/{uid} answers it.
func DecodeFamily(raw json.RawMessage) (Family, error) {
	var family Family
	if err := json.Unmarshal(raw, &family); err != nil {
		return Family{}, fmt.Errorf("decoding the family: %w", err)
	}
	return family, nil
}

// WriteRelations renders a subject's immediate family as one table — a row per
// relative, in the order a family strip reads: parents, siblings, partners,
// children — followed by a line counting them.
//
// The four lists are one table rather than four because they answer one question
// ("who is this person's family?") and because ROLE is the column that separates
// them. KIND carries what each relation is recorded as: how a child belongs to
// their family, or what ties a couple together.
func WriteRelations(w io.Writer, relations Relations) error {
	rows := make([][]string, 0,
		len(relations.Parents)+len(relations.Siblings)+len(relations.Partners)+len(relations.Children))
	for _, parent := range relations.Parents {
		rows = append(rows, relativeRow(RoleParent, parent))
	}
	for _, sibling := range relations.Siblings {
		rows = append(rows, relativeRow(RoleSibling, sibling))
	}
	for _, partnership := range relations.Partners {
		rows = append(rows, partnershipRow(partnership))
	}
	for _, child := range relations.Children {
		rows = append(rows, relativeRow(RoleChild, child))
	}
	if len(rows) == 0 {
		return writeLine(w, "nobody is related to this subject yet")
	}
	if err := writeTable(w, []string{"ROLE", "WHO", "LIFE", "KIND", "FAMILY", "PHOTOS"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+relationsSummary(relations))
}

// relativeRow renders one parent, sibling or child.
func relativeRow(role string, relative Relative) []string {
	return []string{
		role,
		SubjectLabel(relative.Name, relative.UID),
		formatYears(relative.BirthYear, relative.DeathYear),
		dash(relative.ChildKind),
		dash(relative.FamilyUID),
		strconv.Itoa(relative.PhotoCount),
	}
}

// partnershipRow renders one partner, or the lone-parent family that records a
// person whose partner nobody remembers — which is a family all the same, since
// it is where their children hang. The second is labelled RoleLoneParent rather
// than RolePartner: the row is kept because the family uid is what `family edit`
// takes, but there is nobody on the other side to call a partner.
func partnershipRow(partnership Partnership) []string {
	role, who := RoleLoneParent, "- (no partner recorded)"
	life, photos := "-", "-"
	if partner := partnership.Partner; partner != nil {
		role = RolePartner
		who = SubjectLabel(partner.Name, partner.UID)
		life = formatYears(partner.BirthYear, partner.DeathYear)
		photos = strconv.Itoa(partner.PhotoCount)
	}
	family := partnership.Family.UID
	if years := formatYears(partnership.Family.FromYear, partnership.Family.ToYear); years != "-" {
		family += " " + years
	}
	return []string{role, who, life, dash(partnership.Family.Kind), dash(family), photos}
}

// relationsSummary counts the four lists in one line. Only real partners are
// counted: a lone-parent family is a row in the table, never a person.
func relationsSummary(relations Relations) string {
	partners := 0
	for _, partnership := range relations.Partners {
		if !partnership.LoneParent() {
			partners++
		}
	}
	return strings.Join([]string{
		strconv.Itoa(len(relations.Parents)) + " " + plural(len(relations.Parents), "parent", "parents"),
		strconv.Itoa(len(relations.Siblings)) + " " + plural(len(relations.Siblings), "sibling", "siblings"),
		strconv.Itoa(partners) + " " + plural(partners, "partner", "partners"),
		strconv.Itoa(len(relations.Children)) + " " + plural(len(relations.Children), "child", "children"),
	}, " · ")
}

// WriteRelationReport renders a recorded relation: a key/value table naming both
// people, or the report itself for the two machine formats.
func WriteRelationReport(w io.Writer, out Output, report RelationReport) error {
	if out.Format == FormatTable {
		return writeKeyValues(w, relationRows(report))
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encoding the relation: %w", err)
	}
	if out.Format == FormatLLM {
		return WriteLLM(w, encoded, out.Fields)
	}
	return WriteJSON(w, encoded)
}

// relationRows lists the key/value pairs of a recorded relation in display order.
func relationRows(report RelationReport) [][2]string {
	created := "no — this person was already in the library"
	if report.Created {
		created = "yes — this command created them"
	}
	return [][2]string{
		{"RELATIVE", SubjectLabel(report.Relative.Name, report.Relative.UID)},
		{"ROLE", report.Role + " of " + SubjectLabel(report.SubjectName, report.SubjectUID)},
		{"LIFE", formatYears(report.Relative.BirthYear, report.Relative.DeathYear)},
		{"CREATED", created},
		{"FAMILY", familyLabel(report.Family)},
	}
}

// WriteFamily renders one family as an aligned key/value table. names resolves
// the partners' uids to their names where the caller could look them up; a uid
// that is not in it prints as it is, because a family that could not be named is
// still worth printing.
func WriteFamily(w io.Writer, family Family, names map[string]string) error {
	return writeKeyValues(w, [][2]string{
		{"UID", family.UID},
		{"PARTNERS", familyPartners(family, names)},
		{"KIND", dash(family.Kind)},
		{"YEARS", formatYears(family.FromYear, family.ToYear)},
		{"NOTE", dash(family.Note)},
		{"UPDATED", formatStamp(family.UpdatedAt)},
	})
}

// familyPartners names both sides of a family, or says that only one of them is
// recorded — which is a fact about the family, not a gap in the answer.
func familyPartners(family Family, names map[string]string) string {
	partners := family.Partners()
	labels := make([]string, 0, len(partners))
	for _, uid := range partners {
		labels = append(labels, SubjectLabel(names[uid], uid))
	}
	switch len(labels) {
	case 0:
		return "-"
	case 1:
		return labels[0] + " (lone parent)"
	default:
		return strings.Join(labels, " & ")
	}
}

// familyLabel names a family in one cell: its uid, what tied the pair together
// and for how long.
func familyLabel(family Family) string {
	parts := []string{family.UID}
	if family.Kind != "" {
		parts = append(parts, family.Kind)
	}
	if years := formatYears(family.FromYear, family.ToYear); years != "-" {
		parts = append(parts, years)
	}
	return strings.Join(parts, " · ")
}

// formatYears renders a pair of years as a range — a life, or a union — leaving
// the open end blank and dashing a pair nobody recorded. An unknown death year is
// not the same as none: "1921–" says the person was born and nothing more.
func formatYears(from, to *int) string {
	switch {
	case from == nil && to == nil:
		return "-"
	case to == nil:
		return strconv.Itoa(*from) + "–"
	case from == nil:
		return "–" + strconv.Itoa(*to)
	default:
		return strconv.Itoa(*from) + "–" + strconv.Itoa(*to)
	}
}
