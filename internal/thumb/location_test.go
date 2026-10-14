package thumb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// movedPrefix is the storage prefix a storage move puts the test photo under.
const movedPrefix = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/"

// fakeLocation stands in for the move's photo lock over a catalogue a storage
// move may have rewritten since the generation started: it hands publish the
// canned row's key, reports a missing row for photos.ErrPhotoNotFound, and
// returns any other canned error without calling publish — as the lock does
// for a photo a move holds (storekeys.ErrMoving) or a failed re-read. It records
// the project and photo it was asked about.
type fakeLocation struct {
	row   photos.Photo
	err   error
	asked *[]string
}

// Publish calls publish with the canned row, or returns the canned error.
func (f fakeLocation) Publish(
	_ context.Context, projectUID, photoUID string, publish func(filePath string, found bool) error,
) error {
	*f.asked = append(*f.asked, projectUID+"|"+photoUID)
	switch {
	case errors.Is(f.err, photos.ErrPhotoNotFound):
		return publish("", false)
	case f.err != nil:
		return f.err
	}
	return publish(f.row.FilePath, true)
}

// TestGenerate_checksTheLocationBeforePublishing verifies the re-read right
// before a size is published, under the photo's lock: a photo whose prefix
// moved since the generation started publishes nothing (ErrPrefixMoved, the
// cache file removed), and neither does one a storage move holds right now
// (ErrPrefixMoved wrapping storekeys.ErrMoving), while one that is still where it
// was, or whose row is not there yet, publishes as before; a failing re-read
// publishes nothing either.
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
		{name: "a move holds the photo", lookupErr: storekeys.ErrMoving, wantErr: ErrPrefixMoved},
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
			photo.ProjectID = "pjproject"
			current := photo
			current.FilePath = tt.moveTo + photo.FilePath
			var asked []string
			th := New(pub, filepath.Join(root, "cache"),
				WithLocation(fakeLocation{row: current, err: tt.lookupErr, asked: &asked}))

			_, err = th.Generate(t.Context(), photo, GridSize)
			genErr := err
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
			if errors.Is(tt.lookupErr, storekeys.ErrMoving) && !errors.Is(genErr, storekeys.ErrMoving) {
				t.Errorf("Generate = %v, want it to wrap storekeys.ErrMoving too", genErr)
			}
			if len(asked) == 0 || asked[0] != "pjproject|ph1" {
				t.Errorf("lock asked about %v, want the photo's project and uid", asked)
			}
		})
	}
}
