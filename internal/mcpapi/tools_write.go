package mcpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// Every tool in this file is registered only on the server handed to a caller
// that may write, and every handler re-checks that permission through
// writerFromContext. The duplication is deliberate: the registration is a UX
// decision (do not show an agent a tool it cannot use) while the check is the
// security boundary, and a boundary that lives in only one place moves the first
// time somebody edits the other.
//
// Nothing here deletes a photo. There is no purge tool, no empty-trash tool, no
// archive tool, no restore and no user administration — see docs/MCP.md. Adding
// one is a decision about what an autonomous agent may do to somebody's family
// photos, not a gap in the tool list.

// createAlbumIn describes a new album.
type createAlbumIn struct {
	Title       string `json:"title" jsonschema:"The album's title, as a human would write it."`
	Description string `json:"description,omitempty" jsonschema:"An optional description."`
}

// albumPhotosIn moves photos in or out of an album.
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type albumPhotosIn struct {
	AlbumUID  string   `json:"album_uid" jsonschema:"The album's uid, from list_albums or create_album."`
	PhotoUIDs []string `json:"photo_uids" jsonschema:"The photos to move, by uid. The whole batch applies in one transaction."`
}

// createLabelIn describes a new label.
type createLabelIn struct {
	Name string `json:"name" jsonschema:"The label's name, as a human would write it."`
}

// labelPhotoIn attaches or detaches one label on one photo.
type labelPhotoIn struct {
	PhotoUID string `json:"photo_uid" jsonschema:"The photo's uid."`
	LabelUID string `json:"label_uid" jsonschema:"The label's uid, from list_labels or create_label."`
}

// setMetadataIn edits a photo's user-written text. Every field is a pointer so
// that "leave it alone" and "clear it" are different requests: without that an
// agent setting only a title would silently blank the description.
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type setMetadataIn struct {
	UID         string  `json:"uid" jsonschema:"The photo's uid."`
	Title       *string `json:"title,omitempty" jsonschema:"The new title. Omit to leave it unchanged; pass an empty string to clear it."`
	Description *string `json:"description,omitempty" jsonschema:"The new description. Omit to leave it unchanged; pass an empty string to clear it."`
	Notes       *string `json:"notes,omitempty" jsonschema:"The new notes. Omit to leave it unchanged; pass an empty string to clear it."`
}

// setRatingIn records the calling user's opinion of a photo.
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type setRatingIn struct {
	UID      string  `json:"uid" jsonschema:"The photo's uid."`
	Favorite *bool   `json:"favorite,omitempty" jsonschema:"Mark or unmark as a favourite. Omit to leave it unchanged."`
	Rating   *int    `json:"rating,omitempty" jsonschema:"Stars, 0 to 5, where 0 clears the rating. Omit to leave it unchanged."`
	Flag     *string `json:"flag,omitempty" jsonschema:"Set the flag: none, pick, reject or eye. Omit to leave it unchanged."`
}

// okResult is what a write tool returns when the interesting part of the answer
// is simply that it worked, plus a sentence the agent can repeat to its human.
type okResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// registerAlbumWriteTools adds the album mutations.
func (a *API) registerAlbumWriteTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_album",
		Description: "Create an empty album and return it, including the uid you then pass to " +
			"add_photos_to_album. Albums are hand-made collections of specific photos.",
	}, a.handleCreateAlbum)

	mcp.AddTool(s, &mcp.Tool{
		Name: "add_photos_to_album",
		Description: "Add photos to an album. Adding a photo that is already in the album changes " +
			"nothing, so this is safe to repeat. The whole batch applies in one transaction: if any " +
			"photo or the album does not exist, nothing is added.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleAddPhotosToAlbum)

	mcp.AddTool(s, &mcp.Tool{
		Name: "remove_photos_from_album",
		Description: "Remove photos from an album. This only unfiles them — the photos themselves " +
			"are untouched and stay in the library.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleRemovePhotosFromAlbum)
}

// registerLabelWriteTools adds the label mutations.
func (a *API) registerLabelWriteTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_label",
		Description: "Create a label and return it, including the uid you then pass to attach_label. " +
			"Labels are the library's own curated tags (\"beach\", \"birthday\") that apply to any " +
			"number of photos, as opposed to albums, which are collections of specific photos.",
	}, a.handleCreateLabel)

	mcp.AddTool(s, &mcp.Tool{
		Name: "attach_label",
		Description: "Attach a label to one photo. To label many photos at once use bulk_edit_photos " +
			"instead — it is one transaction rather than one per photo.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleAttachLabel)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "detach_label",
		Description: "Remove a label from one photo. The photo and the label both survive.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleDetachLabel)
}

// registerPhotoWriteTools adds the per-photo edits and the bulk lever.
func (a *API) registerPhotoWriteTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_photo_metadata",
		Description: "Set a photo's title, description or notes. Only the fields you pass change; " +
			"pass an empty string to clear one. Returns the photo's new text.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleSetPhotoMetadata)

	mcp.AddTool(s, &mcp.Tool{
		Name: "set_photo_rating",
		Description: "Set the favourite mark, the star rating (0-5) or the pick/reject flag on a " +
			"photo. These are per-user: they record the opinion of the user who owns the token you " +
			"are using, not a library-wide fact.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, a.handleSetPhotoRating)

	mcp.AddTool(s, &mcp.Tool{
		Name: "bulk_edit_photos",
		Description: "Apply one set of changes to many photos in a single transaction: add or remove " +
			"albums and labels, set the title or description, and set the favourite, rating or flag. " +
			"Prefer this over calling the single-photo tools in a loop — it is far faster and, because " +
			"it is one transaction, a change cannot end up half-applied across the batch. Returns how " +
			"many photos were updated, skipped or errored.",
	}, a.handleBulkEdit)
}

// handleCreateAlbum creates an album.
func (a *API) handleCreateAlbum(
	ctx context.Context, _ *mcp.CallToolRequest, in createAlbumIn,
) (*mcp.CallToolResult, albumInfo, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return nil, albumInfo{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, albumInfo{}, errors.New("an album needs a title")
	}
	al, err := a.organize.CreateAlbumAudited(ctx, organize.Album{
		Title:       title,
		Description: strings.TrimSpace(in.Description),
		Type:        organize.AlbumManual,
		CreatedBy:   &c.user.UID,
	}, c.entry(audit.ActionAlbumCreate, "albums", "", map[string]any{"title": title, "via": viaMCP}))
	if err != nil {
		return nil, albumInfo{}, fmt.Errorf("creating album: %w", err)
	}
	return nil, toAlbumInfo(al, 0), nil
}

// handleAddPhotosToAlbum files photos into an album.
func (a *API) handleAddPhotosToAlbum(
	ctx context.Context, _ *mcp.CallToolRequest, in albumPhotosIn,
) (*mcp.CallToolResult, okResult, error) {
	c, uids, err := a.prepareAlbumPhotos(ctx, in)
	if err != nil {
		return nil, okResult{}, err
	}
	entry := c.entry(audit.ActionAlbumAddPhotos, "albums", in.AlbumUID, map[string]any{
		"photo_uids": uids, "count": len(uids), "via": viaMCP,
	})
	if err := a.organize.AddPhotosAudited(ctx, in.AlbumUID, uids, entry); err != nil {
		return nil, okResult{}, albumWriteError(err, "adding photos to the album")
	}
	return nil, okOf("added %d photo(s) to the album", len(uids)), nil
}

// handleRemovePhotosFromAlbum unfiles photos from an album.
func (a *API) handleRemovePhotosFromAlbum(
	ctx context.Context, _ *mcp.CallToolRequest, in albumPhotosIn,
) (*mcp.CallToolResult, okResult, error) {
	c, uids, err := a.prepareAlbumPhotos(ctx, in)
	if err != nil {
		return nil, okResult{}, err
	}
	entry := c.entry(audit.ActionAlbumRemovePhotos, "albums", in.AlbumUID, map[string]any{
		"photo_uids": uids, "count": len(uids), "via": viaMCP,
	})
	if err := a.organize.RemovePhotosAudited(ctx, in.AlbumUID, uids, entry); err != nil {
		return nil, okResult{}, albumWriteError(err, "removing photos from the album")
	}
	return nil, okOf("removed %d photo(s) from the album", len(uids)), nil
}

// prepareAlbumPhotos validates the shared arguments of the two album membership
// tools and resolves the caller.
func (a *API) prepareAlbumPhotos(ctx context.Context, in albumPhotosIn) (caller, []string, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return caller{}, nil, err
	}
	if strings.TrimSpace(in.AlbumUID) == "" {
		return caller{}, nil, errors.New("album_uid is required")
	}
	uids := cleanUIDs(in.PhotoUIDs)
	if len(uids) == 0 {
		return caller{}, nil, errors.New("photo_uids is required and must name at least one photo")
	}
	return c, uids, nil
}

// handleCreateLabel creates a label.
func (a *API) handleCreateLabel(
	ctx context.Context, _ *mcp.CallToolRequest, in createLabelIn,
) (*mcp.CallToolResult, labelInfo, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return nil, labelInfo{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, labelInfo{}, errors.New("a label needs a name")
	}
	l, err := a.organize.CreateLabelAudited(ctx, organize.Label{Name: name},
		c.entry(audit.ActionLabelCreate, "labels", "", map[string]any{"name": name, "via": viaMCP}))
	if err != nil {
		return nil, labelInfo{}, fmt.Errorf("creating label: %w", err)
	}
	return nil, toLabelInfo(l, 0), nil
}

// handleAttachLabel puts a label on a photo.
func (a *API) handleAttachLabel(
	ctx context.Context, _ *mcp.CallToolRequest, in labelPhotoIn,
) (*mcp.CallToolResult, okResult, error) {
	c, err := a.prepareLabelPhoto(ctx, in)
	if err != nil {
		return nil, okResult{}, err
	}
	entry := c.entry(audit.ActionLabelAttach, "photos", in.PhotoUID,
		map[string]any{"label_uid": in.LabelUID, "via": viaMCP})
	// SourceManual with zero uncertainty: an agent acting on an instruction is
	// making the same claim a human clicking the label makes. The audit trail,
	// not a lower confidence, is what records that a machine did it.
	if err := a.organize.AttachLabelAudited(
		ctx, in.PhotoUID, in.LabelUID, organize.SourceManual, 0, entry,
	); err != nil {
		return nil, okResult{}, labelWriteError(err, "attaching the label")
	}
	return nil, okOf("attached the label"), nil
}

// handleDetachLabel takes a label off a photo.
func (a *API) handleDetachLabel(
	ctx context.Context, _ *mcp.CallToolRequest, in labelPhotoIn,
) (*mcp.CallToolResult, okResult, error) {
	c, err := a.prepareLabelPhoto(ctx, in)
	if err != nil {
		return nil, okResult{}, err
	}
	entry := c.entry(audit.ActionLabelDetach, "photos", in.PhotoUID,
		map[string]any{"label_uid": in.LabelUID, "via": viaMCP})
	if err := a.organize.DetachLabelAudited(ctx, in.PhotoUID, in.LabelUID, entry); err != nil {
		return nil, okResult{}, labelWriteError(err, "detaching the label")
	}
	return nil, okOf("detached the label"), nil
}

// prepareLabelPhoto validates the shared arguments of the two label tools.
func (a *API) prepareLabelPhoto(ctx context.Context, in labelPhotoIn) (caller, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return caller{}, err
	}
	if strings.TrimSpace(in.PhotoUID) == "" || strings.TrimSpace(in.LabelUID) == "" {
		return caller{}, errors.New("photo_uid and label_uid are both required")
	}
	return c, nil
}

// handleSetPhotoMetadata edits a photo's user-written text.
func (a *API) handleSetPhotoMetadata(
	ctx context.Context, _ *mcp.CallToolRequest, in setMetadataIn,
) (*mcp.CallToolResult, photoText, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return nil, photoText{}, err
	}
	if in.Title == nil && in.Description == nil && in.Notes == nil {
		return nil, photoText{}, errors.New("pass at least one of title, description or notes")
	}
	uid := strings.TrimSpace(in.UID)
	// Read-modify-write: the store's metadata update replaces the whole record,
	// so anything not read back here would be silently blanked.
	current, err := a.photos.GetByUID(ctx, uid)
	if err != nil {
		if errors.Is(err, photos.ErrPhotoNotFound) {
			return nil, photoText{}, fmt.Errorf("no photo with uid %q", uid)
		}
		return nil, photoText{}, fmt.Errorf("fetching photo: %w", err)
	}
	upd := metadataOf(current)
	applyString(&upd.Title, in.Title)
	applyString(&upd.Description, in.Description)
	applyString(&upd.Notes, in.Notes)
	if in.Title != nil {
		// The title is now the user's (well, the agent's on their behalf): mark it so
		// an incremental PhotoPrism re-import leaves it alone (see internal/ppimport).
		upd.TitleEdited = true
	}

	details := map[string]any{"fields": changedFields(in), "via": viaMCP}
	metadataChanges(current, upd).StampInto(details)
	entry := c.entry(audit.ActionPhotoUpdate, "photos", uid, details)
	updated, err := a.photos.UpdateMetadataAudited(ctx, uid, upd, entry)
	if err != nil {
		return nil, photoText{}, fmt.Errorf("updating the photo: %w", err)
	}
	return nil, photoText{
		UID:         updated.UID,
		Title:       updated.Title,
		Description: updated.Description,
		Notes:       updated.Notes,
	}, nil
}

// photoText is what set_photo_metadata returns: the fields it can change, read
// back from the row that was actually written.
type photoText struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Notes       string `json:"notes"`
}

// metadataChanges builds the old→new diff for the MCP metadata edit, comparing
// the row before the edit against the update to be applied. The tool can only set
// title, description and notes, so only those are compared; the ChangeSet skips
// any that did not change, matching the "changes" convention the HTTP PATCH path
// records (see internal/audit ChangeSet).
func metadataChanges(before photos.Photo, after photos.MetadataUpdate) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("title", before.Title, after.Title)
	changes.Add("description", before.Description, after.Description)
	changes.Add("notes", before.Notes, after.Notes)
	return changes
}

// changedFields names the fields a metadata call actually touched, for the audit
// details — "the agent set the title" is a more useful record than "the agent
// updated the photo".
func changedFields(in setMetadataIn) []string {
	var out []string
	if in.Title != nil {
		out = append(out, "title")
	}
	if in.Description != nil {
		out = append(out, "description")
	}
	if in.Notes != nil {
		out = append(out, "notes")
	}
	return out
}

// applyString overwrites dst when the caller passed a value, and leaves it alone
// otherwise.
func applyString(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}

// metadataOf copies a photo's editable columns into an update. It exists because
// the store's update is a whole-record replace: every field it does not carry is
// written as empty, so a partial edit must start from the current row.
func metadataOf(p photos.Photo) photos.MetadataUpdate {
	return photos.MetadataUpdate{
		Title:            p.Title,
		TitleEdited:      p.TitleEdited,
		Description:      p.Description,
		Notes:            p.Notes,
		AiNote:           p.AiNote,
		Subject:          p.Subject,
		Keywords:         p.Keywords,
		Artist:           p.Artist,
		Copyright:        p.Copyright,
		License:          p.License,
		Scan:             p.Scan,
		TakenAt:          p.TakenAt,
		TakenAtSource:    p.TakenAtSource,
		TakenAtEstimated: p.TakenAtEstimated,
		TakenAtNote:      p.TakenAtNote,
		Lat:              p.Lat,
		Lng:              p.Lng,
		Altitude:         p.Altitude,
		LocationSource:   p.LocationSource,
		Private:          p.Private,
	}
}

// cleanUIDs trims and drops the empty entries of a uid list.
func cleanUIDs(uids []string) []string {
	out := make([]string, 0, len(uids))
	for _, uid := range uids {
		if uid = strings.TrimSpace(uid); uid != "" {
			out = append(out, uid)
		}
	}
	return out
}

// okOf builds the plain success answer.
func okOf(format string, args ...any) okResult {
	return okResult{OK: true, Message: fmt.Sprintf(format, args...)}
}

// albumWriteError maps an album membership failure onto a message the agent can
// act on, rather than leaking the store's error text.
func albumWriteError(err error, what string) error {
	switch {
	case errors.Is(err, organize.ErrAlbumNotFound):
		return errors.New("no album with that uid")
	case errors.Is(err, photos.ErrPhotoNotFound):
		return errors.New("at least one of those photo uids does not exist; nothing was changed")
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// labelWriteError maps a label attach/detach failure onto an actionable message.
func labelWriteError(err error, what string) error {
	switch {
	case errors.Is(err, organize.ErrLabelNotFound):
		return errors.New("no label with that uid")
	case errors.Is(err, photos.ErrPhotoNotFound):
		return errors.New("no photo with that uid")
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}
