package ctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Format selects how a command renders its result.
type Format string

const (
	// FormatTable is a compact human- and agent-readable table (the default).
	FormatTable Format = "table"
	// FormatJSON echoes the API's response bytes unchanged.
	FormatJSON Format = "json"
)

// ErrInvalidFormat indicates an unsupported -o value.
var ErrInvalidFormat = errors.New(`ctl: output format must be "table" or "json"`)

// ParseFormat maps the -o flag value onto a Format, returning ErrInvalidFormat
// for anything else. yaml is deliberately not supported.
func ParseFormat(raw string) (Format, error) {
	switch Format(raw) {
	case FormatTable, FormatJSON:
		return Format(raw), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidFormat, raw)
	}
}

// Column widths that keep a row inside a terminal without wrapping. The point of
// this CLI is a narrow result, so long titles and file names are elided.
const (
	titleWidth = 36
	fileWidth  = 30
)

// WriteJSON echoes the API's raw response bytes to w, unchanged, followed by a
// newline. Nothing is re-marshalled: a machine consumer gets exactly what the
// server sent.
func WriteJSON(w io.Writer, raw json.RawMessage) error {
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("writing json output: %w", err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("writing json output: %w", err)
	}
	return nil
}

// WritePhotoPage renders a page of photos as a compact table followed by a
// single summary line carrying the paging state (and, for a search, the
// effective ranking mode). An empty page prints one line and no header.
func WritePhotoPage(w io.Writer, page PhotoPage) error {
	if len(page.Photos) == 0 {
		if _, err := io.WriteString(w, "no photos found\n"); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "UID\tTAKEN\tTITLE\tFILE\tSIZE")
	for _, photo := range page.Photos {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			photo.UID,
			formatTime(photo.TakenAt),
			elide(dash(photo.Title), titleWidth),
			elide(dash(photo.FileName), fileWidth),
			formatSize(photo.FileSize),
		)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	if _, err := io.WriteString(w, "\n"+pageSummary(page)+"\n"); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

// pageSummary builds the one-line footer describing how much of the result set
// this page covers, how to fetch the next one, and — for a search — which
// ranking mode actually ran.
func pageSummary(page PhotoPage) string {
	parts := []string{
		strconv.Itoa(len(page.Photos)) + " of " + strconv.Itoa(page.Total) + " photos",
		"offset " + strconv.Itoa(page.Offset),
	}
	if page.NextOffset != nil {
		parts = append(parts, "next offset "+strconv.Itoa(*page.NextOffset))
	}
	if page.Mode != "" {
		mode := "mode " + page.Mode
		if page.Degraded {
			mode += " (degraded: semantic ranking unavailable, fell back to full text)"
		}
		parts = append(parts, mode)
	}
	return strings.Join(parts, " · ")
}

// WritePhotoDetail renders one photo as an aligned key/value table.
func WritePhotoDetail(w io.Writer, detail PhotoDetail) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, row := range detailRows(detail) {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

// detailRows lists the key/value pairs of a photo detail in display order.
func detailRows(detail PhotoDetail) [][2]string {
	rows := [][2]string{
		{"UID", detail.UID},
		{"TITLE", dash(detail.Title)},
		{"DESCRIPTION", dash(detail.Description)},
		{"TAKEN", formatTime(detail.TakenAt)},
		{"MEDIA", dash(detail.MediaType)},
		{"FILE", dash(detail.FileName)},
		{"SIZE", formatSize(detail.FileSize)},
		{"MIME", dash(detail.FileMime)},
		{"DIMENSIONS", formatDimensions(detail.FileWidth, detail.FileHeight)},
		{"CAMERA", dash(strings.TrimSpace(detail.CameraMake + " " + detail.CameraModel))},
		{"LENS", dash(detail.LensModel)},
		{"GPS", formatGPS(detail.Lat, detail.Lng)},
		{"FAVORITE", strconv.FormatBool(detail.IsFavorite)},
		{"RATING", strconv.Itoa(detail.Rating)},
		{"FLAG", dash(detail.Flag)},
		{"ARCHIVED", formatTime(detail.ArchivedAt)},
		{"FILES", strconv.Itoa(len(detail.Files))},
		{"ALBUMS", dash(joinRefs(detail.Albums))},
		{"LABELS", dash(joinRefs(detail.Labels))},
	}
	return rows
}

// WriteContexts renders the client-side contexts as a table, marking the current
// one with an asterisk. Tokens are never printed — only whether one is stored.
func WriteContexts(w io.Writer, cfg *Config) error {
	if cfg == nil || len(cfg.Contexts) == 0 {
		if _, err := io.WriteString(w, "no contexts configured\n"); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CURRENT\tNAME\tSERVER\tTOKEN")
	for _, ctx := range cfg.Contexts {
		current := ""
		if ctx.Name == cfg.CurrentContext {
			current = "*"
		}
		token := "not set"
		if ctx.Token != "" {
			token = "stored"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", current, ctx.Name, ctx.Server, token)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

// joinRefs renders album or label references as a comma-separated list of their
// human-readable names.
func joinRefs(refs []NamedRef) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Label())
	}
	return strings.Join(names, ", ")
}

// formatTime renders a nullable timestamp as a minute-precision local-free
// stamp, or a dash when absent.
func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// formatDimensions renders a pixel size, or a dash when either side is unknown.
func formatDimensions(width, height int) string {
	if width <= 0 || height <= 0 {
		return "-"
	}
	return strconv.Itoa(width) + "×" + strconv.Itoa(height)
}

// formatGPS renders a coordinate pair with five decimals (about a metre), or a
// dash when the photo carries no position.
func formatGPS(lat, lng *float64) string {
	if lat == nil || lng == nil {
		return "-"
	}
	return strconv.FormatFloat(*lat, 'f', 5, 64) + ", " + strconv.FormatFloat(*lng, 'f', 5, 64)
}

// sizeUnits are the binary size suffixes formatSize steps through.
var sizeUnits = [...]string{"B", "KiB", "MiB", "GiB", "TiB"}

// formatSize renders a byte count in the largest binary unit that keeps it below
// 1024, with one decimal above bytes.
func formatSize(size int64) string {
	if size <= 0 {
		return "-"
	}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(sizeUnits)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(size, 10) + " B"
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + sizeUnits[unit]
}

// dash renders an empty string as a dash so a table cell is never blank.
func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// elide shortens value to at most width runes, marking the cut with an ellipsis.
func elide(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}
