package hlsjob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// movedPath is where the storage move puts the test video's original.
const movedPath = movedPrefix + "2026/01/clip.mp4"

// movedPrefix is the storage prefix of the project the video moves under.
const movedPrefix = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/"

// movingPhotos is a catalogue of one video whose row a storage move rewrites
// mid-job: every GetForJob call after the first moveAfter answers with the
// moved key. With flip set it instead alternates between the two keys on every
// call, a photo that never settles.
type movingPhotos struct {
	photo     photos.Photo
	moveAfter int
	flip      bool
	calls     int
}

// GetForJob returns the row as the move has left it by this call.
func (m *movingPhotos) GetForJob(_ context.Context, uid string) (photos.Photo, error) {
	if uid != m.photo.UID {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	m.calls++
	row := m.photo
	switch {
	case m.flip && m.calls%2 == 0:
		row.FilePath = movedPath
	case !m.flip && m.calls > m.moveAfter:
		row.FilePath = movedPath
	}
	return row, nil
}

// fakeGate answers AwaitingMove with a canned verdict and records the question.
type fakeGate struct {
	awaiting bool
	err      error
	asked    []string
}

// AwaitingMove records the project and key and returns the canned answer.
func (g *fakeGate) AwaitingMove(_ context.Context, projectUID, filePath string) (bool, error) {
	g.asked = append(g.asked, projectUID+"|"+filePath)
	return g.awaiting, g.err
}

// encodedDir writes a two-segment rendition into a temp dir, as ffmpeg would.
func encodedDir(t *testing.T) (string, encoded) {
	t.Helper()
	dir := t.TempDir()
	names := []string{"init.mp4", "00000.m4s", "00001.m4s"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir, encoded{initName: names[0], segments: names[1:]}
}

// keysUnder returns the sorted keys of objects that start with prefix.
func keysUnder(objects *fakeObjects, prefix string) []string {
	var keys []string
	for key := range objects.put {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// TestPublishCurrent_followsTheMove verifies a rendition is published under the
// prefix the photo's row points at when it is published — whether the move
// committed the photo while ffmpeg ran (before the publish) or during the
// upload itself — and that nothing is left, and no row written, under the
// prefix the photo left.
func TestPublishCurrent_followsTheMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moveAfter int
		wantPuts  int
	}{
		// The first re-read already sees the moved row: one upload, straight to
		// the new prefix.
		{name: "moved while encoding", moveAfter: 0, wantPuts: 3},
		// The re-read before the publish sees the root, the one after it the
		// moved row: the root upload is undone and redone under the prefix.
		{name: "moved while publishing", moveAfter: 1, wantPuts: 6},
		// Never moved: published at the root, as before.
		{name: "not moved", moveAfter: 100, wantPuts: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, produced := encodedDir(t)
			cat := &movingPhotos{photo: videoPhoto("ph1"), moveAfter: tt.moveAfter}
			objects := newFakeObjects()
			rend := &fakeRenditions{}
			svc := newService(t, cat, objects, rend)
			row := Encoded{PhotoUID: "ph1", Rendition: "720p"}

			if _, err := svc.publishCurrent(t.Context(), dir, cat.photo, produced, row); err != nil {
				t.Fatalf("publishCurrent: %v", err)
			}
			wantPrefix := movedPrefix + "hls/" + testHash + "/720p/"
			stalePrefix := "hls/"
			if tt.moveAfter >= 100 {
				wantPrefix, stalePrefix = "hls/"+testHash+"/720p/", "projects/"
			}
			if got := keysUnder(objects, wantPrefix); len(got) != 3 {
				t.Errorf("objects under %s = %v, want the 3 of the rendition", wantPrefix, got)
			}
			if got := keysUnder(objects, stalePrefix); len(got) != 0 {
				t.Errorf("objects left under the stale %s: %v", stalePrefix, got)
			}
			if objects.puts != tt.wantPuts {
				t.Errorf("uploads = %d, want %d", objects.puts, tt.wantPuts)
			}
			if len(rend.saved) != 1 {
				t.Errorf("rows saved = %d, want exactly 1", len(rend.saved))
			}
		})
	}
}

// TestPublishCurrent_defersAPhotoThatKeepsMoving verifies a photo whose prefix
// changes on every re-read is not chased for ever: after maxPublishAttempts the
// job is deferred with ErrPrefixMoved, no row is written and no object is left.
func TestPublishCurrent_defersAPhotoThatKeepsMoving(t *testing.T) {
	t.Parallel()

	dir, produced := encodedDir(t)
	cat := &movingPhotos{photo: videoPhoto("ph1"), flip: true}
	objects := newFakeObjects()
	rend := &fakeRenditions{}
	svc := newService(t, cat, objects, rend)

	_, err := svc.publishCurrent(t.Context(), dir, cat.photo, produced, Encoded{PhotoUID: "ph1", Rendition: "720p"})
	if !errors.Is(err, ErrPrefixMoved) || !worker.IsDeferral(err) {
		t.Fatalf("publishCurrent = %v, want a deferral wrapping ErrPrefixMoved", err)
	}
	if len(rend.saved) != 0 {
		t.Errorf("rows saved = %d, want none", len(rend.saved))
	}
	if len(objects.put) != 0 {
		t.Errorf("objects left behind: %v", keysUnder(objects, ""))
	}
}

// TestTranscode_awaitsTheMove verifies a video a running storage move has yet
// to take is deferred before anything is materialized or encoded, that the gate
// is asked about the video's own project and key, and that a gate failure is an
// ordinary error rather than an encode.
func TestTranscode_awaitsTheMove(t *testing.T) {
	t.Parallel()

	video := videoPhoto("ph1")
	video.ProjectID = "pr1"
	tests := []struct {
		name         string
		gate         *fakeGate
		wantDeferral bool
	}{
		{name: "awaiting", gate: &fakeGate{awaiting: true}, wantDeferral: true},
		{name: "gate fails", gate: &fakeGate{err: errors.New("db down")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objects := newFakeObjects()
			objects.materializeErr = errors.New("materialized despite the move")
			svc := New(Config{
				Photos:          &fakePhotos{rows: map[string]photos.Photo{"ph1": video}},
				Objects:         objects,
				Renditions:      &fakeRenditions{},
				Moves:           tt.gate,
				Plan:            hls.All(),
				FFmpegAvailable: func() bool { return true },
			})
			err := svc.Transcode(t.Context(), "ph1")
			if err == nil || errors.Is(err, objects.materializeErr) {
				t.Fatalf("Transcode = %v, want it stopped before materializing", err)
			}
			if got := worker.IsDeferral(err) && errors.Is(err, ErrAwaitingMove); got != tt.wantDeferral {
				t.Errorf("Transcode = %v, deferral awaiting the move = %v, want %v", err, got, tt.wantDeferral)
			}
			if want := []string{"pr1|2026/01/clip.mp4"}; !slices.Equal(tt.gate.asked, want) {
				t.Errorf("gate asked %v, want %v", tt.gate.asked, want)
			}
		})
	}
}

// TestTranscode_movedVideoIsNotDeferred verifies a gate that answers "not
// awaiting" lets the encode proceed to the original.
func TestTranscode_movedVideoIsNotDeferred(t *testing.T) {
	t.Parallel()

	objects := newFakeObjects()
	objects.materializeErr = errors.New("reached the original")
	svc := New(Config{
		Photos:          &fakePhotos{rows: map[string]photos.Photo{"ph1": videoPhoto("ph1")}},
		Objects:         objects,
		Renditions:      &fakeRenditions{},
		Moves:           &fakeGate{},
		Plan:            hls.All(),
		FFmpegAvailable: func() bool { return true },
	})
	if err := svc.Transcode(t.Context(), "ph1"); !errors.Is(err, objects.materializeErr) {
		t.Fatalf("Transcode = %v, want it to reach the original", err)
	}
}
