package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sidecar"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestChooseMIME verifies the EXIF type is preferred and storage is the fallback.
func TestChooseMIME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, exifMime, storedMime, want string
	}{
		{"exif wins", "image/heic", "application/octet-stream", "image/heic"},
		{"fallback to stored", "", "image/jpeg", "image/jpeg"},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := chooseMIME(tt.exifMime, tt.storedMime); got != tt.want {
				t.Errorf("chooseMIME(%q, %q) = %q, want %q", tt.exifMime, tt.storedMime, got, tt.want)
			}
		})
	}
}

// TestOrientationOrDefault verifies out-of-range orientations clamp to 1.
func TestOrientationOrDefault(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want int }{
		{0, 1}, {-3, 1}, {9, 1}, {1, 1}, {6, 6}, {8, 8},
	}
	for _, tt := range tests {
		if got := orientationOrDefault(tt.in); got != tt.want {
			t.Errorf("orientationOrDefault(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestTakenAtSource verifies an empty source becomes "unknown".
func TestTakenAtSource(t *testing.T) {
	t.Parallel()
	if got := takenAtSource(""); got != string(exif.SourceUnknown) {
		t.Errorf("takenAtSource(\"\") = %q, want %q", got, exif.SourceUnknown)
	}
	if got := takenAtSource(exif.SourceExif); got != "exif" {
		t.Errorf("takenAtSource(exif) = %q, want exif", got)
	}
}

// TestMarshalExif verifies nil/empty documents become nil and a populated map
// round-trips to JSON.
func TestMarshalExif(t *testing.T) {
	t.Parallel()
	if got := marshalExif(nil); got != nil {
		t.Errorf("marshalExif(nil) = %q, want nil", got)
	}
	if got := marshalExif(map[string]any{}); got != nil {
		t.Errorf("marshalExif(empty) = %q, want nil", got)
	}
	got := marshalExif(map[string]any{"Make": "Canon"})
	if string(got) != `{"Make":"Canon"}` {
		t.Errorf("marshalExif = %q, want canonical JSON", got)
	}
}

// TestBuildPhoto verifies the metadata-to-Photo mapping, including the uploader
// pointer and derived filename.
func TestBuildPhoto(t *testing.T) {
	t.Parallel()
	iso := 200
	stored := storage.StoredFile{
		Hash: "abc123", RelPath: "2024/05/pic.jpg", Size: 999, MIME: "image/jpeg",
	}
	meta := exif.Metadata{
		Width: 4000, Height: 3000, Orientation: 6, TakenAtSource: exif.SourceExif,
		CameraMake: "Canon", ISO: &iso, Mime: "",
	}
	p := buildPhoto(stored, mediaMeta{kind: photos.MediaImage, shared: meta}, "holiday/IMG_0042.JPG", "ph_user")

	if p.FileName != "pic.jpg" {
		t.Errorf("FileName = %q, want pic.jpg", p.FileName)
	}
	// The storage layout renamed the file; original_name keeps what it arrived as,
	// reduced to its base name, and image_codec comes off the resolved MIME type.
	if p.OriginalName != "IMG_0042.JPG" {
		t.Errorf("OriginalName = %q, want IMG_0042.JPG", p.OriginalName)
	}
	if p.ImageCodec != "jpeg" {
		t.Errorf("ImageCodec = %q, want jpeg", p.ImageCodec)
	}
	if p.FileHash != "abc123" || p.FilePath != "2024/05/pic.jpg" || p.FileSize != 999 {
		t.Errorf("file fields mismatch: %+v", p)
	}
	if p.FileMime != "image/jpeg" || p.FileOrientation != 6 || p.TakenAtSource != "exif" {
		t.Errorf("derived fields mismatch: %+v", p)
	}
	if p.MediaType != photos.MediaImage {
		t.Errorf("MediaType = %q, want image", p.MediaType)
	}
	if p.ISO == nil || *p.ISO != 200 {
		t.Errorf("ISO not mapped: %+v", p.ISO)
	}
	if p.UploadedBy == nil || *p.UploadedBy != "ph_user" {
		t.Errorf("UploadedBy = %v, want ph_user", p.UploadedBy)
	}
}

// TestBuildPhoto_video verifies the video media type and the video-only fields
// (duration, codecs, audio, fps) are mapped onto the photo.
func TestBuildPhoto_video(t *testing.T) {
	t.Parallel()
	dur, fps := 5312, 29.97
	stored := storage.StoredFile{Hash: "vid1", RelPath: "2024/05/clip.mp4", Size: 4242, MIME: "video/mp4"}
	media := mediaMeta{
		kind:   photos.MediaVideo,
		shared: exif.Metadata{Width: 1280, Height: 720, Mime: "video/mp4", TakenAtSource: exif.SourceExif},
		video: &videoFields{
			durationMs: &dur, videoCodec: "h264", audioCodec: "aac", hasAudio: true, fps: &fps,
		},
	}
	p := buildPhoto(stored, media, "clip.mp4", "")

	if p.MediaType != photos.MediaVideo {
		t.Errorf("MediaType = %q, want video", p.MediaType)
	}
	// A video's compression belongs in video_codec; image_codec stays empty.
	if p.ImageCodec != "" {
		t.Errorf("ImageCodec = %q, want empty for a video", p.ImageCodec)
	}
	if p.FileMime != "video/mp4" || p.FileWidth != 1280 || p.FileHeight != 720 {
		t.Errorf("file fields mismatch: %+v", p)
	}
	if p.DurationMs == nil || *p.DurationMs != 5312 {
		t.Errorf("DurationMs = %v, want 5312", p.DurationMs)
	}
	if p.VideoCodec != "h264" || p.AudioCodec != "aac" || !p.HasAudio {
		t.Errorf("codec/audio fields mismatch: %+v", p)
	}
	if p.FPS == nil || *p.FPS != 29.97 {
		t.Errorf("FPS = %v, want 29.97", p.FPS)
	}
	// Orientation defaults to 1 (no transform) for a poster-derived video frame.
	if p.FileOrientation != 1 {
		t.Errorf("FileOrientation = %d, want 1", p.FileOrientation)
	}
}

// TestBuildPhoto_anonymousLeavesUploaderNil verifies an empty uploader yields a
// nil pointer (SQL NULL) rather than a pointer to "".
func TestBuildPhoto_anonymousLeavesUploaderNil(t *testing.T) {
	t.Parallel()
	p := buildPhoto(storage.StoredFile{RelPath: "a/b.jpg"}, mediaMeta{}, "", "")
	if p.UploadedBy != nil {
		t.Errorf("UploadedBy = %v, want nil for anonymous upload", p.UploadedBy)
	}
	if p.MediaType != photos.MediaImage {
		t.Errorf("MediaType = %q, want image (default)", p.MediaType)
	}
	// A bare stream has no name at all — the column stays empty rather than ".".
	if p.OriginalName != "" {
		t.Errorf("OriginalName = %q, want empty for an unnamed upload", p.OriginalName)
	}
}

// TestResultConstructors verifies the per-file status and outcome mapping.
func TestResultConstructors(t *testing.T) {
	t.Parallel()
	if r := createdResult("a.jpg", "ph1", nil); r.Status != http.StatusCreated || r.Outcome != OutcomeCreated {
		t.Errorf("createdResult = %+v", r)
	}
	if r := duplicateResult("a.jpg", "ph1"); r.Status != http.StatusConflict || r.Outcome != OutcomeDuplicate {
		t.Errorf("duplicateResult = %+v", r)
	}
	if r := errorResult("a.jpg", ErrFileTooLarge); r.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("errorResult(too large) status = %d, want 413", r.Status)
	}
	if r := errorResult("a.jpg", errors.New("boom")); r.Status != http.StatusInternalServerError {
		t.Errorf("errorResult(generic) status = %d, want 500", r.Status)
	}
}

// TestFirstErr verifies the first non-nil error is returned.
func TestFirstErr(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if got := firstErr(nil, nil); got != nil {
		t.Errorf("firstErr(nil, nil) = %v, want nil", got)
	}
	if got := firstErr(nil, boom, errors.New("second")); !errors.Is(got, boom) {
		t.Errorf("firstErr = %v, want boom", got)
	}
}

// TestStage_hashesAndSizes verifies staging computes the correct SHA256 and
// byte count and writes a removable temp file.
func TestStage_hashesAndSizes(t *testing.T) {
	t.Parallel()
	svc := New(Config{TempDir: t.TempDir()})
	payload := "the quick brown fox"
	staged, err := svc.stage(context.Background(), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	defer staged.cleanup()

	sum := sha256.Sum256([]byte(payload))
	if staged.hash != hex.EncodeToString(sum[:]) {
		t.Errorf("hash = %q, want %q", staged.hash, hex.EncodeToString(sum[:]))
	}
	if staged.size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", staged.size, len(payload))
	}
}

// TestStage_rejectsOversize verifies a file over the cap is rejected with
// ErrFileTooLarge and leaves no temp file behind.
func TestStage_rejectsOversize(t *testing.T) {
	t.Parallel()
	svc := New(Config{TempDir: t.TempDir(), MaxFileSize: 4})
	_, err := svc.stage(context.Background(), strings.NewReader("too many bytes"))
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("stage err = %v, want ErrFileTooLarge", err)
	}
}

// TestStage_cancelledContext verifies a cancelled context aborts the stream.
func TestStage_cancelledContext(t *testing.T) {
	t.Parallel()
	svc := New(Config{TempDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.stage(ctx, strings.NewReader("data")); err == nil {
		t.Fatal("stage with cancelled context = nil error, want cancellation")
	}
}

// passthrough is an identity middleware standing in for the auth write guard.
func passthrough(next http.Handler) http.Handler { return next }

// TestHandleUpload_rejectsNonMultipart verifies a non-multipart request is a
// 400 before any ingest work happens (the nil Service is never reached).
func TestHandleUpload_rejectsNonMultipart(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil, passthrough, nil)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/upload", strings.NewReader("not multipart"),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	api.handleUpload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestHandleUpload_rejectsNoFiles verifies a multipart body with only form
// fields (no file parts) is a 400.
func TestHandleUpload_rejectsNoFiles(t *testing.T) {
	t.Parallel()
	body := "--bnd\r\nContent-Disposition: form-data; name=\"caption\"\r\n\r\nhello\r\n--bnd--\r\n"
	api := NewAPI(nil, passthrough, nil)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/upload", strings.NewReader(body),
	)
	req.Header.Set("Content-Type", `multipart/form-data; boundary=bnd`)
	rec := httptest.NewRecorder()

	api.handleUpload(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for no file parts", rec.Code)
	}
}

// compile-time assertion that NopEnqueuer satisfies the JobEnqueuer interface.
var _ JobEnqueuer = NopEnqueuer{}

// TestApplySidecar_strippedExif is the Google Takeout case: the exported JPEG
// carries no EXIF at all, so the sidecar beside it is the photo's whole history —
// its capture time (which also decides the YYYY/MM the original is filed under),
// its caption and its GPS.
func TestApplySidecar_strippedExif(t *testing.T) {
	t.Parallel()

	taken := time.Date(2016, 6, 6, 18, 2, 22, 0, time.UTC)
	media := mediaMeta{kind: photos.MediaImage, shared: exif.Metadata{TakenAtSource: exif.SourceUnknown}}
	applySidecar(&media, &sidecar.Metadata{
		Source:      sidecar.SourceGoogle,
		TakenAt:     &taken,
		Title:       "Lipno",
		Description: "Sunset over Lipno",
		Lat:         new(48.6417),
		Lng:         new(14.0453),
	})

	if media.shared.TakenAt == nil || !media.shared.TakenAt.Equal(taken) {
		t.Fatalf("TakenAt = %v, want %v", media.shared.TakenAt, taken)
	}
	if media.shared.TakenAtSource != exif.SourceSidecar {
		t.Errorf("TakenAtSource = %q, want %q", media.shared.TakenAtSource, exif.SourceSidecar)
	}

	photo := buildPhoto(storage.StoredFile{RelPath: "2016/06/a.jpg", Hash: "h"}, media, "a.jpg", "")
	if photo.Title != "Lipno" || photo.Description != "Sunset over Lipno" {
		t.Errorf("title = %q, description = %q, want the sidecar's caption fields",
			photo.Title, photo.Description)
	}
	if photo.TakenAt == nil || !photo.TakenAt.Equal(taken) {
		t.Errorf("photo taken_at = %v, want the sidecar's %v", photo.TakenAt, taken)
	}
	if photo.TakenAtSource != string(exif.SourceSidecar) {
		t.Errorf("photo taken_at_source = %q, want sidecar", photo.TakenAtSource)
	}
	if photo.Lat == nil || *photo.Lat != 48.6417 {
		t.Errorf("photo lat = %v, want the sidecar's fix", photo.Lat)
	}
}

// TestApplySidecar_none leaves a plain upload exactly as the file described
// itself.
func TestApplySidecar_none(t *testing.T) {
	t.Parallel()

	taken := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	media := mediaMeta{
		kind:   photos.MediaImage,
		shared: exif.Metadata{TakenAt: &taken, TakenAtSource: exif.SourceExif},
	}
	applySidecar(&media, nil)

	photo := buildPhoto(storage.StoredFile{RelPath: "2020/01/a.jpg", Hash: "h"}, media, "a.jpg", "")
	if photo.TakenAtSource != string(exif.SourceExif) || photo.Title != "" || photo.Description != "" {
		t.Errorf("a file with no sidecar was changed: %+v", photo)
	}
}
