package ctl

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// nameWidth keeps an album title, a label name or a subject name inside a
// terminal row without wrapping.
const nameWidth = 40

// Ack is the confirmation the CLI synthesizes for an endpoint that answers 204 No
// Content — attaching a label, favoriting a photo, setting a rating. There is no
// server JSON to pass through for those, so `-o json` gets this object instead of
// nothing at all, and a piped consumer can still tell success from failure.
type Ack struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// WriteAck renders the confirmation of a 204 No Content mutation: one line of
// prose as a table, or a small synthesized object as JSON.
func WriteAck(w io.Writer, format Format, message string) error {
	if format != FormatJSON {
		return writeLine(w, message)
	}
	encoded, err := json.Marshal(Ack{Status: "ok", Message: message})
	if err != nil {
		return fmt.Errorf("encoding the confirmation: %w", err)
	}
	return WriteJSON(w, encoded)
}

// WriteAlbums renders the album list as a compact table, ordered as the API
// returned it. The list carries no paging state, so there is no summary line.
func WriteAlbums(w io.Writer, albums []Album) error {
	if len(albums) == 0 {
		return writeLine(w, "no albums found")
	}
	rows := make([][]string, 0, len(albums))
	for _, album := range albums {
		rows = append(rows, []string{
			album.UID,
			elide(dash(album.Title), nameWidth),
			dash(album.Type),
			strconv.Itoa(album.PhotoCount),
			strconv.FormatBool(album.Private),
		})
	}
	return writeTable(w, []string{"UID", "TITLE", "TYPE", "PHOTOS", "PRIVATE"}, rows)
}

// WriteAlbum renders one album as an aligned key/value table. PHOTOS is absent:
// GET /albums/{uid} returns a bare album, so printing a zero count would be a lie.
func WriteAlbum(w io.Writer, album Album) error {
	return writeKeyValues(w, [][2]string{
		{"UID", album.UID},
		{"TITLE", dash(album.Title)},
		{"SLUG", dash(album.Slug)},
		{"TYPE", dash(album.Type)},
		{"DESCRIPTION", dash(album.Description)},
		{"PRIVATE", strconv.FormatBool(album.Private)},
		{"COVER", dashPtr(album.CoverPhotoUID)},
		{"CREATED", formatStamp(album.CreatedAt)},
		{"UPDATED", formatStamp(album.UpdatedAt)},
	})
}

// WriteLabels renders the label list as a compact table, in the API's priority
// order.
func WriteLabels(w io.Writer, labels []Label) error {
	if len(labels) == 0 {
		return writeLine(w, "no labels found")
	}
	rows := make([][]string, 0, len(labels))
	for _, label := range labels {
		rows = append(rows, []string{
			label.UID,
			elide(dash(label.Name), nameWidth),
			strconv.Itoa(label.Priority),
			strconv.Itoa(label.PhotoCount),
		})
	}
	return writeTable(w, []string{"UID", "NAME", "PRIORITY", "PHOTOS"}, rows)
}

// WriteLabel renders one label as an aligned key/value table. PHOTOS is absent
// for the same reason as on an album: the detail endpoint does not count them.
func WriteLabel(w io.Writer, label Label) error {
	return writeKeyValues(w, [][2]string{
		{"UID", label.UID},
		{"NAME", dash(label.Name)},
		{"SLUG", dash(label.Slug)},
		{"PRIORITY", strconv.Itoa(label.Priority)},
		{"CREATED", formatStamp(label.CreatedAt)},
		{"UPDATED", formatStamp(label.UpdatedAt)},
	})
}

// WriteSubjects renders the subject list as a compact table, in the API's name
// order. MARKERS is how many non-invalid face markers point at the subject.
func WriteSubjects(w io.Writer, subjects []Subject) error {
	if len(subjects) == 0 {
		return writeLine(w, "no subjects found")
	}
	rows := make([][]string, 0, len(subjects))
	for _, subject := range subjects {
		rows = append(rows, []string{
			subject.UID,
			elide(dash(subject.Name), nameWidth),
			dash(subject.Type),
			strconv.Itoa(subject.MarkerCount),
			strconv.FormatBool(subject.Favorite),
		})
	}
	return writeTable(w, []string{"UID", "NAME", "TYPE", "MARKERS", "FAVORITE"}, rows)
}

// WriteSubject renders one subject as an aligned key/value table. MARKERS is
// absent: the detail endpoint does not count them.
func WriteSubject(w io.Writer, subject Subject) error {
	return writeKeyValues(w, [][2]string{
		{"UID", subject.UID},
		{"NAME", dash(subject.Name)},
		{"SLUG", dash(subject.Slug)},
		{"TYPE", dash(subject.Type)},
		{"FAVORITE", strconv.FormatBool(subject.Favorite)},
		{"PRIVATE", strconv.FormatBool(subject.Private)},
		{"NOTES", dash(subject.Notes)},
		{"COVER", dashPtr(subject.CoverPhotoUID)},
		{"CREATED", formatStamp(subject.CreatedAt)},
		{"UPDATED", formatStamp(subject.UpdatedAt)},
	})
}

// WriteMembership renders an album's membership after a mutation as one line: the
// album and how many photos it now holds. The full order is in the API's own
// response, which `-o json` prints unchanged — a table of 500 uids would not be
// the compact result this CLI exists to produce.
func WriteMembership(w io.Writer, albumUID string, photoUIDs []string) error {
	return writeLine(w, "album "+albumUID+" now holds "+
		strconv.Itoa(len(photoUIDs))+" "+plural(len(photoUIDs), "photo", "photos"))
}

// WriteBulkResult renders a bulk edit's outcome: the aggregate counts on one line,
// preceded by a table of the photos that failed, if any. The photos that simply
// succeeded are not listed — `-o json` carries the full per-photo breakdown for a
// consumer that wants it.
func WriteBulkResult(w io.Writer, result BulkResult) error {
	if result.Counts.Errored > 0 {
		rows := make([][]string, 0, result.Counts.Errored)
		for _, photo := range result.Results {
			if photo.Error != "" {
				rows = append(rows, []string{photo.PhotoUID, photo.Status, photo.Error})
			}
		}
		if err := writeTable(w, []string{"UID", "STATUS", "ERROR"}, rows); err != nil {
			return err
		}
		if err := writeLine(w, ""); err != nil {
			return err
		}
	}
	return writeLine(w, bulkSummary(result.Counts))
}

// bulkSummary builds the one-line footer describing what the batch did.
func bulkSummary(counts BulkCounts) string {
	return strings.Join([]string{
		strconv.Itoa(counts.Total) + " " + plural(counts.Total, "photo", "photos"),
		strconv.Itoa(counts.Updated) + " updated",
		strconv.Itoa(counts.Skipped) + " skipped",
		strconv.Itoa(counts.Errored) + " errored",
	}, " · ")
}

// plural picks the singular or plural noun for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// formatStamp renders a non-nullable timestamp at minute precision in UTC, or a
// dash when it is zero.
func formatStamp(t time.Time) string {
	return formatTime(&t)
}

// dashPtr renders a nullable string field, printing a dash when it is unset.
func dashPtr(value *string) string {
	if value == nil {
		return "-"
	}
	return dash(*value)
}
