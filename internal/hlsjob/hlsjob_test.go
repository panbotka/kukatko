package hlsjob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// testHash is a syntactically valid content hash for the fake catalogue rows.
const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakePhotos is an in-memory catalogue keyed by uid.
type fakePhotos struct {
	rows map[string]photos.Photo
}

// GetByUID returns the stored row or photos.ErrPhotoNotFound.
func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	row, ok := f.rows[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return row, nil
}

// fakeObjects is an in-memory object store recording what was published,
// materialized and deleted.
type fakeObjects struct {
	// put maps key to the bytes written under it.
	put map[string][]byte
	// meta maps key to the identity the caller declared for it.
	meta map[string]storage.StoredFile
	// deleted lists the keys Delete was called with, in order.
	deleted []string
	// failAt makes the n-th (1-based) Put fail; 0 never fails.
	failAt int
	// puts counts the Put calls made so far.
	puts int
	// materializeErr is returned by Materialize when set.
	materializeErr error
}

// newFakeObjects returns an empty store.
func newFakeObjects() *fakeObjects {
	return &fakeObjects{put: map[string][]byte{}, meta: map[string]storage.StoredFile{}}
}

// Materialize pretends the original is already local at its relative path.
func (f *fakeObjects) Materialize(_ context.Context, relPath string) (string, func(), error) {
	if f.materializeErr != nil {
		return "", func() {}, f.materializeErr
	}
	return relPath, func() {}, nil
}

// Put records the object, failing the configured call.
func (f *fakeObjects) Put(_ context.Context, src io.Reader, file storage.StoredFile) error {
	f.puts++
	if f.failAt == f.puts {
		return errors.New("store is full")
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	f.put[file.RelPath] = data
	f.meta[file.RelPath] = file
	return nil
}

// Delete records the key and forgets the object.
func (f *fakeObjects) Delete(_ context.Context, relPath string) error {
	f.deleted = append(f.deleted, relPath)
	delete(f.put, relPath)
	return nil
}

// fakeRenditions records the rows a run wrote.
type fakeRenditions struct {
	saved []Encoded
	err   error
}

// Save records enc or reports the configured failure.
func (f *fakeRenditions) Save(_ context.Context, enc Encoded) (Encoded, error) {
	if f.err != nil {
		return Encoded{}, f.err
	}
	f.saved = append(f.saved, enc)
	return enc, nil
}

// videoPhoto returns a catalogued standalone video.
func videoPhoto(uid string) photos.Photo {
	return photos.Photo{
		UID:       uid,
		FileHash:  testHash,
		FilePath:  "2026/01/clip.mp4",
		MediaType: photos.MediaVideo,
	}
}

// newService wires a Service over the given fakes with ffmpeg pinned present, so
// a decision under test never depends on the host running it.
func newService(t *testing.T, cat PhotoStore, objects ObjectStore, rend RenditionStore) *Service {
	t.Helper()
	return New(Config{
		Photos:          cat,
		Objects:         objects,
		Renditions:      rend,
		Plan:            hls.All(),
		FFmpegAvailable: func() bool { return true },
	})
}

// TestHandle_payload verifies the two permanent payload failures — malformed
// JSON and a missing uid — are reported rather than retried against a photo that
// was never named.
func TestHandle_payload(t *testing.T) {
	t.Parallel()

	svc := newService(t, &fakePhotos{}, newFakeObjects(), &fakeRenditions{})
	if err := svc.Handle(t.Context(), jobs.Job{Payload: json.RawMessage(`{`)}); err == nil {
		t.Error("Handle accepted a malformed payload")
	}
	err := svc.Handle(t.Context(), jobs.Job{Payload: json.RawMessage(`{"photo_uid":""}`)})
	if !errors.Is(err, ErrMissingPhotoUID) {
		t.Errorf("Handle error = %v, want ErrMissingPhotoUID", err)
	}
}

// TestHandle_dispatchesToTranscode verifies a well-formed payload reaches the
// catalogue: an unknown photo comes back as the catalogue's own sentinel.
func TestHandle_dispatchesToTranscode(t *testing.T) {
	t.Parallel()

	svc := newService(t, &fakePhotos{}, newFakeObjects(), &fakeRenditions{})
	err := svc.Handle(t.Context(), jobs.Job{Payload: json.RawMessage(`{"photo_uid":"ph-gone"}`)})
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Handle error = %v, want ErrPhotoNotFound", err)
	}
}

// TestTranscode_stillIsANoOp verifies a photo that is not a standalone video is
// left alone: nothing is encoded, published or recorded, and the job succeeds
// rather than dead-lettering. It covers a live photo too, whose motion clip is a
// hover preview nobody streams.
func TestTranscode_stillIsANoOp(t *testing.T) {
	t.Parallel()

	for _, kind := range []photos.MediaType{photos.MediaImage, photos.MediaLive} {
		objects, rend := newFakeObjects(), &fakeRenditions{}
		photo := videoPhoto("ph-still")
		photo.MediaType = kind
		svc := newService(t, &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}}, objects, rend)

		if err := svc.Transcode(t.Context(), photo.UID); err != nil {
			t.Errorf("Transcode(%s) = %v, want nil", kind, err)
		}
		if len(objects.put) != 0 || len(rend.saved) != 0 {
			t.Errorf("Transcode(%s) published %d objects and wrote %d rows, want none",
				kind, len(objects.put), len(rend.saved))
		}
	}
}

// TestTranscode_refusals verifies the two states in which a video cannot be
// encoded are reported as such: nothing configured to produce, and no encoder on
// the host.
func TestTranscode_refusals(t *testing.T) {
	t.Parallel()

	photo := videoPhoto("ph-video")
	cat := &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}}

	empty := New(Config{
		Photos: cat, Objects: newFakeObjects(), Renditions: &fakeRenditions{},
		FFmpegAvailable: func() bool { return true },
	})
	if err := empty.Transcode(t.Context(), photo.UID); !errors.Is(err, ErrNoRenditions) {
		t.Errorf("Transcode with no plan = %v, want ErrNoRenditions", err)
	}

	noFFmpeg := New(Config{
		Photos: cat, Objects: newFakeObjects(), Renditions: &fakeRenditions{},
		Plan: hls.All(), FFmpegAvailable: func() bool { return false },
	})
	if err := noFFmpeg.Transcode(t.Context(), photo.UID); err == nil {
		t.Error("Transcode without ffmpeg succeeded")
	}
}

// TestTranscode_materializeFailureStops verifies a video whose original cannot
// be made local fails before ffmpeg is ever asked to encode it.
func TestTranscode_materializeFailureStops(t *testing.T) {
	t.Parallel()

	photo := videoPhoto("ph-video")
	objects := newFakeObjects()
	objects.materializeErr = errors.New("bucket unreachable")
	svc := newService(t, &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}}, objects, &fakeRenditions{})

	if err := svc.Transcode(t.Context(), photo.UID); err == nil {
		t.Error("Transcode succeeded with an unreachable original")
	}
}

// TestNew_requiresCollaborators verifies the wiring mistakes that cannot produce
// a working handler fail loudly at construction rather than at the first job.
func TestNew_requiresCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no photos", cfg: Config{Objects: newFakeObjects(), Renditions: &fakeRenditions{}}},
		{name: "no objects", cfg: Config{Photos: &fakePhotos{}, Renditions: &fakeRenditions{}}},
		{name: "no renditions", cfg: Config{Photos: &fakePhotos{}, Objects: newFakeObjects()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic")
				}
			}()
			New(tt.cfg)
		})
	}
}

// TestNew_defaultsSegmentLength verifies an unset segment length becomes the
// encoder's default rather than zero, which would ask ffmpeg for segments of no
// length at all.
func TestNew_defaultsSegmentLength(t *testing.T) {
	t.Parallel()

	svc := newService(t, &fakePhotos{}, newFakeObjects(), &fakeRenditions{})
	if svc.segment != hls.DefaultSegmentSeconds {
		t.Errorf("segment = %d, want %d", svc.segment, hls.DefaultSegmentSeconds)
	}
}

// TestEncodeTimeout verifies the deadline scales with the clip: a short one gets
// the base plus a little, a long one gets hours, and an unknown length gets the
// generous fallback rather than a deadline derived from nothing.
func TestEncodeTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		durationMs *int
		want       time.Duration
	}{
		{name: "unknown length", durationMs: nil, want: encodeTimeoutUnknown},
		{name: "nonsense length", durationMs: new(int), want: encodeTimeoutUnknown},
		{name: "ten seconds", durationMs: durationOf(10 * time.Second), want: encodeTimeoutBase + 100*time.Second},
		{name: "two hours", durationMs: durationOf(2 * time.Hour), want: encodeTimeoutBase + 20*time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := encodeTimeout(tt.durationMs); got != tt.want {
				t.Errorf("encodeTimeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// durationOf returns d as a pointer to whole milliseconds, the shape the
// catalogue stores a clip's length in.
func durationOf(d time.Duration) *int {
	return new(int(d / time.Millisecond))
}

// TestPublish verifies the upload step: the initialisation segment goes first,
// every media segment follows in playback order, each object lands under the
// rendition's prefix, and each is declared with its real size, its real digest
// and the media type a player expects — a segment that arrived as text/plain is
// one no player will load.
func TestPublish(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bodies := map[string]string{"init.mp4": "INIT", "00000.m4s": "SEGMENT-ZERO", "00001.m4s": "ONE"}
	for name, body := range bodies {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	objects := newFakeObjects()
	svc := newService(t, &fakePhotos{}, objects, &fakeRenditions{})
	prefix := "hls/" + testHash + "/1080p/"

	written, err := svc.publish(t.Context(), dir, prefix,
		encoded{initName: "init.mp4", segments: []string{"00000.m4s", "00001.m4s"}})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	want := []string{prefix + "init.mp4", prefix + "00000.m4s", prefix + "00001.m4s"}
	if !slices.Equal(written, want) {
		t.Fatalf("publish wrote %v, want %v", written, want)
	}
	for name, body := range bodies {
		key := prefix + name
		if got := string(objects.put[key]); got != body {
			t.Errorf("%s = %q, want %q", key, got, body)
		}
		sum := sha256.Sum256([]byte(body))
		meta := objects.meta[key]
		if meta.Hash != hex.EncodeToString(sum[:]) || meta.Size != int64(len(body)) {
			t.Errorf("%s declared %s/%d bytes, want %x/%d", key, meta.Hash, meta.Size, sum, len(body))
		}
		if want := hls.MIMEFor(name); meta.MIME != want {
			t.Errorf("%s MIME = %q, want %q", key, meta.MIME, want)
		}
	}
}

// TestPublish_reportsWhatItWrote verifies a failed upload names the objects that
// did land, which is what lets the caller undo exactly this run's work.
func TestPublish_reportsWhatItWrote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"init.mp4", "00000.m4s", "00001.m4s"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	objects := newFakeObjects()
	objects.failAt = 2
	svc := newService(t, &fakePhotos{}, objects, &fakeRenditions{})
	prefix := "hls/" + testHash + "/1080p/"

	written, err := svc.publish(t.Context(), dir, prefix,
		encoded{initName: "init.mp4", segments: []string{"00000.m4s", "00001.m4s"}})
	if err == nil {
		t.Fatal("publish succeeded with a failing store")
	}
	if want := []string{prefix + "init.mp4"}; !slices.Equal(written, want) {
		t.Errorf("publish reported %v as written, want %v", written, want)
	}
}

// TestRenditionPrefix verifies the prefix one rendition's objects live under,
// and that a hash or a name the layout refuses never becomes one.
func TestRenditionPrefix(t *testing.T) {
	t.Parallel()

	got, err := renditionPrefix(testHash, hls.Rendition1080p)
	if err != nil {
		t.Fatalf("renditionPrefix: %v", err)
	}
	if want := "hls/" + testHash + "/1080p/"; got != want {
		t.Errorf("renditionPrefix = %q, want %q", got, want)
	}
	if _, err := renditionPrefix("nope", hls.Rendition1080p); !errors.Is(err, hls.ErrInvalidHash) {
		t.Errorf("renditionPrefix with a bad hash = %v, want ErrInvalidHash", err)
	}
	if _, err := renditionPrefix(testHash, "../etc"); !errors.Is(err, hls.ErrInvalidRendition) {
		t.Errorf("renditionPrefix with a bad rendition = %v, want ErrInvalidRendition", err)
	}
}

// TestWithout verifies the set difference the cleanup is built on: what this run
// created (to undo on failure) and what a previous one left behind (to sweep on
// success).
func TestWithout(t *testing.T) {
	t.Parallel()

	old := []string{"a", "b", "c"}
	fresh := []string{"a", "b", "d"}
	if got := without(fresh, old); !slices.Equal(got, []string{"d"}) {
		t.Errorf("without(fresh, old) = %v, want [d]", got)
	}
	if got := without(old, fresh); !slices.Equal(got, []string{"c"}) {
		t.Errorf("without(old, fresh) = %v, want [c]", got)
	}
	if got := without(nil, old); len(got) != 0 {
		t.Errorf("without(nil, old) = %v, want empty", got)
	}
}
