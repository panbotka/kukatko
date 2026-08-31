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
)

// ErrStackTooSmall indicates a grouping of fewer than two photos, which the API
// rejects with 400: a stack of one is just a photo.
var ErrStackTooSmall = errors.New("ctl: a stack needs at least two photos")

// minStackSize is the smallest group the API accepts.
const minStackSize = 2

// StackMember is one file of a stack as the API reports it in a photo's variants
// strip: enough to tell the RAW from the JPEG and to name the one that is shown.
type StackMember struct {
	UID        string `json:"uid"`
	FileName   string `json:"file_name"`
	MediaType  string `json:"media_type"`
	FileMime   string `json:"file_mime"`
	FileWidth  int    `json:"file_width"`
	FileHeight int    `json:"file_height"`
	FileSize   int64  `json:"file_size"`
	IsPrimary  bool   `json:"is_primary"`
}

// Stack is what a stacking command reads back out of the photo detail the API
// answers with: which photo the command acted on, which stack it now belongs to,
// and every file in that stack.
//
// A stack **groups**, it never merges: each member stays its own photo with its
// own uid, its own file and its own metadata, and only the primary is shown in a
// listing. Ungrouping therefore loses nothing — it makes the members visible
// side by side again.
type Stack struct {
	PhotoUID string        `json:"uid"`
	StackUID *string       `json:"stack_uid,omitempty"`
	Members  []StackMember `json:"stack_members"`
}

// Stacked reports whether the photo is in a stack at all. An unstacked photo has
// neither a stack uid nor a variants strip.
func (s Stack) Stacked() bool {
	return s.StackUID != nil && *s.StackUID != ""
}

// stackSelectionBody is the body of POST /photos/stack: the photos to group.
type stackSelectionBody struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// StackPhotos groups the given photos into one stack — the RAW and the JPEG of
// one shot, an edit beside its original — and returns the resulting stack's
// primary photo detail as raw JSON. It needs the editor or admin role.
//
// Nothing is merged and nothing is deleted: every photo keeps its uid, its file
// and its metadata, and the group only decides which one a listing shows. Fewer
// than two photos is refused client-side; a photo that does not exist yields a
// *StatusError with status 404, and an instance with stacking switched off one
// with status 503.
func (c *Client) StackPhotos(ctx context.Context, photoUIDs []string) (json.RawMessage, error) {
	uids, err := NormalizeUIDs(photoUIDs)
	if err != nil {
		return nil, err
	}
	if len(uids) < minStackSize {
		return nil, fmt.Errorf("%w: got %d", ErrStackTooSmall, len(uids))
	}
	return c.send(ctx, http.MethodPost, "/photos/stack", stackSelectionBody{PhotoUIDs: uids})
}

// SetStackPrimary makes one photo the variant its stack is shown as, and returns
// its refreshed detail as raw JSON. A photo that is not in a stack yields a
// *StatusError with status 409. It needs the editor or admin role.
func (c *Client) SetStackPrimary(ctx context.Context, photoUID string) (json.RawMessage, error) {
	return c.stackMutation(ctx, photoUID, "/stack/primary")
}

// UnstackPhoto takes one photo out of its stack, leaving the rest of the group
// intact, and returns the now standalone photo's detail as raw JSON. A photo
// that is not in a stack yields a *StatusError with status 409.
func (c *Client) UnstackPhoto(ctx context.Context, photoUID string) (json.RawMessage, error) {
	return c.stackMutation(ctx, photoUID, "/unstack")
}

// UnstackAll dissolves the whole stack the photo belongs to, so every member
// becomes a standalone photo again, and returns the named photo's refreshed
// detail as raw JSON.
func (c *Client) UnstackAll(ctx context.Context, photoUID string) (json.RawMessage, error) {
	return c.stackMutation(ctx, photoUID, "/unstack-all")
}

// stackMutation drives the three per-photo stacking endpoints, which share a
// verb, take no body and all answer with the photo's refreshed detail.
func (c *Client) stackMutation(ctx context.Context, photoUID, suffix string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/photos/"+url.PathEscape(photoUID)+suffix, nil)
}

// DecodeStack decodes the stack view out of the photo detail a stacking endpoint
// answers with. The rest of the detail is dropped: a stacking command reports
// what the group now is, and `-o json` still carries the whole response.
func DecodeStack(raw json.RawMessage) (Stack, error) {
	var stack Stack
	if err := json.Unmarshal(raw, &stack); err != nil {
		return Stack{}, fmt.Errorf("decoding the stack: %w", err)
	}
	return stack, nil
}

// WriteStack renders a stack as the group's members, primary marked, under a line
// naming the stack. An unstacked photo prints one line saying so rather than an
// empty table — after `unstack` that is the whole result.
func WriteStack(w io.Writer, stack Stack) error {
	if !stack.Stacked() {
		return writeLine(w, "photo "+stack.PhotoUID+" is not stacked")
	}
	rows := make([][]string, 0, len(stack.Members))
	for _, member := range stack.Members {
		rows = append(rows, []string{
			member.UID,
			elide(dash(member.FileName), fileWidth),
			dash(member.MediaType),
			formatDimensions(member.FileWidth, member.FileHeight),
			formatSize(member.FileSize),
			strconv.FormatBool(member.IsPrimary),
		})
	}
	if err := writeTable(w,
		[]string{"UID", "FILE", "MEDIA", "DIMENSIONS", "SIZE", "PRIMARY"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\nstack "+*stack.StackUID+" groups "+
		strconv.Itoa(len(stack.Members))+" "+plural(len(stack.Members), "photo", "photos")+
		"; each keeps its own file and metadata")
}
