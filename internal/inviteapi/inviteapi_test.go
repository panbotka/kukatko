package inviteapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/invite"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// liveCode is a well-formed code the fake store knows.
const liveCode = "Ab3dEf7h"

// fakeStore is a Store over one canned invite; err, when set, is what every
// call returns, and created records the last Create input.
type fakeStore struct {
	inv     invite.Invite
	err     error
	created invite.NewInvite
	revoked string
}

// List returns the canned invite or f.err.
func (f *fakeStore) List(context.Context, string) ([]invite.Invite, error) {
	return []invite.Invite{f.inv}, f.err
}

// Create validates like the real store, records in and returns the canned
// invite with in's role.
func (f *fakeStore) Create(_ context.Context, _ string, in invite.NewInvite, _ audit.Meta) (invite.Invite, error) {
	f.created = in
	if err := in.Validate(); err != nil {
		return invite.Invite{}, err
	}
	inv := f.inv
	inv.Role = in.Role
	return inv, f.err
}

// Revoke records uid and returns the canned invite or f.err.
func (f *fakeStore) Revoke(_ context.Context, _, uid string, _ audit.Meta) (invite.Invite, error) {
	f.revoked = uid
	return f.inv, f.err
}

// Resolve answers the canned invite for liveCode and ErrInvalid otherwise.
func (f *fakeStore) Resolve(_ context.Context, code string) (invite.Invite, error) {
	if f.err != nil {
		return invite.Invite{}, f.err
	}
	if code != liveCode {
		return invite.Invite{}, invite.ErrInvalid
	}
	return f.inv, nil
}

// Accept answers the canned invite's project for liveCode and ErrInvalid
// otherwise.
func (f *fakeStore) Accept(_ context.Context, code, _ string, _ audit.Meta) (invite.Acceptance, error) {
	if code != liveCode {
		return invite.Acceptance{}, invite.ErrInvalid
	}
	return invite.Acceptance{ProjectSlug: f.inv.ProjectSlug, Role: f.inv.Role, Changed: true}, f.err
}

// RedeemTx answers like Resolve.
func (f *fakeStore) RedeemTx(ctx context.Context, _ pgx.Tx, code, _ string, _ audit.Meta) (invite.Invite, error) {
	return f.Resolve(ctx, code)
}

// passThrough is a guard that admits everybody.
func passThrough(next http.Handler) http.Handler { return next }

// newTestServer mounts both halves of an API over store, with every guard
// passing through and the public routes limited by limit (nil: unlimited).
func newTestServer(t *testing.T, store Store, limit *ratelimit.Limiter) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Store: store, BaseURL: "https://kukatko.example/", RequireAdmin: passThrough,
		OptionalAuth: passThrough, RequireSignedIn: passThrough, IPLimit: limit,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	api.RegisterPublicRoutes(r)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// send issues method path with body (JSON, none when empty) and returns the
// status and the body.
func send(t *testing.T, server *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp.StatusCode, buf.String()
}

// cannedInvite is a live invite of project "club".
func cannedInvite() invite.Invite {
	return invite.Invite{
		UID: "iv1", Role: project.RoleAdmin, ExpiresAt: time.Now().Add(time.Hour), Code: liveCode,
		ProjectUID: "pj1", ProjectSlug: "club", ProjectName: "The Club",
	}
}

// TestCreate_validation covers what POST /invites refuses before or through
// the store, and the defaults it passes on.
func TestCreate_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"defaults", `{"role":"viewer"}`, http.StatusCreated, ""},
		{"full", `{"role":"admin","days":90,"max_uses":3}`, http.StatusCreated, ""},
		{"bad role", `{"role":"maintainer"}`, http.StatusUnprocessableEntity, codeInvalidRole},
		{"zero days", `{"role":"viewer","days":0}`, http.StatusUnprocessableEntity, codeInvalidDays},
		{"too many days", `{"role":"viewer","days":91}`, http.StatusUnprocessableEntity, codeInvalidDays},
		{"zero cap", `{"role":"viewer","max_uses":0}`, http.StatusUnprocessableEntity, codeInvalidMaxUses},
		{"unknown field", `{"role":"viewer","code":"x"}`, http.StatusBadRequest, codeInvalidBody},
		{"not json", `nope`, http.StatusBadRequest, codeInvalidBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t, &fakeStore{inv: cannedInvite()}, nil)
			status, body := send(t, server, http.MethodPost, "/invites", tt.body)
			if status != tt.status || (tt.code != "" && !strings.Contains(body, `"error":"`+tt.code+`"`)) {
				t.Errorf("status %d body %s; want %d %q", status, body, tt.status, tt.code)
			}
		})
	}
}

// TestCreate_answersTheURL verifies the 201 carries the code and the full URL
// built from the base URL.
func TestCreate_answersTheURL(t *testing.T) {
	t.Parallel()

	store := &fakeStore{inv: cannedInvite()}
	server := newTestServer(t, store, nil)
	status, body := send(t, server, http.MethodPost, "/invites", `{"role":"editor","max_uses":2}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body %s", status, body)
	}
	var got inviteView
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	if got.URL != "https://kukatko.example/invite/"+liveCode || got.Code != liveCode || got.State != invite.StateActive {
		t.Errorf("got %+v; want the code, its URL and an active state", got)
	}
	if store.created.Role != project.RoleEditor || store.created.MaxUses == nil || *store.created.MaxUses != 2 ||
		store.created.Days != 0 {
		t.Errorf("store got %+v; want editor, cap 2, default days", store.created)
	}
}

// TestList_carriesBounds verifies the list carries every invite with its URL and
// the validity bounds.
func TestList_carriesBounds(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeStore{inv: cannedInvite()}, nil)
	status, body := send(t, server, http.MethodGet, "/invites", "")
	var got listResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil || status != http.StatusOK {
		t.Fatalf("status %d body %s: %v", status, body, err)
	}
	if len(got.Invites) != 1 || got.Invites[0].URL == "" || got.DefaultDays != invite.DefaultDays ||
		got.MaxDays != invite.MaxDays {
		t.Errorf("got %+v", got)
	}
}

// TestRevoke covers the 204 and the 404 of an unknown (or foreign) invite.
func TestRevoke(t *testing.T) {
	t.Parallel()

	store := &fakeStore{inv: cannedInvite()}
	server := newTestServer(t, store, nil)
	if status, body := send(t, server, http.MethodDelete, "/invites/iv1", ""); status != http.StatusNoContent ||
		store.revoked != "iv1" {
		t.Errorf("revoke: status %d body %s revoked %q", status, body, store.revoked)
	}
	missing := newTestServer(t, &fakeStore{err: invite.ErrNotFound}, nil)
	if status, body := send(t, missing, http.MethodDelete, "/invites/iv2", ""); status != http.StatusNotFound ||
		!strings.Contains(body, codeNotFound) {
		t.Errorf("missing: status %d body %s", status, body)
	}
}

// TestPublic covers the live page and the one 404 every unusable code gets.
func TestPublic(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeStore{inv: cannedInvite()}, nil)
	status, body := send(t, server, http.MethodGet, "/invite/"+liveCode, "")
	var got publicResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil || status != http.StatusOK {
		t.Fatalf("status %d body %s: %v", status, body, err)
	}
	want := publicResponse{ProjectName: "The Club", Role: project.RoleAdmin, RegistrationRole: project.RoleEditor}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	unknownStatus, unknownBody := send(t, server, http.MethodGet, "/invite/Zz9zZz9z", "")
	notFound := newTestServer(t, &fakeStore{err: invite.ErrNotFound}, nil)
	otherStatus, otherBody := send(t, notFound, http.MethodGet, "/invite/"+liveCode, "")
	if unknownStatus != http.StatusNotFound || unknownStatus != otherStatus || unknownBody != otherBody {
		t.Errorf("unknown %d %s vs not found %d %s; want one identical 404", unknownStatus, unknownBody,
			otherStatus, otherBody)
	}
}

// TestAccept covers the slug answer and the 404 of an unusable code.
func TestAccept(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeStore{inv: cannedInvite()}, nil)
	status, body := send(t, server, http.MethodPost, "/invite/"+liveCode+"/accept", "")
	if status != http.StatusOK || !strings.Contains(body, `"slug":"club"`) {
		t.Errorf("accept: status %d body %s", status, body)
	}
	if status, _ := send(t, server, http.MethodPost, "/invite/Zz9zZz9z/accept", ""); status != http.StatusNotFound {
		t.Errorf("unknown accept: status %d, want 404", status)
	}
}

// TestPublic_rateLimited verifies the public routes are throttled per IP ahead
// of the lookup: past the burst, even a live code is 429.
func TestPublic_rateLimited(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeStore{inv: cannedInvite()}, ratelimit.New(0.001, 2))
	for i := range 2 {
		if status, _ := send(t, server, http.MethodGet, "/invite/Zz9zZz9z", ""); status != http.StatusNotFound {
			t.Fatalf("request %d: status %d, want 404 within the burst", i, status)
		}
	}
	if status, _ := send(t, server, http.MethodGet, "/invite/"+liveCode, ""); status != http.StatusTooManyRequests {
		t.Errorf("past the burst: status %d, want 429", status)
	}
}

// TestInternalError verifies an unknown store failure is a bare 500.
func TestInternalError(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeStore{err: errors.New("boom")}, nil)
	status, body := send(t, server, http.MethodGet, "/invites", "")
	if status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("status %d body %s; want a bare 500", status, body)
	}
}

// TestRegistrationGate covers the grant of a live invite — capped role, its
// project, a redeem that succeeds — and the one refusal of an unusable code.
func TestRegistrationGate(t *testing.T) {
	t.Parallel()

	gate := NewRegistrationGate(&fakeStore{inv: cannedInvite()})
	grant, err := gate.AdmitInvite(t.Context(), liveCode)
	if err != nil {
		t.Fatalf("AdmitInvite: %v", err)
	}
	if grant.Role != project.RoleEditor || grant.ProjectUID != "pj1" || grant.InviteUID != "iv1" {
		t.Errorf("grant %+v; want an editor of pj1", grant)
	}
	if err := grant.Redeem(t.Context(), nil, "us1", audit.Meta{}); err != nil {
		t.Errorf("Redeem: %v", err)
	}
	if _, err := gate.AdmitInvite(t.Context(), "Zz9zZz9z"); !errors.Is(err, auth.ErrRegistrationInvite) {
		t.Errorf("unknown code: %v, want ErrRegistrationInvite", err)
	}
	boom := NewRegistrationGate(&fakeStore{err: errors.New("boom")})
	if _, err := boom.AdmitInvite(t.Context(), liveCode); err == nil || errors.Is(err, auth.ErrRegistrationInvite) {
		t.Errorf("store failure: %v, want a wrapped server error", err)
	}
}
