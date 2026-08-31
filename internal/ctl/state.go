package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The four reversible states a photo can be moved through from the CLI. They are
// the endpoint's own path segments, so the command name, the flag and the audit
// action all read the same.
const (
	// StateArchive moves a photo to the trash (a soft delete: archived_at is set).
	StateArchive = "archive"
	// StateUnarchive brings an archived photo back out of the trash.
	StateUnarchive = "unarchive"
	// StateHide keeps a photo out of the library grid without deleting anything.
	StateHide = "hide"
	// StateUnhide brings a hidden photo back into the library.
	StateUnhide = "unhide"
)

// PhotoState is the part of a photo the four state endpoints exist to change.
// They answer with the whole refreshed photo, but only these fields are the
// result: everything else is what the photo already was.
type PhotoState struct {
	UID        string     `json:"uid"`
	FileName   string     `json:"file_name"`
	Title      string     `json:"title"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Hidden     bool       `json:"hidden_from_library"`
}

// Archived reports whether the photo is in the trash.
func (s PhotoState) Archived() bool {
	return s.ArchivedAt != nil && !s.ArchivedAt.IsZero()
}

// SetPhotoState moves one photo through a reversible state change — archive,
// unarchive, hide or unhide — and returns the server's refreshed photo as raw
// JSON, so `-o json` prints its own bytes. Decode it with DecodePhotoState.
//
// All four are reversible by the command that undoes them, which is why none of
// them is gated: nothing is deleted, no original is touched, and archiving is
// the reversible step that only later, and only through the trash, becomes
// permanent.
func (c *Client) SetPhotoState(ctx context.Context, uid, state string) (json.RawMessage, error) {
	if err := requireUID("photo", uid); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/photos/"+url.PathEscape(uid)+"/"+state, nil)
}

// DecodePhotoState decodes the refreshed photo a state change answers with.
func DecodePhotoState(raw json.RawMessage) (PhotoState, error) {
	var state PhotoState
	if err := json.Unmarshal(raw, &state); err != nil {
		return PhotoState{}, fmt.Errorf("decoding the photo state: %w", err)
	}
	return state, nil
}

// WritePhotoState renders a photo's lifecycle state as one line: which photo it
// is and, in full, where it now stands — both flags, not just the one the
// command changed. Archiving a photo that is also hidden leaves it in two
// states, and a line reporting only the change would not say so.
func WritePhotoState(w io.Writer, state PhotoState) error {
	return writeLine(w, NamedUID(photoLabel(state), state.UID)+": "+describeState(state))
}

// photoLabel names a photo for a human: its title when it has one, otherwise the
// file it came from.
func photoLabel(state PhotoState) string {
	if state.Title != "" {
		return state.Title
	}
	return state.FileName
}

// describeState spells out both lifecycle flags in prose.
func describeState(state PhotoState) string {
	where := "in the library"
	if state.Archived() {
		where = "in the trash since " + formatTime(state.ArchivedAt)
	}
	if state.Hidden {
		where += ", hidden from the library grid"
	}
	return where
}

// DescribePurgeTarget says what destroying one photo would cost and whether it
// can be destroyed at all: a photo that is still in the library cannot be
// purged, and a rehearsal that did not say so would let the operator confirm a
// command the server is going to refuse.
func DescribePurgeTarget(detail PhotoDetail) string {
	where := "still in the library — purging is refused until it is archived"
	if detail.ArchivedAt != nil && !detail.ArchivedAt.IsZero() {
		where = "in the trash since " + formatTime(detail.ArchivedAt)
	}
	return formatSize(detail.FileSize) + ", " + where
}
