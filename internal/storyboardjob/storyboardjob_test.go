package storyboardjob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboard"
)

// testHash is a syntactically valid content hash for the fake catalogue rows.
const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakePhotos is an in-memory catalogue keyed by uid.
type fakePhotos struct {
	rows map[string]photos.Photo
	err  error
}

// GetByUID returns the stored row, the configured error, or ErrPhotoNotFound.
func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	if f.err != nil {
		return photos.Photo{}, f.err
	}
	row, ok := f.rows[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return row, nil
}

// fakeGenerator records what it was asked to render and pretends a sprite exists
// once `ready` is set.
type fakeGenerator struct {
	ready     bool
	existsErr error
	genErr    error
	calls     []generateCall
}

// generateCall is one recorded Generate invocation.
type generateCall struct {
	hash string
	src  string
	spec storyboard.Spec
}

// Exists reports the pretend cache state.
func (f *fakeGenerator) Exists(string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.ready, nil
}

// Open returns a fixed body when the sprite is "cached", otherwise the
// not-generated sentinel.
func (f *fakeGenerator) Open(string) (io.ReadCloser, error) {
	if !f.ready {
		return nil, storyboard.ErrNotGenerated
	}
	return io.NopCloser(strings.NewReader("sprite")), nil
}

// Generate records the request and marks the sprite ready unless told to fail.
func (f *fakeGenerator) Generate(_ context.Context, hash, src string, spec storyboard.Spec) error {
	f.calls = append(f.calls, generateCall{hash: hash, src: src, spec: spec})
	if f.genErr != nil {
		return f.genErr
	}
	f.ready = true
	return nil
}

// fakeEnqueuer records the uids scheduled for generation.
type fakeEnqueuer struct {
	uids []string
	err  error
}

// EnqueueStoryboard records the uid or reports the configured failure.
func (f *fakeEnqueuer) EnqueueStoryboard(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, uid)
	return nil
}

// videoPhoto returns a catalogued standalone video of the given length.
func videoPhoto(uid string, durationMs int) photos.Photo {
	return photos.Photo{
		UID:        uid,
		FileHash:   testHash,
		FilePath:   "2026/01/clip.mp4",
		FileWidth:  1920,
		FileHeight: 1080,
		MediaType:  photos.MediaVideo,
		DurationMs: &durationMs,
	}
}

// newService wires a Service over the given fakes with ffmpeg pinned present, so
// the scheduling decision under test never depends on the host.
func newService(store PhotoStore, gen Generator, enq Enqueuer) *Service {
	return New(Config{
		Photos:          store,
		Generator:       gen,
		Enqueuer:        enq,
		FFmpegAvailable: func() bool { return true },
	})
}

// TestStatus_pendingSchedulesGeneration is the "not generated yet" case: the
// first ask reports pending and schedules exactly one job, and the layout is
// withheld because there is no sprite to lay anything out against.
func TestStatus_pendingSchedulesGeneration(t *testing.T) {
	t.Parallel()

	store := &fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}}
	enq := &fakeEnqueuer{}
	svc := newService(store, &fakeGenerator{}, enq)

	status, err := svc.Status(t.Context(), "v1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StatePending {
		t.Errorf("State = %q, want %q", status.State, StatePending)
	}
	if status.Spec != (storyboard.Spec{}) {
		t.Errorf("Spec = %+v, want the zero value while pending", status.Spec)
	}
	if len(enq.uids) != 1 || enq.uids[0] != "v1" {
		t.Errorf("scheduled %v, want exactly [v1]", enq.uids)
	}
}

// TestStatus_readyCarriesLayout verifies a cached sprite is reported ready with
// the grid the client needs to place a preview.
func TestStatus_readyCarriesLayout(t *testing.T) {
	t.Parallel()

	store := &fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}}
	enq := &fakeEnqueuer{}
	svc := newService(store, &fakeGenerator{ready: true}, enq)

	status, err := svc.Status(t.Context(), "v1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateReady {
		t.Fatalf("State = %q, want %q", status.State, StateReady)
	}
	if status.Spec.Count != status.Spec.Columns*status.Spec.Rows || status.Spec.Count == 0 {
		t.Errorf("Spec = %+v, want a full non-empty grid", status.Spec)
	}
	if status.Spec.IntervalMs <= 0 {
		t.Errorf("IntervalMs = %d, want a positive tile interval", status.Spec.IntervalMs)
	}
	if len(enq.uids) != 0 {
		t.Errorf("scheduled %v, want nothing for a ready sprite", enq.uids)
	}
}

// TestStatus_unavailable covers every photo that will never have a storyboard: a
// still image, a live photo (its motion clip has no scrubbable timeline) and a
// video whose duration the catalogue does not know. None of them may schedule a
// job — that is what keeps the queue off the rest of the library.
func TestStatus_unavailable(t *testing.T) {
	t.Parallel()

	noDuration := videoPhoto("v3", 0)
	noDuration.DurationMs = nil
	rows := map[string]photos.Photo{
		"p1": {UID: "p1", FileHash: testHash, MediaType: photos.MediaImage},
		"p2": {UID: "p2", FileHash: testHash, MediaType: photos.MediaLive},
		"v3": noDuration,
	}
	for uid := range rows {
		t.Run(uid, func(t *testing.T) {
			t.Parallel()
			enq := &fakeEnqueuer{}
			svc := newService(&fakePhotos{rows: rows}, &fakeGenerator{}, enq)
			status, err := svc.Status(t.Context(), uid)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if status.State != StateUnavailable {
				t.Errorf("State = %q, want %q", status.State, StateUnavailable)
			}
			if len(enq.uids) != 0 {
				t.Errorf("scheduled %v, want nothing", enq.uids)
			}
		})
	}
}

// TestStatus_noFFmpegIsUnavailable verifies a host that cannot render sprites says
// so instead of promising a "pending" the client would poll forever.
func TestStatus_noFFmpegIsUnavailable(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	svc := New(Config{
		Photos:          &fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		Generator:       &fakeGenerator{},
		Enqueuer:        enq,
		FFmpegAvailable: func() bool { return false },
	})

	status, err := svc.Status(t.Context(), "v1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateUnavailable {
		t.Errorf("State = %q, want %q", status.State, StateUnavailable)
	}
	if len(enq.uids) != 0 {
		t.Errorf("scheduled %v, want nothing without ffmpeg", enq.uids)
	}
}

// TestStatus_noEnqueuerIsUnavailable verifies a read-only wiring reports
// unavailable rather than pending, since nothing there will ever produce it.
func TestStatus_noEnqueuerIsUnavailable(t *testing.T) {
	t.Parallel()

	svc := New(Config{
		Photos:          &fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		Generator:       &fakeGenerator{},
		FFmpegAvailable: func() bool { return true },
	})

	status, err := svc.Status(t.Context(), "v1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateUnavailable {
		t.Errorf("State = %q, want %q", status.State, StateUnavailable)
	}
}

// TestStatus_missingPhoto verifies an unknown uid surfaces as ErrPhotoNotFound so
// the HTTP layer answers 404 rather than inventing a state.
func TestStatus_missingPhoto(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotos{rows: map[string]photos.Photo{}}, &fakeGenerator{}, &fakeEnqueuer{})
	if _, err := svc.Status(t.Context(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Status error = %v, want ErrPhotoNotFound", err)
	}
}

// TestStatus_enqueueFailurePropagates verifies a queue failure is reported rather
// than reported as a state, so an operator sees it.
func TestStatus_enqueueFailurePropagates(t *testing.T) {
	t.Parallel()

	boom := errors.New("queue down")
	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		&fakeGenerator{},
		&fakeEnqueuer{err: boom},
	)
	if _, err := svc.Status(t.Context(), "v1"); !errors.Is(err, boom) {
		t.Errorf("Status error = %v, want the queue failure", err)
	}
}

// TestOpen_notGenerated verifies reading a sprite that has not been rendered
// yields the typed sentinel the HTTP layer maps to 404.
func TestOpen_notGenerated(t *testing.T) {
	t.Parallel()

	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		&fakeGenerator{},
		&fakeEnqueuer{},
	)
	if _, _, err := svc.Open(t.Context(), "v1"); !errors.Is(err, storyboard.ErrNotGenerated) {
		t.Errorf("Open error = %v, want ErrNotGenerated", err)
	}
}

// TestOpen_readyReturnsBytesAndSpec verifies a rendered sprite comes back with the
// layout that describes it.
func TestOpen_readyReturnsBytesAndSpec(t *testing.T) {
	t.Parallel()

	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		&fakeGenerator{ready: true},
		&fakeEnqueuer{},
	)
	reader, spec, err := svc.Open(t.Context(), "v1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "sprite" {
		t.Errorf("body = %q, want the sprite bytes", body)
	}
	if spec.Count == 0 {
		t.Error("Open returned an empty Spec for a ready sprite")
	}
}

// TestOpen_notAVideo verifies a still image is refused with the permanent
// sentinel, so the sprite route answers 404 instead of 500.
func TestOpen_notAVideo(t *testing.T) {
	t.Parallel()

	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{
			"p1": {UID: "p1", FileHash: testHash, MediaType: photos.MediaImage},
		}},
		&fakeGenerator{ready: true},
		&fakeEnqueuer{},
	)
	if _, _, err := svc.Open(t.Context(), "p1"); !errors.Is(err, ErrNotAVideo) {
		t.Errorf("Open error = %v, want ErrNotAVideo", err)
	}
}

// TestFileHash verifies the ETag source is the photo's content hash and that an
// unknown photo is reported rather than silently answered.
func TestFileHash(t *testing.T) {
	t.Parallel()

	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		&fakeGenerator{},
		&fakeEnqueuer{},
	)
	hash, err := svc.FileHash(t.Context(), "v1")
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	if hash != testHash {
		t.Errorf("FileHash = %q, want %q", hash, testHash)
	}
	if _, err := svc.FileHash(t.Context(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("FileHash on a missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestHandle_rendersTheScheduledPhoto verifies the worker path decodes the
// payload and renders the clip with the planned layout.
func TestHandle_rendersTheScheduledPhoto(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{}
	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{"v1": videoPhoto("v1", 20000)}},
		gen,
		&fakeEnqueuer{},
	)
	payload, err := json.Marshal(map[string]string{"photo_uid": "v1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := svc.Handle(t.Context(), jobs.Job{Type: jobs.TypeStoryboard, Payload: payload}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(gen.calls) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(gen.calls))
	}
	call := gen.calls[0]
	if call.hash != testHash || call.src != "2026/01/clip.mp4" {
		t.Errorf("rendered %s from %s, want the catalogued hash and path", call.hash, call.src)
	}
	if call.spec.Count == 0 {
		t.Error("rendered with an empty Spec")
	}
}

// TestHandle_badPayload verifies a payload that can never succeed fails
// permanently (so the job dead-letters) rather than retrying forever.
func TestHandle_badPayload(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotos{rows: map[string]photos.Photo{}}, &fakeGenerator{}, &fakeEnqueuer{})

	if err := svc.Handle(t.Context(), jobs.Job{Payload: []byte("{")}); err == nil {
		t.Error("Handle with malformed JSON = nil, want an error")
	}
	err := svc.Handle(t.Context(), jobs.Job{Payload: []byte(`{"photo_uid":""}`)})
	if !errors.Is(err, ErrMissingPhotoUID) {
		t.Errorf("Handle with an empty uid = %v, want ErrMissingPhotoUID", err)
	}
}

// TestGenerate_skipsPhotosWithNoPlan verifies a job scheduled for something that
// cannot have a storyboard is a quiet no-op — it neither renders nor fails, so it
// does not dead-letter noise into the queue.
func TestGenerate_skipsPhotosWithNoPlan(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{}
	svc := newService(
		&fakePhotos{rows: map[string]photos.Photo{
			"p1": {UID: "p1", FileHash: testHash, MediaType: photos.MediaImage},
		}},
		gen,
		&fakeEnqueuer{},
	)
	if err := svc.Generate(t.Context(), "p1"); err != nil {
		t.Fatalf("Generate on a still = %v, want nil", err)
	}
	if len(gen.calls) != 0 {
		t.Errorf("rendered %d sprites for a still, want 0", len(gen.calls))
	}
}

// TestGenerate_missingPhotoFails verifies a uid the catalogue does not know fails
// the job, so it dead-letters instead of looping.
func TestGenerate_missingPhotoFails(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotos{rows: map[string]photos.Photo{}}, &fakeGenerator{}, &fakeEnqueuer{})
	if err := svc.Generate(t.Context(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Generate error = %v, want ErrPhotoNotFound", err)
	}
}

// TestNew_requiresCollaborators verifies a wiring bug is caught at construction
// rather than at the first job.
func TestNew_requiresCollaborators(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New with no Photos did not panic")
		}
	}()
	_ = New(Config{Generator: &fakeGenerator{}})
}
