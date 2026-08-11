package ocrjob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// previewBytes is the fixed payload the fake previewer serves, so a test can
// assert the recogniser received exactly what the previewer handed over.
const previewBytes = "jpeg-bytes"

// fakePhotoStore is an in-memory PhotoStore recording what was saved.
type fakePhotoStore struct {
	photos  map[string]photos.Photo
	saved   map[string]photos.OCR
	saveErr error
}

// GetByUID returns the stored photo or photos.ErrPhotoNotFound.
func (f *fakePhotoStore) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	p, ok := f.photos[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return p, nil
}

// SaveOCR records the result under uid, or returns the configured error.
func (f *fakePhotoStore) SaveOCR(_ context.Context, uid string, result photos.OCR) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.saved == nil {
		f.saved = map[string]photos.OCR{}
	}
	f.saved[uid] = result
	return nil
}

// fakeRecognizer is a Recognizer returning a canned reading.
type fakeRecognizer struct {
	result  embedding.OCRResult
	err     error
	calls   int
	gotBody string
	gotConf float64
}

// ImageOCR records the call and the bytes it was handed, then returns the canned
// result or error.
func (f *fakeRecognizer) ImageOCR(
	_ context.Context, img io.Reader, minConfidence float64,
) (embedding.OCRResult, error) {
	f.calls++
	f.gotConf = minConfidence
	body, err := io.ReadAll(img)
	if err != nil {
		return embedding.OCRResult{}, err
	}
	f.gotBody = string(body)
	if f.err != nil {
		return embedding.OCRResult{}, f.err
	}
	return f.result, nil
}

// fakePreviewer serves a fixed preview payload and records what it was asked for.
type fakePreviewer struct {
	openErr error
	gotSize string
	calls   int
}

// OpenOrGenerate records the request and returns a fixed in-memory reader.
func (f *fakePreviewer) OpenOrGenerate(
	_ context.Context, _ photos.Photo, size string,
) (io.ReadCloser, error) {
	f.calls++
	f.gotSize = size
	if f.openErr != nil {
		return nil, f.openErr
	}
	return io.NopCloser(strings.NewReader(previewBytes)), nil
}

// fakeLister returns canned uid lists and records which list was asked for.
type fakeLister struct {
	missing     []string
	active      []string
	missingErr  error
	activeCalls int
}

func (f *fakeLister) ListPhotosMissingOCR(_ context.Context, _ int) ([]string, error) {
	if f.missingErr != nil {
		return nil, f.missingErr
	}
	return append([]string(nil), f.missing...), nil
}

func (f *fakeLister) ListActiveImageUIDs(_ context.Context) ([]string, error) {
	f.activeCalls++
	return append([]string(nil), f.active...), nil
}

// fakeEnqueuer records enqueued uids.
type fakeEnqueuer struct {
	enqueued []string
	err      error
}

func (f *fakeEnqueuer) EnqueueOCR(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, uid)
	return nil
}

// newService builds a Service over the supplied fakes with the package defaults.
func newService(ps PhotoStore, c Recognizer, p Previewer, cfg Config) *Service {
	cfg.Photos, cfg.Client, cfg.Previewer = ps, c, p
	return New(cfg)
}

// imagePhoto returns a still photo record with the given uid.
func imagePhoto(uid string) photos.Photo {
	return photos.Photo{UID: uid, MediaType: photos.MediaImage}
}

func TestRecognize_storesTextAndModel(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")}}
	rec := &fakeRecognizer{result: embedding.OCRResult{
		Text:  "VESELICE\nPout 2026",
		Model: "PP-OCRv5_mobile",
		Blocks: []embedding.OCRBlock{
			{Text: "VESELICE", BBox: [4]float64{1, 2, 3, 4}, Confidence: 0.99},
		},
	}}
	prev := &fakePreviewer{}
	svc := newService(ps, rec, prev, Config{MinConfidence: 0.7})

	if err := svc.Recognize(context.Background(), "ph1"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if rec.gotBody != previewBytes {
		t.Errorf("recogniser got %q, want the previewer's bytes", rec.gotBody)
	}
	if rec.gotConf != 0.7 {
		t.Errorf("min confidence = %v, want the configured 0.7", rec.gotConf)
	}
	if prev.gotSize != DefaultPreviewSize {
		t.Errorf("preview size = %q, want %q", prev.gotSize, DefaultPreviewSize)
	}
	got := ps.saved["ph1"]
	if got.Text != "VESELICE\nPout 2026" || got.Model != "PP-OCRv5_mobile" {
		t.Errorf("saved = %+v", got)
	}
}

// An empty reading is a success that must still be recorded, so the photo stops
// being a backfill candidate instead of coming back on every run forever.
func TestRecognize_emptyResultIsStored(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")}}
	rec := &fakeRecognizer{result: embedding.OCRResult{Text: "", Model: "PP-OCRv5_mobile"}}
	svc := newService(ps, rec, &fakePreviewer{}, Config{})

	if err := svc.Recognize(context.Background(), "ph1"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	got, ok := ps.saved["ph1"]
	if !ok {
		t.Fatal("nothing saved; an empty reading must still stamp the photo as read")
	}
	if got.Text != "" || got.Model != "PP-OCRv5_mobile" {
		t.Errorf("saved = %+v, want empty text with the model tag", got)
	}
}

// A forced re-run must be able to replace an earlier reading, so the handler
// never short-circuits on "this photo already has OCR".
func TestRecognize_overwritesEarlierReading(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{
		photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")},
		saved:  map[string]photos.OCR{"ph1": {Text: "old", Model: "old-model"}},
	}
	rec := &fakeRecognizer{result: embedding.OCRResult{Text: "new", Model: "new-model"}}
	svc := newService(ps, rec, &fakePreviewer{}, Config{})

	if err := svc.Recognize(context.Background(), "ph1"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if rec.calls != 1 {
		t.Errorf("recogniser calls = %d, want 1", rec.calls)
	}
	if got := ps.saved["ph1"]; got.Text != "new" || got.Model != "new-model" {
		t.Errorf("saved = %+v, want the new reading", got)
	}
}

// Videos are out of scope: no poster-frame recognition, and the job succeeds
// rather than dead-lettering work that was never meant to happen.
func TestRecognize_skipsVideo(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"ph1": {UID: "ph1", MediaType: photos.MediaVideo},
	}}
	rec := &fakeRecognizer{}
	prev := &fakePreviewer{}
	svc := newService(ps, rec, prev, Config{})

	if err := svc.Recognize(context.Background(), "ph1"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if rec.calls != 0 || prev.calls != 0 {
		t.Errorf("recogniser calls=%d previewer calls=%d, want 0/0", rec.calls, prev.calls)
	}
	if len(ps.saved) != 0 {
		t.Errorf("saved %d rows, want none", len(ps.saved))
	}
}

// An offline box defers the job instead of consuming a retry attempt, exactly as
// image_embed does, so the work completes once the box comes back.
func TestRecognize_offlineSidecarDefers(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")}}
	rec := &fakeRecognizer{err: embedding.ErrUnavailable}
	svc := newService(ps, rec, &fakePreviewer{}, Config{OfflineRetryDelay: 42 * time.Second})

	err := svc.Recognize(context.Background(), "ph1")
	var retry *worker.RetryAfterError
	if !errors.As(err, &retry) {
		t.Fatalf("err = %v, want a worker.RetryAfterError", err)
	}
	if retry.Delay != 42*time.Second {
		t.Errorf("retry delay = %v, want 42s", retry.Delay)
	}
	if len(ps.saved) != 0 {
		t.Errorf("saved %d rows, want none when the box is down", len(ps.saved))
	}
}

func TestRecognize_errors(t *testing.T) {
	t.Parallel()

	previewFail := errors.New("no preview")
	saveFail := errors.New("db down")
	recogniseFail := errors.New("bad response")

	tests := map[string]struct {
		uid   string
		setup func(*fakePhotoStore, *fakeRecognizer, *fakePreviewer)
		want  error
	}{
		"missing photo": {
			uid:   "nope",
			setup: func(*fakePhotoStore, *fakeRecognizer, *fakePreviewer) {},
			want:  photos.ErrPhotoNotFound,
		},
		"preview fails": {
			uid:   "ph1",
			setup: func(_ *fakePhotoStore, _ *fakeRecognizer, p *fakePreviewer) { p.openErr = previewFail },
			want:  previewFail,
		},
		"recogniser fails": {
			uid:   "ph1",
			setup: func(_ *fakePhotoStore, r *fakeRecognizer, _ *fakePreviewer) { r.err = recogniseFail },
			want:  recogniseFail,
		},
		"save fails": {
			uid:   "ph1",
			setup: func(s *fakePhotoStore, _ *fakeRecognizer, _ *fakePreviewer) { s.saveErr = saveFail },
			want:  saveFail,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")}}
			rec := &fakeRecognizer{}
			prev := &fakePreviewer{}
			tc.setup(ps, rec, prev)

			err := newService(ps, rec, prev, Config{}).Recognize(context.Background(), tc.uid)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHandle(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		payload string
		wantErr error
	}{
		"malformed payload":   {payload: "{", wantErr: nil}, // any error will do; checked below
		"missing photo_uid":   {payload: `{}`, wantErr: ErrMissingPhotoUID},
		"empty photo_uid":     {payload: `{"photo_uid":""}`, wantErr: ErrMissingPhotoUID},
		"unknown photo fails": {payload: `{"photo_uid":"nope"}`, wantErr: photos.ErrPhotoNotFound},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newService(&fakePhotoStore{}, &fakeRecognizer{}, &fakePreviewer{}, Config{})
			err := svc.Handle(context.Background(), jobs.Job{Payload: json.RawMessage(tc.payload)})
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestHandle_recognisesPayloadPhoto(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": imagePhoto("ph1")}}
	rec := &fakeRecognizer{result: embedding.OCRResult{Text: "sign", Model: "m"}}
	svc := newService(ps, rec, &fakePreviewer{}, Config{})

	job := jobs.Job{Type: jobs.TypeOCR, Payload: json.RawMessage(`{"photo_uid":"ph1"}`)}
	if err := svc.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := ps.saved["ph1"]; got.Text != "sign" {
		t.Errorf("saved = %+v", got)
	}
}

func TestBackfillOCR(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		all          bool
		wantEnqueued []string
		wantActive   int
	}{
		"pending only": {all: false, wantEnqueued: []string{"ph1", "ph2"}, wantActive: 0},
		"forced full":  {all: true, wantEnqueued: []string{"ph1", "ph2", "ph3"}, wantActive: 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lister := &fakeLister{
				missing: []string{"ph1", "ph2"},
				active:  []string{"ph1", "ph2", "ph3"},
			}
			enq := &fakeEnqueuer{}
			svc := newService(&fakePhotoStore{}, &fakeRecognizer{}, &fakePreviewer{}, Config{
				Lister: lister, Enqueuer: enq,
			})

			n, err := svc.BackfillOCR(context.Background(), tc.all)
			if err != nil {
				t.Fatalf("BackfillOCR: %v", err)
			}
			if n != len(tc.wantEnqueued) {
				t.Errorf("enqueued = %d, want %d", n, len(tc.wantEnqueued))
			}
			if strings.Join(enq.enqueued, ",") != strings.Join(tc.wantEnqueued, ",") {
				t.Errorf("enqueued = %v, want %v", enq.enqueued, tc.wantEnqueued)
			}
			if lister.activeCalls != tc.wantActive {
				t.Errorf("ListActiveImageUIDs calls = %d, want %d", lister.activeCalls, tc.wantActive)
			}
		})
	}
}

func TestBackfillOCR_withoutCollaborators(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotoStore{}, &fakeRecognizer{}, &fakePreviewer{}, Config{})
	if _, err := svc.BackfillOCR(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Fatalf("err = %v, want ErrBackfillUnavailable", err)
	}
}

func TestBackfillOCR_enqueueFailureStopsAndReportsProgress(t *testing.T) {
	t.Parallel()

	boom := errors.New("queue down")
	lister := &fakeLister{missing: []string{"ph1", "ph2"}}
	svc := newService(&fakePhotoStore{}, &fakeRecognizer{}, &fakePreviewer{}, Config{
		Lister: lister, Enqueuer: &fakeEnqueuer{err: boom},
	})

	n, err := svc.BackfillOCR(context.Background(), false)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the enqueue failure", err)
	}
	if n != 0 {
		t.Errorf("enqueued = %d, want 0", n)
	}
}

func TestNew_defaultsAndValidation(t *testing.T) {
	t.Parallel()

	svc := New(Config{Photos: &fakePhotoStore{}, Client: &fakeRecognizer{}, Previewer: &fakePreviewer{}})
	if svc.previewSize != DefaultPreviewSize {
		t.Errorf("previewSize = %q, want %q", svc.previewSize, DefaultPreviewSize)
	}
	if svc.retryDelay != DefaultOfflineRetryDelay {
		t.Errorf("retryDelay = %v, want %v", svc.retryDelay, DefaultOfflineRetryDelay)
	}

	defer func() {
		if recover() == nil {
			t.Error("expected New to panic without its required collaborators")
		}
	}()
	New(Config{})
}
