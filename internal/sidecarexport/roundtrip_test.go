package sidecarexport

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// fullDocument returns a Document with every single field set to a distinct
// non-zero value.
//
// "Every field" is the point: this is the fixture the round-trip test asserts
// against, and a field left at its zero value here would round-trip vacuously and
// prove nothing. When a field is added to the format, it is added here — and if
// it is forgotten, TestDocument_fixtureIsExhaustive fails.
func fullDocument() Document {
	taken := time.Date(2024, 5, 17, 14, 30, 0, 0, time.UTC)
	geocoded := time.Date(2024, 5, 18, 9, 0, 0, 0, time.UTC)
	archived := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	added := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	rated := time.Date(2024, 6, 2, 8, 0, 0, 0, time.UTC)

	return Document{
		Version:     Version,
		GeneratedAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		Identity: Identity{
			UID:          "pht000000000001",
			SHA256:       strings.Repeat("ab", 32),
			FileName:     "IMG_1234.jpg",
			FilePath:     "2024/05/IMG_1234.jpg",
			OriginalName: "DSC_0001.JPG",
			MediaType:    "image",
			UploadedBy:   "pan.botka",
			External: &External{
				PhotoprismUID:      "ppuid123",
				PhotoprismFileHash: strings.Repeat("cd", 20),
				PhotosorterUID:     "psuid456",
			},
		},
		Descriptive: Descriptive{
			Title:       "Svatba",
			Description: "Obřad na zahradě",
			Notes:       "poznámka",
			AiNote:      "an outdoor wedding ceremony",
			Subject:     "wedding",
			Keywords:    "svatba,zahrada,rodina",
			Artist:      "Jan Novák",
			Copyright:   "© 2024 Jan Novák",
			License:     "CC BY-NC 4.0",
		},
		Temporal: Temporal{
			TakenAt:       &taken,
			TakenAtSource: "exif",
			Estimated:     true,
			Note:          "kolem roku 1950",
		},
		Spatial: &Spatial{
			Lat:      new(50.0755),
			Lng:      new(14.4378),
			Altitude: new(235.5),
			Source:   "manual",
			Place: &Place{
				Country:    "Česko",
				Region:     "Praha",
				City:       "Praha",
				Name:       "Petřín",
				GeocodedAt: &geocoded,
			},
		},
		Technical: Technical{
			CameraMake:   "NIKON CORPORATION",
			CameraModel:  "NIKON D750",
			CameraSerial: "SN-12345",
			LensModel:    "35mm f/1.8",
			ISO:          new(400),
			Aperture:     new(1.8),
			Exposure:     "1/250",
			FocalLength:  new(35.0),
			Width:        6016,
			Height:       4016,
			Orientation:  1,
			FileSize:     8_388_608,
			FileMIME:     "image/jpeg",
			Software:     "Lightroom 13.2",
			Scan:         true,
			ColorProfile: "sRGB",
			ImageCodec:   "jpeg",
			Projection:   "equirectangular",
			Video: &Video{
				DurationMs: new(12_500),
				VideoCodec: "h264",
				AudioCodec: "aac",
				HasAudio:   true,
				FPS:        new(29.97),
			},
		},
		Curation: Curation{
			Albums: []Album{{UID: "alb001", Slug: "svatba", Title: "Svatba", Type: "album"}},
			Labels: []Label{{
				UID: "lbl001", Slug: "portret", Name: "Portrét",
				Priority: 5, Source: "ai", Uncertainty: 12,
			}},
			People: []Person{{
				MarkerUID:   "mrk001",
				SubjectUID:  "sub001",
				Name:        "Jana Nováková",
				SubjectType: "person",
				Type:        "face",
				Box:         Box{X: 0.25, Y: 0.1, W: 0.2, H: 0.3},
				Score:       88,
				Invalid:     true,
				Reviewed:    true,
			}},
			Favorites: []Favorite{{User: "pan.botka", UserUID: "usr001", AddedAt: &added}},
			Ratings: []Rating{{
				User: "pan.botka", UserUID: "usr001", Stars: 4, Flag: "pick", UpdatedAt: &rated,
			}},
			Private:    true,
			ArchivedAt: &archived,
			Stack:      &Stack{UID: "stk001", Primary: true},
		},
		Edit: &Edit{
			Crop:       &Box{X: 0.1, Y: 0.2, W: 0.6, H: 0.5},
			Rotation:   90,
			Brightness: 0.15,
			Contrast:   -0.25,
		},
	}
}

// TestRoundTrip is the format's contract test: it serialises a fully-populated
// document, parses it back and asserts every field survived byte-for-byte.
//
// This is the test the spec asks for and the one a future
// `kukatko restore --from-sidecars` is built against. If it passes, the file on
// disk is a sufficient record of the photo; if it fails, some piece of a user's
// curation is being written in a form that cannot be read back — which is worse
// than not writing it, because the file looks complete.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	want := fullDocument()

	data, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v\n\nYAML:\n%s", got, want, data)
	}
}

// TestDocument_fixtureIsExhaustive guards the round-trip test's fixture: it walks
// the Document type and fails on any field the fixture left at its zero value.
//
// Without this, adding a field to the format and forgetting to add it to
// fullDocument would leave TestRoundTrip passing while silently no longer
// covering the new field — the exact failure mode that lets an unreadable field
// ship.
func TestDocument_fixtureIsExhaustive(t *testing.T) {
	t.Parallel()

	if zero := zeroFields(reflect.ValueOf(fullDocument()), "Document"); len(zero) > 0 {
		t.Errorf("fullDocument leaves these fields zero, so TestRoundTrip does not cover them: %v\n"+
			"Add them to the fixture.", zero)
	}
}

// zeroFields returns the paths of every field at or under v that holds its type's
// zero value, recursing through structs, pointers and the first element of each
// slice. path names v for the report.
func zeroFields(v reflect.Value, path string) []string {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return []string{path}
		}
		return zeroFields(v.Elem(), path)
	case reflect.Slice:
		if v.Len() == 0 {
			return []string{path}
		}
		return zeroFields(v.Index(0), path+"[0]")
	case reflect.Struct:
		return zeroStructFields(v, path)
	default:
		if v.IsZero() {
			return []string{path}
		}
		return nil
	}
}

// zeroStructFields returns the zero-valued field paths of the struct v. time.Time
// is treated as a leaf rather than recursed into, since its fields are
// unexported.
func zeroStructFields(v reflect.Value, path string) []string {
	if v.Type() == reflect.TypeFor[time.Time]() {
		if v.IsZero() {
			return []string{path}
		}
		return nil
	}
	var out []string
	for i := range v.NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		out = append(out, zeroFields(v.Field(i), path+"."+field.Name)...)
	}
	return out
}

// TestMarshal_headerIsPresentAndParseable asserts the file carries the
// explanatory header and that the header does not break parsing. The header is
// the only defence against someone "fixing" the deliberate omission of the
// embeddings, so its absence is a real regression.
func TestMarshal_headerIsPresentAndParseable(t *testing.T) {
	t.Parallel()

	data, err := Marshal(fullDocument())
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	text := string(data)
	for _, want := range []string{"# Kukátko metadata sidecar.", "embedding", "face vectors", "RESTORE.md"} {
		if !strings.Contains(text, want) {
			t.Errorf("header does not mention %q; file:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "#") {
		t.Error("file does not start with the header comment")
	}
	if _, err := Unmarshal(data); err != nil {
		t.Errorf("header broke parsing: %v", err)
	}
}

// TestMarshal_omitsEmbeddings pins the deliberate omission: neither the image
// embedding nor the face vectors may appear in a sidecar, no matter how the
// format grows. They are large, binary and cheap to recompute; the backfill jobs
// exist for exactly that.
func TestMarshal_omitsEmbeddings(t *testing.T) {
	t.Parallel()

	data, err := Marshal(fullDocument())
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	// The header explains the omission, so only the body is searched for keys.
	body := string(data[strings.Index(string(data), "version:"):])
	for _, forbidden := range []string{"embedding:", "vector:", "face_vector", "halfvec"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("sidecar body contains %q; embeddings and face vectors must never be written", forbidden)
		}
	}
}

// TestMarshal_sparsePhotoStaysShort asserts a photo with nothing on it yields a
// short document rather than a wall of empty keys — the reason every group is
// omitempty. A file a human cannot skim is a file nobody reads.
func TestMarshal_sparsePhotoStaysShort(t *testing.T) {
	t.Parallel()

	doc := Document{
		Version:     Version,
		GeneratedAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		Identity:    Identity{UID: "pht1", SHA256: "abc", FileName: "a.jpg", FilePath: "2024/05/a.jpg"},
	}
	data, err := Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	body := string(data[strings.Index(string(data), "version:"):])
	for _, absent := range []string{"spatial:", "edit:", "albums:", "people:", "ratings:", "video:"} {
		if strings.Contains(body, absent) {
			t.Errorf("sparse document contains empty group %q:\n%s", absent, body)
		}
	}
}

// TestUnmarshal_rejectsGarbage asserts a file that is not YAML is an error rather
// than an empty document, so a corrupt sidecar is noticed rather than read as "a
// photo with no curation".
func TestUnmarshal_rejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := Unmarshal([]byte("\tthis: is: not: yaml: at: all\n  - [")); err == nil {
		t.Error("Unmarshal accepted garbage, want an error")
	}
}

// TestVersion_isWritten asserts the schema version is present and first, so a
// reader can dispatch on it before parsing anything else.
func TestVersion_isWritten(t *testing.T) {
	t.Parallel()

	data, err := Marshal(fullDocument())
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	body := string(data[strings.Index(string(data), "version:"):])
	if !strings.HasPrefix(body, "version: 1\n") {
		t.Errorf("document does not start with the schema version; body starts:\n%.80s", body)
	}
}
