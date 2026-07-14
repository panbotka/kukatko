package sidecar

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
)

// maxRating is the top of the XMP star scale. A negative rating is XMP's
// "rejected" marker, which Kukátko has no field for and reads as unrated.
const maxRating = 5

// readXMP reads a standalone XMP sidecar through exif.Extract — that is, through
// exiftool, the tool the EXIF path already depends on and the only thing here
// that understands every dialect of XMP that Apple, Adobe and darktable write.
//
// It fails when the file yields nothing worth importing, which is what an XMP
// looks like when exiftool is not installed: reporting an unreadable sidecar
// beats silently importing a folder of Apple exports with no dates.
func readXMP(ctx context.Context, path string) (Metadata, error) {
	meta, err := exif.Extract(ctx, path)
	if err != nil {
		return Metadata{}, fmt.Errorf("sidecar: reading %s: %w", path, err)
	}
	sc := xmpMetadata(meta, path)
	if sc.Empty() {
		return Metadata{}, fmt.Errorf("sidecar: %s: no usable XMP metadata (is exiftool installed?)", path)
	}
	return sc, nil
}

// xmpMetadata maps an extracted XMP tag document onto Metadata.
//
// The capture time is taken only when it came from a real tag: exif.Extract
// falls back to parsing the *file name* for a date, and the file name here is
// the sidecar's, which says nothing about when the photo was taken.
func xmpMetadata(meta exif.Metadata, path string) Metadata {
	lat, lng := usableCoords(meta.Lat, meta.Lng)
	sc := Metadata{
		Source:      SourceXMP,
		Path:        path,
		Lat:         lat,
		Lng:         lng,
		Altitude:    usableAltitude(meta.Altitude),
		Title:       firstTag(meta.Exif, "Title", "ObjectName"),
		Description: firstTag(meta.Exif, "Description", "ImageDescription", "Caption-Abstract"),
		Creator:     firstTag(meta.Exif, "Creator", "Artist", "By-line"),
		Keywords:    tagList(meta.Exif, "Subject", "Keywords"),
		Rating:      xmpRating(meta.Exif["Rating"]),
	}
	if meta.TakenAt != nil && meta.TakenAtSource == exif.SourceExif {
		when := *meta.TakenAt
		sc.TakenAt = &when
	}
	return sc
}

// xmpRating normalises an XMP star rating to 0..5. Anything unparseable, out of
// range, or negative (XMP's "rejected") reads as unrated.
func xmpRating(raw any) int {
	value, ok := tagFloat(raw)
	if !ok {
		return 0
	}
	stars := int(math.Round(value))
	if stars < 0 || stars > maxRating {
		return 0
	}
	return stars
}

// firstTag returns the first non-empty string among the given tags. A lang-alt
// value (dc:description is one) may arrive as a list; its first entry is used.
func firstTag(doc map[string]any, keys ...string) string {
	for _, key := range keys {
		if values := tagStrings(doc[key]); len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// tagList returns every string value of the first present tag among keys, which
// is how a dc:subject bag of keywords arrives.
func tagList(doc map[string]any, keys ...string) []string {
	for _, key := range keys {
		if values := tagStrings(doc[key]); len(values) > 0 {
			return values
		}
	}
	return nil
}

// tagStrings coerces one exiftool tag value to a list of non-empty strings,
// accepting the three shapes it can take: a plain string, a number, or a list of
// either (a single-entry list is how a one-keyword bag arrives).
func tagStrings(raw any) []string {
	switch value := raw.(type) {
	case string:
		return nonEmpty(strings.TrimSpace(value))
	case float64:
		return nonEmpty(strconv.FormatFloat(value, 'g', -1, 64))
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, tagStrings(item)...)
		}
		return out
	default:
		return nil
	}
}

// nonEmpty wraps a string in a one-element slice, or returns nothing when it is
// blank.
func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

// tagFloat coerces an exiftool tag value to a number, accepting the numeric
// strings exiftool emits for some tags.
func tagFloat(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
