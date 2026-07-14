package metajob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// errBoom is the generic failure a fake collaborator returns to check the error
// path.
var errBoom = errors.New("boom")

// fakePhotos is a PhotoStore recording what the service asked it to fill.
type fakePhotos struct {
	photo    photos.Photo
	getErr   error
	fillErr  error
	filled   []photos.FileMetadata
	filledAt []string
}

// GetByUID returns the configured photo, or the configured error.
func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	if f.getErr != nil {
		return photos.Photo{}, f.getErr
	}
	photo := f.photo
	photo.UID = uid
	return photo, nil
}

// FillFileMetadata records the fill and reports success, or the configured error.
func (f *fakePhotos) FillFileMetadata(_ context.Context, uid string, m photos.FileMetadata) (bool, error) {
	if f.fillErr != nil {
		return false, f.fillErr
	}
	f.filled = append(f.filled, m)
	f.filledAt = append(f.filledAt, uid)
	return true, nil
}

// fakeExtractor is an Extractor returning a canned extraction or error.
type fakeExtractor struct {
	meta  exif.Metadata
	err   error
	calls int
}

// ExtractOriginal returns the canned metadata, counting the call.
func (f *fakeExtractor) ExtractOriginal(_ context.Context, _ photos.Photo) (exif.Metadata, error) {
	f.calls++
	if f.err != nil {
		return exif.Metadata{}, f.err
	}
	return f.meta, nil
}

// fakeLister is a PhotoLister returning canned uid sets.
type fakeLister struct {
	pending []string
	active  []string
	err     error
}

// ListPhotosMissingFileMetadata returns the pending uids.
func (f *fakeLister) ListPhotosMissingFileMetadata(_ context.Context, _ int) ([]string, error) {
	return f.pending, f.err
}

// ListActiveUIDs returns every active uid.
func (f *fakeLister) ListActiveUIDs(_ context.Context) ([]string, error) {
	return f.active, f.err
}

// fakeEnqueuer is an Enqueuer recording the uids it was asked to schedule.
type fakeEnqueuer struct {
	uids []string
	err  error
}

// EnqueueMetadata records uid, or fails when configured to.
func (f *fakeEnqueuer) EnqueueMetadata(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, uid)
	return nil
}

// fullMeta is an extraction with every mapped field populated.
func fullMeta() exif.Metadata {
	return exif.Metadata{
		Subject:      "Summer holiday at the lake",
		Keywords:     "lake,summer",
		Artist:       "Jan Novák",
		Copyright:    "© 2023 Jan Novák",
		License:      "CC BY-NC 4.0",
		Software:     "Adobe Lightroom 12.4",
		CameraSerial: "SN-12345678",
		ColorProfile: "Display P3",
		ImageCodec:   "heic",
		Projection:   "equirectangular",
	}
}

// TestReextract_fillsEveryField checks the handler maps a full extraction onto the
// catalogue's fill, and reconstructs original_name from the stored file name (the
// file's own tags do not record it).
func TestReextract_fillsEveryField(t *testing.T) {
	t.Parallel()

	store := &fakePhotos{photo: photos.Photo{FileName: "IMG_0042.HEIC", MediaType: photos.MediaImage}}
	svc := New(Config{Photos: store, Extractor: &fakeExtractor{meta: fullMeta()}})

	if err := svc.Reextract(context.Background(), "pht1"); err != nil {
		t.Fatalf("Reextract() error = %v", err)
	}
	if len(store.filled) != 1 {
		t.Fatalf("filled %d times, want 1", len(store.filled))
	}
	got := store.filled[0]
	want := photos.FileMetadata{
		Subject: "Summer holiday at the lake", Keywords: "lake,summer",
		Artist: "Jan Novák", Copyright: "© 2023 Jan Novák", License: "CC BY-NC 4.0",
		Software: "Adobe Lightroom 12.4", CameraSerial: "SN-12345678",
		ColorProfile: "Display P3", ImageCodec: "heic", Projection: "equirectangular",
		OriginalName: "IMG_0042.HEIC",
	}
	if got != want {
		t.Errorf("filled = %+v, want %+v", got, want)
	}
	if store.filledAt[0] != "pht1" {
		t.Errorf("filled uid = %q, want pht1", store.filledAt[0])
	}
}

// TestReextract_videoKeepsImageCodecEmpty checks a video's compression is left to
// the ffprobe-derived video_codec: the still-image codec column stays empty even
// when the extractor guessed one from the container.
func TestReextract_videoKeepsImageCodecEmpty(t *testing.T) {
	t.Parallel()

	store := &fakePhotos{photo: photos.Photo{FileName: "clip.mp4", MediaType: photos.MediaVideo}}
	meta := fullMeta()
	meta.ImageCodec = "jpeg" // as if the extractor had read the poster frame
	svc := New(Config{Photos: store, Extractor: &fakeExtractor{meta: meta}})

	if err := svc.Reextract(context.Background(), "vid1"); err != nil {
		t.Fatalf("Reextract() error = %v", err)
	}
	if got := store.filled[0].ImageCodec; got != "" {
		t.Errorf("ImageCodec = %q, want empty for a video", got)
	}
	if got := store.filled[0].Artist; got != "Jan Novák" {
		t.Errorf("Artist = %q — the credit fields still apply to a video", got)
	}
}

// TestReextract_missingOriginalIsSkipped checks a photo whose original is gone from
// storage is skipped rather than failed: the job must not dead-letter, and a
// library-wide backfill must not stop on it.
func TestReextract_missingOriginalIsSkipped(t *testing.T) {
	t.Parallel()

	store := &fakePhotos{photo: photos.Photo{FileName: "gone.jpg"}}
	missing := fmt.Errorf("storage: materializing 2023/06/gone.jpg: %w", os.ErrNotExist)
	svc := New(Config{Photos: store, Extractor: &fakeExtractor{err: missing}})

	if err := svc.Reextract(context.Background(), "pht1"); err != nil {
		t.Fatalf("Reextract() error = %v, want nil (skip)", err)
	}
	if len(store.filled) != 0 {
		t.Errorf("filled %d times, want 0 — nothing was read", len(store.filled))
	}
}

// TestReextract_errors checks the failures that must reach the queue so the job is
// retried: a photo that cannot be loaded, a storage failure that is not a missing
// file, and a database failure while writing.
func TestReextract_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		store *fakePhotos
		extr  *fakeExtractor
		want  error
	}{
		{
			name:  "photo not found",
			store: &fakePhotos{getErr: photos.ErrPhotoNotFound},
			extr:  &fakeExtractor{},
			want:  photos.ErrPhotoNotFound,
		},
		{
			name:  "storage failure",
			store: &fakePhotos{},
			extr:  &fakeExtractor{err: errBoom},
			want:  errBoom,
		},
		{
			name:  "database failure",
			store: &fakePhotos{fillErr: errBoom},
			extr:  &fakeExtractor{},
			want:  errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := New(Config{Photos: tt.store, Extractor: tt.extr})
			err := svc.Reextract(context.Background(), "pht1")
			if !errors.Is(err, tt.want) {
				t.Errorf("Reextract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestHandle_payload checks the worker entry point: a well-formed payload runs the
// re-extraction, and a payload that can never succeed is a permanent error rather
// than an endless retry.
func TestHandle_payload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantErr error
		wantRun bool
	}{
		{name: "valid", payload: `{"photo_uid":"pht1"}`, wantRun: true},
		{name: "missing uid", payload: `{}`, wantErr: ErrMissingPhotoUID},
		{name: "malformed", payload: `not json`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &fakePhotos{}
			extr := &fakeExtractor{}
			svc := New(Config{Photos: store, Extractor: extr})

			err := svc.Handle(context.Background(), jobs.Job{Payload: json.RawMessage(tt.payload)})
			switch {
			case tt.wantRun && err != nil:
				t.Fatalf("Handle() error = %v, want nil", err)
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("Handle() error = %v, want %v", err, tt.wantErr)
			case !tt.wantRun && tt.wantErr == nil && err == nil:
				t.Fatal("Handle() error = nil, want a decoding error")
			}
			if ran := extr.calls > 0; ran != tt.wantRun {
				t.Errorf("extractor ran = %v, want %v", ran, tt.wantRun)
			}
		})
	}
}

// TestBackfillMetadata covers the backfill: it schedules the photos whose file has
// never been read, schedules every active photo under `all`, and refuses to run at
// all when the Service was built without the backfill collaborators.
func TestBackfillMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		all      bool
		lister   *fakeLister
		wantUIDs []string
	}{
		{
			name:     "pending only",
			lister:   &fakeLister{pending: []string{"a", "b"}, active: []string{"a", "b", "c"}},
			wantUIDs: []string{"a", "b"},
		},
		{
			name:     "all forces a full re-read",
			all:      true,
			lister:   &fakeLister{pending: []string{"a"}, active: []string{"a", "b", "c"}},
			wantUIDs: []string{"a", "b", "c"},
		},
		{
			name:     "nothing pending",
			lister:   &fakeLister{active: []string{"a"}},
			wantUIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq := &fakeEnqueuer{}
			svc := New(Config{
				Photos: &fakePhotos{}, Extractor: &fakeExtractor{},
				Lister: tt.lister, Enqueuer: enq,
			})

			enqueued, err := svc.BackfillMetadata(context.Background(), tt.all)
			if err != nil {
				t.Fatalf("BackfillMetadata() error = %v", err)
			}
			if enqueued != len(tt.wantUIDs) {
				t.Errorf("enqueued = %d, want %d", enqueued, len(tt.wantUIDs))
			}
			if len(enq.uids) != len(tt.wantUIDs) {
				t.Fatalf("scheduled %v, want %v", enq.uids, tt.wantUIDs)
			}
			for i, uid := range tt.wantUIDs {
				if enq.uids[i] != uid {
					t.Errorf("scheduled[%d] = %q, want %q", i, enq.uids[i], uid)
				}
			}
		})
	}
}

// TestBackfillMetadata_unavailable checks a Service built without the Lister and
// Enqueuer (the plain worker-handler wiring) reports the backfill as unavailable
// rather than silently doing nothing.
func TestBackfillMetadata_unavailable(t *testing.T) {
	t.Parallel()

	svc := New(Config{Photos: &fakePhotos{}, Extractor: &fakeExtractor{}})
	if _, err := svc.BackfillMetadata(context.Background(), false); !errors.Is(err, ErrBackfillUnavailable) {
		t.Errorf("BackfillMetadata() error = %v, want ErrBackfillUnavailable", err)
	}
}

// TestNew_requiresCollaborators checks the constructor refuses a Service that could
// never run a job.
func TestNew_requiresCollaborators(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New() without an Extractor should panic")
		}
	}()
	New(Config{Photos: &fakePhotos{}})
}
