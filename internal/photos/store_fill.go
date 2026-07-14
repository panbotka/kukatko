package photos

import (
	"context"
	"fmt"
	"time"
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
