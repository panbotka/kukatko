//go:build integration

package processing_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the evidence query itself — the single
// round trip behind the per-photo processing report — against the real schema.

// TestEvidence_unknownPhoto checks the query's not-found contract, which is what
// turns a bad uid into a 404 rather than an empty report.
func TestEvidence_unknownPhoto(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	_, err := processing.NewStore(db.Pool()).Evidence(t.Context(), "nobody")
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Evidence error = %v, want photos.ErrPhotoNotFound", err)
	}
}

// TestEvidence_freshPhotoHasNone checks a catalogued photo nothing has run on
// yet reads as no evidence at all — every join misses, and the query still
// returns its row.
func TestEvidence_freshPhotoHasNone(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := photos.NewStore(db.Pool())

	photo, err := store.Create(t.Context(), photos.Photo{
		FileHash: "hash-fresh", FilePath: "2026/08/fresh.jpg", FileName: "fresh.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ev, err := processing.NewStore(db.Pool()).Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence: %v", err)
	}
	if ev.MediaType != photos.MediaImage || ev.HasGPS {
		t.Errorf("media/GPS = (%q, %v), want (image, false)", ev.MediaType, ev.HasGPS)
	}
	for name, at := range map[string]bool{
		"thumbnail": ev.ThumbnailAt != nil,
		"embedding": ev.EmbeddingAt != nil,
		"face":      ev.FaceAt != nil,
		"ocr":       ev.OCRAt != nil,
		"place":     ev.PlaceAt != nil,
		"sidecar":   ev.SidecarAt != nil,
		"hls":       ev.HLSAt != nil,
	} {
		if at {
			t.Errorf("%s evidence present on a fresh photo", name)
		}
	}
}

// TestEvidence_hlsIsTheNewestRendition checks the streaming column: it reports
// when the video was last encoded, and it is an aggregate — a video encoded into
// several qualities must still produce exactly one report row, not one per
// rendition.
func TestEvidence_hlsIsTheNewestRendition(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	photo, err := photos.NewStore(db.Pool()).Create(t.Context(), photos.Photo{
		FileHash: "hash-hls", FilePath: "2026/08/clip.mp4", FileName: "clip.mp4",
		FileSize: 1, FileMime: "video/mp4", MediaType: photos.MediaVideo,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store := processing.NewStore(db.Pool())

	before, err := store.Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence before the encode: %v", err)
	}
	if before.HLSAt != nil {
		t.Errorf("HLSAt = %v on a video nothing encoded yet, want none", before.HLSAt)
	}

	renditions := hlsjob.NewStore(db.Pool())
	for _, name := range []string{"1080p", "720p"} {
		if _, err := renditions.Save(t.Context(), hlsjob.Encoded{
			PhotoUID: photo.UID, Rendition: name, Playlist: "#EXTM3U\n",
			Width: 1920, Height: 1080, Bandwidth: 6_128_000,
			Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 2, DurationMs: 10_000,
		}); err != nil {
			t.Fatalf("recording rendition %s: %v", name, err)
		}
	}

	after, err := store.Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence after the encode: %v", err)
	}
	if after.HLSAt == nil {
		t.Fatal("HLSAt = none for a video with two recorded renditions")
	}
}

// TestEvidence_thumbnailNeedsTheThumbnails is the regression of the interrupted
// upload (2026-10-05): the pHash row alone — which the upload pipeline writes
// before it renders anything — must not read as thumbnails, and the thumbnails
// stamp alone is not the whole step either. Only both together are done.
func TestEvidence_thumbnailNeedsTheThumbnails(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	photoStore := photos.NewStore(db.Pool())
	store := processing.NewStore(db.Pool())

	photo, err := photoStore.Create(t.Context(), photos.Photo{
		FileHash: "hash-thumb", FilePath: "2026/10/thumb.jpg", FileName: "thumb.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	thumbnailAt := func(stage string) bool {
		t.Helper()
		ev, err := store.Evidence(t.Context(), photo.UID)
		if err != nil {
			t.Fatalf("Evidence %s: %v", stage, err)
		}
		return ev.ThumbnailAt != nil
	}

	if err := photoStore.MarkThumbnailsBuilt(t.Context(), photo.UID); err != nil {
		t.Fatalf("MarkThumbnailsBuilt: %v", err)
	}
	if thumbnailAt("with thumbnails but no pHash") {
		t.Error("thumbnail step done without its perceptual hashes")
	}

	other, err := photoStore.Create(t.Context(), photos.Photo{
		FileHash: "hash-thumb-2", FilePath: "2026/10/thumb2.jpg", FileName: "thumb2.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := photoStore.SetPhash(t.Context(), photos.Phash{PhotoUID: other.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	ev, err := store.Evidence(t.Context(), other.UID)
	if err != nil {
		t.Fatalf("Evidence: %v", err)
	}
	if ev.ThumbnailAt != nil {
		t.Error("thumbnail step done on a pHash alone — the interrupted-upload lie")
	}

	if err := photoStore.SetPhash(t.Context(), photos.Phash{PhotoUID: photo.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	if !thumbnailAt("with thumbnails and pHash") {
		t.Error("thumbnail step not done with both thumbnails and pHash in place")
	}
}

// TestLibraryReads checks the two library-wide reads of the gap scan against the
// real schema: the evidence walk covers live photos only, and the unfinished
// steps name a photo's queued job types and ignore completed ones.
func TestLibraryReads(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	photoStore := photos.NewStore(db.Pool())
	store := processing.NewStore(db.Pool())
	ctx := t.Context()

	live, err := photoStore.Create(ctx, photos.Photo{
		FileHash: "hash-live", FilePath: "2026/10/live.jpg", FileName: "live.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create live: %v", err)
	}
	archived, err := photoStore.Create(ctx, photos.Photo{
		FileHash: "hash-archived", FilePath: "2026/10/archived.jpg", FileName: "archived.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create archived: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE photos SET archived_at = now() WHERE uid = $1`, archived.UID); err != nil {
		t.Fatalf("archiving: %v", err)
	}

	var walked []string
	if err := store.EachLiveEvidence(ctx, func(uid string, ev processing.Evidence) error {
		walked = append(walked, uid)
		if ev.MediaType != photos.MediaImage {
			t.Errorf("%s media = %q, want image", uid, ev.MediaType)
		}
		return nil
	}); err != nil {
		t.Fatalf("EachLiveEvidence: %v", err)
	}
	if len(walked) != 1 || walked[0] != live.UID {
		t.Errorf("walked %v, want only the live photo %s", walked, live.UID)
	}

	enq := jobs.NewEnqueuer(jobs.NewStore(db.Pool()))
	if err := enq.EnqueueImageEmbed(ctx, live.UID); err != nil {
		t.Fatalf("EnqueueImageEmbed: %v", err)
	}
	if err := enq.EnqueueFaceDetect(ctx, live.UID); err != nil {
		t.Fatalf("EnqueueFaceDetect: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE jobs SET state = 'done' WHERE type = 'face_detect' AND payload ->> 'photo_uid' = $1`,
		live.UID); err != nil {
		t.Fatalf("completing the face job: %v", err)
	}
	unfinished, err := store.UnfinishedSteps(ctx)
	if err != nil {
		t.Fatalf("UnfinishedSteps: %v", err)
	}
	got := unfinished[live.UID]
	if !got[processing.StepImageEmbed] || got[processing.StepFaceDetect] || len(got) != 1 {
		t.Errorf("unfinished steps of %s = %v, want only image_embed", live.UID, got)
	}
}
