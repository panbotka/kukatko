package photoapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// errStorageRead is the cause the failing store reports, so the test can find it
// in the log line.
var errStorageRead = errors.New("simulated storage read failure")

// unreadableStore is a filesystem store whose originals cannot be materialized,
// which turns every thumbnail cache miss into a server-side failure.
type unreadableStore struct{ *storage.FS }

// Materialize always fails with errStorageRead.
func (unreadableStore) Materialize(context.Context, string) (string, func(), error) {
	return "", func() {}, errStorageRead
}

// onePhoto is a photoFinder answering one fixed photo for every uid.
type onePhoto struct{ photo photos.Photo }

// GetByUIDInProject returns the fixed photo.
func (f onePhoto) GetByUIDInProject(context.Context, string, string) (photos.Photo, error) {
	return f.photo, nil
}

// captureJSONLogs installs a JSON logger writing into a buffer as the
// process-wide slog default for the duration of the test, restoring the previous
// default afterwards. A test using it must NOT call t.Parallel: the default
// logger is global, and Go resumes parallel tests only once the sequential ones
// have finished, which is what keeps the buffer to one test.
func captureJSONLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// TestHandleThumb_failureLogsWhy pins the operator's side of a thumbnail 500:
// the access log records only the status, so one ERROR line must name the
// request, the photo, the size, the project and the wrapped cause — which step
// failed — while the client still learns nothing but "thumbnail unavailable".
func TestHandleThumb_failureLogsWhy(t *testing.T) {
	logs := captureJSONLogs(t)

	root := t.TempDir()
	fs, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	photo := photos.Photo{
		UID: "ph_fail", ProjectID: "pj_test", FilePath: "2024/01/a.jpg",
		FileHash: strings.Repeat("ab", 32), FileMime: "image/jpeg",
	}
	api := &API{
		finder:      onePhoto{photo: photo},
		thumbnailer: thumb.New(unreadableStore{fs}, filepath.Join(root, "cache")),
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Get("/photos/{uid}/thumb/{size}", api.handleThumb)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/photos/ph_fail/thumb/fit_1920", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-42")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), errStorageRead.Error()) {
		t.Errorf("the response leaks the internal cause: %s", rec.Body.String())
	}

	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("want exactly one JSON log line, got %q: %v", logs.String(), err)
	}
	want := map[string]string{
		"level":      "ERROR",
		"request_id": "req-42",
		"photo":      "ph_fail",
		"size":       "fit_1920",
		"project":    "pj_test",
	}
	for key, value := range want {
		if got, _ := line[key].(string); got != value {
			t.Errorf("log %s = %q, want %q (line %v)", key, got, value, line)
		}
	}
	cause, _ := line["error"].(string)
	for _, step := range []string{"materializing original", errStorageRead.Error()} {
		if !strings.Contains(cause, step) {
			t.Errorf("log error %q does not name %q", cause, step)
		}
	}
}
