package sidecar

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// googleSidecar is the subset of a Takeout JSON sidecar Kukátko carries over.
// Google writes a good deal more (view counts, the Photos URL, the upload
// origin); none of it belongs in a catalogue.
type googleSidecar struct {
	// Title is the original file name, not a caption — Takeout has no title
	// field, and using this as one would fill every photo's title with "IMG_1234.jpg".
	Title string `json:"title"`
	// Description is the caption the user typed in Google Photos.
	Description string `json:"description"`
	// PhotoTakenTime is the capture time and the reason this package exists.
	PhotoTakenTime *googleTime `json:"photoTakenTime"`
	// CreationTime is when the photo entered Google Photos — an upload date, not
	// a capture date. It is read only as a last resort (see takenAt).
	CreationTime *googleTime `json:"creationTime"`
	// GeoData is the location shown in Google Photos (a user edit wins here),
	// GeoDataExif the one the camera recorded. Both are 0/0 when unknown.
	GeoData     *googleGeo `json:"geoData"`
	GeoDataExif *googleGeo `json:"geoDataExif"`
	// People are names Google attached to the photo, without any face box.
	People []googlePerson `json:"people"`
	// Favorited is the star the user gave the photo in Google Photos.
	Favorited bool `json:"favorited"`
}

// googleTime is a Takeout timestamp: Unix seconds, UTC, written as a string.
type googleTime struct {
	Timestamp epochSeconds `json:"timestamp"`
}

// googleGeo is a Takeout GPS fix. Google fills all three with exact zeros when
// it has no location, which usableCoords/usableAltitude read as absent.
type googleGeo struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Altitude  *float64 `json:"altitude"`
}

// googlePerson is one name Google attached to the photo.
type googlePerson struct {
	Name string `json:"name"`
}

// epochSeconds is a Unix timestamp in seconds. Takeout quotes it ("1465236142"),
// but some exports emit it as a bare number, so both are accepted.
type epochSeconds struct {
	// Time is the decoded timestamp; Set reports whether the field was present
	// and parseable at all.
	Time time.Time
	Set  bool
}

// UnmarshalJSON decodes a Unix-seconds timestamp written either as a JSON string
// or as a JSON number. An empty, null or unparseable value decodes to "not set"
// rather than an error: one malformed timestamp must not cost a photo the rest
// of its metadata.
func (e *epochSeconds) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		return nil
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil //nolint:nilerr // A malformed timestamp is "absent", not a reason to drop the sidecar.
	}
	e.Time = time.Unix(secs, 0).UTC()
	e.Set = true
	return nil
}

// readGoogle parses the Takeout JSON sidecar at path.
func readGoogle(path string) (Metadata, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from walking the folder the operator named.
	if err != nil {
		return Metadata{}, fmt.Errorf("sidecar: reading %s: %w", path, err)
	}
	var doc googleSidecar
	if err := json.Unmarshal(data, &doc); err != nil {
		return Metadata{}, fmt.Errorf("sidecar: parsing %s: %w", path, err)
	}
	return googleMetadata(doc, path), nil
}

// googleMetadata maps a parsed Takeout sidecar onto Metadata. Google's albums
// are deliberately not read: album membership comes from `--album`, never from
// the export (see the package doc).
func googleMetadata(doc googleSidecar, path string) Metadata {
	lat, lng, alt := googleGPS(doc)
	return Metadata{
		Source:      SourceGoogle,
		Path:        path,
		TakenAt:     googleTakenAt(doc),
		Description: strings.TrimSpace(doc.Description),
		Lat:         lat,
		Lng:         lng,
		Altitude:    alt,
		Favorite:    doc.Favorited,
		People:      googlePeople(doc.People),
	}
}

// googleTakenAt returns the capture time: photoTakenTime, falling back to
// creationTime. The fallback is the upload date and therefore a poor capture
// time — but a photo dated the day it was uploaded still sorts roughly right,
// whereas a photo with no date at all disappears from the timeline entirely.
func googleTakenAt(doc googleSidecar) *time.Time {
	for _, candidate := range []*googleTime{doc.PhotoTakenTime, doc.CreationTime} {
		if candidate != nil && candidate.Timestamp.Set {
			when := candidate.Timestamp.Time
			return &when
		}
	}
	return nil
}

// googleGPS picks the GPS fix, preferring geoData (which reflects a location the
// user corrected in Google Photos) over geoDataExif (what the camera recorded),
// and reading the exact 0/0 placeholder as "no location".
func googleGPS(doc googleSidecar) (lat, lng, alt *float64) {
	for _, geo := range []*googleGeo{doc.GeoData, doc.GeoDataExif} {
		if geo == nil {
			continue
		}
		lat, lng = usableCoords(geo.Latitude, geo.Longitude)
		if lat == nil {
			continue
		}
		return lat, lng, usableAltitude(geo.Altitude)
	}
	return nil, nil, nil
}

// googlePeople returns the non-empty names, deduplicated, in export order.
func googlePeople(people []googlePerson) []string {
	if len(people) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(people))
	names := make([]string, 0, len(people))
	for _, person := range people {
		name := strings.TrimSpace(person.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
