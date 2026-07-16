package mcpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/bulk"
)

// bulkEditIn is the batch edit's argument set. It deliberately omits two of the
// bulk service's operations:
//
// Archive, because archiving is how a photo reaches the trash, and the trash is
// purged on a retention schedule — an agent that can archive can, with enough
// patience, delete somebody's photos. That is the one thing this server must not
// be able to do.
//
// Location, because a coordinate an agent invented is indistinguishable from one
// the camera measured once it is written, and the library already has a careful,
// marked path for inferring locations (internal/geoestimate).
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type bulkEditIn struct {
	PhotoUIDs    []string `json:"photo_uids" jsonschema:"The photos to change, by uid."`
	AddAlbums    []string `json:"add_albums,omitempty" jsonschema:"Album uids to add every photo to."`
	RemoveAlbums []string `json:"remove_albums,omitempty" jsonschema:"Album uids to remove every photo from."`
	AddLabels    []string `json:"add_labels,omitempty" jsonschema:"Label uids to attach to every photo."`
	RemoveLabels []string `json:"remove_labels,omitempty" jsonschema:"Label uids to detach from every photo."`
	Title        *string  `json:"title,omitempty" jsonschema:"Set every photo's title to this. Rarely what you want on a batch; an empty string clears it."`
	Description  *string  `json:"description,omitempty" jsonschema:"Set every photo's description to this. An empty string clears it."`
	Favorite     *bool    `json:"favorite,omitempty" jsonschema:"Mark or unmark every photo as the calling user's favourite."`
	Rating       *int     `json:"rating,omitempty" jsonschema:"Set the calling user's star rating on every photo, 0 to 5."`
	Flag         *string  `json:"flag,omitempty" jsonschema:"Set the calling user's flag on every photo: none, pick, reject or eye."`
}

// bulkFailure is one photo the batch could not change.
type bulkFailure struct {
	PhotoUID string `json:"photo_uid"`
	Error    string `json:"error"`
}

// bulkResult reports what the batch did. It returns the counts and only the
// failures, not a row per photo: an agent editing 200 photos does not need 200
// "ok" lines read back into its context, it needs to know what did not work.
type bulkResult struct {
	// Total is how many photos the batch was asked to change.
	Total int `json:"total"`
	// Updated is how many actually changed.
	Updated int `json:"updated"`
	// Skipped is how many already looked the way the request asked for.
	Skipped int `json:"skipped"`
	// Errored is how many failed; Failures says which and why.
	Errored  int           `json:"errored"`
	Failures []bulkFailure `json:"failures,omitempty"`
}

// handleSetPhotoRating records the calling user's opinion of one photo. It runs
// through the bulk service rather than the rating store directly, because that
// path already writes an audit row inside the mutation's transaction — an agent's
// opinion of a photo has to be as traceable as any other change it makes.
func (a *API) handleSetPhotoRating(
	ctx context.Context, _ *mcp.CallToolRequest, in setRatingIn,
) (*mcp.CallToolResult, okResult, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return nil, okResult{}, err
	}
	uid := firstUID(in.UID)
	if uid == "" {
		return nil, okResult{}, errors.New("uid is required")
	}
	ops := bulk.Operations{Favorite: in.Favorite, Rating: in.Rating, Flag: in.Flag}
	if ops.IsEmpty() {
		return nil, okResult{}, errors.New("pass at least one of favorite, rating or flag")
	}
	res, err := a.bulk.Apply(ctx, c.user.UID, []string{uid}, ops)
	if err != nil {
		return nil, okResult{}, bulkError(err, a.bulk.MaxBatch())
	}
	if res.Counts.Errored > 0 {
		return nil, okResult{}, errors.New(firstFailure(res))
	}
	return nil, okOf("updated the photo"), nil
}

// handleBulkEdit applies one set of changes to many photos in one transaction.
func (a *API) handleBulkEdit(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkEditIn,
) (*mcp.CallToolResult, bulkResult, error) {
	c, err := writerFromContext(ctx)
	if err != nil {
		return nil, bulkResult{}, err
	}
	uids := cleanUIDs(in.PhotoUIDs)
	if len(uids) == 0 {
		return nil, bulkResult{}, errors.New("photo_uids is required and must name at least one photo")
	}
	ops := bulk.Operations{
		AddAlbums:    cleanUIDs(in.AddAlbums),
		RemoveAlbums: cleanUIDs(in.RemoveAlbums),
		AddLabels:    cleanUIDs(in.AddLabels),
		RemoveLabels: cleanUIDs(in.RemoveLabels),
		Title:        in.Title,
		Description:  in.Description,
		Favorite:     in.Favorite,
		Rating:       in.Rating,
		Flag:         in.Flag,
	}
	if ops.IsEmpty() {
		return nil, bulkResult{}, errors.New("pass at least one change to apply")
	}
	res, err := a.bulk.Apply(ctx, c.user.UID, uids, ops)
	if err != nil {
		return nil, bulkResult{}, bulkError(err, a.bulk.MaxBatch())
	}
	return nil, toBulkResult(res), nil
}

// toBulkResult projects the service's per-photo results onto the compact report.
func toBulkResult(res bulk.Result) bulkResult {
	out := bulkResult{
		Total:   res.Counts.Total,
		Updated: res.Counts.Updated,
		Skipped: res.Counts.Skipped,
		Errored: res.Counts.Errored,
	}
	for _, r := range res.Results {
		if r.Error != "" {
			out.Failures = append(out.Failures, bulkFailure{PhotoUID: r.PhotoUID, Error: r.Error})
		}
	}
	return out
}

// firstFailure returns the first per-photo error of a single-photo batch, so the
// caller can report it as the call's own error.
func firstFailure(res bulk.Result) string {
	for _, r := range res.Results {
		if r.Error != "" {
			return r.Error
		}
	}
	return "the change could not be applied"
}

// firstUID trims a single uid argument.
func firstUID(uid string) string {
	got := cleanUIDs([]string{uid})
	if len(got) == 0 {
		return ""
	}
	return got[0]
}

// bulkError maps a batch failure onto a message the agent can act on. The batch
// limit is named rather than hinted at, because the agent's next move is to split
// its work and it needs the number to do that.
func bulkError(err error, maxBatch int) error {
	switch {
	case errors.Is(err, bulk.ErrBatchTooLarge):
		return fmt.Errorf("too many photos in one call: the limit is %d, so split the batch", maxBatch)
	case errors.Is(err, bulk.ErrAlbumNotFound):
		return errors.New("one of those album uids does not exist; nothing was changed")
	case errors.Is(err, bulk.ErrLabelNotFound):
		return errors.New("one of those label uids does not exist; nothing was changed")
	case errors.Is(err, bulk.ErrNoPhotos), errors.Is(err, bulk.ErrNoOperations):
		return err
	default:
		return fmt.Errorf("applying the batch: %w", err)
	}
}
