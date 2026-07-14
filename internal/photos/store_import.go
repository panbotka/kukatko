package photos

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ImportMetadata is the metadata an importer carries over from an external
// catalogue that curates it — PhotoPrism's Details block and the file-technical
// fields it indexed — onto a photo that is already in the catalogue. It is the
// import counterpart of MetadataFill and FileMetadata, and it differs from both in
// where it sits in the precedence order (see Store.ApplyImportMetadata):
//
//   - The credit and technical fields are the source's to own. A non-empty value
//     wins over what the photo currently holds, exactly as the camera and exposure
//     fields already do; an empty one never erases a value that is there.
//   - Notes is Kukátko's own field, so it is only ever gap-filled — a note the user
//     typed is never overwritten by the source's.
//
// The zero value is inert: applying it writes nothing at all.
type ImportMetadata struct {
	// Subject is the IPTC subject/headline; Keywords the tag list, comma-separated
	// (normalise it with exif.NormalizeKeywords so it reads like an extracted one).
	Subject  string
	Keywords string
	// Artist, Copyright and License are the credit fields.
	Artist    string
	Copyright string
	License   string
	// Notes is the source's free-text note. It is gap-filled only.
	Notes string
	// Software is what produced the image; Scan marks a digitised print. Scan is
	// true-wins: the source can set the flag, never clear it.
	Software string
	Scan     bool
	// CameraSerial, ColorProfile, ImageCodec and Projection are the file-technical
	// fields. ImageCodec is the still's compression and must be left empty for a
	// video, whose compression lives in the untouched video_codec column.
	CameraSerial string
	ColorProfile string
	ImageCodec   string
	Projection   string
	// OriginalName is the name the file carried in the source catalogue.
	OriginalName string
}

// importOwnedColumns are the columns a source may overwrite with a non-empty value
// of its own, in the order they are passed as $2…$N. The SQL is built from this
// list (see buildApplyImportMetadataSQL), so a newly carried-over field is added
// here and in ImportMetadata.values — the statement cannot drift from the struct.
// notes and scan are deliberately absent: they are written under different rules
// and are appended after this list.
var importOwnedColumns = []string{
	"subject", "keywords", "artist", "copyright", "license", "software",
	"camera_serial", "color_profile", "image_codec", "projection", "original_name",
}

// values returns m's fields as query arguments: the importOwnedColumns in order,
// then notes and scan.
func (m ImportMetadata) values() []any {
	return []any{
		m.Subject, m.Keywords, m.Artist, m.Copyright, m.License, m.Software,
		m.CameraSerial, m.ColorProfile, m.ImageCodec, m.Projection, m.OriginalName,
		m.Notes, m.Scan,
	}
}

// applyImportMetadataSQL applies an import's metadata to a catalogued photo, built
// once from importOwnedColumns.
var applyImportMetadataSQL = buildApplyImportMetadataSQL()

// buildApplyImportMetadataSQL assembles the apply statement. Like the fill
// statement it self-joins the photo's pre-update row (the `o` subquery, which sees
// the snapshot the statement started from) so both the guards and the RETURNING
// clause read the *old* values — a plain RETURNING would see the row it has just
// written and could never report what changed.
//
// Every column is guarded, and each guard is also the condition of its assignment,
// so the statement is a no-op whenever the source has nothing new to say: an empty
// source value never erases a non-empty column, a note the user typed is never
// overwritten, and updated_at is bumped only when something genuinely changes. That
// is what makes a re-import a real no-op, invisible even to a caller ordering by
// updated_at.
func buildApplyImportMetadataSQL() string {
	assignments := make([]string, 0, len(importOwnedColumns)+3)
	guards := make([]string, 0, len(importOwnedColumns)+2)
	for i, col := range importOwnedColumns {
		param := "$" + strconv.Itoa(i+2) + "::text"
		assignments = append(assignments,
			fmt.Sprintf("%s = CASE WHEN %s <> '' THEN %s ELSE o.%s END", col, param, param, col))
		guards = append(guards, fmt.Sprintf("(%s <> '' AND o.%s <> %s)", param, col, param))
	}
	notes := "$" + strconv.Itoa(len(importOwnedColumns)+2) + "::text"
	scan := "$" + strconv.Itoa(len(importOwnedColumns)+3) + "::boolean"
	assignments = append(assignments,
		fmt.Sprintf("notes = CASE WHEN o.notes = '' THEN %s ELSE o.notes END", notes),
		fmt.Sprintf("scan = CASE WHEN %s THEN true ELSE o.scan END", scan))
	guards = append(guards,
		fmt.Sprintf("(o.notes = '' AND %s <> '')", notes),
		fmt.Sprintf("(%s AND NOT o.scan)", scan))

	changed := "(" + strings.Join(guards, "\n\t\tOR ") + ")"
	assignments = append(assignments, "updated_at = CASE WHEN "+changed+" THEN now() ELSE o.updated_at END")
	snapshot := append(slices.Clone(importOwnedColumns), "notes", "scan", "uid", "updated_at")

	return "UPDATE photos p SET\n\t" + strings.Join(assignments, ",\n\t") +
		"\nFROM (SELECT " + strings.Join(snapshot, ", ") + " FROM photos WHERE uid = $1) o" +
		"\nWHERE p.uid = o.uid\nRETURNING " + changed
}

// ApplyImportMetadata writes the metadata an external catalogue holds for a photo
// into the catalogue and reports whether any column actually changed. It backs the
// PhotoPrism import, which reads the source's Details block (subject, artist,
// copyright, licence, keywords, notes, software) and the technical fields it
// indexed (scan, camera serial, colour profile, still codec, projection, original
// name) off the photo detail and carries them over.
//
// The source owns the credit and technical fields: a non-empty value it holds wins
// over the photo's current one, which is the same precedence the camera and
// exposure fields have had since the first import. What it must never do is
// *destroy*: an empty source value leaves a non-empty column exactly as it is, the
// scan flag can be set but not cleared, and notes — Kukátko's own field, which the
// source has no business rewriting — is only ever filled when empty. Nothing
// outside the columns listed here is touched: the captions, the capture time, the
// GPS fix, the ratings, the favourites and every other piece of curation are none
// of this call's business.
//
// Applying the same metadata twice writes nothing the second time (not even
// updated_at), so a re-import over an unchanged source is a genuine no-op.
//
// It returns ErrPhotoNotFound when no such photo exists.
func (s *Store) ApplyImportMetadata(ctx context.Context, uid string, m ImportMetadata) (bool, error) {
	args := append([]any{uid}, m.values()...)
	var changed bool
	if err := s.pool.QueryRow(ctx, applyImportMetadataSQL, args...).Scan(&changed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrPhotoNotFound
		}
		return false, fmt.Errorf("photos: applying import metadata to %s: %w", uid, err)
	}
	return changed, nil
}
