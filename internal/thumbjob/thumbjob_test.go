package thumbjob

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakePhotos is an in-memory PhotoStore tracking pHash reads and writes.
type fakePhotos struct {
	photo     photos.Photo
	getErr    error
	hasPhash  bool
	phashErr  error
	setCalled bool
	setErr    error
}

func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	if f.getErr != nil {
		return photos.Photo{}, f.getErr
	}
	p := f.photo
	p.UID = uid
	return p, nil
}

func (f *fakePhotos) GetPhash(context.Context, string) (photos.Phash, error) {
	if f.phashErr != nil {
		return photos.Phash{}, f.phashErr
	}
	if f.hasPhash {
		return photos.Phash{Phash: 1, Dhash: 2}, nil
	}
	return photos.Phash{}, photos.ErrPhashNotFound
}

func (f *fakePhotos) SetPhash(context.Context, photos.Phash) error {
	f.setCalled = true
	return f.setErr
}

// fakeThumbs records GenerateAll and RegenerateAll calls.
type fakeThumbs struct {
	calls      int
	regenCalls int
	err        error
	regenErr   error
}

func (f *fakeThumbs) GenerateAll(context.Context, photos.Photo) (map[string]string, error) {
	f.calls++
	return map[string]string{}, f.err
}

func (f *fakeThumbs) RegenerateAll(context.Context, photos.Photo) (map[string]string, error) {
	f.regenCalls++
	return map[string]string{"tile_500": "/cache/tile_500.jpg"}, f.regenErr
}

// fakeDecoder returns a fixed image and records whether it was invoked.
type fakeDecoder struct {
	called bool
	err    error
}

func (f *fakeDecoder) DecodeOriginal(context.Context, photos.Photo) (image.Image, func(), error) {
	f.called = true
	if f.err != nil {
		return nil, nil, f.err
	}
	return image.NewRGBA(image.Rect(0, 0, 16, 16)), func() {}, nil
}

// newService wires a Service over the three fakes.
func newService(p *fakePhotos, th *fakeThumbs, d *fakeDecoder) *Service {
	return New(Config{Photos: p, Thumbnailer: th, Decoder: d})
}

// payload builds a thumbnail job payload for uid.
func payload(t *testing.T, uid string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"photo_uid": uid})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

// TestRegenerateComputesMissingPhash verifies a photo with no pHash gets its
// thumbnails generated and its pHash computed and stored.
func TestRegenerateComputesMissingPhash(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: false}
	th := &fakeThumbs{}
	d := &fakeDecoder{}
	svc := newService(p, th, d)

	if err := svc.Regenerate(context.Background(), "ph1"); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if th.calls != 1 {
		t.Errorf("GenerateAll calls = %d, want 1", th.calls)
	}
	if !d.called || !p.setCalled {
		t.Errorf("missing pHash should decode (%v) and store (%v)", d.called, p.setCalled)
	}
}

// TestRegenerateSkipsPhashWhenPresent verifies a photo that already has a pHash
// is not decoded again (idempotent skip).
func TestRegenerateSkipsPhashWhenPresent(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true}
	th := &fakeThumbs{}
	d := &fakeDecoder{}
	svc := newService(p, th, d)

	if err := svc.Regenerate(context.Background(), "ph1"); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if d.called || p.setCalled {
		t.Errorf("present pHash should not decode (%v) or store (%v)", d.called, p.setCalled)
	}
}

// TestRegenerateThumbnailError verifies a thumbnail generation failure is
// returned (retryable).
func TestRegenerateThumbnailError(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{}, &fakeThumbs{err: errors.New("decode failed")}, &fakeDecoder{})
	if err := svc.Regenerate(context.Background(), "ph1"); err == nil {
		t.Error("Regenerate should propagate a thumbnail error")
	}
}

// TestForceRegenerateRefreshesEvenWhenPhashPresent verifies the force path
// overwrites the thumbnails and recomputes the pHash even when one already
// exists (unlike the idempotent Regenerate), returning the regenerated sizes.
func TestForceRegenerateRefreshesEvenWhenPhashPresent(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true}
	th := &fakeThumbs{}
	d := &fakeDecoder{}
	svc := newService(p, th, d)

	sizes, err := svc.ForceRegenerate(context.Background(), "ph1")
	if err != nil {
		t.Fatalf("ForceRegenerate: %v", err)
	}
	if th.regenCalls != 1 || th.calls != 0 {
		t.Errorf("force path should call RegenerateAll (%d) not GenerateAll (%d)", th.regenCalls, th.calls)
	}
	if !d.called || !p.setCalled {
		t.Errorf("force path should always decode (%v) and overwrite pHash (%v)", d.called, p.setCalled)
	}
	if len(sizes) != 1 || sizes[0] != "tile_500" {
		t.Errorf("regenerated sizes = %v, want [tile_500]", sizes)
	}
}

// TestForceRegenerateThumbnailErrorWrapped verifies a thumbnail failure (a
// missing or undecodable original) is wrapped with ErrRegenerateFailed so the
// HTTP layer can answer 422.
func TestForceRegenerateThumbnailErrorWrapped(t *testing.T) {
	t.Parallel()
	th := &fakeThumbs{regenErr: errors.New("no embedded preview")}
	svc := newService(&fakePhotos{}, th, &fakeDecoder{})

	_, err := svc.ForceRegenerate(context.Background(), "ph1")
	if !errors.Is(err, ErrRegenerateFailed) {
		t.Errorf("err = %v, want wrapping ErrRegenerateFailed", err)
	}
}

// TestForceRegeneratePhotoNotFound verifies a missing photo surfaces
// photos.ErrPhotoNotFound (mapped to 404) and not ErrRegenerateFailed.
func TestForceRegeneratePhotoNotFound(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{getErr: photos.ErrPhotoNotFound}, &fakeThumbs{}, &fakeDecoder{})

	_, err := svc.ForceRegenerate(context.Background(), "ph1")
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("err = %v, want photos.ErrPhotoNotFound", err)
	}
	if errors.Is(err, ErrRegenerateFailed) {
		t.Error("a missing photo must not be reported as a regeneration failure")
	}
}

// TestHandlePayload verifies Handle decodes the payload and rejects empty/invalid
// payloads permanently.
func TestHandlePayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload json.RawMessage
		wantErr bool
	}{
		{"valid", payload(t, "ph1"), false},
		{"empty uid", payload(t, ""), true},
		{"malformed", json.RawMessage("{not json"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newService(&fakePhotos{}, &fakeThumbs{}, &fakeDecoder{})
			err := svc.Handle(context.Background(), jobs.Job{Payload: tt.payload})
			if (err != nil) != tt.wantErr {
				t.Errorf("Handle err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestHandleMissingUID verifies an empty uid maps to ErrMissingPhotoUID.
func TestHandleMissingUID(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{}, &fakeThumbs{}, &fakeDecoder{})
	if err := svc.Handle(context.Background(), jobs.Job{Payload: payload(t, "")}); !errors.Is(err, ErrMissingPhotoUID) {
		t.Errorf("Handle err = %v, want ErrMissingPhotoUID", err)
	}
}

// TestNewPanicsOnNil verifies New panics when a collaborator is nil.
func TestNewPanicsOnNil(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New with nil deps should panic")
		}
	}()
	New(Config{})
}

// fakeLister is an in-memory PhotoLister returning canned uid slices.
type fakeLister struct {
	missing    []string
	active     []string
	missingErr error
	activeErr  error
}

func (f *fakeLister) ListPhotosMissingPhash(_ context.Context, _ int) ([]string, error) {
	return f.missing, f.missingErr
}

func (f *fakeLister) ListActiveUIDs(context.Context) ([]string, error) {
	return f.active, f.activeErr
}

// fakeEnqueuer models the queue's per-photo dedup: it records a job only the
// first time a uid is scheduled, mirroring jobs.Enqueuer (which swallows a
// duplicate and returns nil). It counts total calls and genuinely created jobs so
// a test can assert both the reported enqueued count and that repeats do not pile
// up redundant jobs.
type fakeEnqueuer struct {
	pending map[string]bool
	created int
	calls   int
	err     error
}

func newFakeEnqueuer() *fakeEnqueuer { return &fakeEnqueuer{pending: map[string]bool{}} }

func (f *fakeEnqueuer) EnqueueThumbnail(_ context.Context, photoUID string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	if !f.pending[photoUID] {
		f.pending[photoUID] = true
		f.created++
	}
	return nil
}

// newBackfillService wires a Service with the backfill collaborators over the
// three no-op core fakes.
func newBackfillService(l PhotoLister, e Enqueuer) *Service {
	return New(Config{
		Photos: &fakePhotos{}, Thumbnailer: &fakeThumbs{}, Decoder: &fakeDecoder{},
		Lister: l, Enqueuer: e,
	})
}

// TestBackfillThumbnails_missing enqueues a job per photo missing a thumbnail
// (no pHash) and reports the count.
func TestBackfillThumbnails_missing(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	svc := newBackfillService(&fakeLister{missing: []string{"a", "b", "c"}}, enq)

	n, err := svc.BackfillThumbnails(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillThumbnails: %v", err)
	}
	if n != 3 {
		t.Errorf("enqueued = %d, want 3", n)
	}
	if enq.created != 3 {
		t.Errorf("jobs created = %d, want 3", enq.created)
	}
}

// TestBackfillThumbnails_all enqueues a job for every non-archived photo when the
// full-re-run flag is set, using the active listing rather than the missing one.
func TestBackfillThumbnails_all(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	svc := newBackfillService(&fakeLister{missing: []string{"a"}, active: []string{"a", "b", "c", "d"}}, enq)

	n, err := svc.BackfillThumbnails(context.Background(), true)
	if err != nil {
		t.Fatalf("BackfillThumbnails(all): %v", err)
	}
	if n != 4 {
		t.Errorf("enqueued = %d, want 4 (the active listing)", n)
	}
}

// TestBackfillThumbnails_idempotent verifies a repeat run relies on the queue's
// dedup so no redundant jobs pile up: the second call reports the same candidate
// count, yet no new jobs are created.
func TestBackfillThumbnails_idempotent(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	svc := newBackfillService(&fakeLister{missing: []string{"a", "b"}}, enq)

	first, err := svc.BackfillThumbnails(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillThumbnails #1: %v", err)
	}
	second, err := svc.BackfillThumbnails(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillThumbnails #2: %v", err)
	}
	if first != 2 || second != 2 {
		t.Errorf("enqueued = (%d, %d), want (2, 2)", first, second)
	}
	if enq.created != 2 {
		t.Errorf("jobs created across both runs = %d, want 2 (deduped)", enq.created)
	}
	if enq.calls != 4 {
		t.Errorf("enqueue calls = %d, want 4 (two per run)", enq.calls)
	}
}

// TestBackfillThumbnails_listError propagates a listing failure.
func TestBackfillThumbnails_listError(t *testing.T) {
	t.Parallel()
	svc := newBackfillService(&fakeLister{missingErr: errors.New("db down")}, newFakeEnqueuer())
	if _, err := svc.BackfillThumbnails(context.Background(), false); err == nil {
		t.Error("BackfillThumbnails should propagate a listing error")
	}
}

// TestBackfillThumbnails_enqueueError returns the count enqueued so far and the
// error when scheduling a job fails.
func TestBackfillThumbnails_enqueueError(t *testing.T) {
	t.Parallel()
	svc := newBackfillService(&fakeLister{missing: []string{"a"}}, &fakeEnqueuer{err: errors.New("queue full")})
	if _, err := svc.BackfillThumbnails(context.Background(), false); err == nil {
		t.Error("BackfillThumbnails should propagate an enqueue error")
	}
}

// TestBackfillThumbnails_unavailable verifies a Service built without the backfill
// collaborators reports ErrBackfillUnavailable rather than panicking.
func TestBackfillThumbnails_unavailable(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{}, &fakeThumbs{}, &fakeDecoder{})
	if _, err := svc.BackfillThumbnails(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("err = %v, want ErrBackfillUnavailable", err)
	}
}
