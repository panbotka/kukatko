package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/settings"
)

// TestRegistration_RegisterWithLink_refusals covers what a link cannot get past:
// an instance whose registration is switched off, and a settings read that
// fails. Neither reaches the account store.
func TestRegistration_RegisterWithLink_refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings fakeSettings
		want     error
	}{
		{"switched off", fakeSettings{stored: settings.Settings{RegistrationSecret: "x"}}, ErrRegistrationClosed},
		{"settings unavailable", fakeSettings{err: errSettingsUnavailable}, errSettingsUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rg := NewRegistration(RegistrationConfig{Settings: tt.settings})
			_, err := rg.RegisterWithLink(context.Background(), RegisterInput{}, LinkGrant{LinkUID: "ul1"}, audit.Entry{})
			if !errors.Is(err, tt.want) {
				t.Errorf("RegisterWithLink error = %v, want %v", err, tt.want)
			}
		})
	}
}

// stubGate is an UploadLinkGate answering with a fixed error.
type stubGate struct{ err error }

// AdmitRegistration returns the fixed error.
func (g stubGate) AdmitRegistration(context.Context, *http.Request, string) (LinkGrant, error) {
	return LinkGrant{}, g.err
}

// TestAPI_register_linkPath verifies a registration naming a link goes to the
// gate — and is refused as an invalid link when no gate is wired or the gate
// refuses it — while one without a link never consults the gate.
func TestAPI_register_linkPath(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/register", nil)
	closed := NewRegistration(RegistrationConfig{Settings: fakeSettings{}})
	tests := []struct {
		name string
		gate UploadLinkGate
		in   RegisterInput
		want error
	}{
		{"no gate wired", nil, RegisterInput{UploadLink: "Ab3dEf7h"}, ErrRegistrationLink},
		{"gate refuses", stubGate{err: ErrRegistrationLink}, RegisterInput{UploadLink: "Ab3dEf7h"}, ErrRegistrationLink},
		{"no link takes the secret path", stubGate{err: errors.New("must not be called")}, RegisterInput{}, ErrRegistrationClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &API{registration: closed, uploadLinks: tt.gate}
			if _, err := a.register(req, tt.in, audit.Entry{}); !errors.Is(err, tt.want) {
				t.Errorf("register error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestAPI_OptionalAuth_anonymous verifies a request carrying no credential runs
// anonymously instead of being refused.
func TestAPI_OptionalAuth_anonymous(t *testing.T) {
	t.Parallel()

	a := NewAPI(APIConfig{})
	var ran, signedIn bool
	h := a.OptionalAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ran = true
		_, signedIn = UserFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if !ran || signedIn || rec.Code != http.StatusOK {
		t.Errorf("ran=%v signedIn=%v status=%d; want an anonymous pass-through", ran, signedIn, rec.Code)
	}
}
