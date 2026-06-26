package exif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// exiftoolBinary is the metadata reader shelled out to on the primary path.
	exiftoolBinary = "exiftool"
	// exiftoolTimeout caps a single exiftool invocation. Reading tags is cheap;
	// this is a generous backstop against a wedged process on a slow device.
	exiftoolTimeout = 30 * time.Second
)

// exifTimeLayouts lists the timestamp formats exiftool may emit for capture-time
// tags, tried in order. EXIF dates carry no zone, so the zone-less layouts are
// parsed as UTC; the offset variants cover cameras that record OffsetTime.
var exifTimeLayouts = []string{
	"2006:01:02 15:04:05.999999-07:00",
	"2006:01:02 15:04:05-07:00",
	"2006:01:02 15:04:05.999999",
	"2006:01:02 15:04:05",
}

// coordNumberRe matches the signed decimal numbers inside an exiftool GPS string
// such as `39 deg 54' 56.00" N`, used when output is not in numeric (-n) mode.
var coordNumberRe = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)

// exiftoolAvailable reports whether the exiftool binary is on PATH.
func exiftoolAvailable() bool {
	_, err := exec.LookPath(exiftoolBinary)
	return err == nil
}

// extractWithExiftool runs `exiftool -json -n` against path and parses the
// result into Metadata. The -n flag forces numeric (rather than human-readable)
// tag values so dimensions, orientation and coordinates parse deterministically.
// It returns an error if the process fails or its output is not valid JSON.
func extractWithExiftool(ctx context.Context, path string) (Metadata, error) {
	cctx, cancel := context.WithTimeout(ctx, exiftoolTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	// #nosec G204 -- path is the caller-supplied file Extract already stat'ed;
	// the remaining arguments are constant flags.
	cmd := exec.CommandContext(cctx, exiftoolBinary, "-json", "-n", "--", path)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Metadata{}, fmt.Errorf("exif: run exiftool: %w (stderr: %s)", err, stderr.String())
	}
	return parseExiftoolJSON(stdout.Bytes())
}

// parseExiftoolJSON maps a single exiftool `-json` record onto Metadata. The
// output is a one-element array of tag objects; an empty array yields a zero
// Metadata (no EXIF) without error. The whole tag object is retained verbatim in
// Metadata.Exif as the JSON-able document.
func parseExiftoolJSON(data []byte) (Metadata, error) {
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return Metadata{}, fmt.Errorf("exif: parse exiftool json: %w", err)
	}
	if len(records) == 0 {
		return Metadata{}, nil
	}
	obj := records[0]
	meta := Metadata{Exif: obj}
	applyExiftoolCamera(&meta, obj)
	applyExiftoolExposure(&meta, obj)
	applyExiftoolGeometry(&meta, obj)
	applyExiftoolGPS(&meta, obj)
	applyExiftoolTime(&meta, obj)
	return meta, nil
}

// applyExiftoolCamera fills the camera/lens identity fields from the tag object.
func applyExiftoolCamera(meta *Metadata, obj map[string]any) {
	meta.CameraMake = strVal(obj["Make"])
	meta.CameraModel = strVal(obj["Model"])
	meta.LensModel = firstStr(obj, "LensModel", "LensID", "Lens", "LensType")
}

// applyExiftoolExposure fills ISO, aperture, shutter and focal-length fields.
func applyExiftoolExposure(meta *Metadata, obj map[string]any) {
	meta.ISO = intPtrFrom(obj["ISO"])
	meta.Aperture = floatPtrFrom(firstVal(obj, "FNumber", "ApertureValue"))
	meta.FocalLength = floatPtrFrom(obj["FocalLength"])
	meta.Exposure = exposureString(firstVal(obj, "ExposureTime", "ShutterSpeedValue"))
}

// applyExiftoolGeometry fills pixel dimensions, orientation and the MIME type.
func applyExiftoolGeometry(meta *Metadata, obj map[string]any) {
	meta.Width = intVal(obj, "ImageWidth", "ExifImageWidth")
	meta.Height = intVal(obj, "ImageHeight", "ExifImageHeight")
	meta.Orientation = intVal(obj, "Orientation")
	meta.Mime = strVal(obj["MIMEType"])
}

// applyExiftoolGPS fills the decimal latitude, longitude and altitude.
func applyExiftoolGPS(meta *Metadata, obj map[string]any) {
	meta.Lat = gpsCoord(obj["GPSLatitude"], strVal(obj["GPSLatitudeRef"]))
	meta.Lng = gpsCoord(obj["GPSLongitude"], strVal(obj["GPSLongitudeRef"]))
	if alt, ok := gpsAltitude(obj); ok {
		meta.Altitude = &alt
	}
}

// applyExiftoolTime sets TakenAt from the first usable capture-time tag, leaving
// it nil (for filename fallback) when none parse.
func applyExiftoolTime(meta *Metadata, obj map[string]any) {
	raw := firstStr(obj, "DateTimeOriginal", "CreateDate", "DateTimeDigitized")
	if when, ok := parseExifTime(raw); ok {
		meta.TakenAt = &when
	}
}

// gpsCoord converts an exiftool coordinate value plus its hemisphere reference
// to signed decimal degrees, returning nil when the value is missing or
// unparseable. The magnitude is taken from the raw value (numeric in -n mode, or
// a `D deg M' S"` string otherwise) and re-signed from ref when ref is present.
func gpsCoord(raw any, ref string) *float64 {
	val, ok := coordValue(raw)
	if !ok {
		return nil
	}
	if ref != "" {
		val = applyHemisphere(math.Abs(val), ref)
	}
	return &val
}

// coordValue extracts decimal degrees from an exiftool coordinate value. Numbers
// are returned as-is; strings are parsed as one, two or three space/symbol
// separated numbers interpreted as decimal degrees, degrees+minutes, or
// degrees/minutes/seconds respectively.
func coordValue(raw any) (float64, bool) {
	if f, ok := toFloat(raw); ok {
		return f, true
	}
	str, ok := raw.(string)
	if !ok {
		return 0, false
	}
	nums := coordNumberRe.FindAllString(str, 3)
	if len(nums) == 0 {
		return 0, false
	}
	parts := make([]float64, len(nums))
	for i, n := range nums {
		parts[i], _ = strconv.ParseFloat(n, 64)
	}
	return combineDMS(parts), true
}

// combineDMS folds one, two or three positive coordinate components into decimal
// degrees: one value is already decimal degrees, two are degrees and minutes,
// three are degrees, minutes and seconds.
func combineDMS(parts []float64) float64 {
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0] + parts[1]/60
	default:
		return dmsToDecimal(parts[0], parts[1], parts[2])
	}
}

// gpsAltitude returns the signed altitude in metres and true when the tag
// object carries a usable GPSAltitude. A GPSAltitudeRef of 1 (or a "below"
// string) denotes below sea level and negates the value.
func gpsAltitude(obj map[string]any) (float64, bool) {
	alt, ok := toFloat(obj["GPSAltitude"])
	if !ok {
		return 0, false
	}
	ref := strVal(obj["GPSAltitudeRef"])
	if ref == "1" || strings.Contains(strings.ToLower(ref), "below") {
		alt = -math.Abs(alt)
	}
	return alt, true
}

// parseExifTime parses an exiftool capture-time string against the known
// layouts, returning the time and true on the first match. Zone-less inputs are
// interpreted as UTC. The all-zero EXIF placeholder date is rejected.
func parseExifTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0000:00:00") {
		return time.Time{}, false
	}
	for _, layout := range exifTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// exposureString renders a shutter-speed value as it is conventionally
// displayed. A string value (e.g. "1/125") is passed through; a numeric seconds
// value is rendered as "1/N" when under a second and as a trimmed decimal
// otherwise. An empty or non-positive value yields "".
func exposureString(raw any) string {
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	seconds, ok := toFloat(raw)
	if !ok || seconds <= 0 {
		return ""
	}
	if seconds < 1 {
		return fmt.Sprintf("1/%d", int(math.Round(1/seconds)))
	}
	return strconv.FormatFloat(seconds, 'g', -1, 64)
}

// strVal returns v as a string when it already is one, formatting a numeric v
// without a trailing decimal point, and "" for anything else (including nil).
func strVal(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return ""
	}
}

// firstStr returns the first non-empty string value among the given keys.
func firstStr(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := strVal(obj[key]); s != "" {
			return s
		}
	}
	return ""
}

// firstVal returns the value of the first present key, or nil when none exist.
func firstVal(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := obj[key]; ok {
			return v
		}
	}
	return nil
}

// toFloat coerces a JSON-decoded value to float64, accepting float64 and numeric
// strings. It returns false for anything else.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// floatPtrFrom returns a pointer to the float value of v, or nil when v cannot
// be read as a number.
func floatPtrFrom(v any) *float64 {
	if f, ok := toFloat(v); ok {
		return &f
	}
	return nil
}

// intPtrFrom returns a pointer to the rounded integer value of v, or nil when v
// cannot be read as a number.
func intPtrFrom(v any) *int {
	if f, ok := toFloat(v); ok {
		n := int(math.Round(f))
		return &n
	}
	return nil
}

// intVal returns the rounded integer value of the first present key among keys,
// or 0 when none is a number.
func intVal(obj map[string]any, keys ...string) int {
	if f, ok := toFloat(firstVal(obj, keys...)); ok {
		return int(math.Round(f))
	}
	return 0
}
