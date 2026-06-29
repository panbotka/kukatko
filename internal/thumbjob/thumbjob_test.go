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

// fakeThumbs records GenerateAll calls.
type fakeThumbs struct {
	calls int
	err   error
}

func (f *fakeThumbs) GenerateAll(context.Context, photos.Photo) (map[string]string, error) {
	f.calls++
	return map[string]string{}, f.err
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
