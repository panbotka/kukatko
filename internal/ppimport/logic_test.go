package ppimport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestRunState_watermark verifies the watermark advances to the latest success
// but never past the earliest failure, and is nil when nothing was processed.
func TestRunState_watermark(t *testing.T) {
	t.Parallel()
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		successes []time.Time
		failures  []time.Time
		want      *time.Time
	}{
		{name: "nothing processed", want: nil},
		{
			name:      "successes only -> max success",
			successes: []time.Time{base, base.Add(2 * time.Hour), base.Add(time.Hour)},
			want:      new(base.Add(2 * time.Hour)),
		},
		{
			name:      "early failure caps watermark",
			successes: []time.Time{base.Add(3 * time.Hour)},
			failures:  []time.Time{base.Add(time.Hour)},
			want:      new(base.Add(time.Hour)),
		},
		{
			name:      "late failure does not lower max success",
			successes: []time.Time{base.Add(time.Hour)},
			failures:  []time.Time{base.Add(5 * time.Hour)},
			want:      new(base.Add(time.Hour)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := &runState{}
			for _, s := range tt.successes {
				st.recordSuccess(s)
			}
			for _, f := range tt.failures {
				st.recordFailure(f)
			}
			got := st.watermark()
			if !timeEqual(got, tt.want) {
				t.Errorf("watermark = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMapMediaType verifies the PhotoPrism type mapping.
func TestMapMediaType(t *testing.T) {
	t.Parallel()
	cases := map[string]photos.MediaType{
		"image": photos.MediaImage, "video": photos.MediaVideo, "live": photos.MediaLive,
		"VIDEO": photos.MediaVideo, "animated": photos.MediaVideo, "raw": photos.MediaImage,
		"": photos.MediaImage,
	}
	for in, want := range cases {
		if got := mapMediaType(in); got != want {
			t.Errorf("mapMediaType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMapAlbumType verifies the PhotoPrism album-type mapping with a manual
// default for unknown types.
func TestMapAlbumType(t *testing.T) {
	t.Parallel()
	cases := map[string]organize.AlbumType{
		"folder": organize.AlbumFolder, "moment": organize.AlbumMoment,
		"month": organize.AlbumMonth, "state": organize.AlbumState,
		"album": organize.AlbumManual, "weird": organize.AlbumManual, "": organize.AlbumManual,
	}
	for in, want := range cases {
		if got := mapAlbumType(in); got != want {
			t.Errorf("mapAlbumType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLabelQuery verifies the label search expression prefers the slug and falls
// back to the name.
func TestLabelQuery(t *testing.T) {
	t.Parallel()
	if got := labelQuery("beach", "Beach"); got != `label:"beach"` {
		t.Errorf("labelQuery slug = %q", got)
	}
	if got := labelQuery("", "Sea Side"); got != `label:"Sea Side"` {
		t.Errorf("labelQuery name fallback = %q", got)
	}
}

// TestIsNamedFaceMarker verifies only valid, named face markers are imported.
func TestIsNamedFaceMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		m    photoprism.Marker
		want bool
	}{
		{name: "named face", m: photoprism.Marker{Type: "face", Name: "Bob"}, want: true},
		{name: "unnamed face", m: photoprism.Marker{Type: "face"}, want: false},
		{name: "invalid face", m: photoprism.Marker{Type: "face", Name: "Bob", Invalid: true}, want: false},
		{name: "label marker", m: photoprism.Marker{Type: "label", Name: "Tree"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isNamedFaceMarker(tt.m); got != tt.want {
				t.Errorf("isNamedFaceMarker = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOriginalName verifies the name resolution order: OriginalName, then the
// primary file's base name, then the UID.
func TestOriginalName(t *testing.T) {
	t.Parallel()
	if got := originalName(photoprism.Photo{OriginalName: "a/b/IMG.JPG"}, photoprism.File{}); got != "IMG.JPG" {
		t.Errorf("OriginalName = %q", got)
	}
	if got := originalName(photoprism.Photo{}, photoprism.File{Name: "x/y/file.heic"}); got != "file.heic" {
		t.Errorf("primary name = %q", got)
	}
	if got := originalName(photoprism.Photo{UID: "pp9"}, photoprism.File{}); got != "pp9" {
		t.Errorf("uid fallback = %q", got)
	}
}

// TestBuildPhoto_precedence verifies PhotoPrism metadata wins over file EXIF while
// the file supplies orientation, and the external IDs are set.
func TestBuildPhoto_precedence(t *testing.T) {
	t.Parallel()
	taken := time.Date(2022, 5, 4, 3, 2, 1, 0, time.UTC)
	pp := photoprism.Photo{
		UID: "ppX", Type: "video", Title: "PP Title", Description: "PP Desc",
		TakenAt: taken, Lat: 50.1, Lng: 14.4, Altitude: 200, Private: true,
		CameraMake: "PPMake", Iso: 400, FNumber: 2.8, Width: 1920, Height: 1080,
	}
	primary := photoprism.File{Hash: "sha1abc", Mime: "video/mp4"}
	stored := storage.StoredFile{Hash: "sha256xyz", RelPath: "2022/05/clip.mp4", Size: 42, MIME: "image/jpeg"}
	meta := exif.Metadata{Orientation: 6, CameraMake: "ExifMake", Width: 4000, Height: 3000}

	p := buildPhoto(pp, primary, stored, meta)
	if p.MediaType != photos.MediaVideo {
		t.Errorf("media_type = %q, want video", p.MediaType)
	}
	if p.Title != "PP Title" || p.Description != "PP Desc" || !p.Private {
		t.Errorf("metadata = %+v, want PhotoPrism values", p)
	}
	if p.TakenAt == nil || !p.TakenAt.Equal(taken) || p.TakenAtSource != string(exif.SourceExif) {
		t.Errorf("taken_at = %v / %q", p.TakenAt, p.TakenAtSource)
	}
	if p.CameraMake != "PPMake" {
		t.Errorf("camera_make = %q, want PPMake (PhotoPrism wins)", p.CameraMake)
	}
	if p.FileOrientation != 6 {
		t.Errorf("orientation = %d, want 6 (from EXIF)", p.FileOrientation)
	}
	if p.FileWidth != 1920 {
		t.Errorf("width = %d, want 1920 (PhotoPrism display dims)", p.FileWidth)
	}
	if p.FileHash != "sha256xyz" || p.PhotoprismUID == nil || *p.PhotoprismUID != "ppX" ||
		p.PhotoprismFileHash == nil || *p.PhotoprismFileHash != "sha1abc" {
		t.Errorf("ids = %s / %v / %v", p.FileHash, p.PhotoprismUID, p.PhotoprismFileHash)
	}
}

// TestBuildPhoto_exifFallback verifies the file EXIF fills capture and GPS when
// PhotoPrism has none.
func TestBuildPhoto_exifFallback(t *testing.T) {
	t.Parallel()
	taken := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	lat, lng := 10.0, 20.0
	meta := exif.Metadata{
		TakenAt: &taken, TakenAtSource: exif.SourceFilename, Lat: &lat, Lng: &lng,
	}
	p := buildPhoto(photoprism.Photo{UID: "p", Type: "image"}, photoprism.File{}, storage.StoredFile{}, meta)
	if p.TakenAt == nil || !p.TakenAt.Equal(taken) || p.TakenAtSource != string(exif.SourceFilename) {
		t.Errorf("taken_at fallback = %v / %q", p.TakenAt, p.TakenAtSource)
	}
	if p.Lat == nil || *p.Lat != lat || p.Lng == nil || *p.Lng != lng {
		t.Errorf("gps fallback = %v / %v", p.Lat, p.Lng)
	}
}

// TestMetadataUnchanged verifies the no-op detection used to skip re-imports.
func TestMetadataUnchanged(t *testing.T) {
	t.Parallel()
	taken := time.Date(2020, 2, 2, 2, 2, 2, 0, time.UTC)
	existing := photos.Photo{
		Title: "T", Description: "D", Notes: "N", Private: true,
		TakenAt: &taken, TakenAtSource: "exif",
	}
	same := photos.MetadataUpdate{
		Title: "T", Description: "D", Notes: "N", Private: true,
		TakenAt: &taken, TakenAtSource: "exif",
	}
	if !metadataUnchanged(existing, same) {
		t.Error("metadataUnchanged = false for identical metadata")
	}
	changed := same
	changed.Title = "T2"
	if metadataUnchanged(existing, changed) {
		t.Error("metadataUnchanged = true for changed title")
	}
}

// TestMetadataUpdate_preservesNotes verifies notes survive (PhotoPrism has none)
// and PhotoPrism's capture time and GPS overwrite the existing values.
func TestMetadataUpdate_preservesNotes(t *testing.T) {
	t.Parallel()
	taken := time.Date(2024, 3, 3, 3, 3, 3, 0, time.UTC)
	existing := photos.Photo{Notes: "keep me", TakenAtSource: "filename"}
	pp := photoprism.Photo{Title: "New", TakenAt: taken, Lat: 1, Lng: 2}
	u := metadataUpdate(existing, pp)
	if u.Notes != "keep me" {
		t.Errorf("notes = %q, want preserved", u.Notes)
	}
	if u.TakenAt == nil || !u.TakenAt.Equal(taken) || u.TakenAtSource != string(exif.SourceExif) {
		t.Errorf("taken_at = %v / %q", u.TakenAt, u.TakenAtSource)
	}
	if u.Lat == nil || *u.Lat != 1 || u.Lng == nil || *u.Lng != 2 {
		t.Errorf("gps = %v / %v", u.Lat, u.Lng)
	}
}

// TestJobPayload verifies the singleton payload carries the dedup sentinel.
func TestJobPayload(t *testing.T) {
	t.Parallel()
	var decoded map[string]string
	if err := json.Unmarshal(JobPayload(), &decoded); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if decoded["photo_uid"] != singletonPhotoUID {
		t.Errorf("payload photo_uid = %q, want %q", decoded["photo_uid"], singletonPhotoUID)
	}
}

// TestHandle_runsImport verifies the job handler runs an import and surfaces an
// infrastructure error.
func TestHandle_runsImport(t *testing.T) {
	t.Parallel()
	client := &fakeClient{listErr: photoprism.ErrUnavailable}
	h := newHarness(client)
	if err := h.svc.Handle(context.Background(), jobs.Job{Type: jobs.TypePPImport}); err == nil {
		t.Fatal("Handle error = nil, want listing failure propagated")
	}

	ok := newHarness(&fakeClient{})
	if err := ok.svc.Handle(context.Background(), jobs.Job{Type: jobs.TypePPImport}); err != nil {
		t.Errorf("Handle on empty source = %v, want nil", err)
	}
}

// TestNew_panicsOnNilCollaborator verifies a missing collaborator is a startup
// panic.
func TestNew_panicsOnNilCollaborator(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New did not panic on nil collaborators")
		}
	}()
	_ = New(Config{})
}
