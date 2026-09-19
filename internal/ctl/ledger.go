package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ledgerPageSize is how many rows one ledger request asks for: the server's
// own page cap, so a group of a thousand is two round trips rather than ten.
const ledgerPageSize = 500

// ledgerMaxPhotos mirrors the server's cap on a task's group (phototask
// .MaxPhotos): a ledger never reads past it, so a server that somehow answers
// with more cannot keep the client paging forever.
const ledgerMaxPhotos = 1000

// commentWidth bounds the comment excerpt in a ledger row. The first line of
// the agent's note is the summary by convention; the whole thread is one
// `photos comments` away.
const commentWidth = 48

// LastComment is the newest live comment on a photograph, as a task-scoped
// listing carries it: who said what, and when.
type LastComment struct {
	UID        string    `json:"uid"`
	Body       string    `json:"body"`
	AuthorUID  string    `json:"author_uid"`
	AuthorName string    `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// TaskLedger is a task's whole frozen group with, per photograph, what a
// reviewer reads on one line: the date as stated, where it came from, and the
// last word in its thread. Total is the server's count of the group; Photos
// holds every row up to ledgerMaxPhotos.
type TaskLedger struct {
	TaskUID string  `json:"task_uid"`
	Total   int     `json:"total"`
	Photos  []Photo `json:"photos"`
}

// rawPhotoPage is a listing page with its rows left undecoded, so the ledger
// can be reassembled from the server's own bytes for `-o json` and `-o llm`.
type rawPhotoPage struct {
	Photos     []json.RawMessage `json:"photos"`
	Total      int               `json:"total"`
	NextOffset *int              `json:"next_offset"`
}

// rawLedger is the JSON envelope TaskLedger is served as by this client: the
// task's uid, the server's total and the rows verbatim.
type rawLedger struct {
	TaskUID string            `json:"task_uid"`
	Total   int               `json:"total"`
	Photos  []json.RawMessage `json:"photos"`
}

// TaskLedger reads a task's whole group through GET /photos?task={uid}, page by
// page, and returns it as one envelope: {task_uid,total,photos}. The rows are
// the server's own bytes, each carrying `last_comment`, so `-o json` prints
// what the API said and `-o llm` slims it as usual. The read stops at
// ledgerMaxPhotos.
func (c *Client) TaskLedger(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	ledger := rawLedger{TaskUID: uid, Photos: []json.RawMessage{}}
	for offset := 0; ; {
		raw, err := c.ListPhotos(ctx, ListOptions{Task: uid, Limit: ledgerPageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("reading the group of task %s: %w", uid, err)
		}
		var page rawPhotoPage
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decoding the group of task %s: %w", uid, err)
		}
		ledger.Total = page.Total
		ledger.Photos = append(ledger.Photos, page.Photos...)
		if page.NextOffset == nil || len(page.Photos) == 0 || len(ledger.Photos) >= ledgerMaxPhotos {
			break
		}
		offset = *page.NextOffset
	}
	encoded, err := json.Marshal(ledger)
	if err != nil {
		return nil, fmt.Errorf("encoding the ledger of task %s: %w", uid, err)
	}
	return encoded, nil
}

// DecodeTaskLedger decodes the envelope TaskLedger returns.
func DecodeTaskLedger(raw json.RawMessage) (TaskLedger, error) {
	var ledger TaskLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return TaskLedger{}, fmt.Errorf("decoding task ledger: %w", err)
	}
	return ledger, nil
}

// WriteTaskLedger renders the ledger as a table — one row per photograph with
// the date rendered at its stated precision, its source, the first line of the
// last comment and who wrote it — and a summary that counts the rows still
// missing a comment, which is the check an agent runs before it moves a batch
// to review.
func WriteTaskLedger(w io.Writer, ledger TaskLedger) error {
	if len(ledger.Photos) == 0 {
		return writeLine(w, "no photos in the task")
	}
	rows := make([][]string, 0, len(ledger.Photos))
	commented := 0
	for _, photo := range ledger.Photos {
		by := "-"
		excerpt := "-"
		if photo.LastComment != nil {
			commented++
			by = dash(NamedUID(photo.LastComment.AuthorName, photo.LastComment.AuthorUID))
			excerpt = dash(commentExcerpt(photo.LastComment.Body, commentWidth))
		}
		rows = append(rows, []string{
			photo.UID,
			renderTaken(photo.TakenAt, photo.TakenAtPrecision, photo.TakenAtEstimated),
			dash(photo.TakenAtSource),
			excerpt,
			by,
		})
	}
	if err := writeTable(w, []string{"UID", "TAKEN", "SOURCE", "LAST COMMENT", "BY"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+ledgerSummary(ledger, commented))
}

// ledgerSummary is the line under the table: how many rows were read, how many
// carry a comment, and whether the group was cut at the cap.
func ledgerSummary(ledger TaskLedger, commented int) string {
	n := len(ledger.Photos)
	summary := fmt.Sprintf("%d %s · %d with a comment · %d without",
		n, plural(n, "photo", "photos"), commented, n-commented)
	if ledger.Total > n {
		summary += fmt.Sprintf(" · showing %d of %d", n, ledger.Total)
	}
	return summary
}

// renderTaken renders a capture date at the precision it was stated with:
// 1974-06-14 for a day, 1974-06 for a month, 1974 for a year and 1970s for a
// decade. An estimate is prefixed "~" so a guess never reads like a fact; no
// date is a dash. An unknown precision falls back to the full day, which is
// what the value means unless something says otherwise.
func renderTaken(takenAt *time.Time, precision string, estimated bool) string {
	if takenAt == nil {
		return "-"
	}
	t := takenAt.UTC()
	var value string
	switch precision {
	case "month":
		value = t.Format("2006-01")
	case "year":
		value = t.Format("2006")
	case "decade":
		value = strconv.Itoa(t.Year()/10*10) + "s"
	default:
		value = t.Format("2006-01-02")
	}
	if estimated {
		return "~" + value
	}
	return value
}

// commentExcerpt reduces a comment to its first non-empty line, elided to
// width runes — one line per row is the whole point of a ledger.
func commentExcerpt(body string, width int) string {
	for line := range strings.SplitSeq(body, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return elide(trimmed, width)
		}
	}
	return ""
}
