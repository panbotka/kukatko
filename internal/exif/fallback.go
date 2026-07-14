package exif

import (
	"fmt"
	"image"
	"math"
	"net/http"
	"os"
	"strconv"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"

	// Register the decoders used for DecodeConfig so the fallback can read pixel
	// dimensions of the formats Kukátko handles purely in Go.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// extractWithFallback reads metadata without exiftool, using image.DecodeConfig
// for dimensions/MIME and the pure-Go goexif parser for EXIF tags. It never
// returns an error: a file with no EXIF (e.g. a PNG screenshot) simply yields a
// Metadata with the geometry/MIME it could determine and zero values elsewhere.
func extractWithFallback(path string) Metadata {
	meta := Metadata{}
	meta.Mime, meta.Width, meta.Height = fileMimeAndDims(path)
	// The codec is readable from the sniffed MIME type alone, so a file with no
	// EXIF block at all (a screenshot PNG) still gets one.
	meta.ImageCodec = codecToken(meta.Mime)
	decodeExifInto(&meta, path)
	return meta
}

// fileMimeAndDims sniffs the file's MIME type from its leading bytes and reads
// its pixel dimensions via image.DecodeConfig. Width and height are 0 when the
// file is not a decodable image; MIME falls back to whatever DetectContentType
// reports.
func fileMimeAndDims(path string) (mime string, width, height int) {
	file, err := os.Open(path) //nolint:gosec // G304: path was stat'ed by Extract.
	if err != nil {
		return "", 0, 0
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, 512)
	n, _ := file.Read(head)
	mime = http.DetectContentType(head[:n])

	if _, err := file.Seek(0, 0); err != nil {
		return mime, 0, 0
	}
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return mime, 0, 0
	}
	return mime, cfg.Width, cfg.Height
}

// decodeExifInto opens path, decodes its EXIF block and fills the EXIF-derived
// fields of meta. A missing or unreadable EXIF block is silently ignored, per
// the package's tolerance contract.
func decodeExifInto(meta *Metadata, path string) {
	file, err := os.Open(path) //nolint:gosec // G304: path was stat'ed by Extract.
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	decoded, err := exif.Decode(file)
	if err != nil {
		return
	}
	exifCamera(meta, decoded)
	exifExposure(meta, decoded)
	exifGeometry(meta, decoded)
	exifGPS(meta, decoded)
	exifTime(meta, decoded)
	exifIPTC(meta, decoded)
	meta.Exif = walkExif(decoded)
}

// exifIPTC fills the credit and technical fields the pure-Go parser can actually
// reach. goexif reads the baseline TIFF/EXIF tags only: it has no IPTC or XMP
// segment parser, so the subject, keywords, licence, camera serial and projection
// have no source here and are deliberately left empty rather than guessed at from
// a neighbouring tag. Whatever the file really says about them is read on the
// exiftool path (and by the metadata backfill, which re-runs it).
func exifIPTC(meta *Metadata, x *exif.Exif) {
	meta.Artist = cleanText(tagString(x, exif.Artist))
	meta.Copyright = cleanText(tagString(x, exif.Copyright))
	meta.Software = cleanText(tagString(x, exif.Software))
	if space, ok := tagInt(x, exif.ColorSpace); ok {
		meta.ColorProfile = colorSpaceName(strconv.Itoa(space))
	}
}

// exifCamera fills the camera/lens identity from goexif string tags.
func exifCamera(meta *Metadata, x *exif.Exif) {
	meta.CameraMake = tagString(x, exif.Make)
	meta.CameraModel = tagString(x, exif.Model)
	meta.LensModel = tagString(x, exif.LensModel)
}

// exifExposure fills ISO, aperture, shutter and focal-length from goexif tags.
func exifExposure(meta *Metadata, x *exif.Exif) {
	if iso, ok := tagInt(x, exif.ISOSpeedRatings); ok {
		meta.ISO = &iso
	}
	if f, ok := tagRat(x, exif.FNumber); ok {
		meta.Aperture = &f
	}
	if f, ok := tagRat(x, exif.FocalLength); ok {
		meta.FocalLength = &f
	}
	meta.Exposure = exposureFromTag(x)
}

// exifGeometry fills orientation from the EXIF tag (dimensions already come from
// DecodeConfig, which is authoritative for the actual pixel buffer).
func exifGeometry(meta *Metadata, x *exif.Exif) {
	if o, ok := tagInt(x, exif.Orientation); ok {
		meta.Orientation = o
	}
}

// exifGPS fills decimal latitude, longitude and altitude from goexif GPS tags.
func exifGPS(meta *Metadata, x *exif.Exif) {
	meta.Lat = gpsCoordFromTags(x, exif.GPSLatitude, exif.GPSLatitudeRef)
	meta.Lng = gpsCoordFromTags(x, exif.GPSLongitude, exif.GPSLongitudeRef)
	if alt, ok := gpsAltitudeFromTags(x); ok {
		meta.Altitude = &alt
	}
}

// exifTime sets TakenAt from DateTimeOriginal, leaving it nil when absent or
// unparseable so the caller can try the filename.
func exifTime(meta *Metadata, x *exif.Exif) {
	tag, err := x.Get(exif.DateTimeOriginal)
	if err != nil {
		return
	}
	raw, err := tag.StringVal()
	if err != nil {
		return
	}
	if when, ok := parseExifTime(raw); ok {
		meta.TakenAt = &when
	}
}

// gpsCoordFromTags converts a three-rational GPS coordinate plus its hemisphere
// reference tag to signed decimal degrees, returning nil when either tag is
// missing or malformed.
func gpsCoordFromTags(x *exif.Exif, coord, ref exif.FieldName) *float64 {
	tag, err := x.Get(coord)
	if err != nil {
		return nil
	}
	degrees, dOK := ratAt(tag, 0)
	minutes, mOK := ratAt(tag, 1)
	seconds, sOK := ratAt(tag, 2)
	if !dOK || !mOK || !sOK {
		return nil
	}
	value := applyHemisphere(dmsToDecimal(degrees, minutes, seconds), tagString(x, ref))
	return &value
}

// gpsAltitudeFromTags returns the signed altitude in metres and true when a
// GPSAltitude tag is present; a GPSAltitudeRef of 1 marks below sea level.
func gpsAltitudeFromTags(x *exif.Exif) (float64, bool) {
	tag, err := x.Get(exif.GPSAltitude)
	if err != nil {
		return 0, false
	}
	alt, ok := ratAt(tag, 0)
	if !ok {
		return 0, false
	}
	if ref, refOK := tagInt(x, exif.GPSAltitudeRef); refOK && ref == 1 {
		alt = -math.Abs(alt)
	}
	return alt, true
}

// exposureFromTag renders ExposureTime as a display string ("1/125"), or "" when
// the tag is missing.
func exposureFromTag(x *exif.Exif) string {
	tag, err := x.Get(exif.ExposureTime)
	if err != nil {
		return ""
	}
	num, den, err := tag.Rat2(0)
	if err != nil || den == 0 {
		return ""
	}
	if num != 1 {
		// Reduce so e.g. 10/1250 still displays as the conventional 1/125.
		if g := gcd(absInt64(num), absInt64(den)); g > 1 {
			num, den = num/g, den/g
		}
	}
	if den == 1 {
		return strconv.FormatInt(num, 10)
	}
	return fmt.Sprintf("%d/%d", num, den)
}

// walkExif collects every EXIF tag into a JSON-able map of field name to its
// string representation, suitable for storage as the raw EXIF document.
func walkExif(x *exif.Exif) map[string]any {
	collector := exifCollector{out: make(map[string]any)}
	_ = x.Walk(&collector)
	if len(collector.out) == 0 {
		return nil
	}
	return collector.out
}

// exifCollector implements exif.Walker, accumulating each tag's string form.
type exifCollector struct {
	out map[string]any
}

// Walk records one tag's name and string representation. It never errors, so the
// full document is always collected.
func (c *exifCollector) Walk(name exif.FieldName, tag *tiff.Tag) error {
	c.out[string(name)] = tag.String()
	return nil
}

// tagString returns the string value of the named tag, or "" when missing.
func tagString(x *exif.Exif, name exif.FieldName) string {
	tag, err := x.Get(name)
	if err != nil {
		return ""
	}
	value, err := tag.StringVal()
	if err != nil {
		return ""
	}
	return value
}

// tagInt returns the first integer component of the named tag and true, or false
// when the tag is missing or not integral.
func tagInt(x *exif.Exif, name exif.FieldName) (int, bool) {
	tag, err := x.Get(name)
	if err != nil {
		return 0, false
	}
	value, err := tag.Int(0)
	if err != nil {
		return 0, false
	}
	return value, true
}

// tagRat returns the first rational component of the named tag as a float64 and
// true, or false when the tag is missing or not rational.
func tagRat(x *exif.Exif, name exif.FieldName) (float64, bool) {
	tag, err := x.Get(name)
	if err != nil {
		return 0, false
	}
	return ratAt(tag, 0)
}

// ratAt returns component i of a rational tag as a float64 and true, or false
// when the component is absent or has a zero denominator.
func ratAt(tag *tiff.Tag, i int) (float64, bool) {
	num, den, err := tag.Rat2(i)
	if err != nil || den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}

// gcd returns the greatest common divisor of a and b via Euclid's algorithm.
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// absInt64 returns the absolute value of n.
func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
