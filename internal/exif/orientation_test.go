package exif

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// tinyJPEG encodes a 4x2 JPEG with no metadata segments of any kind — Go's
// encoder writes SOI straight into the tables, so the fixture starts out with
// neither an EXIF nor an XMP packet and every test splices in exactly what it
// wants to read back.
func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

// app1 wraps payload in an APP1 segment with its two-byte big-endian length.
func app1(payload []byte) []byte {
	segment := make([]byte, 4, 4+len(payload))
	segment[0], segment[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	return append(segment, payload...)
}

// exifPayload builds the payload of an EXIF APP1 segment holding nothing but the
// orientation tag: big-endian TIFF header, one IFD entry (0x0112, SHORT), no next
// IFD. Written by hand so the fixtures stay pure Go.
func exifPayload(orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM")                                   // big-endian
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))    // TIFF magic
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))     // offset of IFD0
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))     // one entry
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x112)) // Orientation
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))     // type SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))     // count
	_ = binary.Write(&tiff, binary.BigEndian, orientation)   // value
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))     // value padding
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))     // no next IFD
	return append([]byte("Exif\x00\x00"), tiff.Bytes()...)
}

// xmpPayload builds the payload of an XMP APP1 segment around one rdf:Description
// body, wrapped the way a real writer wraps it (xpacket processing instructions
// included, since the reader has to survive them).
func xmpPayload(body string) []byte {
	packet := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		body +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	return append([]byte(xmpSignature), packet...)
}

// attributeXMP is the attribute serialisation of tiff:Orientation, which is what
// the production batch that exposed the defect carried.
func attributeXMP(orientation string) []byte {
	return xmpPayload(`<rdf:Description rdf:about="" xmlns:tiff="http://ns.adobe.com/tiff/1.0/" ` +
		`tiff:Orientation="` + orientation + `"/>`)
}

// elementXMP is the element serialisation of the same property.
func elementXMP(orientation string) []byte {
	return xmpPayload(`<rdf:Description rdf:about="" xmlns:tiff="http://ns.adobe.com/tiff/1.0/">` +
		`<tiff:Orientation>` + orientation + `</tiff:Orientation></rdf:Description>`)
}

// writeJPEGWith writes the tiny JPEG with the given segments spliced in right
// after the SOI marker and returns its path.
func writeJPEGWith(t *testing.T, segments ...[]byte) string {
	t.Helper()
	base := tinyJPEG(t)
	out := append([]byte{}, base[:2]...)
	for _, segment := range segments {
		out = append(out, segment...)
	}
	out = append(out, base[2:]...)
	path := filepath.Join(t.TempDir(), "img.jpg")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestFileOrientation_sources covers where an orientation may live and who wins:
// EXIF when it is there, the XMP packet when it is not (both serialisations), and
// nothing at all when neither says anything. The XMP-only cases are the regression
// for the production defect — those files have no EXIF block whatsoever, and read
// as 0 they were sent to the face detector lying on their side.
func TestFileOrientation_sources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		segments [][]byte
		want     int
	}{
		{"no metadata at all", nil, 0},
		{"exif only", [][]byte{app1(exifPayload(6))}, 6},
		{"xmp attribute form, no exif segment", [][]byte{app1(attributeXMP("8"))}, 8},
		{"xmp element form, no exif segment", [][]byte{app1(elementXMP("6"))}, 6},
		{"exif wins over xmp", [][]byte{app1(exifPayload(3)), app1(attributeXMP("8"))}, 3},
		{"xmp read past a leading exif segment without orientation",
			[][]byte{app1([]byte("Exif\x00\x00")), app1(elementXMP("8"))}, 8},
		{"xmp value out of range", [][]byte{app1(attributeXMP("9"))}, 0},
		{"xmp value not a number", [][]byte{app1(attributeXMP("Rotate 270 CW"))}, 0},
		{"unrelated app1 segment", [][]byte{app1([]byte("http://ns.adobe.com/xmp/extension/\x00junk"))}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeJPEGWith(t, tt.segments...)
			if got := FileOrientation(path); got != tt.want {
				t.Errorf("FileOrientation(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

// TestFileOrientation_malformed proves a broken file is answered with "nothing to
// read" rather than a panic: a bad packet must never take the face-detect job down.
func TestFileOrientation_malformed(t *testing.T) {
	t.Parallel()

	truncated := app1(attributeXMP("8"))
	truncated = truncated[:len(truncated)-20] // the segment promises more than it holds

	shortLength := append([]byte{}, app1(attributeXMP("8"))...)
	binary.BigEndian.PutUint16(shortLength[2:], 1) // a length that cannot hold its own field

	tests := []struct {
		name    string
		content []byte
	}{
		{"empty file", nil},
		{"not a jpeg", []byte("PNG-ish bytes, definitely not a JPEG")},
		{"soi and nothing else", []byte{0xFF, 0xD8}},
		{"segment marker without a length", []byte{0xFF, 0xD8, 0xFF, 0xE1}},
		{"truncated xmp segment", append([]byte{0xFF, 0xD8}, truncated...)},
		{"impossible segment length", append([]byte{0xFF, 0xD8}, shortLength...)},
		{"xmp packet is not xml", append([]byte{0xFF, 0xD8}, app1(append([]byte(xmpSignature), "<<<>"...))...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "broken.jpg")
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if got := FileOrientation(path); got != 0 {
				t.Errorf("FileOrientation(%s) = %d, want 0", tt.name, got)
			}
		})
	}
}

// TestFileOrientation_unopenable returns 0 for a file that is not there, since the
// caller's question ("does this file say anything?") has an answer even then.
func TestFileOrientation_unopenable(t *testing.T) {
	t.Parallel()

	if got := FileOrientation(filepath.Join(t.TempDir(), "missing.jpg")); got != 0 {
		t.Errorf("FileOrientation(missing) = %d, want 0", got)
	}
}

// TestParseOrientation covers the whole value domain of an XMP property, including
// the surrounding whitespace an element-form serialisation may indent it with.
func TestParseOrientation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  int
		ok    bool
	}{
		{"1", 1, true},
		{"8", 8, true},
		{" \n 6 \t", 6, true},
		{"0", 0, false},
		{"9", 0, false},
		{"-1", 0, false},
		{"", 0, false},
		{"eight", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseOrientation(tt.value)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseOrientation(%q) = %d, %v; want %d, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}
