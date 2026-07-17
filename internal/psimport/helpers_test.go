package psimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestRemapSubject covers the nil, mapped and unmapped cases of the subject
// remapping helper.
func TestRemapSubject(t *testing.T) {
	t.Parallel()
	ps := "su_ps"
	m := map[string]string{"su_ps": "su_kk"}

	if got := remapSubject(nil, m); got != nil {
		t.Errorf("nil input = %v, want nil", got)
	}
	if got := remapSubject(&ps, m); got == nil || *got != "su_kk" {
		t.Errorf("mapped input = %v, want su_kk", got)
	}
	unknown := "su_x"
	if got := remapSubject(&unknown, m); got != nil {
		t.Errorf("unmapped input = %v, want nil", got)
	}
}

// TestMapSubjectType covers the recognised types and the default.
func TestMapSubjectType(t *testing.T) {
	t.Parallel()
	tests := map[string]people.SubjectType{
		"person":  people.SubjectPerson,
		"pet":     people.SubjectPet,
		"other":   people.SubjectOther,
		"PET":     people.SubjectPet,
		"":        people.SubjectPerson,
		"unknown": people.SubjectPerson,
	}
	for in, want := range tests {
		if got := mapSubjectType(in); got != want {
			t.Errorf("mapSubjectType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMapMarkerType covers the label case and the face default.
func TestMapMarkerType(t *testing.T) {
	t.Parallel()
	if got := mapMarkerType("label"); got != people.MarkerLabel {
		t.Errorf("label = %q, want %q", got, people.MarkerLabel)
	}
	if got := mapMarkerType("face"); got != people.MarkerFace {
		t.Errorf("face = %q, want %q", got, people.MarkerFace)
	}
	if got := mapMarkerType("weird"); got != people.MarkerFace {
		t.Errorf("default = %q, want %q", got, people.MarkerFace)
	}
}

// TestMapAlbumType covers the structural types and the manual default.
func TestMapAlbumType(t *testing.T) {
	t.Parallel()
	tests := map[string]organize.AlbumType{
		"album":  organize.AlbumManual,
		"folder": organize.AlbumFolder,
		"moment": organize.AlbumMoment,
		"state":  organize.AlbumState,
		"month":  organize.AlbumMonth,
		"":       organize.AlbumManual,
	}
	for in, want := range tests {
		if got := mapAlbumType(in); got != want {
			t.Errorf("mapAlbumType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMapLabelSource covers the recognised sources and the import default.
func TestMapLabelSource(t *testing.T) {
	t.Parallel()
	tests := map[string]organize.LabelSource{
		"manual":  organize.SourceManual,
		"ai":      organize.SourceAI,
		"import":  organize.SourceImport,
		"":        organize.SourceImport,
		"unknown": organize.SourceImport,
	}
	for in, want := range tests {
		if got := mapLabelSource(in); got != want {
			t.Errorf("mapLabelSource(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOriginalName covers the file-name, path-base and uid fallbacks.
func TestOriginalName(t *testing.T) {
	t.Parallel()
	if got := originalName(photosorter.Photo{FileName: "a.jpg"}); got != "a.jpg" {
		t.Errorf("filename = %q, want a.jpg", got)
	}
	if got := originalName(photosorter.Photo{FilePath: "/x/y/b.jpg"}); got != "b.jpg" {
		t.Errorf("path base = %q, want b.jpg", got)
	}
	if got := originalName(photosorter.Photo{UID: "ph_z"}); got != "ph_z" {
		t.Errorf("uid fallback = %q, want ph_z", got)
	}
}

// TestPhotoMime prefers photo-sorter's MIME, else the sniffed one.
func TestPhotoMime(t *testing.T) {
	t.Parallel()
	got := photoMime(photosorter.Photo{FileMime: "image/jpeg"}, storage.StoredFile{MIME: "image/png"})
	if got != "image/jpeg" {
		t.Errorf("ps mime = %q, want image/jpeg", got)
	}
	got = photoMime(photosorter.Photo{}, storage.StoredFile{MIME: "image/png"})
	if got != "image/png" {
		t.Errorf("fallback = %q, want image/png", got)
	}
}

// TestFaceModel returns the first non-empty model.
func TestFaceModel(t *testing.T) {
	t.Parallel()
	if got := faceModel(nil); got != "" {
		t.Errorf("empty = %q, want \"\"", got)
	}
	faces := []photosorter.Face{{Model: ""}, {Model: "buffalo_l"}}
	if got := faceModel(faces); got != "buffalo_l" {
		t.Errorf("first non-empty = %q, want buffalo_l", got)
	}
}

// TestBuildPhoto asserts buildPhoto carries every photo-sorter field onto the
// Kukátko photo record. The whole struct is compared, so a newly mapped (or
// silently dropped) field fails the test until it is reflected in want — the
// completeness guardrail the migration audit calls for. The file identity
// (hash/path/size) comes from the stored original, not the source row.
func TestBuildPhoto(t *testing.T) {
	t.Parallel()
	takenAt := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	archivedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	lat, lng, altitude := 1.5, 2.5, 10.0
	iso := 100
	aperture, focalLength := 1.8, 50.0
	ps := photosorter.Photo{
		UID: "ps123", FileHash: "wronghash", FilePath: "/orig/psp.jpg",
		FileName: "psp.jpg", FileSize: 999, FileMime: "image/jpeg",
		FileWidth: 800, FileHeight: 600, FileOrientation: 6,
		TakenAt: &takenAt, TakenAtSource: "exif",
		Title: "Sunset", Description: "Golden hour", Notes: "keep",
		Keywords: []string{"beach", "sunset", "beach"},
		Artist:   "Ansel", Copyright: "(c) 2023", License: "CC-BY", Software: "Lightroom",
		Scan: true, Panorama: true,
		Lat: &lat, Lng: &lng, Altitude: &altitude,
		CameraMake: "Canon", CameraModel: "R5", LensModel: "RF50",
		ISO: &iso, Aperture: &aperture, Exposure: "1/200", FocalLength: &focalLength,
		Exif: []byte(`{"k":"v"}`), Private: true, ArchivedAt: &archivedAt,
		UpdatedAt: takenAt,
	}
	stored := storage.StoredFile{Hash: "realhash", RelPath: "2024/01/x.jpg", Size: 42, MIME: "image/png"}
	psUID := "ps123"
	want := photos.Photo{
		FileHash: "realhash", FilePath: "2024/01/x.jpg", FileName: "psp.jpg",
		FileSize: 42, FileMime: "image/jpeg", FileWidth: 800, FileHeight: 600,
		FileOrientation: 6, TakenAt: &takenAt, TakenAtSource: "exif",
		Title: "Sunset", Description: "Golden hour", Notes: "keep",
		Keywords: "beach,sunset", Artist: "Ansel", Copyright: "(c) 2023",
		License: "CC-BY", Software: "Lightroom", Scan: true, Projection: "equirectangular",
		Lat: &lat, Lng: &lng, Altitude: &altitude, CameraMake: "Canon", CameraModel: "R5",
		LensModel: "RF50", ISO: &iso, Aperture: &aperture, Exposure: "1/200",
		FocalLength: &focalLength, Exif: []byte(`{"k":"v"}`), Private: true,
		ArchivedAt: &archivedAt, PhotosorterUID: &psUID,
	}
	if got := buildPhoto(ps, stored); !reflect.DeepEqual(got, want) {
		t.Errorf("buildPhoto mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}

// TestPanoramaProjection maps the boolean panorama flag onto the projection
// string.
func TestPanoramaProjection(t *testing.T) {
	t.Parallel()
	if got := panoramaProjection(true); got != "equirectangular" {
		t.Errorf("panorama true = %q, want equirectangular", got)
	}
	if got := panoramaProjection(false); got != "" {
		t.Errorf("panorama false = %q, want empty", got)
	}
}
