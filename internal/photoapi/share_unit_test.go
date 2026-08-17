package photoapi

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestShareManifestFiles_originalsAndPreviews proves the manifest states what each
// selected photo is as a file: an ordinary photo and a video keep their original
// name and stored type, while a RAW original is marked for its JPEG preview and
// renamed accordingly — the phone library would make nothing of a .cr2.
func TestShareManifestFiles_originalsAndPreviews(t *testing.T) {
	t.Parallel()

	got := shareManifestFiles([]photos.Photo{
		{UID: "p1", FileName: "beach.jpg", FileMime: "image/jpeg", FileSize: 2048},
		{UID: "p2", FileName: "clip.mp4", FileMime: "video/mp4", FileSize: 4096},
		{UID: "p3", FileName: "IMG_0007.CR2", FileMime: "image/x-canon-cr2", FileSize: 26_000_000},
	})

	want := []shareManifestFile{
		{UID: "p1", Name: "beach.jpg", Mime: "image/jpeg", Size: 2048, Preview: false},
		{UID: "p2", Name: "clip.mp4", Mime: "video/mp4", Size: 4096, Preview: false},
		{UID: "p3", Name: "IMG_0007.jpg", Mime: "image/jpeg", Size: 26_000_000, Preview: true},
	}
	if len(got) != len(want) {
		t.Fatalf("manifest holds %d files, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestShareManifestFiles_namesAreUniqueAndSafe proves two photos sharing a file
// name arrive under two names (a phone showing one of two files is worse than a
// suffix), and that a name carrying directory components or control characters is
// reduced to a plain base name before it leaves the server.
func TestShareManifestFiles_namesAreUniqueAndSafe(t *testing.T) {
	t.Parallel()

	got := shareManifestFiles([]photos.Photo{
		{UID: "p1", FileName: "IMG.jpg", FileMime: "image/jpeg"},
		{UID: "p2", FileName: "IMG.jpg", FileMime: "image/jpeg"},
		{UID: "p3", FileName: "IMG.jpg", FileMime: "image/jpeg"},
		{UID: "p4", FileName: "../../etc/pas\nswd.jpg", FileMime: "image/jpeg"},
		{UID: "p5", FileName: "", FileMime: "image/jpeg"},
	})

	wantNames := []string{"IMG.jpg", "IMG (2).jpg", "IMG (3).jpg", "passwd.jpg", "file"}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Errorf("file[%d].Name = %q, want %q", i, got[i].Name, want)
		}
	}
}

// TestShareMime covers the label a shared file carries: a preview is JPEG whatever
// the RAW original claimed, an original keeps its stored type, and a row that never
// recorded one falls back to the generic binary type rather than an empty string,
// which a share sheet may refuse.
func TestShareMime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		photo   photos.Photo
		preview bool
		want    string
	}{
		{name: "jpeg original", photo: photos.Photo{FileMime: "image/jpeg"}, want: "image/jpeg"},
		{name: "video original", photo: photos.Photo{FileMime: "video/quicktime"}, want: "video/quicktime"},
		{
			name:    "raw preview is jpeg",
			photo:   photos.Photo{FileMime: "image/x-canon-cr2"},
			preview: true,
			want:    "image/jpeg",
		},
		{name: "unknown falls back", photo: photos.Photo{}, want: "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shareMime(tt.photo, tt.preview); got != tt.want {
				t.Errorf("shareMime(%+v, %v) = %q, want %q", tt.photo, tt.preview, got, tt.want)
			}
		})
	}
}

// TestShareManifestFiles_empty proves an empty selection yields an empty (never
// nil) list, so the JSON body is always `{"files":[]}` and a client never has to
// distinguish an absent field from an empty one.
func TestShareManifestFiles_empty(t *testing.T) {
	t.Parallel()
	if got := shareManifestFiles(nil); got == nil || len(got) != 0 {
		t.Errorf("shareManifestFiles(nil) = %+v, want an empty non-nil slice", got)
	}
}
