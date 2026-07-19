package announcementapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/announcement"
	"github.com/panbotka/kukatko/internal/announcementapi"
	"github.com/panbotka/kukatko/internal/audit"
)

// fakeStore is an in-memory announcementapi.Store for handler tests. The err
// fields force a specific error from the matching method; the recorded fields
// capture the inputs the handler passed down.
type fakeStore struct {
	current   announcement.Announcement
	getErr    error
	setResult announcement.Announcement
	setErr    error
	clearErr  error

	lastSetMessage string
	lastSetLevel   string
	lastSetAuthor  string
	lastEntry      audit.Entry
	clearCalled    bool
}

// Get returns the configured current announcement (or error).
func (f *fakeStore) Get(context.Context) (announcement.Announcement, error) {
	return f.current, f.getErr
}

// Set records its inputs and returns the configured result (or error).
func (f *fakeStore) Set(
	_ context.Context, message, level, authorUID string, entry audit.Entry,
) (announcement.Announcement, error) {
	f.lastSetMessage = message
	f.lastSetLevel = level
	f.lastSetAuthor = authorUID
	f.lastEntry = entry
	return f.setResult, f.setErr
}

// Clear records that it was called and returns the configured error.
func (f *fakeStore) Clear(_ context.Context, entry audit.Entry) error {
	f.clearCalled = true
	f.lastEntry = entry
	return f.clearErr
}

// passThrough is a no-op guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler { return next }

// newServer mounts an API backed by store behind pass-through guards.
func newServer(store announcementapi.Store) http.Handler {
	api := announcementapi.NewAPI(announcementapi.Config{
		Store:             store,
		RequireAuth:       passThrough,
		RequireMaintainer: passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// do issues a request against the mounted API and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(context.Background(), method, target, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals the recorder body into a generic map for assertions.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return out
}

// TestGet_none returns 200 with an empty-message body when nothing is published.
func TestGet_none(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{getErr: announcement.ErrNotFound})
	rec := do(t, h, http.MethodGet, "/announcement", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["message"] != "" {
		t.Fatalf("message = %v, want empty", body["message"])
	}
	if _, ok := body["level"]; ok {
		t.Fatalf("level should be omitted when none published, got %v", body["level"])
	}
}

// TestGet_present writes the current announcement's message, level and timestamp.
func TestGet_present(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)
	h := newServer(&fakeStore{current: announcement.Announcement{
		Message: "Downtime tonight", Level: announcement.LevelWarning, AuthorUID: "u1", UpdatedAt: when,
	}})
	rec := do(t, h, http.MethodGet, "/announcement", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["message"] != "Downtime tonight" || body["level"] != "warning" {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["updated_at"] != when.Format(time.RFC3339Nano) {
		t.Fatalf("updated_at = %v, want %s", body["updated_at"], when.Format(time.RFC3339Nano))
	}
}

// TestGet_storeError maps an unexpected store error to 500.
func TestGet_storeError(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{getErr: context.DeadlineExceeded})
	rec := do(t, h, http.MethodGet, "/announcement", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestSet_ok publishes, passing the message/level to the store and recording an
// announcement.set audit entry, then echoes the persisted record.
func TestSet_ok(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 7, 19, 21, 0, 0, 0, time.UTC)
	store := &fakeStore{setResult: announcement.Announcement{
		Message: "Hello", Level: announcement.LevelInfo, UpdatedAt: when,
	}}
	h := newServer(store)
	rec := do(t, h, http.MethodPut, "/announcement", `{"message":"Hello","level":"info"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.lastSetMessage != "Hello" || store.lastSetLevel != "info" {
		t.Fatalf("store got message=%q level=%q", store.lastSetMessage, store.lastSetLevel)
	}
	if store.lastEntry.Action != audit.ActionAnnouncementSet {
		t.Fatalf("audit action = %q, want %q", store.lastEntry.Action, audit.ActionAnnouncementSet)
	}
	if store.lastEntry.Details["message"] != "Hello" {
		t.Fatalf("audit details message = %v, want Hello", store.lastEntry.Details["message"])
	}
	body := decode(t, rec)
	if body["message"] != "Hello" {
		t.Fatalf("echoed message = %v, want Hello", body["message"])
	}
}

// TestSet_badBody rejects a malformed JSON body with 400.
func TestSet_badBody(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{})
	rec := do(t, h, http.MethodPut, "/announcement", `{"message":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestSet_unknownField rejects a body carrying an unexpected field with 400.
func TestSet_unknownField(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{})
	rec := do(t, h, http.MethodPut, "/announcement", `{"message":"x","extra":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestSet_validationError maps the store's ErrEmptyMessage/ErrInvalidLevel to 400.
func TestSet_validationError(t *testing.T) {
	t.Parallel()
	for name, setErr := range map[string]error{
		"empty":    announcement.ErrEmptyMessage,
		"badLevel": announcement.ErrInvalidLevel,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newServer(&fakeStore{setErr: setErr})
			rec := do(t, h, http.MethodPut, "/announcement", `{"message":"x","level":"bogus"}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestSet_storeError maps an unexpected store error to 500.
func TestSet_storeError(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{setErr: context.DeadlineExceeded})
	rec := do(t, h, http.MethodPut, "/announcement", `{"message":"x"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestClear_ok clears the announcement, records an announcement.clear entry and
// answers 204.
func TestClear_ok(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	h := newServer(store)
	rec := do(t, h, http.MethodDelete, "/announcement", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !store.clearCalled {
		t.Fatal("store.Clear was not called")
	}
	if store.lastEntry.Action != audit.ActionAnnouncementClear {
		t.Fatalf("audit action = %q, want %q", store.lastEntry.Action, audit.ActionAnnouncementClear)
	}
}

// TestClear_storeError maps a store error to 500.
func TestClear_storeError(t *testing.T) {
	t.Parallel()
	h := newServer(&fakeStore{clearErr: context.DeadlineExceeded})
	rec := do(t, h, http.MethodDelete, "/announcement", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
