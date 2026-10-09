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

// ErrEmptyLinkCode indicates a restore-code call without a code, which could
// never match a link.
var ErrEmptyLinkCode = errors.New("ctl: the upload link code must not be empty")

// unknownLinkURL is what the tables print for a link created before the code
// was stored readably: its URL works, but only `restore-code` can show it again.
const unknownLinkURL = "unknown (restore-code)"

// UploadLink is one upload link as the management routes list it. Code and Path
// are present only for a link the caller manages (its creator or an admin) and
// whose code is known; a link created before codes were stored readably has
// neither until its code is restored.
type UploadLink struct {
	UID         string    `json:"uid"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	ExpiresAt   time.Time `json:"expires_at"`
	UploadCount int       `json:"upload_count"`
	CreatorName string    `json:"created_by_name"`
	Code        string    `json:"code"`
	Path        string    `json:"path"`
}

// ListUploadLinks fetches GET /upload-links and returns the raw JSON body: the
// caller's own links (an admin's: everybody's), newest first, each with its
// code and path when it is known. Decode it with DecodeUploadLinks.
func (c *Client) ListUploadLinks(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/upload-links", nil)
}

// RestoreUploadLinkCode makes a link's original code readable again — the
// server stores it only when it hashes to the link's stored hash, so the URL
// never changes. code may be the bare code or the whole /u/<code> URL. A wrong
// code is the server's 422 with nothing changed.
func (c *Client) RestoreUploadLinkCode(ctx context.Context, uid, code string) (json.RawMessage, error) {
	if err := requireUID("upload link", uid); err != nil {
		return nil, err
	}
	code = ParseUploadLinkCode(code)
	if code == "" {
		return nil, ErrEmptyLinkCode
	}
	return c.send(ctx, http.MethodPost, uploadLinkPath(uid)+"/restore-code", map[string]string{"code": code})
}

// NewUploadLinkCode replaces a link's code with a fresh one and returns the
// updated link as raw JSON. The old URL stops working at once.
func (c *Client) NewUploadLinkCode(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("upload link", uid); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, uploadLinkPath(uid)+"/new-code", nil)
}

// ParseUploadLinkCode reads a code out of what somebody pasted: the bare code,
// or a whole link whose path holds /u/<code> (query, fragment and a trailing
// slash dropped).
func ParseUploadLinkCode(input string) string {
	input = strings.TrimSpace(input)
	_, after, found := strings.Cut(input, "/u/")
	if !found {
		return input
	}
	after, _, _ = strings.Cut(after, "?")
	after, _, _ = strings.Cut(after, "#")
	return strings.TrimSuffix(after, "/")
}

// uploadLinkPath renders the path of one upload link's management routes.
func uploadLinkPath(uid string) string {
	return "/upload-links/" + url.PathEscape(uid)
}

// DecodeUploadLinks decodes the {"links": […]} envelope of GET /upload-links.
func DecodeUploadLinks(raw json.RawMessage) ([]UploadLink, error) {
	var payload struct {
		Links []UploadLink `json:"links"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the upload link list: %w", err)
	}
	return payload.Links, nil
}

// DecodeUploadLink decodes the {"link": …} envelope the code routes answer.
func DecodeUploadLink(raw json.RawMessage) (UploadLink, error) {
	var payload struct {
		Link UploadLink `json:"link"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return UploadLink{}, fmt.Errorf("decoding the upload link: %w", err)
	}
	return payload.Link, nil
}

// UploadLinkURL is the absolute address of link on server, or a pointer to
// restore-code when its code is unknown.
func UploadLinkURL(server string, link UploadLink) string {
	if link.Path == "" {
		return unknownLinkURL
	}
	return strings.TrimSuffix(server, "/") + link.Path
}

// WriteUploadLinks renders the links as a compact table whose URL column is
// the absolute address on server, ready to paste into a chat.
func WriteUploadLinks(w io.Writer, server string, links []UploadLink) error {
	if len(links) == 0 {
		return writeLine(w, "no upload links found")
	}
	rows := make([][]string, 0, len(links))
	for _, link := range links {
		rows = append(rows, []string{
			link.UID,
			elide(dash(link.Title), nameWidth),
			link.State,
			formatStamp(link.ExpiresAt),
			strconv.Itoa(link.UploadCount),
			UploadLinkURL(server, link),
		})
	}
	return writeTable(w, []string{"UID", "TITLE", "STATE", "EXPIRES", "UPLOADS", "URL"}, rows)
}

// WriteUploadLink renders one link as an aligned key/value table.
func WriteUploadLink(w io.Writer, server string, link UploadLink) error {
	return writeKeyValues(w, [][2]string{
		{"UID", link.UID},
		{"TITLE", dash(link.Title)},
		{"STATE", link.State},
		{"EXPIRES", formatStamp(link.ExpiresAt)},
		{"UPLOADS", strconv.Itoa(link.UploadCount)},
		{"URL", UploadLinkURL(server, link)},
	})
}
