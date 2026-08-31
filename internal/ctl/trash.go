package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors of the trash commands, checked before anything irreversible is
// asked of the server.
var (
	// ErrInvalidRetentionDays indicates a negative --days on the age-bounded
	// purge, which the API rejects with 400.
	ErrInvalidRetentionDays = errors.New("ctl: days must not be negative")
)

const (
	// trashPageSize is how many archived photos one listing request asks for. It
	// is the server's own cap, so the trash is read in as few round trips as the
	// API allows.
	trashPageSize = 500
	// maxTrashItems bounds how much of the trash a single command will read. A
	// dry run has to name what would be lost, so the listing is not a sample by
	// choice; this only stops a runaway library from being paged forever, and the
	// renderer says out loud when it stopped short.
	maxTrashItems = 10000
	// hoursPerDay converts a retention window in days into a duration.
	hoursPerDay = 24
)

// TrashItem is one archived photo: what it is, when it went to the trash, and
// when retention will destroy it.
type TrashItem struct {
	UID        string     `json:"uid"`
	FileName   string     `json:"file_name"`
	Title      string     `json:"title,omitempty"`
	FileSize   int64      `json:"file_size,omitempty"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	// PurgeAt is when the scheduled retention will permanently delete this photo,
	// nil when retention is switched off (retention_days <= 0) or the photo
	// carries no archived_at at all — neither is a date anybody may be told.
	PurgeAt *time.Time `json:"purge_at,omitempty"`
}

// Trash is what the trash holds right now: the retention window, how many photos
// are in it, and the photos themselves, oldest-archived first — the order in
// which retention will take them.
type Trash struct {
	// RetentionDays is the configured window; 0 or less means the scheduled purge
	// is disabled and nothing in the trash goes away on its own.
	RetentionDays int `json:"retention_days"`
	// Total is how many archived photos the library holds, as the server counted
	// them, whether or not Photos lists them all.
	Total int `json:"total"`
	// Photos are the archived photos that were read, oldest-archived first.
	Photos []TrashItem `json:"photos"`
	// Truncated reports that reading stopped at maxTrashItems, so Photos is not
	// the whole trash. A dry run says so rather than letting a bounded list read
	// like a complete one.
	Truncated bool `json:"truncated"`
}

// RetentionEnabled reports whether anything in the trash is on a clock.
func (t Trash) RetentionEnabled() bool {
	return t.RetentionDays > 0
}

// ArchivedBefore returns the trash narrowed to the photos archived at or before
// cutoff — what an age-bounded purge would destroy. Total becomes the number of
// matches, since the server counts the whole trash and not this subset.
func (t Trash) ArchivedBefore(cutoff time.Time) Trash {
	narrowed := t
	matches := make([]TrashItem, 0, len(t.Photos))
	for _, item := range t.Photos {
		if item.ArchivedAt != nil && !item.ArchivedAt.After(cutoff) {
			matches = append(matches, item)
		}
	}
	narrowed.Photos = matches
	narrowed.Total = len(matches)
	return narrowed
}

// trashInfoBody decodes GET /trash/info, which reports only the retention window.
type trashInfoBody struct {
	RetentionDays int `json:"retention_days"`
}

// PurgeResult is what a batch purge destroyed: how many photos were permanently
// removed, and how many failed and were left in the trash for a later attempt.
type PurgeResult struct {
	Purged int `json:"purged"`
	Failed int `json:"failed"`
}

// FetchTrash reads what is in the trash: the retention window from
// GET /trash/info, and the archived photos themselves through the ordinary
// listing (GET /photos?archived=only), paged until the trash is exhausted or
// maxTrashItems have been read.
//
// The result is sorted by archived_at, oldest first — the order retention takes
// them in, and the order a purge destroys them in. The sort is done here rather
// than asked of the server because the listing has no archived_at sort key, and
// a "what goes next" answer computed over one arbitrary page would be wrong
// rather than merely partial.
func (c *Client) FetchTrash(ctx context.Context) (Trash, error) {
	retention, err := c.retentionDays(ctx)
	if err != nil {
		return Trash{}, err
	}
	items, total, truncated, err := c.readTrashPages(ctx)
	if err != nil {
		return Trash{}, err
	}
	slices.SortStableFunc(items, byArchivedAt)
	stampPurgeDates(items, retention)
	return Trash{RetentionDays: retention, Total: total, Photos: items, Truncated: truncated}, nil
}

// retentionDays reads the configured retention window. Any authenticated role
// may ask, so this works with the same token that later has to be an admin's to
// purge anything.
func (c *Client) retentionDays(ctx context.Context) (int, error) {
	raw, err := c.get(ctx, "/trash/info", nil)
	if err != nil {
		return 0, fmt.Errorf("reading the trash retention: %w", err)
	}
	var info trashInfoBody
	if err := json.Unmarshal(raw, &info); err != nil {
		return 0, fmt.Errorf("decoding the trash retention: %w", err)
	}
	return info.RetentionDays, nil
}

// readTrashPages pages through the archived photos, returning what it read, the
// server's own total, and whether it stopped at maxTrashItems.
func (c *Client) readTrashPages(ctx context.Context) (items []TrashItem, total int, truncated bool, err error) {
	for offset := 0; ; {
		raw, err := c.ListPhotos(ctx, ListOptions{Archived: "only", Limit: trashPageSize, Offset: offset})
		if err != nil {
			return nil, 0, false, fmt.Errorf("listing the trash: %w", err)
		}
		page, err := DecodePhotoPage(raw)
		if err != nil {
			return nil, 0, false, err
		}
		total = page.Total
		for _, photo := range page.Photos {
			items = append(items, trashItem(photo))
		}
		if len(items) >= maxTrashItems {
			return items[:maxTrashItems], total, true, nil
		}
		if page.NextOffset == nil || len(page.Photos) == 0 {
			return items, total, false, nil
		}
		offset = *page.NextOffset
	}
}

// trashItem projects a listed photo onto the fields a trash listing needs.
func trashItem(photo Photo) TrashItem {
	return TrashItem{
		UID:        photo.UID,
		FileName:   photo.FileName,
		Title:      photo.Title,
		FileSize:   photo.FileSize,
		ArchivedAt: photo.ArchivedAt,
	}
}

// byArchivedAt orders archived photos oldest first. A photo with no archived_at
// sorts last: it is not on the retention clock at all, and putting it first
// would name it as the next to go.
func byArchivedAt(a, b TrashItem) int {
	switch {
	case a.ArchivedAt == nil && b.ArchivedAt == nil:
		return 0
	case a.ArchivedAt == nil:
		return 1
	case b.ArchivedAt == nil:
		return -1
	default:
		return a.ArchivedAt.Compare(*b.ArchivedAt)
	}
}

// stampPurgeDates fills in when retention will destroy each photo, leaving the
// date unset when retention is disabled — then nothing goes away on its own and
// a countdown would be a promise the instance does not make.
func stampPurgeDates(items []TrashItem, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	window := time.Duration(retentionDays) * hoursPerDay * time.Hour
	for i := range items {
		if items[i].ArchivedAt == nil {
			continue
		}
		due := items[i].ArchivedAt.Add(window)
		items[i].PurgeAt = &due
	}
}

// PurgePhoto permanently deletes one archived photo. **This cannot be undone**:
// the row, the original, the thumbnails and the remote backup object all go.
//
// It carries the API's own confirm=true guard, which the server requires and
// which is not a substitute for the CLI's --yes: the flag is what a human or an
// agent answers, this is only what the endpoint demands of any client.
// The endpoint is admin-only, answers 404 for a missing photo and 409 for one
// that is not archived — a photo has to be in the trash before it can be
// destroyed, so nothing live is one command away from gone.
func (c *Client) PurgePhoto(ctx context.Context, uid string) error {
	if err := requireUID("photo", uid); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/photos/"+url.PathEscape(uid)+"/purge", confirmQuery(), nil)
	return err
}

// EmptyTrash permanently deletes every archived photo and returns the raw
// {purged,failed} answer. **This cannot be undone.** Admin only.
func (c *Client) EmptyTrash(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, "/trash/empty", confirmQuery(), nil)
}

// PurgeOlderThan permanently deletes every photo archived longer ago than days
// and returns the raw {purged,failed} answer. days == 0 is the whole trash, the
// same as EmptyTrash; a negative value is refused here rather than at the server.
// **This cannot be undone.** Admin only.
func (c *Client) PurgeOlderThan(ctx context.Context, days int) (json.RawMessage, error) {
	if days < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidRetentionDays, days)
	}
	q := confirmQuery()
	q.Set("days", strconv.Itoa(days))
	return c.do(ctx, http.MethodPost, "/trash/purge-older", q, nil)
}

// confirmQuery renders the confirm=true parameter every purge endpoint requires.
func confirmQuery() url.Values {
	return url.Values{"confirm": []string{"true"}}
}

// PurgeCutoff returns the instant a purge of everything older than days would
// cut at, relative to now. It is the same arithmetic the server does, computed
// here so a dry run can name the photos before anything is destroyed.
func PurgeCutoff(now time.Time, days int) time.Time {
	return now.Add(-time.Duration(days) * hoursPerDay * time.Hour)
}

// DecodePurgeResult decodes the {purged,failed} answer of a batch purge.
func DecodePurgeResult(raw json.RawMessage) (PurgeResult, error) {
	var result PurgeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return PurgeResult{}, fmt.Errorf("decoding the purge result: %w", err)
	}
	return result, nil
}

// WritePurgeResult renders what a batch purge destroyed. A failure is not hidden
// in a count nobody reads: the line says outright that the failed photos are
// still in the trash, because the operator who just emptied it will otherwise
// assume it is empty.
func WritePurgeResult(w io.Writer, out Output, result PurgeResult) error {
	if out.Format == FormatTable {
		line := strconv.Itoa(result.Purged) + " photos permanently deleted"
		if result.Failed > 0 {
			line += "; " + strconv.Itoa(result.Failed) +
				" could not be deleted and are still in the trash"
		}
		return writeLine(w, line)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encoding the purge result: %w", err)
	}
	if out.Format == FormatLLM {
		return WriteLLM(w, encoded, out.Fields)
	}
	return WriteJSON(w, encoded)
}

// TrashView is a trash listing under the heading that says what it is: the trash
// as it stands, or exactly what a purge would destroy. The heading is part of
// the value because the same rows mean something very different under each.
type TrashView struct {
	// Heading is the sentence the listing is printed under.
	Heading string `json:"heading"`
	// DryRun marks a listing that describes a purge nobody has run — which is the
	// one thing a reader of a destruction list must not have to infer.
	DryRun bool `json:"dry_run"`
	Trash
}

// WriteTrash renders a trash listing: one row per photo, oldest-archived first,
// with when retention will take it, followed by a summary. In the machine
// formats the whole view is emitted as JSON — this is a value ctl composes out
// of two endpoints, so there are no server bytes to pass through.
func WriteTrash(w io.Writer, out Output, view TrashView) error {
	if out.Format != FormatTable {
		encoded, err := json.Marshal(view)
		if err != nil {
			return fmt.Errorf("encoding the trash listing: %w", err)
		}
		if out.Format == FormatLLM {
			return WriteLLM(w, encoded, out.Fields)
		}
		return WriteJSON(w, encoded)
	}
	if err := writeLine(w, view.Heading); err != nil {
		return err
	}
	if len(view.Photos) == 0 {
		return writeLine(w, emptyTrashLine(view))
	}
	if err := writeTrashTable(w, view.Photos); err != nil {
		return err
	}
	return writeLine(w, "\n"+trashSummary(view))
}

// emptyTrashLine says what an empty listing means, which is not the same thing
// under every heading: under a rehearsal it means nothing matched the purge, not
// that the trash has nothing in it.
func emptyTrashLine(view TrashView) string {
	if view.DryRun {
		return "no photos match: nothing would be deleted"
	}
	return "the trash is empty"
}

// writeTrashTable renders the archived photos themselves.
func writeTrashTable(w io.Writer, items []TrashItem) error {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.UID,
			elide(dash(item.FileName), fileWidth),
			elide(dash(item.Title), titleWidth),
			formatSize(item.FileSize),
			formatTime(item.ArchivedAt),
			formatTime(item.PurgeAt),
		})
	}
	return writeTable(w, []string{"UID", "FILE", "TITLE", "SIZE", "ARCHIVED", "PURGE AT"}, rows)
}

// trashSummary builds the footer of a trash listing: how many photos it names,
// how much they weigh, what retention does to them, and whether the list
// stopped short of the whole trash.
func trashSummary(view TrashView) string {
	parts := []string{
		strconv.Itoa(len(view.Photos)) + " of " + strconv.Itoa(view.Total) + " photos",
		formatSize(trashBytes(view.Photos)),
	}
	if view.RetentionEnabled() {
		parts = append(parts, "retention "+strconv.Itoa(view.RetentionDays)+" days")
	} else {
		parts = append(parts, "retention off: nothing is purged automatically")
	}
	if view.Truncated {
		parts = append(parts, "listing stopped at "+strconv.Itoa(maxTrashItems)+" photos")
	}
	if view.DryRun {
		parts = append(parts, "dry run: nothing was deleted")
	}
	return strings.Join(parts, " · ")
}

// trashBytes adds up what the listed photos take on disk, so the summary can say
// what emptying the trash actually reclaims.
func trashBytes(items []TrashItem) int64 {
	var total int64
	for _, item := range items {
		total += item.FileSize
	}
	return total
}
