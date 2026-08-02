package ppimport

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// makeRAW registers a photo the source keeps the way it keeps every `type:raw`
// shot: the JPEG rendered from the capture is the primary file and the RAW hangs
// off the same photo as a non-primary sibling with its own hash and bytes.
func (c *fakeClient) makeRAW(uid string, updated time.Time, title string) photoprism.Photo {
	photo := c.makePhoto(uid, updated, title)
	rawHash := "hraw-" + uid
	c.files[rawHash] = []byte("raw-bytes-" + uid)
	photo.Type = "raw"
	photo.Files = append(photo.Files, photoprism.File{
		UID: "fraw-" + uid, Hash: rawHash, Mime: "image/x-canon-cr2",
		Name: uid + ".cr2", FileType: "raw", Width: 120, Height: 96,
	})
	return photo
}

// TestSiblingFiles covers which of a source photo's files the main import path
// leaves for the sibling pass: everything but the stored original and a live
// photo's already-linked motion clip, deduplicated and never a file without a
// download key.
func TestSiblingFiles(t *testing.T) {
	t.Parallel()
	jpg := photoprism.File{UID: "f1", Hash: "hjpg", Primary: true, Mime: "image/jpeg"}
	raw := photoprism.File{UID: "f2", Hash: "hraw", Mime: "image/x-canon-cr2"}
	clip := photoprism.File{UID: "f3", Hash: "hmov", Video: true, Mime: "video/quicktime"}

	tests := []struct {
		name  string
		photo photoprism.Photo
		want  []string
	}{
		{
			name:  "a single-file photo has no siblings",
			photo: photoprism.Photo{Type: "image", Files: []photoprism.File{jpg}},
			want:  nil,
		},
		{
			name:  "the RAW beside a JPEG is a sibling",
			photo: photoprism.Photo{Type: "raw", Files: []photoprism.File{jpg, raw}},
			want:  []string{"hraw"},
		},
		{
			name:  "a live photo's motion clip is not, it is already a sidecar",
			photo: photoprism.Photo{Type: "live", Files: []photoprism.File{jpg, clip}},
			want:  nil,
		},
		{
			name:  "a video's generated still is, the clip itself is the original",
			photo: photoprism.Photo{Type: "video", Files: []photoprism.File{jpg, clip}},
			want:  []string{"hjpg"},
		},
		{
			name:  "a file listed twice is returned once",
			photo: photoprism.Photo{Type: "raw", Files: []photoprism.File{jpg, raw, raw}},
			want:  []string{"hraw"},
		},
		{
			name: "a file with no hash cannot be downloaded and is dropped",
			photo: photoprism.Photo{Type: "raw", Files: []photoprism.File{
				jpg, {UID: "f4", Mime: "application/xmp"},
			}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel, ok := selectMedia(tt.photo)
			if !ok {
				t.Fatalf("selectMedia(%+v) found no importable file", tt.photo)
			}
			got := hashesOf(siblingFiles(tt.photo, sel))
			if !slices.Equal(got, tt.want) {
				t.Errorf("siblingFiles = %v, want %v", got, tt.want)
			}
		})
	}
}

// hashesOf projects files onto their hashes, nil for none, so a test can compare
// a selection by identity alone.
func hashesOf(files []photoprism.File) []string {
	if len(files) == 0 {
		return nil
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Hash
	}
	return out
}

// TestImport_rawSiblingIsStackedWithItsJPEG is the point of the sibling pass: the
// RAW of a RAW+JPEG shot is imported as its own catalogue row, grouped with the
// displayable JPEG in one stack, while a plain single-file photo in the same run
// is untouched.
func TestImport_rawSiblingIsStackedWithItsJPEG(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{
		client.makeRAW("pp1", t0, "Shot"),
		client.makePhoto("pp2", t0.Add(time.Hour), "Plain"),
	}
	h := newHarness(client)

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 2 {
		t.Fatalf("imported = %d, want 2 (one per source photo)", result.Counts.Imported)
	}
	if len(h.photos.byUID) != 3 {
		t.Fatalf("catalogue rows = %d, want 3 (JPEG + its RAW + the plain photo)", len(h.photos.byUID))
	}

	primary := h.photoFor(t, "pp1")
	raw := h.siblingFor(t, "hraw-pp1")
	assertSiblingRow(t, h, raw)
	assertStacked(t, primary, raw)
	if slices.Contains(h.enq.embeds, raw.UID) || slices.Contains(h.enq.faces, raw.UID) {
		t.Error("the RAW sibling was queued for embedding/faces; it is the primary's own shot")
	}
	if !slices.Contains(h.enq.thumbs, raw.UID) {
		t.Error("the RAW sibling was not queued for thumbnails; it still needs its own tile")
	}

	plain := h.photoFor(t, "pp2")
	if plain.StackUID != nil {
		t.Errorf("single-file photo was stacked: %v", plain.StackUID)
	}
	if got := len(h.photos.files[plain.UID]); got != 1 {
		t.Errorf("single-file photo has %d file rows, want 1", got)
	}
}

// assertSiblingRow checks the RAW is a full catalogue row of its own: its own
// primary original file, its own source file hash, and NO photoprism_uid — that
// column stays the 1:1 key of the source photo's displayable file.
func assertSiblingRow(t *testing.T, h *harness, raw photos.Photo) {
	t.Helper()
	if raw.PhotoprismUID != nil {
		t.Errorf("sibling photoprism_uid = %v, want nil", *raw.PhotoprismUID)
	}
	if raw.PhotoprismFileHash == nil || *raw.PhotoprismFileHash != "hraw-pp1" {
		t.Errorf("sibling photoprism_file_hash = %v, want hraw-pp1", raw.PhotoprismFileHash)
	}
	if raw.MediaType != photos.MediaImage {
		t.Errorf("sibling media_type = %q, want image", raw.MediaType)
	}
	if raw.FileWidth != 120 || raw.FileHeight != 96 {
		t.Errorf("sibling geometry = %dx%d, want 120x96 (the file's own)", raw.FileWidth, raw.FileHeight)
	}
	files := h.photos.files[raw.UID]
	if len(files) != 1 {
		t.Fatalf("sibling file rows = %d, want 1", len(files))
	}
	if !files[0].IsPrimary || files[0].Role != photos.RoleOriginal {
		t.Errorf("sibling file row = %+v, want a primary original", files[0])
	}
}

// assertStacked checks both rows share one stack whose primary is the displayable
// original.
func assertStacked(t *testing.T, primary, sibling photos.Photo) {
	t.Helper()
	if primary.StackUID == nil || sibling.StackUID == nil {
		t.Fatalf("not stacked: primary %v, sibling %v", primary.StackUID, sibling.StackUID)
	}
	if *primary.StackUID != *sibling.StackUID {
		t.Errorf("stacks differ: %s vs %s", *primary.StackUID, *sibling.StackUID)
	}
	if !primary.StackPrimary {
		t.Error("the JPEG is not the stack primary")
	}
	if sibling.StackPrimary {
		t.Error("the sibling is the stack primary, want the displayable JPEG")
	}
}

// photoFor returns the catalogue row imported from a PhotoPrism uid.
func (h *harness) photoFor(t *testing.T, ppUID string) photos.Photo {
	t.Helper()
	uid, ok := h.photos.byPPUID[ppUID]
	if !ok {
		t.Fatalf("photo for %s not imported", ppUID)
	}
	return h.photos.byUID[uid]
}

// siblingFor returns the catalogue row holding the source file with the given
// PhotoPrism hash.
func (h *harness) siblingFor(t *testing.T, ppFileHash string) photos.Photo {
	t.Helper()
	uid, ok := h.photos.byPPFileHash[ppFileHash]
	if !ok {
		t.Fatalf("sibling file %s not imported", ppFileHash)
	}
	return h.photos.byUID[uid]
}

// TestImport_rawSiblingRerunAddsNothing pins the idempotence the incremental
// import lives by: a second pass neither re-downloads the RAW nor duplicates its
// row, and leaves the stack it formed alone.
func TestImport_rawSiblingRerunAddsNothing(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{client.makeRAW("pp1", t0, "Shot")}
	h := newHarness(client)

	if _, err := h.svc.Import(context.Background()); err != nil {
		t.Fatalf("first import: %v", err)
	}
	rowsBefore := len(h.photos.byUID)
	downloadsBefore := h.client.downloadCount()
	stackBefore := *h.photoFor(t, "pp1").StackUID

	second, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(h.photos.byUID) != rowsBefore {
		t.Errorf("catalogue rows changed on re-run: %d -> %d", rowsBefore, len(h.photos.byUID))
	}
	if h.client.downloadCount() != downloadsBefore {
		t.Errorf("re-run re-downloaded: %d -> %d", downloadsBefore, h.client.downloadCount())
	}
	if got := *h.photoFor(t, "pp1").StackUID; got != stackBefore {
		t.Errorf("stack re-formed on re-run: %s -> %s", stackBefore, got)
	}
	if second.Counts.Imported != 0 {
		t.Errorf("re-run imported = %d, want 0", second.Counts.Imported)
	}
}

// TestImport_rawSiblingBackfilledOnRerun covers the library imported before the
// sibling pass existed: the photo is already catalogued and unchanged upstream, so
// the listing pass has nothing to do — and the run still brings its RAW across,
// reporting the photo as updated rather than skipped.
func TestImport_rawSiblingBackfilledOnRerun(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{client.makePhoto("pp1", t0, "Shot")}
	h := newHarness(client)

	if _, err := h.svc.Import(context.Background()); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// The same photo, untouched upstream, now serving the RAW it always had.
	client.photos = []photoprism.Photo{client.makeRAW("pp1", t0, "Shot")}

	second, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Counts.Updated != 1 {
		t.Errorf("updated = %d, want 1 (the photo grew a file)", second.Counts.Updated)
	}
	assertStacked(t, h.photoFor(t, "pp1"), h.siblingFor(t, "hraw-pp1"))
}

// TestImport_siblingFailureIsRecordedNotFatal verifies an undownloadable sibling
// costs the run only that file: the photo itself stays imported and is not tallied
// as failed, the loss is recorded against the file stage — and the watermark is
// held at that photo, so the next incremental run is served it again and can
// retry the file it dropped.
func TestImport_siblingFailureIsRecordedNotFatal(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{
		client.makeRAW("pp1", t0, "Shot"),
		client.makePhoto("pp2", t0.Add(time.Hour), "Later"),
	}
	client.downloadErr = map[string]error{"hraw-pp1": photoprism.ErrUnavailable}
	h := newHarness(client)

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 2 {
		t.Errorf("imported = %d, want 2 (both photos themselves)", result.Counts.Imported)
	}
	if result.Counts.Failed != 0 {
		t.Errorf("failed photos = %d, want 0 (only a file failed)", result.Counts.Failed)
	}
	if len(h.runs.failures) != 1 {
		t.Fatalf("recorded failures = %d, want 1", len(h.runs.failures))
	}
	if got := h.runs.failures[0].SourceRef; got != "hraw-pp1" {
		t.Errorf("failure source ref = %q, want the sibling's hash", got)
	}
	if got := h.runs.failures[0].Stage; got != importer.StageFile {
		t.Errorf("failure stage = %q, want file", got)
	}
	if result.Watermark == nil || !result.Watermark.Equal(t0) {
		t.Errorf("watermark = %v, want %v (held at the photo that lost a file)", result.Watermark, t0)
	}
	if h.photoFor(t, "pp1").StackUID != nil {
		t.Error("photo was stacked even though its only sibling failed")
	}
}
