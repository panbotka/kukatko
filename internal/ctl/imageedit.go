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

// Sentinel errors for the non-destructive image edit, checked client-side so an
// obvious mistake costs neither a round trip nor an audit entry.
var (
	// ErrInvalidRotation indicates a rotation the API does not accept.
	ErrInvalidRotation = errors.New("ctl: rotation must be one of 0, 90, 180, 270")
	// ErrInvalidAdjustment indicates a brightness or contrast outside [-1, 1].
	ErrInvalidAdjustment = errors.New("ctl: brightness and contrast must be between -1 and 1")
	// ErrInvalidCrop indicates a crop rectangle that is not four numbers within
	// the normalised 0..1 image.
	ErrInvalidCrop = errors.New(
		"ctl: crop must be x,y,w,h — four numbers in 0..1 describing a rectangle inside the image")
	// ErrNoImageEdits indicates an image edit that would change nothing. Saving
	// the neutral edit is a legitimate act, but it is `edits reset`, not an
	// argument-less `edits set`.
	ErrNoImageEdits = errors.New(
		"ctl: image edit needs at least one of --crop, --clear-crop, --rotate, --brightness, --contrast; " +
			"to clear the whole edit use `edits reset`")
)

// The rotations PUT /photos/{uid}/edit accepts, in degrees clockwise.
const (
	rotationMax  = 270
	rotationStep = 90
)

// adjustmentLimit is the inclusive bound of brightness and contrast; 0 is neutral.
const adjustmentLimit = 1.0

// Crop is a normalised crop rectangle: the origin and the size as fractions of
// the whole image, so the same rectangle survives a thumbnail of any size.
type Crop struct {
	X float64
	Y float64
	W float64
	H float64
}

// validate enforces the same bounds internal/photoapi does, so a typo is caught
// before it becomes a 400.
func (c Crop) validate() error {
	if c.X < 0 || c.Y < 0 || c.W <= 0 || c.H <= 0 {
		return fmt.Errorf("%w: the origin must be non-negative and the size positive", ErrInvalidCrop)
	}
	if c.X+c.W > 1 || c.Y+c.H > 1 {
		return fmt.Errorf("%w: the rectangle must lie within the image", ErrInvalidCrop)
	}
	return nil
}

// String renders the rectangle the way --crop takes it, so a read value can be
// pasted straight back into a write.
func (c Crop) String() string {
	parts := make([]string, 0, 4)
	for _, value := range []float64{c.X, c.Y, c.W, c.H} {
		parts = append(parts, formatAdjustment(value))
	}
	return strings.Join(parts, ",")
}

// adjustmentDigits is how many significant digits an edit's numbers are printed
// to. The columns are `real`, so a crop saved as 0.1 comes back as
// 0.10000000149011612 — the float32 nearest to it, written out in full. Six
// digits is more precision than a fraction of an image or a brightness step can
// carry, and it is what keeps a printed crop pasteable back into --crop.
const adjustmentDigits = 6

// formatAdjustment renders one of an edit's numbers without the float32 noise
// the round trip through the database adds.
func formatAdjustment(value float64) string {
	return strconv.FormatFloat(value, 'g', adjustmentDigits, 64)
}

// ParseCrop reads a --crop value, `x,y,w,h` in normalised 0..1 coordinates.
func ParseCrop(raw string) (Crop, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return Crop{}, fmt.Errorf("%w: got %q", ErrInvalidCrop, raw)
	}
	values := make([]float64, 0, 4)
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return Crop{}, fmt.Errorf("%w: %q is not a number", ErrInvalidCrop, part)
		}
		values = append(values, value)
	}
	crop := Crop{X: values[0], Y: values[1], W: values[2], H: values[3]}
	if err := crop.validate(); err != nil {
		return Crop{}, err
	}
	return crop, nil
}

// ImageEdit is the non-destructive edit stored for one photo: how the library
// renders the original, without ever rewriting it. A photo nobody has edited
// reads back as the neutral edit — no crop, no rotation, no adjustment.
type ImageEdit struct {
	PhotoUID   string    `json:"photo_uid"`
	CropX      *float64  `json:"crop_x,omitempty"`
	CropY      *float64  `json:"crop_y,omitempty"`
	CropW      *float64  `json:"crop_w,omitempty"`
	CropH      *float64  `json:"crop_h,omitempty"`
	Rotation   int       `json:"rotation"`
	Brightness float64   `json:"brightness"`
	Contrast   float64   `json:"contrast"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Crop returns the stored crop rectangle, or nil when the photo is uncropped.
// The four coordinates are all-or-nothing server-side, so one of them present is
// enough to read the rectangle.
func (e ImageEdit) Crop() *Crop {
	if e.CropX == nil || e.CropY == nil || e.CropW == nil || e.CropH == nil {
		return nil
	}
	return &Crop{X: *e.CropX, Y: *e.CropY, W: *e.CropW, H: *e.CropH}
}

// IsNeutral reports whether the edit changes nothing about how the photo renders.
func (e ImageEdit) IsNeutral() bool {
	return e.Crop() == nil && e.Rotation == 0 && e.Brightness == 0 && e.Contrast == 0
}

// ImageEditPatch is what `ctl edits set` changes. A nil field means "leave this
// as it is", which the client — not the server — is what keeps, since PUT
// /photos/{uid}/edit replaces the whole edit (see SetImageEdit).
type ImageEditPatch struct {
	// Crop replaces the crop rectangle.
	Crop *Crop
	// ClearCrop removes the crop, leaving the rest of the edit alone. It is a
	// separate field because a nil Crop already means "do not touch it".
	ClearCrop  bool
	Rotation   *int
	Brightness *float64
	Contrast   *float64
}

// isEmpty reports whether the patch would change nothing.
func (p ImageEditPatch) isEmpty() bool {
	return p.Crop == nil && !p.ClearCrop && p.Rotation == nil &&
		p.Brightness == nil && p.Contrast == nil
}

// imageEditBody is the body of PUT /photos/{uid}/edit: the whole edit, with the
// crop written as four explicit nulls when there is none. Nothing is omitted at
// its zero value — for this endpoint an omitted field is not a default, it is a
// reset — and an omitted crop coordinate would be read as no crop at all.
type imageEditBody struct {
	CropX      *float64 `json:"crop_x"`
	CropY      *float64 `json:"crop_y"`
	CropW      *float64 `json:"crop_w"`
	CropH      *float64 `json:"crop_h"`
	Rotation   int      `json:"rotation"`
	Brightness float64  `json:"brightness"`
	Contrast   float64  `json:"contrast"`
}

// newImageEditBody renders one edit as the whole record to send.
func newImageEditBody(crop *Crop, rotation int, brightness, contrast float64) imageEditBody {
	body := imageEditBody{Rotation: rotation, Brightness: brightness, Contrast: contrast}
	if crop != nil {
		body.CropX, body.CropY = &crop.X, &crop.Y
		body.CropW, body.CropH = &crop.W, &crop.H
	}
	return body
}

// validate mirrors internal/photoapi's own checks so a bad value never costs a
// round trip.
func (b imageEditBody) validate() error {
	if b.Rotation < 0 || b.Rotation > rotationMax || b.Rotation%rotationStep != 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidRotation, b.Rotation)
	}
	for _, value := range []float64{b.Brightness, b.Contrast} {
		if value < -adjustmentLimit || value > adjustmentLimit {
			return fmt.Errorf("%w: got %g", ErrInvalidAdjustment, value)
		}
	}
	if b.CropX == nil {
		return nil
	}
	crop := Crop{X: *b.CropX, Y: *b.CropY, W: *b.CropW, H: *b.CropH}
	return crop.validate()
}

// apply folds the patch onto the edit as it is stored, producing the whole
// record to send back.
func (p ImageEditPatch) apply(stored ImageEdit) imageEditBody {
	crop := stored.Crop()
	switch {
	case p.ClearCrop:
		crop = nil
	case p.Crop != nil:
		crop = p.Crop
	}
	rotation, brightness, contrast := stored.Rotation, stored.Brightness, stored.Contrast
	if p.Rotation != nil {
		rotation = *p.Rotation
	}
	if p.Brightness != nil {
		brightness = *p.Brightness
	}
	if p.Contrast != nil {
		contrast = *p.Contrast
	}
	return newImageEditBody(crop, rotation, brightness, contrast)
}

// GetImageEdit fetches GET /photos/{uid}/edit and returns the raw JSON body: the
// photo's stored non-destructive edit, or the neutral one when nobody has edited
// it. Reading needs any role; a missing photo yields a *StatusError with status
// 404. Decode it with DecodeImageEdit.
func (c *Client) GetImageEdit(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.get(ctx, imageEditPath(photoUID), nil)
}

// SetImageEdit changes a photo's non-destructive edit and returns the stored
// result as raw JSON. It needs the editor or admin role.
//
// It reads the current edit first and sends the merged record back, because PUT
// /photos/{uid}/edit replaces the whole edit: a body carrying only a rotation
// would silently drop an existing crop. Nothing about the original file changes
// either way — the edit is how the library renders it, and the derived
// thumbnails are rebuilt from it in the background.
func (c *Client) SetImageEdit(
	ctx context.Context, photoUID string, patch ImageEditPatch,
) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	if patch.isEmpty() {
		return nil, ErrNoImageEdits
	}
	stored, err := c.FetchImageEdit(ctx, photoUID)
	if err != nil {
		return nil, err
	}
	return c.putImageEdit(ctx, photoUID, patch.apply(stored))
}

// ResetImageEdit saves the neutral edit — no crop, no rotation, no adjustment —
// and returns it as raw JSON.
//
// It is a write, not the absence of one: the derived thumbnails were rendered
// through the previous edit and are rebuilt from this one, so resetting is what
// actually puts the photo back the way the file has it. It needs the editor or
// admin role.
func (c *Client) ResetImageEdit(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.putImageEdit(ctx, photoUID, imageEditBody{})
}

// putImageEdit validates one whole edit and writes it.
func (c *Client) putImageEdit(
	ctx context.Context, photoUID string, body imageEditBody,
) (json.RawMessage, error) {
	if err := body.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPut, imageEditPath(photoUID), body)
}

// FetchImageEdit reads one photo's stored edit and decodes it, for the commands
// that need the record itself rather than its raw bytes.
func (c *Client) FetchImageEdit(ctx context.Context, photoUID string) (ImageEdit, error) {
	raw, err := c.GetImageEdit(ctx, photoUID)
	if err != nil {
		return ImageEdit{}, err
	}
	return DecodeImageEdit(raw)
}

// imageEditPath renders the edit path for one photo.
func imageEditPath(photoUID string) string {
	return "/photos/" + url.PathEscape(photoUID) + "/edit"
}

// DecodeImageEdit decodes one stored non-destructive edit.
func DecodeImageEdit(raw json.RawMessage) (ImageEdit, error) {
	var edit ImageEdit
	if err := json.Unmarshal(raw, &edit); err != nil {
		return ImageEdit{}, fmt.Errorf("decoding the image edit: %w", err)
	}
	return edit, nil
}

// WriteImageEdit renders one non-destructive edit as an aligned key/value table,
// saying outright when it is the neutral one — "everything is zero" and "nobody
// has edited this photo" look identical in a table of zeroes otherwise.
func WriteImageEdit(w io.Writer, edit ImageEdit) error {
	rows := [][2]string{
		{"PHOTO", edit.PhotoUID},
		{"CROP", formatCrop(edit.Crop())},
		{"ROTATION", strconv.Itoa(edit.Rotation) + "°"},
		{"BRIGHTNESS", formatAdjustment(edit.Brightness)},
		{"CONTRAST", formatAdjustment(edit.Contrast)},
		{"UPDATED", formatStamp(edit.UpdatedAt)},
	}
	if edit.IsNeutral() {
		rows = append(rows, [2]string{"NEUTRAL", "yes — the photo renders exactly as the file has it"})
	}
	return writeKeyValues(w, rows)
}

// formatCrop renders a crop rectangle, or a dash when the photo is uncropped.
func formatCrop(crop *Crop) string {
	if crop == nil {
		return "-"
	}
	return crop.String()
}
