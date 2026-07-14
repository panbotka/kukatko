package exif

import (
	"strconv"
	"strings"
)

// junkValues are the placeholder readings a camera, a scanner driver or a
// careless exporter writes into a text tag when it has nothing to say. They are
// worse than an empty column — they look like data — so they are dropped at the
// point of extraction rather than filtered on the way out.
var junkValues = []string{"unknown", "0"}

// keywordSeparators are the characters a writer may use between keywords inside a
// single scalar tag: IPTC keywords are comma-separated, the Windows XPKeywords tag
// is conventionally semicolon-separated.
const keywordSeparators = ",;"

// codecTokens maps a substring of a codec-ish tag value (an exiftool FileType, a
// Compression description, a MIME type) onto the short lowercase token stored in
// photos.image_codec. The order matters: the first match wins, so the longer
// spelling of a pair ("tiff" before "tif") comes first.
var codecTokens = []struct{ match, token string }{
	{"jpeg", "jpeg"},
	{"jpg", "jpeg"},
	{"heic", "heic"},
	{"heif", "heic"},
	{"avif", "avif"},
	{"webp", "webp"},
	{"png", "png"},
	{"tiff", "tiff"},
	{"tif", "tiff"},
	{"gif", "gif"},
	{"bmp", "bmp"},
}

// rawCodecMarkers are the vendor RAW file extensions (as they appear in an
// exiftool FileType or inside a RAW MIME type such as "image/x-canon-cr2"). They
// all collapse onto the single "raw" token: which vendor's RAW it is belongs to
// the file's MIME type, not to the codec column.
var rawCodecMarkers = []string{
	"cr2", "cr3", "crw", "nef", "nrw", "arw", "sr2", "srf", "dng", "orf",
	"rw2", "raf", "pef", "srw", "x3f", "3fr", "erf", "kdc", "mrw", "iiq", "raw",
}

// jpegCompressions are the numeric EXIF Compression values that mean JPEG. In
// `-n` mode exiftool reports Compression as a number, so the descriptive form
// ("JPEG (old-style)") that codecToken would recognise never arrives; the codes
// are 6 (JPEG), 7 (JPEG, old-style) and 34892 (lossy JPEG, DNG previews).
var jpegCompressions = map[string]string{"6": "jpeg", "7": "jpeg", "34892": "jpeg"}

// colorSpaces maps the numeric EXIF ColorSpace tag (as `-n` reports it) onto the
// profile name stored in photos.color_profile. Anything else numeric is a code
// nobody can read, and yields nothing rather than a misleading digit.
var colorSpaces = map[string]string{"1": "sRGB", "2": "Adobe RGB", "65535": "Uncalibrated"}

// cleanText trims s and drops it when it carries no information — empty, or one of
// the junk placeholders. It returns the cleaned value, or "" when there is nothing
// worth storing.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	for _, junk := range junkValues {
		if strings.EqualFold(s, junk) {
			return ""
		}
	}
	return s
}

// textValue renders one exiftool tag value as display text: a scalar (string or
// number) becomes itself, and a list becomes its items joined with ", " — an XMP
// dc:creator with two photographers is two names, not one of them. The result is
// cleaned, so a junk value yields "".
func textValue(raw any) string {
	items, ok := raw.([]any)
	if !ok {
		return cleanText(strVal(raw))
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if s := cleanText(strVal(item)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// firstText returns the textValue of the first key that carries one, in the order
// given — the fallback chain of a field ("Artist", then "Creator", then
// "By-line"). It returns "" when no key holds a usable value.
func firstText(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := textValue(obj[key]); s != "" {
			return s
		}
	}
	return ""
}

// firstScalar is firstText restricted to scalar values: a list-valued tag is
// skipped rather than joined. It is what separates the two readings of `Subject`,
// which is written both as an IPTC headline (a scalar sentence about the photo)
// and as an XMP dc:subject keyword list — only the scalar form is a subject, the
// list form is keywords and is picked up by keywordsFrom.
func firstScalar(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if _, isList := obj[key].([]any); isList {
			continue
		}
		if s := cleanText(strVal(obj[key])); s != "" {
			return s
		}
	}
	return ""
}

// keywordsFrom reads the photo's keywords as a single comma-separated string:
// `Keywords` first, then a *list-valued* `Subject` (XMP dc:subject — a scalar
// Subject is a headline, see firstScalar), then the Windows `XPKeywords` tag. The
// first tag that yields any keyword wins.
func keywordsFrom(obj map[string]any) string {
	if kw := joinKeywords(keywordValues(obj["Keywords"])); kw != "" {
		return kw
	}
	if list, isList := obj["Subject"].([]any); isList {
		if kw := joinKeywords(keywordValues(list)); kw != "" {
			return kw
		}
	}
	return joinKeywords(keywordValues(obj["XPKeywords"]))
}

// keywordValues splits one exiftool tag value into individual keywords. exiftool
// hands them over either as a list (XMP dc:subject, IPTC Keywords with several
// entries) or as one scalar string holding a separated list ("lake, summer" —
// which is also the shape of a single-entry IPTC tag), so both are accepted.
func keywordValues(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return splitKeywords(strVal(raw))
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, splitKeywords(strVal(item))...)
	}
	return values
}

// splitKeywords cuts a scalar keyword string on the recognised separators and
// returns the non-junk parts, trimmed.
func splitKeywords(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(keywordSeparators, r)
	})
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := cleanText(part); cleaned != "" {
			values = append(values, cleaned)
		}
	}
	return values
}

// joinKeywords renders keywords as the comma-separated string stored in
// photos.keywords, de-duplicated while preserving the writer's order — the source
// file's own sequence is information (the first keyword is usually the main
// subject), so the list is never sorted.
func joinKeywords(values []string) string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return strings.Join(unique, ",")
}

// colorProfileFrom names the image's colour profile: the embedded ICC profile's
// name when the file carries one, otherwise the EXIF ColorSpace tag rendered as a
// name ("sRGB") rather than the raw numeric code.
func colorProfileFrom(obj map[string]any) string {
	if name := firstText(obj, "ICCProfileName", "ProfileDescription"); name != "" {
		return name
	}
	return colorSpaceName(strVal(obj["ColorSpace"]))
}

// colorSpaceName renders an EXIF ColorSpace reading as a profile name. A known
// numeric code becomes its name; an unknown numeric code yields "" (a bare digit
// in the column would be worse than nothing); a non-numeric reading is exiftool's
// own descriptive output and passes through cleaned.
func colorSpaceName(raw string) string {
	value := cleanText(raw)
	if name, known := colorSpaces[value]; known {
		return name
	}
	if _, numeric := strconv.Atoi(value); numeric == nil {
		return ""
	}
	return value
}

// codecFrom derives the still image's codec token from the tag object: the EXIF
// Compression tag first, then the container exiftool detected (FileType), and
// finally the MIME type. An unrecognised reading yields "" rather than a guess —
// videos land here too, and their compression belongs in video_codec.
func codecFrom(obj map[string]any, mime string) string {
	if token := compressionCodec(strVal(obj["Compression"])); token != "" {
		return token
	}
	if token := codecToken(firstText(obj, "FileType", "FileTypeExtension")); token != "" {
		return token
	}
	return codecToken(mime)
}

// compressionCodec reads the EXIF Compression tag, which arrives as a bare number
// under exiftool's `-n` and as a description otherwise. Only the JPEG codes carry
// codec information worth storing; every other value (1 = uncompressed, and the
// vendor-specific codes) says nothing the FileType does not say better.
func compressionCodec(raw string) string {
	value := cleanText(raw)
	if token, jpeg := jpegCompressions[value]; jpeg {
		return token
	}
	if _, numeric := strconv.Atoi(value); numeric == nil {
		return ""
	}
	return codecToken(value)
}

// codecToken normalises any codec-ish spelling — an exiftool FileType ("HEIC"), a
// MIME type ("image/x-canon-cr2"), a compression description ("JPEG (old-style)")
// — onto the short lowercase token stored in photos.image_codec, or "" when it
// recognises nothing. Every vendor RAW collapses onto "raw".
func codecToken(s string) string {
	value := strings.ToLower(cleanText(s))
	if value == "" {
		return ""
	}
	for _, candidate := range codecTokens {
		if strings.Contains(value, candidate.match) {
			return candidate.token
		}
	}
	for _, marker := range rawCodecMarkers {
		if strings.Contains(value, marker) {
			return "raw"
		}
	}
	return ""
}
