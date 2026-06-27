package organizeapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/organize"
)

// maxBodyBytes caps the request body size for album/label mutations; these
// records are small, so a tight limit guards against oversized payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// errEmptyTitle is returned when an album create/update request omits the title,
// which the slug derivation and display both require.
var errEmptyTitle = errors.New("album title is required")

// errEmptyName is returned when a label create/update request omits the name,
// which the slug derivation and display both require.
var errEmptyName = errors.New("label name is required")

// errNoPhotoUIDs is returned when a membership request carries no photo UIDs.
var errNoPhotoUIDs = errors.New("photo_uids must not be empty")

// errNoPhotoUID is returned when an attach/detach request omits the photo UID.
var errNoPhotoUID = errors.New("photo_uid is required")

// albumInput is the JSON body accepted by the album create and update endpoints.
// UID, slug and timestamps are managed by the store. Type is honoured only on
// create; on update the album's existing type is preserved because the structural
// types (folder, moment, …) are not user-editable.
type albumInput struct {
	Title         string             `json:"title"`
	Description   string             `json:"description"`
	Type          organize.AlbumType `json:"type"`
	CoverPhotoUID *string            `json:"cover_photo_uid"`
	Private       bool               `json:"private"`
	OrderBy       string             `json:"order_by"`
}

// labelInput is the JSON body accepted by the label create and update endpoints.
type labelInput struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// photoUIDsInput is the JSON body accepted by the album membership endpoints
// (add, remove, reorder): the photos to add/remove, or the album's desired order.
type photoUIDsInput struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// labelPhotoInput is the JSON body accepted by the label attach/detach endpoints.
// Source and Uncertainty are honoured only on attach; detach uses just PhotoUID.
type labelPhotoInput struct {
	PhotoUID    string               `json:"photo_uid"`
	Source      organize.LabelSource `json:"source"`
	Uncertainty int                  `json:"uncertainty"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields and
// an oversized body. The returned error message is safe to surface to the client.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeAlbumInput decodes and validates an album body, requiring a non-empty
// title.
func decodeAlbumInput(r *http.Request) (albumInput, error) {
	var in albumInput
	if err := decodeJSON(r, &in); err != nil {
		return albumInput{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return albumInput{}, errEmptyTitle
	}
	return in, nil
}

// decodeLabelInput decodes and validates a label body, requiring a non-empty
// name.
func decodeLabelInput(r *http.Request) (labelInput, error) {
	var in labelInput
	if err := decodeJSON(r, &in); err != nil {
		return labelInput{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return labelInput{}, errEmptyName
	}
	return in, nil
}

// decodePhotoUIDs decodes and validates a membership body, requiring at least
// one photo UID.
func decodePhotoUIDs(r *http.Request) (photoUIDsInput, error) {
	var in photoUIDsInput
	if err := decodeJSON(r, &in); err != nil {
		return photoUIDsInput{}, err
	}
	if len(in.PhotoUIDs) == 0 {
		return photoUIDsInput{}, errNoPhotoUIDs
	}
	return in, nil
}

// decodeLabelPhoto decodes and validates an attach/detach body, requiring a
// non-empty photo UID.
func decodeLabelPhoto(r *http.Request) (labelPhotoInput, error) {
	var in labelPhotoInput
	if err := decodeJSON(r, &in); err != nil {
		return labelPhotoInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	if in.PhotoUID == "" {
		return labelPhotoInput{}, errNoPhotoUID
	}
	return in, nil
}

// toAlbum converts the request input into an organize.Album for creation.
func (in albumInput) toAlbum() organize.Album {
	return organize.Album{
		Title:         in.Title,
		Description:   in.Description,
		Type:          in.Type,
		CoverPhotoUID: in.CoverPhotoUID,
		Private:       in.Private,
		OrderBy:       in.OrderBy,
	}
}

// toUpdate converts the request input into an organize.AlbumUpdate for editing,
// carrying the album's existing type because it is not user-editable.
func (in albumInput) toUpdate(existing organize.AlbumType) organize.AlbumUpdate {
	return organize.AlbumUpdate{
		Title:         in.Title,
		Description:   in.Description,
		Type:          existing,
		CoverPhotoUID: in.CoverPhotoUID,
		Private:       in.Private,
		OrderBy:       in.OrderBy,
	}
}

// toLabel converts the request input into an organize.Label for creation.
func (in labelInput) toLabel() organize.Label {
	return organize.Label{Name: in.Name, Priority: in.Priority}
}

// toUpdate converts the request input into an organize.LabelUpdate for editing.
func (in labelInput) toUpdate() organize.LabelUpdate {
	return organize.LabelUpdate{Name: in.Name, Priority: in.Priority}
}
