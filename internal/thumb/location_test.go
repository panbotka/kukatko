package thumb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// movedPrefix is the storage prefix a storage move puts the test photo under.
const movedPrefix = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/"

// fakeLocation answers GetForJob with a canned row or error, standing in for a
// catalogue a storage move may have rewritten since the generation started.
type fakeLocation struct {
	row photos.Photo
	err error
}

// GetForJob returns the canned row or error.
func (f fakeLocation) GetForJob(context.Context, string) (photos.Photo, error) {
	return f.row, f.err
}

// TestGenerate_checksTheLocationBeforePublishing verifies the re-read right
// before a size is published: a photo whose prefix moved since the generation
// started publishes nothing (ErrPrefixMoved, the cache file removed), while one
// that is still where it was, or whose row is not there yet, publishes as
// before; a failing re-read publishes nothing either.
func TestGenerate_checksTheLocationBeforePublishing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		moveTo      string
		lookupErr   error
		wantErr     error
		wantAnyErr  bool
		wantPublish bool
	}{
		{name: "still at the root", wantPublish: true},
		{name: "moved under a prefix", moveTo: movedPrefix, wantErr: ErrPrefixMoved},
		{name: "row not committed yet", lookupErr: photos.ErrPhotoNotFound, wantPublish: true},
		{name: "re-read fails", lookupErr: errors.New("db down"), wantAnyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			store, err := storage.NewFS(filepath.Join(root, "originals"))
			if err != nil {
				t.Fatalf("storage.NewFS: %v", err)
			}
			pub := newPublishingFS(store)
			photo := storeJPEG(t, store, 400, 300, 1)
			photo.UID = "ph1"
			current := photo
			current.FilePath = tt.moveTo + photo.FilePath
			th := New(pub, filepath.Join(root, "cache"),
				WithLocation(fakeLocation{row: current, err: tt.lookupErr}))

			_, err = th.Generate(t.Context(), photo, GridSize)
			switch {
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("Generate = %v, want %v", err, tt.wantErr)
			case tt.wantAnyErr && err == nil:
				t.Fatal("Generate succeeded although the re-read failed")
			case tt.wantPublish && err != nil:
				t.Fatalf("Generate: %v", err)
			}
			rel, err := RelPath("", photo.FileHash, GridSize)
			if err != nil {
				t.Fatalf("RelPath: %v", err)
			}
			if _, published := pub.recorded(rel); published != tt.wantPublish {
				t.Errorf("published at %s = %v, want %v", rel, published, tt.wantPublish)
			}
			abs, err := th.Path("", photo.FileHash, GridSize)
			if err != nil {
				t.Fatalf("Path: %v", err)
			}
			if fileExists(abs) != tt.wantPublish {
				t.Errorf("cache file present = %v, want %v", fileExists(abs), tt.wantPublish)
			}
		})
	}
}
