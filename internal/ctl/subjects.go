package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Subject is the subset of a subject payload the CLI renders. A subject is a
// person, a pet or another recurring thing the face pipeline groups markers
// under. MarkerCount is only populated by GET /subjects, which pairs every
// subject with its non-invalid marker count; GET /subjects/{uid} returns a bare
// subject and leaves it zero.
type Subject struct {
	UID           string    `json:"uid"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Favorite      bool      `json:"favorite"`
	Private       bool      `json:"private"`
	Notes         string    `json:"notes"`
	CoverPhotoUID *string   `json:"cover_photo_uid,omitempty"`
	MarkerCount   int       `json:"marker_count"`
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
