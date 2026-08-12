package photoapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// decodeEditBody runs decodeEdit over a raw JSON string via a test request.
func decodeEditBody(t *testing.T, body string) (editBody, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPut, "/photos/p1/edit", strings.NewReader(body),
	)
	return decodeEdit(req)
}

func TestDecodeEdit_rejectsUnknownField(t *testing.T) {
	t.Parallel()
	if _, err := decodeEditBody(t, `{"sharpen": 1}`); err == nil {
		t.Error("decodeEdit accepted an unknown field, want error")
	}
}

func TestDecodeEdit_rejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	if _, err := decodeEditBody(t, `{not json}`); err == nil {
		t.Error("decodeEdit accepted malformed JSON, want error")
	}
}

func TestDecodeEdit_valid(t *testing.T) {
	t.Parallel()
	body, err := decodeEditBody(t, `{"rotation": 90, "brightness": 0.2, "contrast": -0.1}`)
	if err != nil {
		t.Fatalf("decodeEdit: %v", err)
	}
	if body.Rotation != 90 || body.Brightness != 0.2 || body.Contrast != -0.1 {
		t.Errorf("decoded = %+v, want rotation 90 / brightness 0.2 / contrast -0.1", body)
	}
}

func TestValidateEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    editBody
		wantErr bool
	}{
		{name: "neutral edit is valid", body: editBody{}, wantErr: false},
		{name: "full rotation and colour", body: editBody{Rotation: 270, Brightness: 1, Contrast: -1}},
		{name: "invalid rotation", body: editBody{Rotation: 45}, wantErr: true},
		{name: "brightness too high", body: editBody{Brightness: 1.5}, wantErr: true},
		{name: "brightness too low", body: editBody{Brightness: -2}, wantErr: true},
		{name: "contrast out of range", body: editBody{Contrast: 2}, wantErr: true},
		{
			name: "valid crop",
			body: editBody{CropX: new(0.1), CropY: new(0.1), CropW: new(0.5), CropH: new(0.5)},
		},
		{
			name:    "partial crop rejected",
			body:    editBody{CropX: new(0.1), CropW: new(0.5)},
			wantErr: true,
		},
		{
			name:    "crop out of bounds",
			body:    editBody{CropX: new(0.6), CropY: new(0.0), CropW: new(0.6), CropH: new(0.5)},
			wantErr: true,
		},
		{
			name:    "crop with zero size",
			body:    editBody{CropX: new(0.0), CropY: new(0.0), CropW: new(0.0), CropH: new(0.5)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateEdit(tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEdit(%+v) error = %v, wantErr = %v", tt.body, err, tt.wantErr)
			}
		})
	}
}

// fakeThumbnailEnqueuer is a ThumbnailEnqueuer recording the forced rebuilds it
// was asked for, and optionally failing them.
type fakeThumbnailEnqueuer struct {
	uids []string
	err  error
}

// EnqueueThumbnailRebuild records photoUID and returns the preset error.
func (f *fakeThumbnailEnqueuer) EnqueueThumbnailRebuild(_ context.Context, photoUID string) error {
	f.uids = append(f.uids, photoUID)
	return f.err
}

// TestEnqueueThumbnailRebuild covers the three shapes of the best-effort enqueue:
// it schedules the rebuild, it is a no-op without an enqueuer or a uid (a zero-value
// API built by a test must not panic), and a failing enqueuer is swallowed — the
// edit is already saved, and reporting an error would say the rotation was lost.
func TestEnqueueThumbnailRebuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enqueuer *fakeThumbnailEnqueuer
		uid      string
		wantUIDs []string
	}{
		{name: "schedules the rebuild", enqueuer: &fakeThumbnailEnqueuer{}, uid: "ph1", wantUIDs: []string{"ph1"}},
		{name: "no enqueuer wired", enqueuer: nil, uid: "ph1"},
		{name: "empty uid asks nothing", enqueuer: &fakeThumbnailEnqueuer{}, uid: ""},
		{
			name:     "a failure is swallowed",
			enqueuer: &fakeThumbnailEnqueuer{err: errors.New("queue down")},
			uid:      "ph1",
			wantUIDs: []string{"ph1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{}
			if tt.enqueuer != nil {
				api.thumbnails = tt.enqueuer
			}
			api.enqueueThumbnailRebuild(t.Context(), tt.uid)
			if tt.enqueuer == nil {
				return
			}
			if len(tt.enqueuer.uids) != len(tt.wantUIDs) {
				t.Fatalf("enqueued %v, want %v", tt.enqueuer.uids, tt.wantUIDs)
			}
			for i, uid := range tt.wantUIDs {
				if tt.enqueuer.uids[i] != uid {
					t.Errorf("enqueued[%d] = %q, want %q", i, tt.enqueuer.uids[i], uid)
				}
			}
		})
	}
}

// TestRecordEditAudit_noRecorder confirms the audit is optional: an API without a
// recorder saves the edit and records nothing rather than panicking.
func TestRecordEditAudit_noRecorder(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/photos/p1/edit", nil)
	(&API{}).recordEditAudit(req, photos.Edit{PhotoUID: "p1", Rotation: 90})
}

func TestEditedFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "jpg keeps base", in: "beach.jpg", want: "beach.jpg"},
		{name: "heic becomes jpg", in: "IMG_1234.heic", want: "IMG_1234.jpg"},
		{name: "no extension gets jpg", in: "scan", want: "scan.jpg"},
		{name: "empty falls back", in: "", want: "download.jpg"},
		{name: "leading dot is not an extension", in: ".hidden", want: ".hidden.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editedFileName(tt.in); got != tt.want {
				t.Errorf("editedFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
