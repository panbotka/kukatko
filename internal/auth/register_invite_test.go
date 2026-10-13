package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// stubInviteGate is an InviteGate answering with a fixed error.
type stubInviteGate struct{ err error }

// AdmitInvite returns the fixed error.
func (g stubInviteGate) AdmitInvite(context.Context, string) (InviteGrant, error) {
	return InviteGrant{}, g.err
}

// TestAPI_register_invitePath verifies a registration naming an invite goes to
// the invite gate — and is refused as an invalid invite when no gate is wired
// or the gate refuses it — while one without an invite never consults it.
func TestAPI_register_invitePath(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/register", nil)
	closed := NewRegistration(RegistrationConfig{Settings: fakeSettings{}})
	tests := []struct {
		name string
		gate InviteGate
		in   RegisterInput
		want error
	}{
		{"no gate wired", nil, RegisterInput{InviteCode: "Ab3dEf7h"}, ErrRegistrationInvite},
		{"gate refuses", stubInviteGate{err: ErrRegistrationInvite}, RegisterInput{InviteCode: "Ab3dEf7h"},
			ErrRegistrationInvite},
		{"invite wins over a link", stubInviteGate{err: ErrRegistrationInvite},
			RegisterInput{InviteCode: "Ab3dEf7h", UploadLink: "Ab3dEf7h"}, ErrRegistrationInvite},
		{"no invite takes the secret path", stubInviteGate{err: errors.New("must not be called")},
			RegisterInput{}, ErrRegistrationClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &API{registration: closed, invites: tt.gate}
			if _, err := a.register(req, tt.in, "", audit.Entry{}); !errors.Is(err, tt.want) {
				t.Errorf("register error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestWriteRegisterError_invite verifies an unusable invite is a 403 naming the
// invitation, nothing more.
func TestWriteRegisterError_invite(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeRegisterError(rec, ErrRegistrationInvite)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// TestAPI_RequireSignedIn_anonymous verifies a request carrying no credential
// is refused with 401 and never reaches the handler.
func TestAPI_RequireSignedIn_anonymous(t *testing.T) {
	t.Parallel()

	a := NewAPI(APIConfig{})
	ran := false
	h := a.RequireSignedIn(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil))
	if ran || rec.Code != http.StatusUnauthorized {
		t.Errorf("ran=%v status=%d; want a 401 refusal", ran, rec.Code)
	}
}

// TestWithRegistrationDetails verifies the shared registration details are
// added to a nil map and to an existing one without dropping its keys.
func TestWithRegistrationDetails(t *testing.T) {
	t.Parallel()

	user := User{Username: "anna", AccountRole: AccountEditor}
	got := withRegistrationDetails(nil, user)
	if got["username"] != "anna" || got["role"] != "editor" {
		t.Errorf("nil map: got %v", got)
	}
	got = withRegistrationDetails(map[string]any{"invite": "iv1"}, user)
	if got["invite"] != "iv1" || got["username"] != "anna" {
		t.Errorf("existing map: got %v", got)
	}
}
