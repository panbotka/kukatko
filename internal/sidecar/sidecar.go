// Package sidecar reads the metadata a photo export writes *beside* the media
// file instead of inside it, and folds it into what the file's own EXIF says.
//
// Two shapes matter, and both are lossy without this package:
//
//   - Google Photos (Takeout) writes a JSON sidecar per media file. The exported
//     JPEG has usually been re-encoded with its EXIF stripped, so the real
//     capture date, the caption and the GPS fix only exist in that JSON. Import
//     such a folder naively and every photo lands with no date and no
//     description. See google.go — including the filename minefield the matcher
//     in match.go has to survive.
//   - Apple Photos writes standalone XMP sidecars (`.xmp`) carrying the capture
//     date, GPS, title/description, keywords, rating and creator. They are read
//     through exiftool, the same tool the EXIF path already shells out to. Apple
//     also writes `.AAE` files: those describe *edits*, not metadata, and are
//     ignored.
//
// Precedence (Apply): the file's own EXIF is the primary source and the sidecar
// fills what EXIF does not have — with one exception that matters more than the
// rule. Google's re-encoding sometimes leaves a bogus DateTimeOriginal equal to
// the *export* date, years after the photo was taken. So an EXIF capture time
// that lands more than takenAtTolerance *after* the sidecar's is treated as the
// export artefact it is, and the sidecar wins. A capture time only guessed from
// the filename always loses to a sidecar.
//
// What this package deliberately does not do: it never creates albums, subjects
// or face markers. Takeout's folder structure and its album `metadata.json`
// files are full of auto-generated junk from the phone, and Google's `people`
// entries carry no face boxes, so they cannot honestly become markers — the
// names are kept as metadata (see Apply) and nothing else.
package sidecar

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
)

// takenAtTolerance is how far after a sidecar's capture time an EXIF capture
// time may still be believed. Anything later is a re-encode artefact (Google
// writes the export date into DateTimeOriginal) and the sidecar wins. The window
// is a day because EXIF carries no time zone: a genuine local-time reading can
// legitimately sit up to fourteen hours away from the sidecar's UTC timestamp.
const takenAtTolerance = 24 * time.Hour

// Source names the export a sidecar came from.
type Source string

const (
	// SourceGoogle is a Google Photos (Takeout) JSON sidecar.
	SourceGoogle Source = "google_takeout"
	// SourceXMP is a standalone XMP sidecar, as Apple Photos and Lightroom write.
	SourceXMP Source = "xmp"
)

// exifKey is the key under which a sidecar's own document is stamped into the
// photo's EXIF JSON, so what the export claimed survives the import verbatim.
const exifKey = "Sidecar"

// Metadata is what one sidecar says about its media file. Every field is
// optional: an export that carries only a capture time is the common case.
type Metadata struct {
	// Source is the export shape the sidecar came from.
	Source Source
	// Path is the sidecar file this was read from.
	Path string

	// TakenAt is the capture time the export recorded, nil when it carried none.
	// For Takeout this is photoTakenTime and it is the field the whole feature
	// exists for.
	TakenAt *time.Time

	// Title and Description are the caption fields; both may be empty.
	Title       string
	Description string

	// Lat, Lng and Altitude are the exported GPS fix, nil when the export had
	// none. An exact 0/0 (Google's "unknown" placeholder) is read as absent, not
	// as a point in the Gulf of Guinea.
	Lat      *float64
	Lng      *float64
	Altitude *float64

	// Favorite is Takeout's `favorited` flag. Favourites are per-user in
	// Kukátko, so it is the importing user who gets the favourite.
	Favorite bool
	// Rating is an XMP star rating in 0..5; 0 means unrated (a negative XMP
	// "rejected" rating is read as unrated).
	Rating int

	// Keywords are XMP dc:subject entries.
	Keywords []string
	// People are the names Google attached to the photo. They stay metadata: the
	// export has no face boxes, so they cannot become markers and no subject is
	// ever created from them.
	People []string
	// Creator is the XMP dc:creator / Artist.
	Creator string
}

// Empty reports whether the sidecar carried nothing worth importing.
func (m Metadata) Empty() bool {
	return m.TakenAt == nil && m.Lat == nil && m.Lng == nil && m.Altitude == nil &&
		!m.Favorite && m.Rating == 0 && m.emptyText()
}

// emptyText reports whether the sidecar carried none of the text fields.
func (m Metadata) emptyText() bool {
	return m.Title == "" && m.Description == "" && m.Creator == "" &&
		len(m.Keywords) == 0 && len(m.People) == 0
}

// Read parses the sidecar at path, dispatching on its extension: `.json` is a
// Google Takeout sidecar, `.xmp` an XMP one (read through exiftool). Any other
// extension is an error — `.aae` in particular is an Apple *edit* description,
// not metadata, and is never offered here.
func Read(ctx context.Context, path string) (Metadata, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case extJSON:
		return readGoogle(path)
	case extXMP:
		return readXMP(ctx, path)
	default:
		return Metadata{}, fmt.Errorf("sidecar: %s: unsupported sidecar format", path)
	}
}

// Apply folds a sidecar into the metadata extracted from the media file itself,
// which the caller has already read. EXIF stays the primary source and the
// sidecar fills its gaps; see the package doc for the one case where the sidecar
// overrules EXIF (a capture time that is really the export date).
//
// It never clears a field: a value the file itself carried is either kept or
// replaced, never dropped.
func Apply(meta *exif.Metadata, sc Metadata) {
	if meta == nil || sc.Empty() {
		return
	}
	applyTakenAt(meta, sc)
	applyGPS(meta, sc)
	stamp(meta, sc)
}

// applyTakenAt resolves the capture time between EXIF and the sidecar. The
// sidecar wins whenever EXIF has no capture time at all, when EXIF's time is
// only a guess parsed from the filename, and when EXIF's time sits implausibly
// far after the sidecar's — the signature of a Takeout re-encode that stamped
// the export date into DateTimeOriginal.
func applyTakenAt(meta *exif.Metadata, sc Metadata) {
	if sc.TakenAt == nil {
		return
	}
	switch {
	case meta.TakenAt == nil, meta.TakenAtSource != exif.SourceExif:
		// No capture time, or only a filename guess: the export knows better.
	case meta.TakenAt.After(sc.TakenAt.Add(takenAtTolerance)):
		// A DateTimeOriginal well after the export's own capture time is the
		// export date, not the capture date.
	default:
		return
	}
	when := *sc.TakenAt
	meta.TakenAt = &when
	meta.TakenAtSource = exif.SourceSidecar
}

// applyGPS fills the GPS fix from the sidecar when the file itself carries none.
// Latitude and longitude move together — half a fix is not a location — and the
// altitude is filled on its own.
func applyGPS(meta *exif.Metadata, sc Metadata) {
	if meta.Lat == nil && meta.Lng == nil && sc.Lat != nil && sc.Lng != nil {
		lat, lng := *sc.Lat, *sc.Lng
		meta.Lat, meta.Lng = &lat, &lng
	}
	if meta.Altitude == nil && sc.Altitude != nil {
		alt := *sc.Altitude
		meta.Altitude = &alt
	}
}

// stamp records the sidecar's own claims in the photo's EXIF document, under a
// single "Sidecar" key. It is where the export's people and keywords land: they
// are kept as metadata because there is nothing honest to turn them into (see
// the package doc), and it is where a later question — "where did this date come
// from?" — can still be answered from the catalogue alone.
func stamp(meta *exif.Metadata, sc Metadata) {
	doc := map[string]any{"Source": string(sc.Source)}
	if sc.Path != "" {
		doc["File"] = filepath.Base(sc.Path)
	}
	if sc.TakenAt != nil {
		doc["TakenAt"] = sc.TakenAt.UTC().Format(time.RFC3339)
	}
	putIfSet(doc, "Title", sc.Title)
	putIfSet(doc, "Description", sc.Description)
	putIfSet(doc, "Creator", sc.Creator)
	if len(sc.People) > 0 {
		doc["People"] = sc.People
	}
	if len(sc.Keywords) > 0 {
		doc["Keywords"] = sc.Keywords
	}
	if sc.Favorite {
		doc["Favorited"] = true
	}
	if sc.Rating > 0 {
		doc["Rating"] = sc.Rating
	}
	if meta.Exif == nil {
		meta.Exif = make(map[string]any, 1)
	}
	meta.Exif[exifKey] = doc
}

// putIfSet adds a string field to the document unless it is empty.
func putIfSet(doc map[string]any, key, value string) {
	if value != "" {
		doc[key] = value
	}
}

// usableCoords reports the latitude and longitude when they are a real fix.
// Exports write an exact 0/0 to mean "no location known", and Null Island is not
// a place anybody photographs, so exact zeros are read as absent.
func usableCoords(lat, lng *float64) (*float64, *float64) {
	if lat == nil || lng == nil {
		return nil, nil
	}
	if *lat == 0 && *lng == 0 {
		return nil, nil
	}
	return lat, lng
}

// usableAltitude reports the altitude unless it is the exact zero exports write
// for "unknown".
func usableAltitude(alt *float64) *float64 {
	if alt == nil || *alt == 0 {
		return nil
	}
	return alt
}
