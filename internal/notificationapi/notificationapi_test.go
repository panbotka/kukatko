package notificationapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/push"
)

// passthrough admits every request without putting a user on the context, which
// is what makes the handlers' own defensive 401 observable.
func passthrough(next http.Handler) http.Handler { return next }

// newServer mounts an API with no stores under /api/v1. The routes it is used
// for never reach a store.
func newServer(t *testing.T, settings PushSettings) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Push: settings, RequireAuth: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// do issues a request against srv and returns the status and the body.
func do(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp.StatusCode, string(raw)
}

// TestPushConfig_reportsOnlyThePublicHalf pins the capability response: on, it
// carries the public key; off, it says so and carries no key at all. The private
// key has no way into the response — the API is never handed it — and the body
// has no field that could hold it.
func TestPushConfig_reportsOnlyThePublicHalf(t *testing.T) {
	t.Parallel()
	pair, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	tests := []struct {
		name    string
		enabled bool
		wantKey string
	}{
		{name: "enabled carries the public key", enabled: true, wantKey: pair.PublicKey},
		{name: "disabled carries no key", enabled: false, wantKey: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newServer(t, PushSettings{Enabled: tt.enabled, PublicKey: pair.PublicKey})
			status, body := do(t, srv, http.MethodGet, "/api/v1/push/config", "")
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", status, body)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if got["enabled"] != tt.enabled || got["public_key"] != tt.wantKey {
				t.Errorf("body = %s, want enabled %v and public_key %q", body, tt.enabled, tt.wantKey)
			}
			if len(got) != 2 {
				t.Errorf("body has %d fields, want exactly enabled and public_key: %s", len(got), body)
			}
			if strings.Contains(body, pair.PrivateKey) || strings.Contains(strings.ToLower(body), "private") {
				t.Errorf("the capability response mentions the private key: %s", body)
			}
		})
	}
}

// TestRoutes_withoutUserAre401 checks every route that needs the caller refuses
// a request the guard let through without one, before touching a store.
func TestRoutes_withoutUserAre401(t *testing.T) {
	t.Parallel()
	srv := newServer(t, PushSettings{Enabled: true})
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/push/subscriptions"},
		{http.MethodPost, "/api/v1/push/subscriptions"},
		{http.MethodDelete, "/api/v1/push/subscriptions?endpoint=https://push.example/x"},
		{http.MethodGet, "/api/v1/notifications/preferences"},
		{http.MethodPut, "/api/v1/notifications/preferences"},
		{http.MethodGet, "/api/v1/notifications/nt123"},
		{http.MethodPost, "/api/v1/notifications/nt123/read"},
	}
	for _, route := range routes {
		status, body := do(t, srv, route.method, route.path, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401: %s", route.method, route.path, status, body)
		}
	}
}

// TestNewAPI_throttleWrapsOnlyTheSubscribe checks the supplied throttle guards
// the subscription write and nothing else.
func TestNewAPI_throttleWrapsOnlyTheSubscribe(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	throttle := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
		})
	}
	api := NewAPI(Config{RequireAuth: passthrough, SubscribeThrottle: throttle})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if status, _ := do(t, srv, http.MethodPost, "/api/v1/push/subscriptions", "{}"); status != http.StatusTooManyRequests {
		t.Errorf("POST /push/subscriptions = %d, want the throttle's 429", status)
	}
	if status, _ := do(t, srv, http.MethodGet, "/api/v1/push/config", ""); status != http.StatusOK {
		t.Errorf("GET /push/config = %d, want 200 (not throttled)", status)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("throttle saw %d requests, want only the subscription write", got)
	}
}

// TestToSubscriptionView_dropsTheClientKeys pins that a subscription is never
// serialised with p256dh or auth, which are encryption secrets, not display data.
func TestToSubscriptionView_dropsTheClientKeys(t *testing.T) {
	t.Parallel()
	used := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	sub := push.Subscription{
		ID: "ps1", UserUID: "us1", Endpoint: "https://push.example/abc",
		P256dh: "P256DH-SECRET", Auth: "AUTH-SECRET", UserAgent: "Firefox",
		CreatedAt: used.Add(-time.Hour), LastUsedAt: &used, FailureCount: 3,
	}
	raw, err := json.Marshal(toSubscriptionView(sub))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	body := string(raw)
	for _, leak := range []string{"P256DH-SECRET", "AUTH-SECRET", "p256dh", `"auth"`, "keys", "us1"} {
		if strings.Contains(body, leak) {
			t.Errorf("view %s contains %q", body, leak)
		}
	}
	for _, want := range []string{`"id":"ps1"`, `"endpoint":"https://push.example/abc"`, `"user_agent":"Firefox"`,
		`"created_at"`, `"last_used_at"`} {
		if !strings.Contains(body, want) {
			t.Errorf("view %s lacks %s", body, want)
		}
	}
}

// TestDecodeSubscribe covers the subscription body: the browser's toJSON shape
// posts as it is, the user-agent falls back to the header, and missing fields
// or unknown ones are refused.
func TestDecodeSubscribe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		header  string
		wantUA  string
		wantErr bool
	}{
		{
			name:   "browser toJSON shape, header names the device",
			body:   `{"endpoint":" https://push.example/a ","expirationTime":null,"keys":{"p256dh":"k","auth":"a"}}`,
			header: "Mozilla/5.0 Phone", wantUA: "Mozilla/5.0 Phone",
		},
		{
			name:   "explicit user_agent wins",
			body:   `{"endpoint":"https://push.example/a","keys":{"p256dh":"k","auth":"a"},"user_agent":"Laptop"}`,
			header: "Mozilla/5.0 Phone", wantUA: "Laptop",
		},
		{name: "missing endpoint", body: `{"keys":{"p256dh":"k","auth":"a"}}`, wantErr: true},
		{name: "missing auth", body: `{"endpoint":"https://push.example/a","keys":{"p256dh":"k"}}`, wantErr: true},
		{name: "unknown field", body: `{"endpoint":"https://push.example/a","user_uid":"x"}`, wantErr: true},
		{name: "not JSON", body: `nope`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("User-Agent", tt.header)
			sub, err := decodeSubscribe(req, "us1")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeSubscribe = %+v, want an error", sub)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeSubscribe: %v", err)
			}
			if sub.UserUID != "us1" || sub.Endpoint != "https://push.example/a" || sub.P256dh != "k" ||
				sub.Auth != "a" || sub.UserAgent != tt.wantUA {
				t.Errorf("decodeSubscribe = %+v, want bound to us1 with user agent %q", sub, tt.wantUA)
			}
		})
	}
}

// TestTruncateUTF8 checks the user-agent cap never splits a character and drops
// bytes the TEXT column would refuse.
func TestTruncateUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short stays", in: "abc", max: 5, want: "abc"},
		{name: "ascii cut", in: "abcdef", max: 4, want: "abcd"},
		{name: "never splits a character", in: "ažž", max: 4, want: "až"},
		{name: "invalid bytes dropped", in: "a\xffb", max: 10, want: "ab"},
		{name: "zero keeps nothing", in: "abc", max: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateUTF8(tt.in, tt.max)
			if got != tt.want || !utf8.ValidString(got) {
				t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

// TestDecodePreferences covers the replace body: a read's shape (is_default
// included) posts back unchanged, an empty list resets everything, and an
// unknown kind, a missing enabled or a missing list are refused.
func TestDecodePreferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    int
		wantErr string
	}{
		{name: "read shape round-trips", body: `{"preferences":[{"kind":"tagged","enabled":false,"is_default":true}]}`,
			want: 1},
		{name: "empty list resets", body: `{"preferences":[]}`, want: 0},
		{name: "unknown kind", body: `{"preferences":[{"kind":"nope","enabled":true}]}`, wantErr: "unknown"},
		{name: "missing enabled", body: `{"preferences":[{"kind":"tagged"}]}`, wantErr: "enabled"},
		{name: "missing list", body: `{}`, wantErr: "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/", strings.NewReader(tt.body))
			prefs, err := decodePreferences(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("decodePreferences err = %v, want one mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodePreferences: %v", err)
			}
			if len(prefs) != tt.want {
				t.Fatalf("decodePreferences = %+v, want %d entries", prefs, tt.want)
			}
			if tt.want == 1 && (prefs[0].Kind != notification.KindTagged || prefs[0].Enabled) {
				t.Errorf("decodePreferences = %+v, want tagged off", prefs)
			}
		})
	}
}

// TestDroppedCount checks the dropped figure is the difference and never
// negative.
func TestDroppedCount(t *testing.T) {
	t.Parallel()
	tests := []struct{ total, visible, want int }{
		{12, 10, 2},
		{3, 3, 0},
		{0, 0, 0},
		{2, 3, 0},
	}
	for _, tt := range tests {
		if got := droppedCount(tt.total, tt.visible); got != tt.want {
			t.Errorf("droppedCount(%d, %d) = %d, want %d", tt.total, tt.visible, got, tt.want)
		}
	}
}

// TestOrderByUIDs checks the frozen order is restored and an unresolved uid is
// skipped.
func TestOrderByUIDs(t *testing.T) {
	t.Parallel()
	list := []photos.Photo{{UID: "c"}, {UID: "a"}}
	got := orderByUIDs([]string{"a", "b", "c"}, list)
	if len(got) != 2 || got[0].UID != "a" || got[1].UID != "c" {
		t.Errorf("orderByUIDs = %+v, want a then c", got)
	}
}

// TestSubscriptionIDFor checks the endpoint lookup among the caller's rows.
func TestSubscriptionIDFor(t *testing.T) {
	t.Parallel()
	subs := []push.Subscription{{ID: "ps1", Endpoint: "https://a"}, {ID: "ps2", Endpoint: "https://b"}}
	if id, ok := subscriptionIDFor(subs, "https://b"); !ok || id != "ps2" {
		t.Errorf("subscriptionIDFor(b) = %q, %v, want ps2, true", id, ok)
	}
	if id, ok := subscriptionIDFor(subs, "https://c"); ok {
		t.Errorf("subscriptionIDFor(c) = %q, true, want not found", id)
	}
}

// TestToNotificationView_omitsTheOwner checks the owner never reaches the
// client and an unread notification says read_at null.
func TestToNotificationView_omitsTheOwner(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(toNotificationView(notification.Notification{
		UID: "nt1", UserUID: "us-owner", Kind: notification.KindTagged, Title: "T",
	}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "us-owner") || !strings.Contains(string(raw), `"read_at":null`) {
		t.Errorf("view = %s, want no owner and read_at null", raw)
	}
}
