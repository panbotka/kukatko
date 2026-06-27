package psimport

import (
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
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

// TestBuildPhoto maps the curated metadata and stamps photosorter_uid; the file
// identity comes from the stored original.
func TestBuildPhoto(t *testing.T) {
	t.Parallel()
	ps := photosorter.Photo{
		UID: "ps123", FileHash: "wronghash", FileWidth: 800, FileHeight: 600,
		FileOrientation: 6, Title: "Sunset", Private: true, TakenAtSource: "exif",
	}
	stored := storage.StoredFile{Hash: "realhash", RelPath: "2024/01/x.jpg", Size: 42, MIME: "image/jpeg"}

	got := buildPhoto(ps, stored)
	if got.FileHash != "realhash" {
		t.Errorf("FileHash = %q, want realhash (from stored)", got.FileHash)
	}
	if got.FilePath != "2024/01/x.jpg" || got.FileSize != 42 {
		t.Errorf("file identity = %q/%d, want stored values", got.FilePath, got.FileSize)
	}
	if got.Title != "Sunset" || !got.Private || got.FileOrientation != 6 {
		t.Errorf("metadata not carried: %+v", got)
	}
	if got.PhotosorterUID == nil || *got.PhotosorterUID != "ps123" {
		t.Errorf("PhotosorterUID = %v, want ps123", got.PhotosorterUID)
	}
}
