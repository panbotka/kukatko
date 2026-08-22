package settingsapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// fakeStore is an in-memory settingsapi.Store for handler tests. The err fields
// force a specific error from the matching method; the recorded fields capture
// what the handler passed down.
type fakeStore struct {
	current   settings.Settings
	getErr    error
	setResult settings.Settings
	setErr    error

	lastUpdate settings.Update
	lastActor  string
	lastEntry  audit.Entry
}

// Get returns the configured current settings (or error).
func (f *fakeStore) Get(context.Context) (settings.Settings, error) {
	return f.current, f.getErr
}

// Set records its inputs and returns the configured result (or error).
func (f *fakeStore) Set(
	_ context.Context, in settings.Update, actorUID string, entry audit.Entry,
) (settings.Settings, error) {
	f.lastUpdate = in
	f.lastActor = actorUID
	f.lastEntry = entry
	return f.setResult, f.setErr
}

// passThrough is a no-op guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler { return next }

// newServer mounts an API backed by store behind pass-through guards.
func newServer(store settingsapi.Store) http.Handler {
	api := settingsapi.NewAPI(settingsapi.Config{
		Store:        store,
		RequireAuth:  passThrough,
		RequireAdmin: passThrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// do issues a request against the mounted API and returns the recorder.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals the recorder body into a generic map for assertions.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return body
}

// full is a settings record with all three values set, used as the store's state.
func full() settings.Settings {
	return settings.Settings{
		RegistrationEnabled: true,
		RegistrationSecret:  "rodina2026",
		WelcomeMarkdown:     "# Vítej",
		UpdatedAt:           time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
		UpdatedByUID:        "us_admin",
	}
}

// TestPublicReturnsOnlyTheFlag: the anonymous endpoint answers exactly one
// field, so neither the secret nor the welcome text can leak from it.
func TestPublicReturnsOnlyTheFlag(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeStore{current: full()}), http.MethodGet, "/settings/public", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if len(body) != 1 {
		t.Fatalf("public body = %v, want exactly one field", body)
	}
	if body["registration_enabled"] != true {
		t.Errorf("registration_enabled = %v, want true", body["registration_enabled"])
	}
}

// TestWelcomeReturnsOnlyTheMarkdown: the authenticated endpoint answers the
// greeting and nothing else — a signed-in viewer must not see the secret.
func TestWelcomeReturnsOnlyTheMarkdown(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeStore{current: full()}), http.MethodGet, "/settings/welcome", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if len(body) != 1 {
		t.Fatalf("welcome body = %v, want exactly one field", body)
	}
	if body["welcome_markdown"] != "# Vítej" {
		t.Errorf("welcome_markdown = %v, want the greeting", body["welcome_markdown"])
	}
}

// TestGetReturnsTheSecret: the admin record carries the readable secret, which
// is the reason the route is admin-only.
func TestGetReturnsTheSecret(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeStore{current: full()}), http.MethodGet, "/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["registration_secret"] != "rodina2026" {
		t.Errorf("registration_secret = %v, want the stored secret", body["registration_secret"])
	}
	if body["updated_at"] != "2026-08-23T10:00:00Z" {
		t.Errorf("updated_at = %v, want RFC3339", body["updated_at"])
	}
	if body["updated_by_uid"] != "us_admin" {
		t.Errorf("updated_by_uid = %v, want us_admin", body["updated_by_uid"])
	}
}

// TestGetStoreErrorIs500 maps an unexpected store failure to a 500 on every read.
func TestGetStoreErrorIs500(t *testing.T) {
	t.Parallel()
	srv := newServer(&fakeStore{getErr: errors.New("boom")})
	for _, target := range []string{"/settings/public", "/settings/welcome", "/settings"} {
		rec := do(t, srv, http.MethodGet, target, "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s status = %d, want 500", target, rec.Code)
		}
	}
}

// TestPutReplacesAllThree passes the decoded body straight to the store and
// writes the persisted record back.
func TestPutReplacesAllThree(t *testing.T) {
	t.Parallel()
	store := &fakeStore{setResult: full()}
	rec := do(t, newServer(store), http.MethodPut, "/settings",
		`{"registration_enabled":true,"registration_secret":"rodina2026","welcome_markdown":"# Vítej"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := settings.Update{
		RegistrationEnabled: true,
		RegistrationSecret:  "rodina2026",
		WelcomeMarkdown:     "# Vítej",
	}
	if store.lastUpdate != want {
		t.Fatalf("update passed down = %+v, want %+v", store.lastUpdate, want)
	}
	if store.lastEntry.Action != audit.ActionSettingsUpdate || store.lastEntry.TargetType != "settings" {
		t.Fatalf("audit entry = %+v, want a settings.update entry", store.lastEntry)
	}
	if got := store.lastEntry.Details["secret_set"]; got != true {
		t.Errorf("details.secret_set = %v, want true", got)
	}
	for key, value := range store.lastEntry.Details {
		if str, ok := value.(string); ok && strings.Contains(str, "rodina2026") {
			t.Fatalf("audit details %q leaked the secret: %q", key, str)
		}
	}
}

// TestPutRejectsSecretlessRegistration surfaces the store's refusal as a 400.
func TestPutRejectsSecretlessRegistration(t *testing.T) {
	t.Parallel()
	store := &fakeStore{setErr: settings.ErrSecretRequired}
	rec := do(t, newServer(store), http.MethodPut, "/settings",
		`{"registration_enabled":true,"registration_secret":"","welcome_markdown":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestPutRejectsBadBody: malformed JSON and an unknown field are both 400, and
// neither reaches the store.
func TestPutRejectsBadBody(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{`, `{"registration_enabled":true,"nope":1}`} {
		store := &fakeStore{}
		rec := do(t, newServer(store), http.MethodPut, "/settings", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s status = %d, want 400", body, rec.Code)
		}
		if store.lastActor != "" || store.lastUpdate != (settings.Update{}) {
			t.Errorf("PUT %s reached the store: %+v", body, store.lastUpdate)
		}
	}
}

// TestPutStoreErrorIs500 maps an unexpected store failure to a 500.
func TestPutStoreErrorIs500(t *testing.T) {
	t.Parallel()
	rec := do(t, newServer(&fakeStore{setErr: errors.New("boom")}), http.MethodPut, "/settings",
		`{"registration_enabled":false,"registration_secret":"","welcome_markdown":""}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
