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

// Sentinel errors for the comment inputs, checked client-side so an obvious
// mistake costs no round trip.
var (
	// ErrEmptyComment indicates a blank comment body, which the API rejects with
	// 400.
	ErrEmptyComment = errors.New("ctl: a comment must not be empty")
	// ErrCommentTooLong indicates a body over the API's length cap.
	ErrCommentTooLong = errors.New("ctl: a comment is at most " +
		strconv.Itoa(MaxCommentLen) + " characters")
)

// MaxCommentLen mirrors comments.MaxBodyLen, the API's own cap. It is duplicated
// rather than imported so `ctl` stays a client of the HTTP surface and links
// none of the server's domain packages; the server enforces it either way.
const MaxCommentLen = 2000

// Comment is one entry of a photo's thread. A thread often holds the only record
// of who is on a photo, where it was taken and when — which is exactly what an
// agent trying to date a photo needs to read.
type Comment struct {
	UID        string    `json:"uid"`
	PhotoUID   string    `json:"photo_uid"`
	AuthorUID  string    `json:"author_uid"`
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	// EditedAt is nil until the author first rewrote the comment.
	EditedAt *time.Time `json:"edited_at,omitempty"`
}

// commentBody is the body of POST /photos/{uid}/comments.
type commentBody struct {
	Body string `json:"body"`
}

// ListComments fetches GET /photos/{uid}/comments and returns the raw JSON body:
// the photo's live comments, oldest first, each with its author's name resolved.
// Every signed-in role may read a thread. A missing photo answers with an empty
// thread rather than a 404 — the endpoint reads comments, not photos. Decode it
// with DecodeComments.
func (c *Client) ListComments(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.get(ctx, commentsPath(photoUID), nil)
}

// AddComment appends a comment to a photo's thread and returns the created
// comment as raw JSON.
//
// The author is the token's owner, always: the API takes it from the
// authenticated principal and the audit trail records it there too. So an agent
// must comment under its **own** account — writing through a person's token
// would put words in that person's mouth, in a thread whose whole value is that
// it says who remembered what. This is why the MCP server exposes no comment
// tool at all; the CLI may have one because its account is a distinct one.
//
// Every role may write, viewers included: a comment is participation, not
// curation. A blank or over-long body is refused client-side, a missing photo
// yields a *StatusError with status 404, and the route is rate limited per user,
// which surfaces as status 429.
func (c *Client) AddComment(ctx context.Context, photoUID, body string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	switch {
	case body == "":
		return nil, ErrEmptyComment
	case len([]rune(body)) > MaxCommentLen:
		return nil, fmt.Errorf("%w: got %d", ErrCommentTooLong, len([]rune(body)))
	}
	return c.send(ctx, http.MethodPost, commentsPath(photoUID), commentBody{Body: body})
}

// commentsPath renders the thread path for one photo.
func commentsPath(photoUID string) string {
	return "/photos/" + url.PathEscape(photoUID) + "/comments"
}

// DecodeComments decodes the bare {"comments": […]} envelope.
func DecodeComments(raw json.RawMessage) ([]Comment, error) {
	var payload struct {
		Comments []Comment `json:"comments"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the comment thread: %w", err)
	}
	return payload.Comments, nil
}

// DecodeComment decodes one comment, as returned by the create endpoint.
func DecodeComment(raw json.RawMessage) (Comment, error) {
	var comment Comment
	if err := json.Unmarshal(raw, &comment); err != nil {
		return Comment{}, fmt.Errorf("decoding the comment: %w", err)
	}
	return comment, nil
}

// WriteComments renders a thread as prose rather than a table: one header line
// per comment and its body indented below, wrapped at nothing and elided at
// nothing.
//
// Every other listing in this CLI is a table because its rows are fields. A
// comment is a paragraph somebody wrote, and it is the only record of what they
// knew — cutting it to a column width would throw away the answer the reader
// came for.
func WriteComments(w io.Writer, comments []Comment) error {
	if len(comments) == 0 {
		return writeLine(w, "no comments on this photo")
	}
	for i, comment := range comments {
		if i > 0 {
			if err := writeLine(w, ""); err != nil {
				return err
			}
		}
		if err := WriteComment(w, comment); err != nil {
			return err
		}
	}
	return nil
}

// WriteComment renders one comment — its header line and its indented body — as
// the create command echoes back what it just wrote.
func WriteComment(w io.Writer, comment Comment) error {
	header := formatStamp(comment.CreatedAt) + "  " +
		NamedUID(comment.AuthorName, comment.AuthorUID) + "  " + comment.UID
	if comment.EditedAt != nil {
		header += " (edited " + formatTime(comment.EditedAt) + ")"
	}
	if err := writeLine(w, header); err != nil {
		return err
	}
	for line := range strings.SplitSeq(comment.Body, "\n") {
		if err := writeLine(w, "  "+line); err != nil {
			return err
		}
	}
	return nil
}
