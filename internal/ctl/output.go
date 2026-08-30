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
	// FormatLLM is the API's answer stripped to what an agent can learn from:
	// compact JSON with the empty fields, the machine-derived columns and the
	// signed media URLs removed. See WriteLLM.
	FormatLLM Format = "llm"
)

// ErrInvalidFormat indicates an unsupported -o value.
var ErrInvalidFormat = errors.New(`ctl: output format must be "table", "json" or "llm"`)

// ParseFormat maps the -o flag value onto a Format, returning ErrInvalidFormat
// for anything else. yaml is deliberately not supported.
func ParseFormat(raw string) (Format, error) {
	switch Format(raw) {
	case FormatTable, FormatJSON, FormatLLM:
		return Format(raw), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidFormat, raw)
	}
}

// Output is how one command renders its result: the chosen format plus, for
// FormatLLM, the field allowlist --fields narrowed it to.
//
// It travels as one value rather than two parameters because every renderer
// needs both and a signature carrying only the format would quietly drop the
// allowlist on the resources that were added later.
type Output struct {
	// Format is the -o value.
	Format Format
	// Fields is the --fields allowlist, empty when the caller named none. It is
	// only consulted by FormatLLM: table output is already narrow, and JSON output
	// is the server's own bytes, which ctl does not rewrite.
	Fields []string
}

// NewOutput parses the -o value and pairs it with the --fields allowlist.
func NewOutput(raw string, fields []string) (Output, error) {
	format, err := ParseFormat(raw)
	if err != nil {
		return Output{}, err
	}
	return Output{Format: format, Fields: fields}, nil
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

// writeLine writes one line of prose, used for an empty result or a summary.
func writeLine(w io.Writer, line string) error {
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

// writeTable renders a header and its rows through a tabwriter, so every resource
// prints the same column-aligned shape without repeating the plumbing.
func writeTable(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

// writeKeyValues renders a detail view as an aligned key/value table.
func writeKeyValues(w io.Writer, rows [][2]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

// WritePhotoPage renders a page of photos as a compact table followed by a
// single summary line carrying the paging state (and, for a search, the
// effective ranking mode). An empty page prints one line and no header.
func WritePhotoPage(w io.Writer, page PhotoPage) error {
	if len(page.Photos) == 0 {
		return writeLine(w, "no photos found")
	}
	rows := make([][]string, 0, len(page.Photos))
	for _, photo := range page.Photos {
		rows = append(rows, []string{
			photo.UID,
			formatTime(photo.TakenAt),
			elide(dash(photo.Title), titleWidth),
			elide(dash(photo.FileName), fileWidth),
			formatSize(photo.FileSize),
		})
	}
	if err := writeTable(w, []string{"UID", "TAKEN", "TITLE", "FILE", "SIZE"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+pageSummary(page))
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
	return writeKeyValues(w, detailRows(detail))
}

// ocrWidth keeps the text read *in* a photo to a couple of terminal lines. The
// whole reading is in `-o json` and `-o llm`; the table is a glance, and a
// scanned page of newsprint would otherwise bury every row under it.
const ocrWidth = 120

// detailRows lists the key/value pairs of a photo detail in display order.
//
// The date and location rows carry their provenance beside their value: an
// estimated date and an estimated location are the two things a reader must not
// mistake for measured facts (see photos.Photo.TakenAtEstimated / LocationSource).
func detailRows(detail PhotoDetail) [][2]string {
	rows := [][2]string{
		{"UID", detail.UID},
		{"TITLE", dash(detail.Title)},
		{"DESCRIPTION", dash(detail.Description)},
		{"NOTES", dash(detail.Notes)},
		{"AI NOTE", dash(detail.AiNote)},
		{"TAKEN", formatTakenAt(detail)},
		{"TAKEN NOTE", dash(detail.TakenAtNote)},
		{"MEDIA", dash(detail.MediaType)},
		{"FILE", dash(detail.FileName)},
		{"SIZE", formatSize(detail.FileSize)},
		{"MIME", dash(detail.FileMime)},
		{"DIMENSIONS", formatDimensions(detail.FileWidth, detail.FileHeight)},
		{"CAMERA", dash(strings.TrimSpace(detail.CameraMake + " " + detail.CameraModel))},
		{"LENS", dash(detail.LensModel)},
		{"GPS", formatLocation(detail)},
		{"SUBJECT", dash(detail.Subject)},
		{"KEYWORDS", dash(detail.Keywords)},
		{"ARTIST", dash(detail.Artist)},
		{"COPYRIGHT", dash(detail.Copyright)},
		{"LICENSE", dash(detail.License)},
		{"SCAN", strconv.FormatBool(detail.Scan)},
		{"FAVORITE", strconv.FormatBool(detail.IsFavorite)},
		{"RATING", strconv.Itoa(detail.Rating)},
		{"FLAG", dash(detail.Flag)},
		{"ARCHIVED", formatTime(detail.ArchivedAt)},
		{"FILES", strconv.Itoa(len(detail.Files))},
		{"ALBUMS", dash(joinRefs(detail.Albums))},
		{"LABELS", dash(joinRefs(detail.Labels))},
		{"PEOPLE", formatPeople(detail.People)},
		{"OCR", dash(elide(collapseLines(detail.OCRText), ocrWidth))},
	}
	return rows
}

// formatTakenAt renders the capture time with the three things that qualify it:
// where the date came from, how coarsely it was stated, and whether it is an
// estimate rather than a fact.
func formatTakenAt(detail PhotoDetail) string {
	value := formatTime(detail.TakenAt)
	qualifiers := make([]string, 0, 3)
	if detail.TakenAtEstimated {
		qualifiers = append(qualifiers, "estimated")
	}
	if detail.TakenAtPrecision != "" && detail.TakenAtPrecision != "day" {
		qualifiers = append(qualifiers, detail.TakenAtPrecision)
	}
	if detail.TakenAtSource != "" {
		qualifiers = append(qualifiers, detail.TakenAtSource)
	}
	if len(qualifiers) == 0 {
		return value
	}
	return value + " (" + strings.Join(qualifiers, ", ") + ")"
}

// formatLocation renders the coordinates with where they came from, so an
// inferred position never reads like a measured one.
func formatLocation(detail PhotoDetail) string {
	value := formatGPS(detail.Lat, detail.Lng)
	if detail.LocationSource == "" {
		return value
	}
	return value + " (" + detail.LocationSource + ")"
}

// formatPeople renders who is on the photo: the named subjects first, then how
// many detections are still waiting for a name.
//
// A nil slice is not an empty photo: it means the response carried no roll-call
// at all — the caller passed --people=false, or the instance has no face backend
// wired — and saying "nobody" there would be a claim nobody made.
func formatPeople(onPhoto []PhotoPerson) string {
	if onPhoto == nil {
		return "- (not reported)"
	}
	names := make([]string, 0, len(onPhoto))
	unassigned := 0
	for _, person := range onPhoto {
		if !person.Named() {
			unassigned++
			continue
		}
		names = append(names, personLabel(person))
	}
	if unassigned > 0 {
		names = append(names, strconv.Itoa(unassigned)+" unassigned")
	}
	return dash(strings.Join(names, ", "))
}

// personLabel names one person on a photo, falling back to the subject uid when
// the subject could not be resolved to a name.
func personLabel(person PhotoPerson) string {
	if person.SubjectName != "" {
		return person.SubjectName
	}
	return person.SubjectUID
}

// collapseLines folds a multi-line value onto one row, so a block of recognised
// text cannot break the table's alignment.
func collapseLines(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// WriteContexts renders the client-side contexts as a table, marking the current
// one with an asterisk. Tokens are never printed — only whether one is stored.
func WriteContexts(w io.Writer, cfg *Config) error {
	if cfg == nil || len(cfg.Contexts) == 0 {
		return writeLine(w, "no contexts configured")
	}
	rows := make([][]string, 0, len(cfg.Contexts))
	for _, ctx := range cfg.Contexts {
		current := ""
		if ctx.Name == cfg.CurrentContext {
			current = "*"
		}
		token := "not set"
		if ctx.Token != "" {
			token = "stored"
		}
		rows = append(rows, []string{current, ctx.Name, ctx.Server, token})
	}
	return writeTable(w, []string{"CURRENT", "NAME", "SERVER", "TOKEN"}, rows)
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
