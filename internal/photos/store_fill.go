package photos

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// MetadataFill is metadata an importer offers to a photo that is already in the
// catalogue — a folder import that meets a file it has seen before, now with the
// sidecar it lacked the first time. Every field is optional and none of them ever
// overwrites what is already there: an import fills gaps, it does not rewrite
// history (see Store.FillMissingMetadata).
type MetadataFill struct {
	// TakenAt is the capture time the sidecar recorded, nil when it had none.
	TakenAt *time.Time
	// TakenAtSource records where TakenAt came from; it is written only together
	// with TakenAt.
	TakenAtSource string
	// Lat and Lng are a GPS fix; they are written only as a pair, and only when
	// the photo has neither.
	Lat *float64
	Lng *float64
	// Altitude is metres above sea level.
	Altitude *float64
	// Title and Description are the caption fields.
	Title       string
	Description string
}

// weakTakenAtSources are the capture-time provenances a sidecar may overrule: no
// date at all, and a date merely guessed from the file name. Everything else —
// the file's own EXIF, a date the user typed in ("manual"), an earlier sidecar —
// stands.
var weakTakenAtSources = []string{string(sourceUnknown), string(sourceFilename)}

// Capture-time provenances written by the ingest pipeline (mirrored from
// internal/exif, which cannot be imported here without an import cycle).
const (
	sourceUnknown  = "unknown"
	sourceFilename = "filename"
)

// fillMissingSQL fills a photo's empty metadata fields and touches nothing else.
// Every assignment is guarded, and the WHERE clause repeats the guards so a photo
// with nothing to fill is not written at all: re-running an import must be a
// genuine no-op, down to updated_at.
const fillMissingSQL = `UPDATE photos SET
	taken_at = CASE WHEN $2::timestamptz IS NOT NULL AND (taken_at IS NULL OR taken_at_source = ANY($3))
		THEN $2::timestamptz ELSE taken_at END,
	taken_at_source = CASE WHEN $2::timestamptz IS NOT NULL AND (taken_at IS NULL OR taken_at_source = ANY($3))
		THEN $4 ELSE taken_at_source END,
	lat = CASE WHEN lat IS NULL AND lng IS NULL THEN $5::double precision ELSE lat END,
	lng = CASE WHEN lat IS NULL AND lng IS NULL THEN $6::double precision ELSE lng END,
	altitude = COALESCE(altitude, $7::double precision),
	title = CASE WHEN title = '' THEN $8 ELSE title END,
	description = CASE WHEN description = '' THEN $9 ELSE description END,
	updated_at = now()
WHERE uid = $1 AND (
	($2::timestamptz IS NOT NULL AND (taken_at IS NULL OR taken_at_source = ANY($3))
		AND (taken_at IS DISTINCT FROM $2::timestamptz OR taken_at_source IS DISTINCT FROM $4))
	OR ($5::double precision IS NOT NULL AND $6::double precision IS NOT NULL AND lat IS NULL AND lng IS NULL)
	OR ($7::double precision IS NOT NULL AND altitude IS NULL)
	OR ($8 <> '' AND title = '')
	OR ($9 <> '' AND description = '')
)`

// FillMissingMetadata fills the gaps in a catalogued photo's metadata from an
// import's sidecar and reports whether anything actually changed. It is how a
// folder re-imported *after* its Google Takeout JSON files were noticed still
// gets its dates: the photo is a content duplicate, so nothing new is created,
// but the date it never had is written.
//
// It never overwrites: a field the photo already carries — a GPS fix from its
// own EXIF, a description the user typed, a capture time from EXIF or from a
// manual edit — is left exactly as it is. The one field with a hierarchy is the
// capture time, which a sidecar may overrule when it is missing or was only
// guessed from the file name (weakTakenAtSources).
//
// Filling a photo twice writes nothing the second time, so an import stays
// idempotent: running it again changes no row, not even updated_at.
func (s *Store) FillMissingMetadata(ctx context.Context, uid string, fill MetadataFill) (bool, error) {
	lat, lng := fill.Lat, fill.Lng
	if lat == nil || lng == nil {
		// Half a fix is not a location; neither half is written on its own.
		lat, lng = nil, nil
	}
	takenAt := fill.TakenAt
	if fill.TakenAtSource == "" {
		// A capture time with no provenance would blank taken_at_source; the column
		// is never empty, so an unsourced time is simply not offered.
		takenAt = nil
	}
	tag, err := s.pool.Exec(ctx, fillMissingSQL, uid,
		takenAt, weakTakenAtSources, fill.TakenAtSource,
		lat, lng, fill.Altitude, fill.Title, fill.Description)
	if err != nil {
		return false, fmt.Errorf("photos: filling metadata of %s: %w", uid, err)
	}
	return tag.RowsAffected() > 0, nil
}

// FileMetadata is what re-reading a photo's original file says about it: the
// IPTC/XMP credit fields and the file-technical fields, as internal/exif extracted
// them. Every field is optional — a file that carries no IPTC at all yields the
// zero value — and none of them ever overwrites a value the photo already has (see
// Store.FillFileMetadata).
type FileMetadata struct {
	// Subject is the IPTC headline; Keywords the tag list, comma-separated.
	Subject  string
	Keywords string
	// Artist, Copyright and License are the credit fields.
	Artist    string
	Copyright string
	License   string
	// Software, CameraSerial, ColorProfile, ImageCodec and Projection are the
	// machine-derived technical fields. ImageCodec is empty for a video, whose
	// compression lives in the untouched video_codec column.
	Software     string
	CameraSerial string
	ColorProfile string
	ImageCodec   string
	Projection   string
	// OriginalName is the name the file carried before it was ingested.
	OriginalName string
}

// fileMetadataColumns are the columns FillFileMetadata fills, in the order their
// values are passed as $2…$N. The SQL is built from this list (see
// buildFillFileMetadataSQL), so a new extracted field is added here and in
// FileMetadata.values — the statement cannot drift from the struct.
var fileMetadataColumns = []string{
	"subject", "keywords", "artist", "copyright", "license", "software",
	"camera_serial", "color_profile", "image_codec", "projection", "original_name",
}

// values returns m's fields as query arguments in fileMetadataColumns order.
func (m FileMetadata) values() []any {
	return []any{
		m.Subject, m.Keywords, m.Artist, m.Copyright, m.License, m.Software,
		m.CameraSerial, m.ColorProfile, m.ImageCodec, m.Projection, m.OriginalName,
	}
}

// fillFileMetadataSQL fills a photo's empty metadata columns from a fresh
// extraction and stamps metadata_extracted_at, built once from
// fileMetadataColumns.
var fillFileMetadataSQL = buildFillFileMetadataSQL()

// buildFillFileMetadataSQL assembles the fill statement. It self-joins the photo's
// pre-update row (the `o` subquery, which sees the snapshot the statement started
// from) so that both the guards and the RETURNING clause can read the *old* values
// — a plain RETURNING would see the row it has just written and could never report
// what changed.
//
// Every column is guarded: a value is written only where the column is still
// empty, so an extracted blank never erases what the user typed and a second run
// over the same photo writes nothing. updated_at is bumped only when a column is
// genuinely filled, which keeps a no-op backfill invisible to every caller that
// orders by it. metadata_extracted_at is always re-stamped: the file was read,
// whatever it turned out to say.
func buildFillFileMetadataSQL() string {
	assignments := make([]string, 0, len(fileMetadataColumns)+2)
	guards := make([]string, 0, len(fileMetadataColumns))
	snapshot := make([]string, 0, len(fileMetadataColumns)+2)
	for i, col := range fileMetadataColumns {
		param := "$" + strconv.Itoa(i+2)
		assignments = append(assignments,
			fmt.Sprintf("%s = CASE WHEN o.%s = '' THEN %s ELSE o.%s END", col, col, param, col))
		guards = append(guards, fmt.Sprintf("(o.%s = '' AND %s <> '')", col, param))
		snapshot = append(snapshot, col)
	}
	filled := "(" + strings.Join(guards, "\n\t\tOR ") + ")"
	assignments = append(assignments,
		"metadata_extracted_at = now()",
		"updated_at = CASE WHEN "+filled+" THEN now() ELSE o.updated_at END")
	snapshot = append(snapshot, "uid", "updated_at")

	return "UPDATE photos p SET\n\t" + strings.Join(assignments, ",\n\t") +
		"\nFROM (SELECT " + strings.Join(snapshot, ", ") + " FROM photos WHERE uid = $1) o" +
		"\nWHERE p.uid = o.uid\nRETURNING " + filled
}

// FillFileMetadata writes the metadata read out of a photo's original into the
// catalogue and reports whether any column was actually filled. It backs both the
// `metadata` job (which re-reads one photo's original) and, through it, the
// metadata backfill over the photos that predate extraction.
//
// It only ever fills gaps. A column that already holds a value — a subject the
// user typed, an artist an earlier extraction found — is left exactly as it is, so
// an empty extraction can never erase a user's edit and running the job twice
// changes nothing the second time (not even updated_at). Nothing outside
// fileMetadataColumns is touched: the captions, the capture time, the GPS fix, the
// ratings and every other piece of curation are none of this call's business.
//
// It returns ErrPhotoNotFound when no such photo exists.
func (s *Store) FillFileMetadata(ctx context.Context, uid string, m FileMetadata) (bool, error) {
	args := append([]any{uid}, m.values()...)
	var filled bool
	if err := s.pool.QueryRow(ctx, fillFileMetadataSQL, args...).Scan(&filled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrPhotoNotFound
		}
		return false, fmt.Errorf("photos: filling file metadata of %s: %w", uid, err)
	}
	return filled, nil
}
