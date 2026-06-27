package ppimport

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// makeVideoPhoto registers a video original (a single video file) on the client
// and returns the PhotoPrism photo for it.
func (c *fakeClient) makeVideoPhoto(uid string, updated time.Time, title string) photoprism.Photo {
	hash := "h-" + uid
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	c.files[hash] = []byte("video-" + uid)
	return photoprism.Photo{
		UID: uid, Type: "video", Title: title, TakenAt: updated, UpdatedAt: updated,
		Width: 64, Height: 64,
		Files: []photoprism.File{
			{UID: "f-" + uid, Hash: hash, Primary: true, Video: true, Mime: "video/mp4", Name: uid + ".mp4"},
		},
	}
}

// makeLivePhoto registers a still primary and a companion motion clip on the
// client and returns the PhotoPrism live photo linking them.
func (c *fakeClient) makeLivePhoto(uid string, updated time.Time, title string) photoprism.Photo {
	still := "h-" + uid
	motion := "hm-" + uid
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	c.files[still] = []byte("still-" + uid)
	c.files[motion] = []byte("motion-" + uid)
	return photoprism.Photo{
		UID: uid, Type: "live", Title: title, TakenAt: updated, UpdatedAt: updated,
		Width: 8, Height: 8,
		Files: []photoprism.File{
			{UID: "fs-" + uid, Hash: still, Primary: true, Mime: "image/jpeg", Name: uid + ".jpg"},
			{UID: "fm-" + uid, Hash: motion, Video: true, Mime: "video/mp4", Name: uid + ".mov"},
		},
	}
}

// cannedVideoMeta returns a fully populated video.Metadata for prober fakes.
func cannedVideoMeta() video.Metadata {
	dur := 1500
	fps := 30.0
	return video.Metadata{
		DurationMs: &dur, VideoCodec: "h264", AudioCodec: "aac", HasAudio: true, FPS: &fps,
	}
}

// TestSelectMedia verifies the file selection and resolved media kind for each
// PhotoPrism photo type, including the live still/motion split and the no-stream
// fallback.
func TestSelectMedia(t *testing.T) {
	t.Parallel()
	still := photoprism.File{UID: "s", Hash: "hs", Primary: true, Mime: "image/jpeg"}
	motion := photoprism.File{UID: "m", Hash: "hm", Video: true, Mime: "video/mp4"}

	tests := []struct {
		name       string
		photo      photoprism.Photo
		wantOK     bool
		wantKind   photos.MediaType
		wantOrig   string // expected original file UID
		wantMotion string // expected motion file UID, "" for none
	}{
		{
			name:   "image uses primary",
			photo:  photoprism.Photo{Type: "image", Files: []photoprism.File{still}},
			wantOK: true, wantKind: photos.MediaImage, wantOrig: "s",
		},
		{
			name:   "video uses the video file",
			photo:  photoprism.Photo{Type: "video", Files: []photoprism.File{still, motion}},
			wantOK: true, wantKind: photos.MediaVideo, wantOrig: "m",
		},
		{
			name:   "live splits still and motion",
			photo:  photoprism.Photo{Type: "live", Files: []photoprism.File{still, motion}},
			wantOK: true, wantKind: photos.MediaLive, wantOrig: "s", wantMotion: "m",
		},
		{
			name:   "animated maps to video",
			photo:  photoprism.Photo{Type: "animated", Files: []photoprism.File{motion}},
			wantOK: true, wantKind: photos.MediaVideo, wantOrig: "m",
		},
		{
			name:   "video without a stream falls back to image",
			photo:  photoprism.Photo{Type: "video", Files: []photoprism.File{still}},
			wantOK: true, wantKind: photos.MediaImage, wantOrig: "s",
		},
		{
			name:   "no importable file",
			photo:  photoprism.Photo{Type: "image", Files: []photoprism.File{{UID: "x"}}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel, ok := selectMedia(tt.photo)
			if ok != tt.wantOK {
				t.Fatalf("selectMedia ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if sel.kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", sel.kind, tt.wantKind)
			}
			if sel.original.UID != tt.wantOrig {
				t.Errorf("original = %q, want %q", sel.original.UID, tt.wantOrig)
			}
			gotMotion := ""
			if sel.motion != nil {
				gotMotion = sel.motion.UID
			}
			if gotMotion != tt.wantMotion {
				t.Errorf("motion = %q, want %q", gotMotion, tt.wantMotion)
			}
		})
	}
}

// TestImport_video verifies a video photo is catalogued as media_type video with
// the probed video metadata, external IDs, and enqueued embed/face jobs.
func TestImport_video(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{client.makeVideoPhoto("vid1", t0, "Clip")}
	h := newHarness(client)
	h.prober.meta = cannedVideoMeta()

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1", result.Counts.Imported)
	}

	uid := h.photos.byPPUID["vid1"]
	photo := h.photos.byUID[uid]
	if photo.MediaType != photos.MediaVideo {
		t.Errorf("media_type = %q, want video", photo.MediaType)
	}
	if photo.DurationMs == nil || *photo.DurationMs != 1500 {
		t.Errorf("duration_ms = %v, want 1500", photo.DurationMs)
	}
	if photo.VideoCodec != "h264" || photo.AudioCodec != "aac" || !photo.HasAudio {
		t.Errorf("video codecs = %q/%q hasAudio=%v", photo.VideoCodec, photo.AudioCodec, photo.HasAudio)
	}
	if photo.FPS == nil || *photo.FPS != 30 {
		t.Errorf("fps = %v, want 30", photo.FPS)
	}
	if photo.PhotoprismUID == nil || *photo.PhotoprismUID != "vid1" {
		t.Errorf("photoprism_uid = %v, want vid1", photo.PhotoprismUID)
	}
	if h.prober.probeCalls() != 1 {
		t.Errorf("probe calls = %d, want 1", h.prober.probeCalls())
	}
	if len(h.enq.embeds) != 1 || len(h.enq.faces) != 1 {
		t.Errorf("jobs = embeds %d faces %d, want 1/1", len(h.enq.embeds), len(h.enq.faces))
	}
}

// TestImport_livePhoto verifies a live photo stores the still as the primary
// original and the motion clip as a sidecar, downloading both files and probing
// the motion for the video metadata.
func TestImport_livePhoto(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	client.photos = []photoprism.Photo{client.makeLivePhoto("live1", t0, "Moment")}
	h := newHarness(client)
	h.prober.meta = cannedVideoMeta()

	result, err := h.svc.Import(context.Background())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 1 {
		t.Fatalf("imported = %d, want 1", result.Counts.Imported)
	}

	uid := h.photos.byPPUID["live1"]
	photo := h.photos.byUID[uid]
	if photo.MediaType != photos.MediaLive {
		t.Errorf("media_type = %q, want live", photo.MediaType)
	}
	if photo.DurationMs == nil || *photo.DurationMs != 1500 {
		t.Errorf("duration_ms = %v, want 1500 (from motion probe)", photo.DurationMs)
	}
	files := h.photos.files[uid]
	if len(files) != 2 {
		t.Fatalf("file rows = %d, want 2 (still + motion)", len(files))
	}
	assertLiveFiles(t, files)
	if h.client.downloadCount() != 2 {
		t.Errorf("downloads = %d, want 2 (still + motion)", h.client.downloadCount())
	}
}

// assertLiveFiles checks the live photo's two rows: one primary original still and
// one non-primary sidecar motion clip.
func assertLiveFiles(t *testing.T, files []photos.PhotoFile) {
	t.Helper()
	var primary, sidecar *photos.PhotoFile
	for i := range files {
		switch files[i].Role {
		case photos.RoleOriginal:
			primary = &files[i]
		case photos.RoleSidecar:
			sidecar = &files[i]
		default:
		}
	}
	if primary == nil || !primary.IsPrimary {
		t.Errorf("primary original missing or not primary: %+v", primary)
	}
	if sidecar == nil || sidecar.IsPrimary {
		t.Errorf("sidecar motion missing or marked primary: %+v", sidecar)
	}
	if sidecar != nil && sidecar.FileMime != "video/mp4" {
		t.Errorf("sidecar mime = %q, want video/mp4", sidecar.FileMime)
	}
}
