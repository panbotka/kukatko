package mediaurl_test

import (
	"context"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// testHash is a valid lowercase hex file hash, long enough to shard the
// thumbnail cache tree.
const testHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// publishing is a storage.Storage whose URL method answers with a fixed prefix
// plus the object key, standing in for a backend that publishes its objects. Only
// URL is exercised; every other method panics so a test that accidentally reaches
// for the bytes fails loudly.
type publishing struct{ prefix string }

// URL returns the published address of relPath.
func (p publishing) URL(relPath string) string { return p.prefix + relPath + "?sig=deadbeef" }

// Store panics: the builder never writes.
func (publishing) Store(context.Context, io.Reader, time.Time, string) (storage.StoredFile, error) {
	panic("unexpected Store")
}

// Put panics: the builder never writes.
func (publishing) Put(context.Context, io.Reader, storage.StoredFile) error {
	panic("unexpected Put")
}

// Head panics: the builder never inspects objects.
func (publishing) Head(context.Context, string) (storage.StoredFile, error) {
	panic("unexpected Head")
}

// Check panics: the builder never probes the backend.
func (publishing) Check(context.Context) error { panic("unexpected Check") }

// Open panics: the builder never reads bytes.
func (publishing) Open(context.Context, string) (io.ReadCloser, error) { panic("unexpected Open") }

// Stat panics: the builder never stats.
func (publishing) Stat(context.Context, string) (os.FileInfo, error) { panic("unexpected Stat") }

// Delete panics: the builder never deletes.
func (publishing) Delete(context.Context, string) error { panic("unexpected Delete") }

// Materialize panics: the builder never downloads.
func (publishing) Materialize(context.Context, string) (string, func(), error) {
	panic("unexpected Materialize")
}

// TestThumb_publishingBackendReturnsObjectURL proves a backend that publishes its
// objects decides the address, and that the address is the thumbnail's cache path
// used verbatim as the object key.
func TestThumb_publishingBackendReturnsObjectURL(t *testing.T) {
	t.Parallel()

	builder := mediaurl.NewBuilder(publishing{prefix: "https://media.example/"})
	rel, err := thumb.RelPath(testHash, thumb.GridSize)
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}

	got := builder.Thumb("ph1", testHash, thumb.GridSize)
	want := "https://media.example/" + rel + "?sig=deadbeef"
	if got != want {
		t.Errorf("Thumb() = %q, want %q", got, want)
	}
}

// TestDownload_publishingBackendReturnsObjectURL proves the original's own
// storage path is the object key handed to the backend.
func TestDownload_publishingBackendReturnsObjectURL(t *testing.T) {
	t.Parallel()

	builder := mediaurl.NewBuilder(publishing{prefix: "https://media.example/"})

	got := builder.Download("ph1", "2024/05/photo.jpg")
	want := "https://media.example/2024/05/photo.jpg?sig=deadbeef"
	if got != want {
		t.Errorf("Download() = %q, want %q", got, want)
	}
}

// TestThumb_filesystemBackendFallsBackToRoute proves a backend with no
// client-fetchable address (the filesystem one) leaves the client pointing at the
// application's own media route, so nothing breaks locally.
func TestThumb_filesystemBackendFallsBackToRoute(t *testing.T) {
	t.Parallel()

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	builder := mediaurl.NewBuilder(fs)

	if got, want := builder.Thumb("ph1", testHash, "tile_100"), "/api/v1/photos/ph1/thumb/tile_100"; got != want {
		t.Errorf("Thumb() = %q, want %q", got, want)
	}
	if got, want := builder.Download("ph1", "2024/05/x.jpg"), "/api/v1/photos/ph1/download?original=true"; got != want {
		t.Errorf("Download() = %q, want %q", got, want)
	}
}

// TestNilBuilder_fallsBackToRoutes proves a nil *Builder — an API constructed
// without a storage backend — still yields working, application-served routes
// rather than panicking or emitting an empty src.
func TestNilBuilder_fallsBackToRoutes(t *testing.T) {
	t.Parallel()

	var builder *mediaurl.Builder

	if got := builder.Thumb("ph1", testHash, "tile_100"); got != "/api/v1/photos/ph1/thumb/tile_100" {
		t.Errorf("Thumb() = %q", got)
	}
	if got := builder.Object("2024/05/x.jpg"); got != "" {
		t.Errorf("Object() = %q, want empty", got)
	}
}

// TestThumbObject_invalidHashYieldsNoURL proves a photo whose file hash cannot
// address the cache tree yields no object URL rather than a malformed key, so the
// caller streams from the application instead of redirecting to nowhere.
func TestThumbObject_invalidHashYieldsNoURL(t *testing.T) {
	t.Parallel()

	builder := mediaurl.NewBuilder(publishing{prefix: "https://media.example/"})

	if got := builder.ThumbObject("zz", thumb.GridSize); got != "" {
		t.Errorf("ThumbObject(short hash) = %q, want empty", got)
	}
	if got := builder.ThumbObject(testHash, "no_such_size"); got != "" {
		t.Errorf("ThumbObject(unknown size) = %q, want empty", got)
	}
	// The route fallback still addresses the photo by UID, which always works.
	if got := builder.Thumb("ph1", "zz", thumb.GridSize); got != "/api/v1/photos/ph1/thumb/tile_500" {
		t.Errorf("Thumb(short hash) = %q", got)
	}
}

// TestThumb_escapesUIDAndSize proves a UID or size that would otherwise break out
// of the route path is percent-encoded.
func TestThumb_escapesUIDAndSize(t *testing.T) {
	t.Parallel()

	var builder *mediaurl.Builder

	got := builder.Thumb("../etc", testHash, "tile_100")
	if strings.Contains(got, "../") {
		t.Errorf("Thumb() = %q, want the UID escaped", got)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
	if parsed.Path != "/api/v1/photos/../etc/thumb/tile_100" {
		t.Errorf("decoded path = %q", parsed.Path)
	}
}

// TestDecorate_stampsEveryPhoto proves a whole page is decorated in place and
// that each photo's own hash and path address its own media.
func TestDecorate_stampsEveryPhoto(t *testing.T) {
	t.Parallel()

	builder := mediaurl.NewBuilder(publishing{prefix: "https://media.example/"})
	list := []photos.Photo{
		{UID: "ph1", FileHash: testHash, FilePath: "2024/05/one.jpg"},
		{UID: "ph2", FileHash: testHash, FilePath: "2024/06/two.jpg"},
	}

	builder.Decorate(list)

	for i, photo := range list {
		if photo.ThumbURL == "" || photo.DownloadURL == "" {
			t.Fatalf("photo %d not decorated: %+v", i, photo)
		}
	}
	if !strings.Contains(list[0].DownloadURL, "2024/05/one.jpg") {
		t.Errorf("photo 0 download URL = %q", list[0].DownloadURL)
	}
	if !strings.Contains(list[1].DownloadURL, "2024/06/two.jpg") {
		t.Errorf("photo 1 download URL = %q", list[1].DownloadURL)
	}
}
