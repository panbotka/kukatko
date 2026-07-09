package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sentinel errors for the label command inputs, checked client-side so an obvious
// typo costs no round trip.
var (
	// ErrEmptyName indicates a blank label name, which the API rejects with 400.
	ErrEmptyName = errors.New("ctl: label name must not be empty")
	// ErrInvalidLabelSource indicates a label source the API does not recognise.
	ErrInvalidLabelSource = errors.New(`ctl: label source must be "manual", "ai" or "import"`)
)

// Label sources accepted by POST /labels/{uid}/photos. A label attached from the
// terminal is manual by default; the other two exist because an operator may need
// to reproduce what the classifier or an import would have written.
const (
	// SourceManual is a label attached by hand (the API's default for a blank source).
	SourceManual = "manual"
	// SourceAI is a label produced by automatic classification.
	SourceAI = "ai"
	// SourceImport is a label carried over from a PhotoPrism / photo-sorter import.
	SourceImport = "import"
)

// Label is the subset of a label payload the CLI renders. PhotoCount is only
// populated by GET /labels, which pairs every label with how many photos carry it;
// GET /labels/{uid} returns a bare label and leaves it zero.
type Label struct {
	UID        string    `json:"uid"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	Priority   int       `json:"priority"`
	PhotoCount int       `json:"photo_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// LabelInput is the body of POST /labels. Priority floats a label up the UI's
// list and is omitted from the wire at its zero value.
type LabelInput struct {
	Name     string `json:"name"`
	Priority int    `json:"priority,omitempty"`
}

// validate rejects a blank name the way the API would.
func (in LabelInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrEmptyName
	}
	return nil
}

// labelPhotoBody is the body of the label attach and detach endpoints. Source and
// Uncertainty are read only on attach; detach uses just PhotoUID.
type labelPhotoBody struct {
	PhotoUID    string `json:"photo_uid"`
	Source      string `json:"source,omitempty"`
	Uncertainty int    `json:"uncertainty,omitempty"`
}

// ListLabels fetches GET /labels and returns the raw JSON body.
//
// The envelope is a bare {"labels": […]} ordered by priority, with no paging
// fields — a third shape next to the /photos envelope and the /albums one. See
// ListAlbums on why each resource keeps its own decoder. Decode this one with
// DecodeLabels.
func (c *Client) ListLabels(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/labels", nil)
}

// GetLabel fetches GET /labels/{uid} and returns the raw JSON body: a bare label
// object, without the photo count the list carries. A missing label yields a
// *StatusError with status 404.
func (c *Client) GetLabel(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("label", uid); err != nil {
		return nil, err
	}
	return c.get(ctx, "/labels/"+url.PathEscape(uid), nil)
}

// CreateLabel posts a new label to POST /labels and returns the created label's
// raw JSON, generated UID and unique slug included. It needs the editor or admin
// role: a viewer's token yields a *ForbiddenError.
func (c *Client) CreateLabel(ctx context.Context, in LabelInput) (json.RawMessage, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/labels", in)
}

// AttachLabel attaches the label to one photo, recording where the attachment came
// from (a blank source means manual) and how uncertain it is. The endpoint answers
// 204, so there is nothing to return but an error. It needs the editor or admin role.
func (c *Client) AttachLabel(ctx context.Context, uid, photoUID, source string, uncertainty int) error {
	if err := requireUID("label", uid); err != nil {
		return err
	}
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	switch source {
	case "", SourceManual, SourceAI, SourceImport:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidLabelSource, source)
	}
	body := labelPhotoBody{PhotoUID: photoUID, Source: source, Uncertainty: uncertainty}
	_, err := c.send(ctx, http.MethodPost, labelPhotosPath(uid), body)
	return err
}

// DetachLabel removes the label from one photo. Detaching a label that is not
// attached is a no-op server-side, so the call is idempotent; a missing label
// still yields a *StatusError with status 404. It needs the editor or admin role.
func (c *Client) DetachLabel(ctx context.Context, uid, photoUID string) error {
	if err := requireUID("label", uid); err != nil {
		return err
	}
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodDelete, labelPhotosPath(uid), labelPhotoBody{PhotoUID: photoUID})
	return err
}

// labelPhotosPath renders the attach/detach path for one label.
func labelPhotosPath(uid string) string {
	return "/labels/" + url.PathEscape(uid) + "/photos"
}

// DecodeLabels decodes the bare {"labels": […]} envelope of GET /labels.
func DecodeLabels(raw json.RawMessage) ([]Label, error) {
	var payload struct {
		Labels []Label `json:"labels"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the label list: %w", err)
	}
	return payload.Labels, nil
}

// DecodeLabel decodes one label, as returned by GET /labels/{uid} and POST /labels.
func DecodeLabel(raw json.RawMessage) (Label, error) {
	var label Label
	if err := json.Unmarshal(raw, &label); err != nil {
		return Label{}, fmt.Errorf("decoding the label: %w", err)
	}
	return label, nil
}
