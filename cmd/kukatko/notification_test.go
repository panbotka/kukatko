package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/notificationapi"
	"github.com/panbotka/kukatko/internal/push"
)

// TestPushSettings_neverCarriesThePrivateKey follows the real configuration —
// both halves of the VAPID pair set, as in production — through the wiring to
// the capability response, and asserts the private key comes out nowhere.
func TestPushSettings_neverCarriesThePrivateKey(t *testing.T) {
	t.Parallel()
	pair, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	cfg := &config.Config{}
	cfg.Push.Enabled = true
	cfg.Push.VAPID.PublicKey = pair.PublicKey
	cfg.Push.VAPID.PrivateKey = pair.PrivateKey
	cfg.Push.VAPID.Subject = "mailto:ops@example.test"

	settings := pushSettings(cfg)
	if !settings.Enabled || settings.PublicKey != pair.PublicKey {
		t.Fatalf("pushSettings = %+v, want enabled with the public key", settings)
	}

	api := notificationapi.NewAPI(notificationapi.Config{
		Push:        settings,
		RequireAuth: func(next http.Handler) http.Handler { return next },
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/push/config", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /push/config: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), pair.PublicKey) {
		t.Fatalf("GET /push/config = %d %s, want 200 with the public key", resp.StatusCode, body)
	}
	if strings.Contains(string(body), pair.PrivateKey) {
		t.Fatalf("the capability response carries the VAPID private key: %s", body)
	}
}
