package thumbjob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakePhotos is an in-memory PhotoStore tracking pHash and placeholder reads and
// writes.
type fakePhotos struct {
	photo     photos.Photo
	getErr    error
	hasPhash  bool
	phashErr  error
	setCalled bool
	setErr    error
	// savedBlurhash is the last placeholder written; blurCalls counts the writes
	// and blurErr fails them.
	savedBlurhash string
	blurCalls     int
	blurErr       error
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

func (f *fakePhotos) SaveBlurhash(_ context.Context, _, hash string) error {
	f.blurCalls++
	if f.blurErr != nil {
		return f.blurErr
	}
	f.savedBlurhash = hash
	return nil
}

// mustJPEG returns a small solid-colour JPEG, standing in for a cached preview
// rendition the placeholder is encoded from. It panics rather than returning an
// error so it can also build a package-level fixture.
func mustJPEG(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := range 24 {
		for x := range 32 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("thumbjob test: encoding preview: " + err.Error())
	}
	return buf.Bytes()
}

// defaultPreview is what fakeThumbs serves when a test does not care what the
// preview looks like.
var defaultPreview = mustJPEG(color.RGBA{R: 30, G: 90, B: 160, A: 255})

// fakeThumbs records GenerateAll, RegenerateAll and OpenOrGenerate calls. preview
// is the bytes OpenOrGenerate hands back; when it is nil a valid tiny JPEG is
// served, so a test only sets it to exercise a broken preview.
type fakeThumbs struct {
	calls      int
	regenCalls int
	err        error
	regenErr   error
	// preview and previewErr control OpenOrGenerate; openedSizes records which
	// sizes were asked for, in order.
	preview     []byte
	previewErr  error
	openedSizes []string
}

func (f *fakeThumbs) OpenOrGenerate(_ context.Context, _ photos.Photo, size string) (io.ReadCloser, error) {
	f.openedSizes = append(f.openedSizes, size)
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	if f.preview == nil {
		return io.NopCloser(bytes.NewReader(defaultPreview)), nil
	}
	return io.NopCloser(bytes.NewReader(f.preview)), nil
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

// forcedPayload builds a thumbnail job payload for uid that asks for the forced
// rebuild rather than the repair.
func forcedPayload(t *testing.T, uid string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"photo_uid": uid, "force": true})
	if err != nil {
		t.Fatalf("marshal forced payload: %v", err)
	}
	return raw
}

// TestHandleForcedPayloadRebuilds verifies the payload's force flag routes the job
// to the rebuild (RegenerateAll, which overwrites the cached sizes) rather than to
// the repair (GenerateAll, which skips them). It is what makes a saved edit
// actually reach the grid: the cache is keyed by the original's hash, so a repair
// would find every size present and change nothing.
func TestHandleForcedPayloadRebuilds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		payload    json.RawMessage
		wantGen    int
		wantRegen  int
		wantPhash  bool
		wantDecode bool
	}{
		// The repair path leaves an existing pHash alone, so it never decodes.
		{name: "plain payload repairs", payload: payload(t, "ph1"), wantGen: 1},
		{
			name: "forced payload rebuilds", payload: forcedPayload(t, "ph1"),
			wantRegen: 1, wantPhash: true, wantDecode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakePhotos{hasPhash: true}
			th := &fakeThumbs{}
			d := &fakeDecoder{}
			svc := newService(p, th, d)

			if err := svc.Handle(context.Background(), jobs.Job{Payload: tt.payload}); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if th.calls != tt.wantGen || th.regenCalls != tt.wantRegen {
				t.Errorf("GenerateAll/RegenerateAll calls = %d/%d, want %d/%d",
					th.calls, th.regenCalls, tt.wantGen, tt.wantRegen)
			}
			if p.setCalled != tt.wantPhash || d.called != tt.wantDecode {
				t.Errorf("pHash stored = %v / decoded = %v, want %v / %v",
					p.setCalled, d.called, tt.wantPhash, tt.wantDecode)
			}
		})
	}
}

// TestHandleForcedPayloadPropagatesFailure verifies a failed rebuild is returned
// (so the job retries) rather than swallowed by the force branch's discarded size
// list.
func TestHandleForcedPayloadPropagatesFailure(t *testing.T) {
	t.Parallel()
	th := &fakeThumbs{regenErr: errors.New("boom")}
	svc := newService(&fakePhotos{hasPhash: true}, th, &fakeDecoder{})

	err := svc.Handle(context.Background(), jobs.Job{Payload: forcedPayload(t, "ph1")})
	if !errors.Is(err, ErrRegenerateFailed) {
		t.Errorf("Handle err = %v, want it to wrap ErrRegenerateFailed", err)
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

// fakeLister is an in-memory PhotoLister returning canned uid slices. The counts
// it reports are derived from those slices, as the real store's are derived from
// the same predicates, so a dry run and the run it predicts cannot drift apart in
// the fake in a way they could not in Postgres.
type fakeLister struct {
	missing    []string
	active     []string
	missingErr error
	activeErr  error
	// missingBlurhash is the placeholder backfill's own predicate — a different
	// set from the missing-thumbnail one, which is exactly what the two backfills
	// differ in.
	missingBlurhash    []string
	missingBlurhashErr error
}

func (f *fakeLister) ListPhotosMissingPhash(_ context.Context, _ int) ([]string, error) {
	return f.missing, f.missingErr
}

func (f *fakeLister) ListActiveUIDs(context.Context) ([]string, error) {
	return f.active, f.activeErr
}

func (f *fakeLister) CountPhotosMissingPhash(context.Context) (int, error) {
	return len(f.missing), f.missingErr
}

func (f *fakeLister) CountActivePhotos(context.Context) (int, error) {
	return len(f.active), f.activeErr
}

func (f *fakeLister) ListPhotosMissingBlurhash(_ context.Context, _ int) ([]string, error) {
	return f.missingBlurhash, f.missingBlurhashErr
}

func (f *fakeLister) CountPhotosMissingBlurhash(context.Context) (int, error) {
	return len(f.missingBlurhash), f.missingBlurhashErr
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

// TestCountBackfillThumbnails_matchesTheRunItPredicts verifies the dry run
// answers the same number the real run would schedule, for both predicates, and
// schedules nothing while doing it. That is the whole point: an operator must be
// able to see a full-library run coming without starting one.
func TestCountBackfillThumbnails_matchesTheRunItPredicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		all  bool
		want int
	}{
		{name: "narrow predicate counts the unhashed photos", all: false, want: 3},
		{name: "forced full re-run counts every active photo", all: true, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lister := &fakeLister{
				missing: []string{"a", "b", "c"},
				active:  []string{"a", "b", "c", "d", "e"},
			}
			enq := newFakeEnqueuer()
			svc := newBackfillService(lister, enq)

			count, err := svc.CountBackfillThumbnails(context.Background(), tt.all)
			if err != nil {
				t.Fatalf("CountBackfillThumbnails: %v", err)
			}
			if count != tt.want {
				t.Errorf("count = %d, want %d", count, tt.want)
			}
			if enq.calls != 0 {
				t.Errorf("counting scheduled %d job(s), want none", enq.calls)
			}

			enqueued, err := svc.BackfillThumbnails(context.Background(), tt.all)
			if err != nil {
				t.Fatalf("BackfillThumbnails: %v", err)
			}
			if enqueued != count {
				t.Errorf("the run scheduled %d job(s) after a dry run promised %d", enqueued, count)
			}
		})
	}
}

// TestCountBackfillThumbnails_countError propagates a counting failure rather
// than reporting a reassuring zero.
func TestCountBackfillThumbnails_countError(t *testing.T) {
	t.Parallel()
	svc := newBackfillService(&fakeLister{missingErr: errors.New("db down")}, newFakeEnqueuer())
	if _, err := svc.CountBackfillThumbnails(context.Background(), false); err == nil {
		t.Error("CountBackfillThumbnails should propagate a counting error")
	}
}

// TestCountBackfillThumbnails_unavailable verifies a Service built without the
// backfill collaborators reports ErrBackfillUnavailable rather than panicking.
func TestCountBackfillThumbnails_unavailable(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{}, &fakeThumbs{}, &fakeDecoder{})
	if _, err := svc.CountBackfillThumbnails(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("err = %v, want ErrBackfillUnavailable", err)
	}
}

// TestRegenerateComputesMissingBlurhash verifies a photo with no placeholder gets
// one encoded from its preview rendition and stored.
func TestRegenerateComputesMissingBlurhash(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true}
	th := &fakeThumbs{}
	svc := newService(p, th, &fakeDecoder{})

	if err := svc.Regenerate(context.Background(), "ph1"); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if p.blurCalls != 1 {
		t.Fatalf("SaveBlurhash calls = %d, want 1", p.blurCalls)
	}
	if p.savedBlurhash == "" {
		t.Error("stored placeholder is empty")
	}
	if len(th.openedSizes) != 1 || th.openedSizes[0] != PlaceholderSize {
		t.Errorf("opened sizes = %v, want [%s]", th.openedSizes, PlaceholderSize)
	}
}

// TestRegenerateSkipsBlurhashWhenPresent verifies a photo that already carries a
// placeholder is not re-encoded — the idempotent skip that lets the backfill
// converge and a re-run cost nothing.
func TestRegenerateSkipsBlurhashWhenPresent(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true, photo: photos.Photo{Blurhash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj"}}
	th := &fakeThumbs{}
	svc := newService(p, th, &fakeDecoder{})

	if err := svc.Regenerate(context.Background(), "ph1"); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if p.blurCalls != 0 {
		t.Errorf("SaveBlurhash calls = %d, want 0", p.blurCalls)
	}
	if len(th.openedSizes) != 0 {
		t.Errorf("opened %v, want the preview left untouched", th.openedSizes)
	}
}

// TestForceRegenerateRefreshesBlurhash verifies the force path re-encodes the
// placeholder even when one exists, and does so *after* the thumbnails were
// rebuilt — a saved crop or rotation changes the rendering, and a placeholder
// read from the previous one would blur the wrong picture.
func TestForceRegenerateRefreshesBlurhash(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true, photo: photos.Photo{Blurhash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj"}}
	th := &fakeThumbs{}
	svc := newService(p, th, &fakeDecoder{})

	if _, err := svc.ForceRegenerate(context.Background(), "ph1"); err != nil {
		t.Fatalf("ForceRegenerate: %v", err)
	}
	if p.blurCalls != 1 {
		t.Fatalf("SaveBlurhash calls = %d, want 1", p.blurCalls)
	}
	if p.savedBlurhash == "" || p.savedBlurhash == "LEHV6nWB2yk8pyo0adR*.7kCMdnj" {
		t.Errorf("stored placeholder = %q, want a freshly encoded one", p.savedBlurhash)
	}
	if th.regenCalls != 1 {
		t.Errorf("RegenerateAll calls = %d, want 1 before the placeholder is read", th.regenCalls)
	}
}

// TestBlurhashReflectsThePreview verifies the placeholder actually describes the
// rendition it was read from: two different previews must not encode to the same
// string.
func TestBlurhashReflectsThePreview(t *testing.T) {
	t.Parallel()

	encode := func(t *testing.T, preview []byte) string {
		t.Helper()
		p := &fakePhotos{hasPhash: true}
		svc := newService(p, &fakeThumbs{preview: preview}, &fakeDecoder{})
		if err := svc.Regenerate(context.Background(), "ph1"); err != nil {
			t.Fatalf("Regenerate: %v", err)
		}
		return p.savedBlurhash
	}

	red := encode(t, mustJPEG(color.RGBA{R: 220, A: 255}))
	blue := encode(t, mustJPEG(color.RGBA{B: 220, A: 255}))
	if red == blue {
		t.Errorf("a red and a blue preview both encoded to %q", red)
	}
}

// TestRegenerateBlurhashPreviewError verifies an unreadable preview fails the job
// (retryable) rather than silently leaving the photo without a placeholder while
// reporting success.
func TestRegenerateBlurhashPreviewError(t *testing.T) {
	t.Parallel()
	th := &fakeThumbs{previewErr: errors.New("preview gone")}
	p := &fakePhotos{hasPhash: true}
	svc := newService(p, th, &fakeDecoder{})

	err := svc.Regenerate(context.Background(), "ph1")
	if err == nil {
		t.Fatal("Regenerate should propagate a preview failure")
	}
	if !strings.Contains(err.Error(), PlaceholderSize) {
		t.Errorf("err = %v, want it to name the preview size", err)
	}
	if p.blurCalls != 0 {
		t.Errorf("SaveBlurhash calls = %d, want 0 when the preview could not be read", p.blurCalls)
	}
}

// TestRegenerateBlurhashUndecodablePreview verifies bytes that are not an image
// are reported rather than stored as a placeholder.
func TestRegenerateBlurhashUndecodablePreview(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true}
	svc := newService(p, &fakeThumbs{preview: []byte("not a jpeg")}, &fakeDecoder{})

	if err := svc.Regenerate(context.Background(), "ph1"); err == nil {
		t.Error("Regenerate should propagate an undecodable preview")
	}
	if p.blurCalls != 0 {
		t.Errorf("SaveBlurhash calls = %d, want 0", p.blurCalls)
	}
}

// TestRegenerateBlurhashStoreError verifies a failed write is returned so the job
// retries.
func TestRegenerateBlurhashStoreError(t *testing.T) {
	t.Parallel()
	p := &fakePhotos{hasPhash: true, blurErr: errors.New("db down")}
	svc := newService(p, &fakeThumbs{}, &fakeDecoder{})

	if err := svc.Regenerate(context.Background(), "ph1"); err == nil {
		t.Error("Regenerate should propagate a placeholder store failure")
	}
}

// TestBackfillBlurhash_missing enqueues a thumbnail job per photo without a
// placeholder — its own predicate, not the missing-thumbnail one.
func TestBackfillBlurhash_missing(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	lister := &fakeLister{missing: []string{"a"}, missingBlurhash: []string{"x", "y", "z"}}
	svc := newBackfillService(lister, enq)

	n, err := svc.BackfillBlurhash(context.Background(), false)
	if err != nil {
		t.Fatalf("BackfillBlurhash: %v", err)
	}
	if n != 3 {
		t.Errorf("enqueued = %d, want 3 (the photos missing a placeholder)", n)
	}
	if enq.created != 3 {
		t.Errorf("jobs created = %d, want 3", enq.created)
	}
}

// TestBackfillBlurhash_all schedules every non-archived photo when the
// full-re-run flag is set.
func TestBackfillBlurhash_all(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	lister := &fakeLister{missingBlurhash: []string{"x"}, active: []string{"a", "b", "c", "d"}}
	svc := newBackfillService(lister, enq)

	n, err := svc.BackfillBlurhash(context.Background(), true)
	if err != nil {
		t.Fatalf("BackfillBlurhash(all): %v", err)
	}
	if n != 4 {
		t.Errorf("enqueued = %d, want 4 (the active listing)", n)
	}
}

// TestBackfillBlurhash_idempotent verifies a repeat run leans on the queue's
// dedup instead of piling up redundant jobs.
func TestBackfillBlurhash_idempotent(t *testing.T) {
	t.Parallel()
	enq := newFakeEnqueuer()
	svc := newBackfillService(&fakeLister{missingBlurhash: []string{"x", "y"}}, enq)

	for range 2 {
		if _, err := svc.BackfillBlurhash(context.Background(), false); err != nil {
			t.Fatalf("BackfillBlurhash: %v", err)
		}
	}
	if enq.created != 2 {
		t.Errorf("jobs created across both runs = %d, want 2 (deduped)", enq.created)
	}
}

// TestBackfillBlurhash_listError propagates a listing failure.
func TestBackfillBlurhash_listError(t *testing.T) {
	t.Parallel()
	svc := newBackfillService(&fakeLister{missingBlurhashErr: errors.New("db down")}, newFakeEnqueuer())
	if _, err := svc.BackfillBlurhash(context.Background(), false); err == nil {
		t.Error("BackfillBlurhash should propagate a listing error")
	}
}

// TestBackfillBlurhash_unavailable verifies a Service built without the backfill
// collaborators reports ErrBackfillUnavailable rather than panicking.
func TestBackfillBlurhash_unavailable(t *testing.T) {
	t.Parallel()
	svc := newService(&fakePhotos{}, &fakeThumbs{}, &fakeDecoder{})
	if _, err := svc.BackfillBlurhash(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("err = %v, want ErrBackfillUnavailable", err)
	}
	if _, err := svc.CountBackfillBlurhash(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("count err = %v, want ErrBackfillUnavailable", err)
	}
}

// TestCountBackfillBlurhash_matchesTheRunItPredicts verifies the dry run answers
// the number the real run schedules, for both predicates, without scheduling
// anything.
func TestCountBackfillBlurhash_matchesTheRunItPredicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		all  bool
		want int
	}{
		{name: "narrow predicate counts the photos with no placeholder", all: false, want: 2},
		{name: "forced full re-run counts every active photo", all: true, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lister := &fakeLister{
				missingBlurhash: []string{"x", "y"},
				active:          []string{"a", "b", "c", "d", "e"},
			}
			enq := newFakeEnqueuer()
			svc := newBackfillService(lister, enq)

			count, err := svc.CountBackfillBlurhash(context.Background(), tt.all)
			if err != nil {
				t.Fatalf("CountBackfillBlurhash: %v", err)
			}
			if count != tt.want {
				t.Errorf("count = %d, want %d", count, tt.want)
			}
			if enq.calls != 0 {
				t.Errorf("counting scheduled %d job(s), want none", enq.calls)
			}

			enqueued, err := svc.BackfillBlurhash(context.Background(), tt.all)
			if err != nil {
				t.Fatalf("BackfillBlurhash: %v", err)
			}
			if enqueued != count {
				t.Errorf("the run scheduled %d job(s) after a dry run promised %d", enqueued, count)
			}
		})
	}
}
