package exif

import (
	"encoding/json"
	"testing"
)

// iptcJSON is a realistic exiftool `-json -n` record for a photo edited in
// Lightroom and exported with full IPTC/XMP credit metadata: the subject arrives
// as a scalar IPTC headline, the keywords as an XMP list, and the colour space and
// compression as the bare numbers `-n` produces.
const iptcJSON = `[{
  "SourceFile": "sample.jpg",
  "MIMEType": "image/jpeg",
  "Subject": "Summer holiday at the lake",
  "Keywords": ["lake", "summer", "holiday", "lake"],
  "Artist": "Jan Novák",
  "Copyright": "© 2023 Jan Novák",
  "UsageTerms": "CC BY-NC 4.0",
  "Software": "Adobe Lightroom 12.4",
  "SerialNumber": "SN-12345678",
  "ICCProfileName": "Display P3",
  "Compression": 6,
  "ColorSpace": 65535,
  "ProjectionType": "equirectangular"
}]`

// parseIPTC decodes an exiftool record and runs the IPTC/technical mapping over
// it, returning the resulting Metadata. It is the table-driven tests' entry point.
func parseIPTC(t *testing.T, doc string) Metadata {
	t.Helper()
	meta, err := parseExiftoolJSON([]byte(doc))
	if err != nil {
		t.Fatalf("parseExiftoolJSON() error = %v", err)
	}
	return meta
}

// TestParseExiftoolJSON_iptc checks that a full IPTC/XMP record maps onto every
// credit and technical field, with the numeric ColorSpace and Compression readings
// rendered as names rather than digits.
func TestParseExiftoolJSON_iptc(t *testing.T) {
	t.Parallel()

	meta := parseIPTC(t, iptcJSON)
	tests := []struct {
		field string
		got   string
		want  string
	}{
		{"Subject", meta.Subject, "Summer holiday at the lake"},
		{"Keywords", meta.Keywords, "lake,summer,holiday"},
		{"Artist", meta.Artist, "Jan Novák"},
		{"Copyright", meta.Copyright, "© 2023 Jan Novák"},
		{"License", meta.License, "CC BY-NC 4.0"},
		{"Software", meta.Software, "Adobe Lightroom 12.4"},
		{"CameraSerial", meta.CameraSerial, "SN-12345678"},
		{"ColorProfile", meta.ColorProfile, "Display P3"},
		{"ImageCodec", meta.ImageCodec, "jpeg"},
		{"Projection", meta.Projection, "equirectangular"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
		}
	}
}

// TestParseExiftoolJSON_subjectScalarVsList covers the one genuinely ambiguous
// mapping: `Subject` is written both as an IPTC headline (a scalar sentence, which
// is a subject) and as an XMP dc:subject keyword list (which is keywords). The
// shape of the value decides, and a list Subject must never leak into the subject
// column — nor a scalar one into keywords.
func TestParseExiftoolJSON_subjectScalarVsList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		doc          string
		wantSubject  string
		wantKeywords string
	}{
		{
			name:         "scalar subject is a headline",
			doc:          `[{"Subject": "Sunset over the lake"}]`,
			wantSubject:  "Sunset over the lake",
			wantKeywords: "",
		},
		{
			name:         "list subject is keywords",
			doc:          `[{"Subject": ["sunset", "lake"]}]`,
			wantSubject:  "",
			wantKeywords: "sunset,lake",
		},
		{
			name:         "list subject falls through to Headline for the subject",
			doc:          `[{"Subject": ["sunset"], "Headline": "Sunset over the lake"}]`,
			wantSubject:  "Sunset over the lake",
			wantKeywords: "sunset",
		},
		{
			name:         "explicit Keywords outrank a list Subject",
			doc:          `[{"Subject": ["sunset"], "Keywords": ["lake", "dusk"]}]`,
			wantSubject:  "",
			wantKeywords: "lake,dusk",
		},
		{
			name:         "scalar subject leaves keywords to XPKeywords",
			doc:          `[{"Subject": "Headline", "XPKeywords": "lake;dusk"}]`,
			wantSubject:  "Headline",
			wantKeywords: "lake,dusk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			meta := parseIPTC(t, tt.doc)
			if meta.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", meta.Subject, tt.wantSubject)
			}
			if meta.Keywords != tt.wantKeywords {
				t.Errorf("Keywords = %q, want %q", meta.Keywords, tt.wantKeywords)
			}
		})
	}
}

// TestKeywordsFrom covers keyword normalisation: lists and separated scalars both
// collapse onto one comma-separated string, trimmed, de-duplicated and in the
// writer's order, with junk readings dropped.
func TestKeywordsFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "absent", doc: `{}`, want: ""},
		{name: "list", doc: `{"Keywords": ["lake", "summer"]}`, want: "lake,summer"},
		{name: "scalar comma list", doc: `{"Keywords": "lake, summer , holiday"}`, want: "lake,summer,holiday"},
		{name: "semicolon separated", doc: `{"XPKeywords": "lake; summer"}`, want: "lake,summer"},
		{name: "de-duplicated, order kept", doc: `{"Keywords": ["lake", "summer", "lake"]}`, want: "lake,summer"},
		{name: "junk dropped", doc: `{"Keywords": ["lake", "unknown", "0", "  "]}`, want: "lake"},
		{name: "all junk", doc: `{"Keywords": ["unknown"]}`, want: ""},
		{name: "nested separators in a list", doc: `{"Keywords": ["lake,summer", "dusk"]}`, want: "lake,summer,dusk"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var obj map[string]any
			if err := json.Unmarshal([]byte(tt.doc), &obj); err != nil {
				t.Fatalf("unmarshaling fixture: %v", err)
			}
			if got := keywordsFrom(obj); got != tt.want {
				t.Errorf("keywordsFrom(%s) = %q, want %q", tt.doc, got, tt.want)
			}
		})
	}
}

// TestParseExiftoolJSON_fallbackChains walks each field's fallback chain: the
// first tag that carries a usable value wins, and junk readings are skipped as if
// the tag were absent.
func TestParseExiftoolJSON_fallbackChains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		doc   string
		field func(Metadata) string
		want  string
	}{
		{
			name:  "subject falls back to ObjectName",
			doc:   `[{"ObjectName": "Lake"}]`,
			field: func(m Metadata) string { return m.Subject },
			want:  "Lake",
		},
		{
			name:  "subject skips a junk Headline",
			doc:   `[{"Headline": "unknown", "XPSubject": "Lake"}]`,
			field: func(m Metadata) string { return m.Subject },
			want:  "Lake",
		},
		{
			name:  "artist falls back to Creator, joining a list",
			doc:   `[{"Creator": ["Jan Novák", "Petra Nová"]}]`,
			field: func(m Metadata) string { return m.Artist },
			want:  "Jan Novák, Petra Nová",
		},
		{
			name:  "artist falls back to By-line",
			doc:   `[{"By-line": "Jan Novák"}]`,
			field: func(m Metadata) string { return m.Artist },
			want:  "Jan Novák",
		},
		{
			name:  "copyright falls back to Rights",
			doc:   `[{"Rights": "© 2024 Studio"}]`,
			field: func(m Metadata) string { return m.Copyright },
			want:  "© 2024 Studio",
		},
		{
			name:  "copyright falls back to CopyrightNotice",
			doc:   `[{"CopyrightNotice": "© 2024 Studio"}]`,
			field: func(m Metadata) string { return m.Copyright },
			want:  "© 2024 Studio",
		},
		{
			name:  "license falls back to WebStatement",
			doc:   `[{"WebStatement": "https://example.test/licence"}]`,
			field: func(m Metadata) string { return m.License },
			want:  "https://example.test/licence",
		},
		{
			name:  "software falls back to CreatorTool",
			doc:   `[{"CreatorTool": "darktable 4.6"}]`,
			field: func(m Metadata) string { return m.Software },
			want:  "darktable 4.6",
		},
		{
			name:  "software falls back to ProcessingSoftware",
			doc:   `[{"ProcessingSoftware": "Epson Scan 2"}]`,
			field: func(m Metadata) string { return m.Software },
			want:  "Epson Scan 2",
		},
		{
			name:  "serial falls back to BodySerialNumber",
			doc:   `[{"SerialNumber": "0", "BodySerialNumber": "SN-99"}]`,
			field: func(m Metadata) string { return m.CameraSerial },
			want:  "SN-99",
		},
		{
			name:  "serial falls back to InternalSerialNumber",
			doc:   `[{"InternalSerialNumber": "SN-42"}]`,
			field: func(m Metadata) string { return m.CameraSerial },
			want:  "SN-42",
		},
		{
			name:  "colour profile falls back to ProfileDescription",
			doc:   `[{"ProfileDescription": "Apple Wide Color Sharing Profile"}]`,
			field: func(m Metadata) string { return m.ColorProfile },
			want:  "Apple Wide Color Sharing Profile",
		},
		{
			name:  "colour profile falls back to the ColorSpace name",
			doc:   `[{"ColorSpace": 1}]`,
			field: func(m Metadata) string { return m.ColorProfile },
			want:  "sRGB",
		},
		{
			name:  "an unreadable ColorSpace code yields nothing",
			doc:   `[{"ColorSpace": 4}]`,
			field: func(m Metadata) string { return m.ColorProfile },
			want:  "",
		},
		{
			name:  "projection is empty for an ordinary photo",
			doc:   `[{"MIMEType": "image/jpeg"}]`,
			field: func(m Metadata) string { return m.Projection },
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.field(parseIPTC(t, tt.doc)); got != tt.want {
				t.Errorf("field = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCodecFrom checks the image-codec chain: the JPEG compression codes, the
// container exiftool detected, the MIME fallback, every vendor RAW collapsing onto
// "raw", and a video yielding nothing (its compression lives in video_codec).
func TestCodecFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		mime string
		want string
	}{
		{name: "numeric jpeg compression", doc: `{"Compression": 6}`, mime: "image/jpeg", want: "jpeg"},
		{name: "old-style jpeg compression", doc: `{"Compression": 7}`, mime: "", want: "jpeg"},
		{name: "uncompressed falls through", doc: `{"Compression": 1, "FileType": "TIFF"}`, want: "tiff"},
		{name: "descriptive compression", doc: `{"Compression": "JPEG (old-style)"}`, want: "jpeg"},
		{name: "file type", doc: `{"FileType": "HEIC"}`, mime: "image/heic", want: "heic"},
		{name: "file type extension", doc: `{"FileTypeExtension": "webp"}`, want: "webp"},
		{name: "raw file type", doc: `{"FileType": "CR3"}`, want: "raw"},
		{name: "raw mime", doc: `{}`, mime: "image/x-canon-cr2", want: "raw"},
		{name: "mime fallback", doc: `{}`, mime: "image/png", want: "png"},
		{name: "avif mime", doc: `{}`, mime: "image/avif", want: "avif"},
		{name: "video yields nothing", doc: `{"FileType": "MP4"}`, mime: "video/mp4", want: ""},
		{name: "nothing at all", doc: `{}`, mime: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var obj map[string]any
			if err := json.Unmarshal([]byte(tt.doc), &obj); err != nil {
				t.Fatalf("unmarshaling fixture: %v", err)
			}
			if got := codecFrom(obj, tt.mime); got != tt.want {
				t.Errorf("codecFrom(%s, %q) = %q, want %q", tt.doc, tt.mime, got, tt.want)
			}
		})
	}
}

// TestCleanText covers the junk filter: a value is trimmed, and the placeholders a
// writer leaves behind when it has nothing to say are dropped rather than stored.
func TestCleanText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "trimmed", in: "  Jan Novák \n", want: "Jan Novák"},
		{name: "empty", in: "   ", want: ""},
		{name: "unknown", in: "unknown", want: ""},
		{name: "unknown any case", in: "Unknown", want: ""},
		{name: "zero", in: "0", want: ""},
		{name: "a real zero-ish value survives", in: "0.0", want: "0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanText(tt.in); got != tt.want {
				t.Errorf("cleanText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
