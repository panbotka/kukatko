package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrSameDuplicatePhoto indicates a duplicate pair naming one photo twice, which
// the API rejects with 400.
var ErrSameDuplicatePhoto = errors.New("ctl: a duplicate pair needs two different photos")

// DuplicateMember is one photo of a duplicate group, with how far it is from the
// group's keeper by each of the two detectors.
type DuplicateMember struct {
	UID               string     `json:"uid"`
	Title             string     `json:"title"`
	FileName          string     `json:"file_name"`
	FileWidth         int        `json:"file_width"`
	FileHeight        int        `json:"file_height"`
	FileSize          int64      `json:"file_size"`
	MediaType         string     `json:"media_type"`
	TakenAt           *time.Time `json:"taken_at,omitempty"`
	IsKeeper          bool       `json:"is_keeper"`
	PhashDistance     *int       `json:"phash_distance,omitempty"`
	EmbeddingDistance *float64   `json:"embedding_distance,omitempty"`
}

// DuplicateGroup is a set of photos detected as likely the same shot, with the
// copy the server suggests keeping. Confirmed means a human has already agreed
// about at least one of its pairs; nothing is merged either way.
type DuplicateGroup struct {
	ID        string            `json:"id"`
	Reason    string            `json:"reason"`
	KeeperUID string            `json:"keeper_uid"`
	Confirmed bool              `json:"confirmed"`
	Members   []DuplicateMember `json:"members"`
}

// DuplicatePage is one page of duplicate groups plus its paging cursor.
type DuplicatePage struct {
	Groups     []DuplicateGroup `json:"groups"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
	NextOffset *int             `json:"next_offset"`
}

// duplicatePairBody is the body every duplicate-feedback endpoint takes: the two
// photos. The pair is unordered — the server normalises it — so which uid goes in
// which field does not matter, and confirming the same pair twice, either way
// round, is a no-op.
type duplicatePairBody struct {
	PhotoUID string `json:"photo_uid"`
	OtherUID string `json:"other_uid"`
}

// ListDuplicates fetches GET /duplicates and returns the raw JSON body: a page of
// likely-duplicate groups, the ones somebody has already confirmed first. It
// needs the editor or admin role and changes nothing — the scan is read-only.
//
// An instance with duplicate detection switched off answers 503. Decode the page
// with DecodeDuplicates.
func (c *Client) ListDuplicates(ctx context.Context, opts PageOptions) (json.RawMessage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	return c.get(ctx, "/duplicates", q)
}

// ConfirmDuplicate records that the two photos really are the same shot. It
// merges nothing and archives nothing: it is an opinion the duplicates page ranks
// on, and it is what tells the library a human has already looked. The endpoint
// answers 204 and the write is idempotent.
func (c *Client) ConfirmDuplicate(ctx context.Context, photoUID, otherUID string) error {
	return c.duplicateFeedback(ctx, http.MethodPost, "/duplicate-confirmations", photoUID, otherUID)
}

// UnconfirmDuplicate takes a confirmation back, dropping the pair to a machine
// guess again. It is idempotent.
func (c *Client) UnconfirmDuplicate(ctx context.Context, photoUID, otherUID string) error {
	return c.duplicateFeedback(ctx, http.MethodDelete, "/duplicate-confirmations", photoUID, otherUID)
}

// DismissDuplicate records that the two photos are NOT duplicates of each other,
// so the pair stops being offered for review. It is the exact opposite of
// ConfirmDuplicate — reaching for one meaning the other records the opposite of
// what you decided. It is idempotent.
func (c *Client) DismissDuplicate(ctx context.Context, photoUID, otherUID string) error {
	return c.duplicateFeedback(ctx, http.MethodPost, "/duplicate-dismissals", photoUID, otherUID)
}

// UndismissDuplicate takes a dismissal back, letting the pair be offered again.
// It is idempotent.
func (c *Client) UndismissDuplicate(ctx context.Context, photoUID, otherUID string) error {
	return c.duplicateFeedback(ctx, http.MethodDelete, "/duplicate-dismissals", photoUID, otherUID)
}

// duplicateFeedback drives the four duplicate-opinion endpoints, which share a
// body and differ only in their path and verb.
func (c *Client) duplicateFeedback(ctx context.Context, method, path, photoUID, otherUID string) error {
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	if err := requireUID("photo", otherUID); err != nil {
		return err
	}
	if strings.TrimSpace(photoUID) == strings.TrimSpace(otherUID) {
		return fmt.Errorf("%w: both are %s", ErrSameDuplicatePhoto, photoUID)
	}
	_, err := c.send(ctx, method, "/feedback"+path,
		duplicatePairBody{PhotoUID: photoUID, OtherUID: otherUID})
	return err
}

// DecodeDuplicates decodes one page of duplicate groups.
func DecodeDuplicates(raw json.RawMessage) (DuplicatePage, error) {
	var page DuplicatePage
	if err := json.Unmarshal(raw, &page); err != nil {
		return DuplicatePage{}, fmt.Errorf("decoding the duplicate groups: %w", err)
	}
	return page, nil
}

// WriteDuplicates renders a page of duplicate groups as one row per member, the
// keeper marked, followed by the paging summary. One row per group would hide
// the very uids the confirm and dismiss commands take.
func WriteDuplicates(w io.Writer, page DuplicatePage) error {
	if len(page.Groups) == 0 {
		return writeLine(w, "no duplicate groups found")
	}
	rows := make([][]string, 0, len(page.Groups))
	for _, group := range page.Groups {
		for _, member := range group.Members {
			rows = append(rows, []string{
				group.ID,
				member.UID,
				strconv.FormatBool(member.IsKeeper),
				elide(dash(member.FileName), fileWidth),
				formatSize(member.FileSize),
				formatDuplicateDistance(member),
				dash(group.Reason),
				strconv.FormatBool(group.Confirmed),
			})
		}
	}
	header := []string{"GROUP", "UID", "KEEPER", "FILE", "SIZE", "DISTANCE", "REASON", "CONFIRMED"}
	if err := writeTable(w, header, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+duplicateSummary(page))
}

// duplicateSummary builds the one-line footer describing how much of the scan
// this page covers.
func duplicateSummary(page DuplicatePage) string {
	parts := []string{
		strconv.Itoa(len(page.Groups)) + " of " + strconv.Itoa(page.Total) + " groups",
		"offset " + strconv.Itoa(page.Offset),
	}
	if page.NextOffset != nil {
		parts = append(parts, "next offset "+strconv.Itoa(*page.NextOffset))
	}
	return strings.Join(parts, " · ")
}

// formatDuplicateDistance renders how far a member is from its keeper, naming
// which detector measured it — a Hamming distance between perceptual hashes and
// a cosine distance between embeddings are not the same number.
func formatDuplicateDistance(member DuplicateMember) string {
	parts := make([]string, 0, 2)
	if member.PhashDistance != nil {
		parts = append(parts, "phash "+strconv.Itoa(*member.PhashDistance))
	}
	if member.EmbeddingDistance != nil {
		parts = append(parts, "cos "+formatDistance(*member.EmbeddingDistance))
	}
	return dash(strings.Join(parts, ", "))
}
