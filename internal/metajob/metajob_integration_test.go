//go:build integration

package metajob_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/metajob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.
//
// They cover the backfill end to end over real originals on a real FS store: a
// photo catalogued before extraction existed (empty columns, no extraction marker)
// gets its metadata filled from its own file, a second run changes nothing, and a
// value the user typed is never clobbered by an empty extraction.

// xmpPacket is the XMP an exporter writes into a JPEG — the same shape the ingest
// tests use: an IPTC headline, a dc:subject keyword bag with a duplicate, the
// credit fields and the creating tool.
const xmpPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/"
    xmp:CreatorTool="Adobe Lightroom 12.4"
    photoshop:Headline="Summer holiday at the lake">
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
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// requireExiftool skips the test when exiftool is not installed: it is the only
// reader that understands XMP, so without it there is nothing to assert.
func requireExiftool(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed; XMP extraction has no reader")
	}
}

// xmpJPEG returns a small JPEG carrying xmpPacket in an APP1 segment spliced in
// right after the SOI marker, which is where an exporter puts it.
func xmpJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 80, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	raw := buf.Bytes()

	payload := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmpPacket...)
	segment := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(payload)+2))
	segment = append(segment, payload...)

	out := make([]byte, 0, len(raw)+len(segment))
	out = append(out, raw[:2]...)
	out = append(out, segment...)
	return append(out, raw[2:]...)
}

// testEnv bundles a metadata service over a freshly truncated database and an FS
// store holding real originals.
type testEnv struct {
	svc   *metajob.Service
	store *photos.Store
	root  string
}

// newEnv builds a metadata service wired to a real FS store and photo repository
// over the integration database, with the backfill collaborators supplied so both
// the job handler and the backfill can be exercised.
func newEnv(t *testing.T, enq metajob.Enqueuer) *testEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	store := photos.NewStore(db.Pool())
	svc := metajob.New(metajob.Config{
		Photos:    store,
		Extractor: metajob.NewStorageExtractor(fs),
		Lister:    store,
		Enqueuer:  enq,
	})
	return &testEnv{svc: svc, store: store, root: root}
}

// seedLegacyPhoto writes an original into the store's layout and catalogues it the
// way a pre-extraction row looks: the metadata columns empty and no extraction
// marker, so the backfill sees it as pending. Any of the columns can be pre-filled
// via edit, standing in for a value the user typed.
func (e *testEnv) seedLegacyPhoto(t *testing.T, hash string, edit func(*photos.Photo)) photos.Photo {
	t.Helper()
	relPath := filepath.Join("2023", "06", hash+".jpg")
	abs := filepath.Join(e.root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, xmpJPEG(t), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	photo := photos.Photo{
		FileHash: hash,
		FilePath: "2023/06/" + hash + ".jpg",
		FileName: hash + ".jpg",
		FileMime: "image/jpeg",
	}
	if edit != nil {
		edit(&photo)
	}
	created, err := e.store.Create(t.Context(), photo)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.MetadataExtractedAt != nil {
		t.Fatal("a seeded legacy photo must not be marked as extracted")
	}
	return created
}

// TestReextract_fillsLegacyPhoto verifies the job reads a catalogued photo's own
// original and fills the metadata columns it never had — and that a second run
// changes nothing at all, including updated_at.
func TestReextract_fillsLegacyPhoto(t *testing.T) {
	requireExiftool(t)
	env := newEnv(t, nil)
	ctx := t.Context()
	photo := env.seedLegacyPhoto(t, "aa11", nil)

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	filled, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	fields := []struct{ name, got, want string }{
		{"subject", filled.Subject, "Summer holiday at the lake"},
		{"keywords", filled.Keywords, "lake,summer"},
		{"artist", filled.Artist, "Jan Novák"},
		{"copyright", filled.Copyright, "© 2023 Jan Novák"},
		{"software", filled.Software, "Adobe Lightroom 12.4"},
		{"image_codec", filled.ImageCodec, "jpeg"},
		{"original_name", filled.OriginalName, "aa11.jpg"},
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
	if filled.MetadataExtractedAt == nil {
		t.Fatal("metadata_extracted_at is nil; the file has been read")
	}

	// Second run: idempotent. Nothing may change, not even updated_at.
	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("second Reextract: %v", err)
	}
	again, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("second GetByUID: %v", err)
	}
	if !again.UpdatedAt.Equal(filled.UpdatedAt) {
		t.Errorf("updated_at moved on a no-op re-run: %v → %v", filled.UpdatedAt, again.UpdatedAt)
	}
	if again.Subject != filled.Subject || again.Keywords != filled.Keywords {
		t.Errorf("a second run rewrote the metadata: %+v", again)
	}
}

// TestReextract_neverClobbersUserEdits verifies the fill is a gap-filler: a value
// the user typed survives an extraction that would have written a different one,
// and the curation fields the job has no business touching are left alone.
func TestReextract_neverClobbersUserEdits(t *testing.T) {
	requireExiftool(t)
	env := newEnv(t, nil)
	ctx := t.Context()

	taken := time.Date(2019, 4, 1, 12, 0, 0, 0, time.UTC)
	photo := env.seedLegacyPhoto(t, "bb22", func(p *photos.Photo) {
		p.Subject = "What I actually called it"
		p.Title = "My title"
		p.Description = "My description"
		p.TakenAt = &taken
		p.TakenAtSource = "manual"
	})

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	got, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	if got.Subject != "What I actually called it" {
		t.Errorf("subject = %q; the user's value was clobbered", got.Subject)
	}
	if got.Keywords != "lake,summer" {
		t.Errorf("keywords = %q; the empty column should still have been filled", got.Keywords)
	}
	if got.Title != "My title" || got.Description != "My description" {
		t.Errorf("captions touched: %q / %q", got.Title, got.Description)
	}
	if got.TakenAt == nil || !got.TakenAt.Equal(taken) || got.TakenAtSource != "manual" {
		t.Errorf("capture time touched: %v / %q", got.TakenAt, got.TakenAtSource)
	}
}

// TestReextract_missingOriginalIsSkipped verifies a photo whose original is gone
// from storage is logged and skipped: the job succeeds (it must not dead-letter,
// and a library-wide backfill must not stop on it) and nothing is written.
func TestReextract_missingOriginalIsSkipped(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()

	created, err := env.store.Create(ctx, photos.Photo{
		FileHash: "cc33", FilePath: "2023/06/gone.jpg", FileName: "gone.jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := env.svc.Reextract(ctx, created.UID); err != nil {
		t.Fatalf("Reextract over a missing original = %v, want nil (skip)", err)
	}
	got, err := env.store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.MetadataExtractedAt != nil {
		t.Error("a photo whose file is gone must stay unread, not be marked as extracted")
	}
}

// recordingEnqueuer records the uids a backfill scheduled.
type recordingEnqueuer struct {
	uids []string
}

// EnqueueMetadata records uid and reports success.
func (r *recordingEnqueuer) EnqueueMetadata(_ context.Context, uid string) error {
	r.uids = append(r.uids, uid)
	return nil
}

// TestBackfillMetadata_resumes verifies the backfill is resumable and converges: it
// schedules the photos whose file has never been read, and once their jobs have run
// a second backfill enqueues nothing — while `?all=true` still re-reads everything.
func TestBackfillMetadata_resumes(t *testing.T) {
	requireExiftool(t)
	enq := &recordingEnqueuer{}
	env := newEnv(t, enq)
	ctx := t.Context()

	first := env.seedLegacyPhoto(t, "dd44", nil)
	second := env.seedLegacyPhoto(t, "ee55", nil)

	enqueued, err := env.svc.BackfillMetadata(ctx, false)
	if err != nil {
		t.Fatalf("BackfillMetadata: %v", err)
	}
	if enqueued != 2 || len(enq.uids) != 2 {
		t.Fatalf("enqueued %d (%v), want 2", enqueued, enq.uids)
	}

	// Run one photo's job — the backfill must now consider only the other pending.
	if err := env.svc.Reextract(ctx, first.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	enq.uids = nil
	enqueued, err = env.svc.BackfillMetadata(ctx, false)
	if err != nil {
		t.Fatalf("second BackfillMetadata: %v", err)
	}
	if enqueued != 1 || len(enq.uids) != 1 || enq.uids[0] != second.UID {
		t.Fatalf("resumed backfill enqueued %d (%v), want just %s", enqueued, enq.uids, second.UID)
	}

	// Drain the rest: a third run has nothing left to do.
	if err := env.svc.Reextract(ctx, second.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	enq.uids = nil
	enqueued, err = env.svc.BackfillMetadata(ctx, false)
	if err != nil {
		t.Fatalf("third BackfillMetadata: %v", err)
	}
	if enqueued != 0 {
		t.Errorf("drained backfill enqueued %d (%v), want 0", enqueued, enq.uids)
	}

	// A forced full re-run still covers the whole library.
	enq.uids = nil
	enqueued, err = env.svc.BackfillMetadata(ctx, true)
	if err != nil {
		t.Fatalf("forced BackfillMetadata: %v", err)
	}
	if enqueued != 2 {
		t.Errorf("forced backfill enqueued %d (%v), want 2", enqueued, enq.uids)
	}
}
