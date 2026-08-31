package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for the subject write commands, checked client-side so an
// obvious mistake costs no round trip.
var (
	// ErrEmptySubjectName indicates a blank subject name, which the API rejects.
	ErrEmptySubjectName = errors.New("ctl: subject name must not be empty")
	// ErrInvalidSubjectType indicates a subject type the API does not recognise.
	ErrInvalidSubjectType = errors.New(`ctl: subject type must be "person", "pet" or "other"`)
	// ErrSubjectRequired indicates a command that names a person was given neither
	// a subject uid nor a name.
	ErrSubjectRequired = errors.New("ctl: name the subject by uid or with --name")
	// ErrSubjectAmbiguous indicates both a subject uid and a --name were given,
	// which the API would silently resolve by uid.
	ErrSubjectAmbiguous = errors.New("ctl: give the subject uid or --name, not both")
	// ErrMergeIntoSelf indicates a merge whose two subjects are the same one.
	ErrMergeIntoSelf = errors.New("ctl: a subject cannot be merged into itself")
)

// The subject types accepted by POST /subjects and PATCH /subjects/{uid}.
const (
	// SubjectPerson is a human subject and the server's default.
	SubjectPerson = "person"
	// SubjectPet is an animal subject.
	SubjectPet = "pet"
	// SubjectOther is any other recurring subject.
	SubjectOther = "other"
)

// Subject is the subset of a subject payload the CLI renders. A subject is a
// person, a pet or another recurring thing the face pipeline groups markers
// under. MarkerCount and PhotoCount are only populated by GET /subjects, which
// pairs every subject with both; GET /subjects/{uid} returns a bare subject and
// leaves them zero. They differ whenever one photo carries several markers of
// the same subject.
type Subject struct {
	UID           string    `json:"uid"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Favorite      bool      `json:"favorite"`
	Private       bool      `json:"private"`
	Notes         string    `json:"notes"`
	CoverPhotoUID *string   `json:"cover_photo_uid,omitempty"`
	BirthYear     *int      `json:"birth_year,omitempty"`
	DeathYear     *int      `json:"death_year,omitempty"`
	MarkerCount   int       `json:"marker_count"`
	PhotoCount    int       `json:"photo_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PageOptions is the paging GET /subjects/{uid}/photos accepts. It carries no
// filters: the subject's gallery is scoped by the subject alone, so the catalogue
// filters of ListOptions would be silently ignored and are not offered.
type PageOptions struct {
	Limit  int
	Offset int
}

// query renders the paging parameters, omitting each one left at zero so the
// server applies its own default page size.
func (o PageOptions) query() (url.Values, error) {
	if o.Limit < 0 || o.Offset < 0 {
		return nil, ErrInvalidPaging
	}
	q := url.Values{}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	return q, nil
}

// ListSubjects fetches GET /subjects and returns the raw JSON body.
//
// The envelope is a bare {"subjects": […]} ordered by name, with no paging fields.
// See ListAlbums on why each resource keeps its own decoder. Decode this one with
// DecodeSubjects.
func (c *Client) ListSubjects(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/subjects", nil)
}

// GetSubject fetches GET /subjects/{uid} and returns the raw JSON body: a bare
// subject object, without the marker count the list carries. A missing subject
// yields a *StatusError with status 404.
func (c *Client) GetSubject(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("subject", uid); err != nil {
		return nil, err
	}
	return c.get(ctx, "/subjects/"+url.PathEscape(uid), nil)
}

// SubjectPhotos fetches one page of GET /subjects/{uid}/photos, the subject's
// photo gallery, and returns the raw JSON body.
//
// This envelope happens to match the /photos one field for field, so it decodes
// with DecodePhotoPage — the same shape, not a normalised one. A missing subject
// is not a 404 here: the server answers an empty page, because a subject with no
// markers has no photos either.
func (c *Client) SubjectPhotos(ctx context.Context, uid string, opts PageOptions) (json.RawMessage, error) {
	if err := requireUID("subject", uid); err != nil {
		return nil, err
	}
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	return c.get(ctx, "/subjects/"+url.PathEscape(uid)+"/photos", q)
}

// DecodeSubjects decodes the bare {"subjects": […]} envelope of GET /subjects.
func DecodeSubjects(raw json.RawMessage) ([]Subject, error) {
	var payload struct {
		Subjects []Subject `json:"subjects"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the subject list: %w", err)
	}
	return payload.Subjects, nil
}

// DecodeSubject decodes one subject, as returned by GET /subjects/{uid}.
func DecodeSubject(raw json.RawMessage) (Subject, error) {
	var subject Subject
	if err := json.Unmarshal(raw, &subject); err != nil {
		return Subject{}, fmt.Errorf("decoding the subject: %w", err)
	}
	return subject, nil
}

// SubjectRef names a person either by uid or by name. A name is resolved by the
// server, which finds the subject with that slug or creates it — so an agent can
// name a face after somebody the library has never heard of, in one command.
type SubjectRef struct {
	// UID identifies an existing subject.
	UID string
	// Name is resolved (or created) server-side by slug.
	Name string
}

// SubjectRefFromArgs reads a subject reference from an optional positional uid
// and the --name flag, which are alternatives rather than a pair.
func SubjectRefFromArgs(uid, name string) (SubjectRef, error) {
	ref := SubjectRef{UID: strings.TrimSpace(uid), Name: strings.TrimSpace(name)}
	switch {
	case ref.UID == "" && ref.Name == "":
		return SubjectRef{}, ErrSubjectRequired
	case ref.UID != "" && ref.Name != "":
		return SubjectRef{}, ErrSubjectAmbiguous
	default:
		return ref, nil
	}
}

// String renders the reference for a confirmation line.
func (r SubjectRef) String() string {
	if r.UID != "" {
		return r.UID
	}
	return strconv.Quote(r.Name)
}

// SubjectInput is the body of POST /subjects and PATCH /subjects/{uid}.
//
// The PATCH rewrites the whole editable record rather than patching it — an
// omitted field is cleared, not kept — which is why every write of an existing
// subject goes through a read first (see RenameSubject) instead of sending the one
// field the operator meant to change.
type SubjectInput struct {
	Name          string  `json:"name"`
	Type          string  `json:"type,omitempty"`
	Favorite      bool    `json:"favorite,omitempty"`
	Private       bool    `json:"private,omitempty"`
	Notes         string  `json:"notes,omitempty"`
	CoverPhotoUID *string `json:"cover_photo_uid,omitempty"`
	BirthYear     *int    `json:"birth_year,omitempty"`
	DeathYear     *int    `json:"death_year,omitempty"`
}

// validate range-checks what the CLI can reject without a round trip.
func (in SubjectInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrEmptySubjectName
	}
	switch in.Type {
	case "", SubjectPerson, SubjectPet, SubjectOther:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidSubjectType, in.Type)
	}
}

// input projects a fetched subject back onto the body that would rewrite it
// unchanged, so a single-field edit does not erase everything it did not mention.
func (s Subject) input() SubjectInput {
	return SubjectInput{
		Name:          s.Name,
		Type:          s.Type,
		Favorite:      s.Favorite,
		Private:       s.Private,
		Notes:         s.Notes,
		CoverPhotoUID: s.CoverPhotoUID,
		BirthYear:     s.BirthYear,
		DeathYear:     s.DeathYear,
	}
}

// subjectMerge is the body of POST /subjects/{uid}/merge: which of the two
// subjects survives. The one named in the path is the one merged away.
type subjectMerge struct {
	KeeperUID string `json:"keeper_uid"`
}

// MergeResult is what a merge moved from the source onto the keeper. The source is
// gone by the time this is read — a merge cannot be undone — so these counts are
// the only account of what happened.
type MergeResult struct {
	KeeperUID          string `json:"keeper_uid"`
	SourceUID          string `json:"source_uid"`
	MarkersMoved       int    `json:"markers_moved"`
	FacesMoved         int    `json:"faces_moved"`
	ConfirmationsMoved int    `json:"confirmations_moved"`
	RejectionsMoved    int    `json:"rejections_moved"`
	RejectionsDropped  int    `json:"rejections_dropped"`
	DismissalsMoved    int    `json:"dismissals_moved"`
	SharedPhotos       int    `json:"shared_photos"`
}

// MergeReport is a merge's outcome with both people named: the server's counts,
// plus the names its body does not carry. See WriteMergeReport for why ctl
// synthesizes them here rather than passing the response through.
type MergeReport struct {
	MergeResult
	KeeperName string `json:"keeper_name,omitempty"`
	SourceName string `json:"source_name,omitempty"`
}

// CreateSubject posts a new subject to POST /subjects and returns the created
// record, generated uid and unique slug included. It needs the editor or admin
// role: a viewer's token yields a *ForbiddenError.
func (c *Client) CreateSubject(ctx context.Context, in SubjectInput) (json.RawMessage, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/subjects", in)
}

// UpdateSubject rewrites a subject's whole editable record via PATCH
// /subjects/{uid}. Every field is sent, so callers build the body from the stored
// record rather than from scratch.
func (c *Client) UpdateSubject(ctx context.Context, uid string, in SubjectInput) (json.RawMessage, error) {
	if err := requireUID("subject", uid); err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPatch, "/subjects/"+url.PathEscape(uid), in)
}

// RenameSubject changes only a subject's name, reading the record first and
// sending it back with the new name.
//
// The read is not optional: PATCH /subjects/{uid} rewrites the whole editable
// record, so a body carrying the name alone would reclassify a pet as a person and
// erase the notes, the cover and the life years along with it.
func (c *Client) RenameSubject(ctx context.Context, uid, name string) (json.RawMessage, error) {
	if err := requireUID("subject", uid); err != nil {
		return nil, err
	}
	raw, err := c.GetSubject(ctx, uid)
	if err != nil {
		return nil, err
	}
	stored, err := DecodeSubject(raw)
	if err != nil {
		return nil, err
	}
	in := stored.input()
	in.Name = strings.TrimSpace(name)
	return c.UpdateSubject(ctx, uid, in)
}

// DeleteSubject removes a subject via DELETE /subjects/{uid}. The markers that
// named it are detached server-side; the photos themselves are untouched.
func (c *Client) DeleteSubject(ctx context.Context, uid string) error {
	if err := requireUID("subject", uid); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodDelete, "/subjects/"+url.PathEscape(uid), nil)
	return err
}

// MergeSubjects merges sourceUID into keeperUID via POST /subjects/{uid}/merge and
// returns the raw counts of what moved. The source subject is deleted by the
// server in the same transaction and cannot be recovered.
func (c *Client) MergeSubjects(ctx context.Context, sourceUID, keeperUID string) (json.RawMessage, error) {
	if err := requireUID("source subject", sourceUID); err != nil {
		return nil, err
	}
	if err := requireUID("keeper subject", keeperUID); err != nil {
		return nil, err
	}
	if sourceUID == keeperUID {
		return nil, fmt.Errorf("%w: %s", ErrMergeIntoSelf, sourceUID)
	}
	path := "/subjects/" + url.PathEscape(sourceUID) + "/merge"
	return c.send(ctx, http.MethodPost, path, subjectMerge{KeeperUID: keeperUID})
}

// DecodeMergeResult decodes the counts POST /subjects/{uid}/merge answers with.
func DecodeMergeResult(raw json.RawMessage) (MergeResult, error) {
	var result MergeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return MergeResult{}, fmt.Errorf("decoding the merge result: %w", err)
	}
	return result, nil
}

// DescribeSubject fetches a subject and renders it as SubjectLabel does, so a
// command whose endpoint answers 204 can still say who it was about.
//
// It costs one extra request, deliberately: the feedback endpoints take a subject
// uid and give nothing back, and a confirmation line reading "rejected as sub1a2b3"
// tells the reader nothing about whether they refused the right person. The lookup
// also turns a mistyped uid into a 404 before the opinion is written rather than
// after.
func (c *Client) DescribeSubject(ctx context.Context, uid string) (string, error) {
	subject, err := c.FetchSubject(ctx, uid)
	if err != nil {
		return "", err
	}
	return SubjectLabel(subject.Name, subject.UID), nil
}

// FetchSubject reads one subject and decodes it, for the commands that need the
// record itself rather than its raw bytes.
func (c *Client) FetchSubject(ctx context.Context, uid string) (Subject, error) {
	raw, err := c.GetSubject(ctx, uid)
	if err != nil {
		return Subject{}, err
	}
	return DecodeSubject(raw)
}
