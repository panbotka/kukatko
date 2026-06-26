package exif

import (
	"math"
	"testing"
	"time"
)

// fullExiftoolJSON is a numeric (-n style) exiftool record with every field the
// parser maps, used as the happy-path fixture.
const fullExiftoolJSON = `[{
  "SourceFile": "sample.jpg",
  "Make": "Canon",
  "Model": "Canon EOS R5",
  "LensModel": "RF24-70mm F2.8 L IS USM",
  "ISO": 400,
  "FNumber": 2.8,
  "ExposureTime": 0.005,
  "FocalLength": 50,
  "Orientation": 6,
  "ImageWidth": 8192,
  "ImageHeight": 5464,
  "MIMEType": "image/jpeg",
  "GPSLatitude": 48.8584,
  "GPSLatitudeRef": "N",
  "GPSLongitude": 2.2945,
  "GPSLongitudeRef": "E",
  "GPSAltitude": 35,
  "GPSAltitudeRef": 0,
  "DateTimeOriginal": "2023:07:14 09:30:00"
}]`

// floatEq compares two float pointers for test assertions, treating nil/non-nil
// mismatches as failures and using a small tolerance for present values.
func floatEq(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", label, want)
		return
	}
	if math.Abs(*got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", label, *got, want)
	}
}

// TestParseExiftoolJSON_full checks that a complete numeric exiftool record maps
// onto every Metadata field, including GPS, exposure formatting and capture time.
func TestParseExiftoolJSON_full(t *testing.T) {
	t.Parallel()

	meta, err := parseExiftoolJSON([]byte(fullExiftoolJSON))
	if err != nil {
		t.Fatalf("parseExiftoolJSON() error = %v", err)
	}

	if meta.CameraMake != "Canon" || meta.CameraModel != "Canon EOS R5" {
		t.Errorf("camera = %q / %q, want Canon / Canon EOS R5", meta.CameraMake, meta.CameraModel)
	}
	if meta.LensModel != "RF24-70mm F2.8 L IS USM" {
		t.Errorf("LensModel = %q", meta.LensModel)
	}
	if meta.ISO == nil || *meta.ISO != 400 {
		t.Errorf("ISO = %v, want 400", meta.ISO)
	}
	floatEq(t, "Aperture", meta.Aperture, 2.8)
	floatEq(t, "FocalLength", meta.FocalLength, 50)
	if meta.Exposure != "1/200" {
		t.Errorf("Exposure = %q, want 1/200", meta.Exposure)
	}
	if meta.Orientation != 6 {
		t.Errorf("Orientation = %d, want 6", meta.Orientation)
	}
	if meta.Width != 8192 || meta.Height != 5464 {
		t.Errorf("dims = %dx%d, want 8192x5464", meta.Width, meta.Height)
	}
	if meta.Mime != "image/jpeg" {
		t.Errorf("Mime = %q", meta.Mime)
	}
	floatEq(t, "Lat", meta.Lat, 48.8584)
	floatEq(t, "Lng", meta.Lng, 2.2945)
	floatEq(t, "Altitude", meta.Altitude, 35)
	want := time.Date(2023, 7, 14, 9, 30, 0, 0, time.UTC)
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Exif == nil {
		t.Error("Exif map should retain the raw record")
	}
}

// TestParseExiftoolJSON_hemispheres confirms southern/western refs and a
// below-sea-level altitude produce negative values, and that string-form DMS
// coordinates (non -n output) are parsed equivalently.
func TestParseExiftoolJSON_hemispheres(t *testing.T) {
	t.Parallel()

	const south = `[{
	  "GPSLatitude": 33.8688, "GPSLatitudeRef": "S",
	  "GPSLongitude": 70.6483, "GPSLongitudeRef": "W",
	  "GPSAltitude": 120, "GPSAltitudeRef": 1
	}]`
	meta, err := parseExiftoolJSON([]byte(south))
	if err != nil {
		t.Fatalf("parseExiftoolJSON() error = %v", err)
	}
	floatEq(t, "Lat", meta.Lat, -33.8688)
	floatEq(t, "Lng", meta.Lng, -70.6483)
	floatEq(t, "Altitude", meta.Altitude, -120)

	const dms = `[{
	  "GPSLatitude": "48 deg 51' 30.24\" N", "GPSLatitudeRef": "N",
	  "GPSLongitude": "2 deg 17' 40.20\" E", "GPSLongitudeRef": "E"
	}]`
	meta, err = parseExiftoolJSON([]byte(dms))
	if err != nil {
		t.Fatalf("parseExiftoolJSON() dms error = %v", err)
	}
	floatEq(t, "Lat", meta.Lat, 48.8584)
	floatEq(t, "Lng", meta.Lng, 2.2945)
}

// TestParseExiftoolJSON_missing checks tolerance: an empty record yields a zero
// Metadata with no error and no EXIF map, and invalid JSON is an error.
func TestParseExiftoolJSON_missing(t *testing.T) {
	t.Parallel()

	meta, err := parseExiftoolJSON([]byte(`[]`))
	if err != nil {
		t.Fatalf("empty array error = %v", err)
	}
	if meta.Exif != nil || meta.Lat != nil || meta.TakenAt != nil {
		t.Errorf("empty record should be zero, got %+v", meta)
	}

	if _, err := parseExiftoolJSON([]byte(`not json`)); err == nil {
		t.Error("invalid JSON should error")
	}
}

// TestExposureString covers the string passthrough, sub-second numeric and
// whole-second numeric renderings, plus the empty/invalid cases.
func TestExposureString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  any
		want string
	}{
		{name: "string passthrough", raw: "1/125", want: "1/125"},
		{name: "sub-second numeric", raw: 0.005, want: "1/200"},
		{name: "whole second", raw: 2.0, want: "2"},
		{name: "fractional seconds", raw: 1.3, want: "1.3"},
		{name: "zero", raw: 0.0, want: ""},
		{name: "non numeric", raw: true, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exposureString(tt.raw); got != tt.want {
				t.Errorf("exposureString(%v) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestParseExifTime covers the supported timestamp layouts and the rejection of
// blanks and the all-zero EXIF placeholder date.
func TestParseExifTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{
			name: "plain",
			in:   "2003:11:23 18:07:37",
			want: time.Date(2003, 11, 23, 18, 7, 37, 0, time.UTC),
			ok:   true,
		},
		{
			name: "with offset",
			in:   "2023:07:14 09:30:00+02:00",
			want: time.Date(2023, 7, 14, 9, 30, 0, 0, time.FixedZone("", 2*60*60)),
			ok:   true,
		},
		{name: "blank", in: "   ", ok: false},
		{name: "zero placeholder", in: "0000:00:00 00:00:00", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseExifTime(tt.in)
			if ok != tt.ok {
				t.Fatalf("parseExifTime(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Errorf("parseExifTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
