//go:build integration

package ingest_test

import (
	"encoding/binary"
	"os/exec"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/ingest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the IPTC/XMP metadata the upload
// pipeline reads out of a file and writes into the catalogue's credit and
// file-technical columns.

// xmpPacket is the XMP an exporter writes into a JPEG: an IPTC headline (the
// subject), a dc:subject keyword bag holding a duplicate (which normalisation must
// drop), a creator, a rights statement, usage terms, the creating tool and a GPano
// projection. It exercises the scalar-vs-list reading of Subject/Headline in the
// one place it actually matters — a real file.
const xmpPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:xmpRights="http://ns.adobe.com/xap/1.0/rights/"
    xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/"
    xmlns:GPano="http://ns.google.com/photos/1.0/panorama/"
    xmp:CreatorTool="Adobe Lightroom 12.4"
    photoshop:Headline="Summer holiday at the lake"
    GPano:ProjectionType="equirectangular">
   <dc:subject>
    <rdf:Bag>
     <rdf:li>lake</rdf:li>
     <rdf:li>summer</rdf:li>
     <rdf:li>lake</rdf:li>
    </rdf:Bag>
   </dc:subject>
   <dc:creator>
    <rdf:Seq><rdf:li>Jan Novák</rdf:li></rdf:Seq>
   </dc:creator>
   <dc:rights>
    <rdf:Alt><rdf:li xml:lang="x-default">© 2023 Jan Novák</rdf:li></rdf:Alt>
   </dc:rights>
   <xmpRights:UsageTerms>
    <rdf:Alt><rdf:li xml:lang="x-default">CC BY-NC 4.0</rdf:li></rdf:Alt>
   </xmpRights:UsageTerms>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// xmpNamespace is the identifier that opens an XMP APP1 segment.
const xmpNamespace = "http://ns.adobe.com/xap/1.0/\x00"

// requireExiftool skips the test when exiftool is not installed: it is the only
// reader that understands XMP (the pure-Go fallback reads baseline EXIF only), so
// without it there is nothing to assert.
func requireExiftool(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed; XMP extraction has no reader")
	}
}

// xmpJPEG returns a JPEG carrying xmpPacket, by splicing an APP1 XMP segment in
// right after the SOI marker — which is exactly where an exporter puts it.
func xmpJPEG(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	raw := jpegBytes(t, r, g, b, 90)

	payload := append([]byte(xmpNamespace), xmpPacket...)
	segment := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(payload)+2))
	segment = append(segment, payload...)

	out := make([]byte, 0, len(raw)+len(segment))
	out = append(out, raw[:2]...) // SOI
	out = append(out, segment...)
	return append(out, raw[2:]...)
}

// TestIngest_readsIPTCMetadata verifies an upload's IPTC/XMP tags land in the
// catalogue's credit and file-technical columns: the headline as the subject, the
// dc:subject bag as de-duplicated keywords, the credit fields, the creating tool,
// the panorama projection and the still-image codec. The uploaded file name is kept
// as original_name (the storage layout renames the file), and `scan` stays false —
// it is a flag the user sets, never something extraction infers.
func TestIngest_readsIPTCMetadata(t *testing.T) {
	requireExiftool(t)
	env := newEnv(t, config.DuplicateConfig{})
	ctx := t.Context()

	res := env.ingest(ctx, xmpJPEG(t, 200, 50, 50), "IMG_0042.jpg")
	if res.Outcome != ingest.OutcomeCreated {
		t.Fatalf("result = %+v, want created", res)
	}
	photo, err := env.store.GetByUID(ctx, res.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	fields := []struct {
		name string
		got  string
		want string
	}{
		{"subject", photo.Subject, "Summer holiday at the lake"},
		{"keywords", photo.Keywords, "lake,summer"},
		{"artist", photo.Artist, "Jan Novák"},
		{"copyright", photo.Copyright, "© 2023 Jan Novák"},
		{"license", photo.License, "CC BY-NC 4.0"},
		{"software", photo.Software, "Adobe Lightroom 12.4"},
		{"projection", photo.Projection, "equirectangular"},
		{"image_codec", photo.ImageCodec, "jpeg"},
		{"original_name", photo.OriginalName, "IMG_0042.jpg"},
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
	if photo.Scan {
		t.Error("scan = true; extraction must never infer it")
	}
	if photo.MetadataExtractedAt == nil {
		t.Error("metadata_extracted_at is nil; ingest read the file, so the photo is done")
	}
}

// TestIngest_plainFileHasNoIPTC verifies a file with no IPTC/XMP at all is not a
// problem: the credit columns stay empty rather than picking up junk, the codec is
// still derived, and the photo is still marked as read so the backfill leaves it
// alone.
func TestIngest_plainFileHasNoIPTC(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{})
	ctx := t.Context()

	res := env.ingest(ctx, jpegBytes(t, 30, 160, 90, 88), "plain.jpg")
	if res.Outcome != ingest.OutcomeCreated {
		t.Fatalf("result = %+v, want created", res)
	}
	photo, err := env.store.GetByUID(ctx, res.PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	if photo.Subject != "" || photo.Keywords != "" || photo.Artist != "" ||
		photo.Copyright != "" || photo.License != "" || photo.Projection != "" {
		t.Errorf("credit fields should be empty, got %+v", photo)
	}
	if photo.ImageCodec != "jpeg" {
		t.Errorf("image_codec = %q, want jpeg", photo.ImageCodec)
	}
	if photo.MetadataExtractedAt == nil {
		t.Error("metadata_extracted_at is nil; a file with nothing to say has still been read")
	}
}
