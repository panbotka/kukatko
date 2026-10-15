//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests pin that signing out forgets the push subscription of the browser
// it happens in: on a shared device the next person must not keep receiving the
// previous account's notifications (multi-tenancy review III, F3).

// Push endpoints of the fixtures: two of alice's devices and one of bob's.
const (
	aliceHere  = "https://push.example.test/send/alice-here"
	aliceThere = "https://push.example.test/send/alice-there"
	bobsDevice = "https://push.example.test/send/bob"
)

// seedPushSubscription stores a subscription row for userUID straight into the
// table — the sign-out reads nothing but the owner and the endpoint.
func seedPushSubscription(t *testing.T, db *database.DB, id, userUID, endpoint string) {
	t.Helper()
	_, err := db.Pool().Exec(t.Context(), `
INSERT INTO push_subscriptions (id, user_uid, endpoint, p256dh, auth)
VALUES ($1, $2, $3, 'p256dh', 'auth')`, id, userUID, endpoint)
	if err != nil {
		t.Fatalf("seeding push subscription %s: %v", id, err)
	}
}

// pushEndpoints returns the endpoints still stored, as a set.
func pushEndpoints(t *testing.T, db *database.DB) map[string]bool {
	t.Helper()
	rows, err := db.Pool().Query(t.Context(), `SELECT endpoint FROM push_subscriptions`)
	if err != nil {
		t.Fatalf("listing push subscriptions: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var endpoint string
		if err := rows.Scan(&endpoint); err != nil {
			t.Fatalf("scanning endpoint: %v", err)
		}
		got[endpoint] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating endpoints: %v", err)
	}
	return got
}

// logoutBody is the JSON body naming endpoint as the signing-out browser's.
func logoutBody(t *testing.T, endpoint string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"push_endpoint": endpoint})
	if err != nil {
		t.Fatalf("marshalling logout body: %v", err)
	}
	return string(b)
}

func TestHTTP_logoutForgetsThisDevicesPushSubscription(t *testing.T) {
	tests := []struct {
		name string
		// body is the logout request body.
		body func(t *testing.T) string
		// gone are the endpoints the sign-out must have deleted.
		gone []string
		// kept are the endpoints it must have left alone.
		kept []string
	}{
		{
			name: "the named device of the signing-out account goes, its other device stays",
			body: func(t *testing.T) string { return logoutBody(t, aliceHere) },
			gone: []string{aliceHere},
			kept: []string{aliceThere, bobsDevice},
		},
		{
			name: "another account's endpoint is never deleted",
			body: func(t *testing.T) string { return logoutBody(t, bobsDevice) },
			kept: []string{aliceHere, aliceThere, bobsDevice},
		},
		{
			name: "no body ends the session and forgets nothing",
			body: func(*testing.T) string { return "" },
			kept: []string{aliceHere, aliceThere, bobsDevice},
		},
		{
			name: "an unreadable body still signs out",
			body: func(*testing.T) string { return `{"push_endpoint":` },
			kept: []string{aliceHere, aliceThere, bobsDevice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newHTTPEnv(t, 10)
			alice := env.mustCreate(t, "alice", auth.AccountEditor)
			bob := env.mustCreate(t, "bob", auth.AccountEditor)
			seedPushSubscription(t, env.db, "psalicehere", alice.UID, aliceHere)
			seedPushSubscription(t, env.db, "psalicethere", alice.UID, aliceThere)
			seedPushSubscription(t, env.db, "psbob", bob.UID, bobsDevice)

			client := newClient(t)
			if status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
				loginJSON("alice", testPassword)); status != http.StatusOK {
				t.Fatalf("login status = %d, want 200 (body %s)", status, body)
			}
			if status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/logout",
				tt.body(t)); status != http.StatusNoContent {
				t.Fatalf("logout status = %d, want 204 (body %s)", status, body)
			}
			if status, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusUnauthorized {
				t.Errorf("me after logout status = %d, want 401", status)
			}

			stored := pushEndpoints(t, env.db)
			for _, endpoint := range tt.gone {
				if stored[endpoint] {
					t.Errorf("subscription %s survived the sign-out", endpoint)
				}
			}
			for _, endpoint := range tt.kept {
				if !stored[endpoint] {
					t.Errorf("subscription %s was deleted, want it kept", endpoint)
				}
			}
		})
	}
}

func TestHTTP_logoutWithoutSessionForgetsNothing(t *testing.T) {
	env := newHTTPEnv(t, 10)
	alice := env.mustCreate(t, "alice", auth.AccountEditor)
	seedPushSubscription(t, env.db, "psalicehere", alice.UID, aliceHere)

	// Nobody is signed in, so the endpoint names nobody's subscription: an
	// anonymous request must not be able to delete one.
	if status, body := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/logout",
		logoutBody(t, aliceHere)); status != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204 (body %s)", status, body)
	}
	if !pushEndpoints(t, env.db)[aliceHere] {
		t.Error("an anonymous logout deleted alice's subscription")
	}
}

// failingPushDevices is a PushDevices whose every delete fails.
type failingPushDevices struct{}

// DeleteForUserByEndpoint always fails.
func (failingPushDevices) DeleteForUserByEndpoint(context.Context, string, string) (int64, error) {
	return 0, errors.New("push store is down")
}

func TestHTTP_logoutSucceedsWhenForgettingThePushSubscriptionFails(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	api := auth.NewAPI(auth.APIConfig{
		Service:     svc,
		Limiter:     auth.NewLimiter(10, time.Minute),
		PushDevices: failingPushDevices{},
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterInstanceRoutes)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	env := &httpEnv{server: server, svc: svc, store: store, db: db}
	env.mustCreate(t, "alice", auth.AccountEditor)

	client := newClient(t)
	if status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("alice", testPassword)); status != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", status, body)
	}
	if status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/logout",
		logoutBody(t, aliceHere)); status != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204 (body %s)", status, body)
	}
	if status, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusUnauthorized {
		t.Errorf("me after logout status = %d, want 401", status)
	}
}
