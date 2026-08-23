package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/settings"
)

// fakeSettings is a SettingsSource answering with fixed values, so the secret
// rules can be exercised without a database.
type fakeSettings struct {
	stored settings.Settings
	err    error
}

// Get returns the fixed settings (or the fixed failure).
func (f fakeSettings) Get(context.Context) (settings.Settings, error) {
	return f.stored, f.err
}

// errSettingsUnavailable stands in for a settings read that fails.
var errSettingsUnavailable = errors.New("settings are unavailable")

// TestRegistration_checkSecret covers who may register at all: nobody while
// registration is switched off, nobody while it is on with a blank secret (an
// open door with no lock), and only a caller carrying the stored secret
// otherwise — whitespace around it being no part of it.
func TestRegistration_checkSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stored   settings.Settings
		readErr  error
		secret   string
		wantErr  error
		wantNoOp bool
	}{
		{
			name:    "registration switched off",
			stored:  settings.Settings{RegistrationEnabled: false, RegistrationSecret: "open sesame"},
			secret:  "open sesame",
			wantErr: ErrRegistrationClosed,
		},
		{
			name:    "enabled with a blank secret refuses everybody",
			stored:  settings.Settings{RegistrationEnabled: true, RegistrationSecret: ""},
			secret:  "",
			wantErr: ErrRegistrationClosed,
		},
		{
			name:    "enabled with a whitespace secret refuses everybody",
			stored:  settings.Settings{RegistrationEnabled: true, RegistrationSecret: "   "},
			secret:  "   ",
			wantErr: ErrRegistrationClosed,
		},
		{
			name:    "wrong secret",
			stored:  settings.Settings{RegistrationEnabled: true, RegistrationSecret: "open sesame"},
			secret:  "open sesam",
			wantErr: ErrRegistrationSecret,
		},
		{
			name:    "empty secret against a real one",
			stored:  settings.Settings{RegistrationEnabled: true, RegistrationSecret: "open sesame"},
			secret:  "",
			wantErr: ErrRegistrationSecret,
		},
		{
			name:   "right secret",
			stored: settings.Settings{RegistrationEnabled: true, RegistrationSecret: "open sesame"},
			secret: "open sesame",
		},
		{
			name:   "right secret, sloppily typed",
			stored: settings.Settings{RegistrationEnabled: true, RegistrationSecret: "open sesame"},
			secret: "  open sesame\n",
		},
		{
			name:    "settings unreadable",
			readErr: errSettingsUnavailable,
			secret:  "open sesame",
			wantErr: errSettingsUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := NewRegistration(RegistrationConfig{
				Settings: fakeSettings{stored: tt.stored, err: tt.readErr},
			})
			err := rg.checkSecret(t.Context(), tt.secret)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("checkSecret() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("checkSecret() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestPrepareRegistration_isUnapprovedViewer checks the account a registration
// builds: the lowest role, no approval stamp, and the same normalization the
// admin path applies. The nil stamp is the feature — it is what makes the
// account exist without being usable.
func TestPrepareRegistration_isUnapprovedViewer(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 23, 10, 30, 0, 0, time.UTC)
	svc := NewService(nil, SessionPolicy{}).WithClock(func() time.Time { return now })

	user, err := svc.prepareRegistration(CreateUserInput{
		Username: "  Newcomer ",
		Password: "correct horse battery",
		Email:    "Newcomer@EXAMPLE.com",
		Role:     RoleViewer,
	})
	if err != nil {
		t.Fatalf("prepareRegistration: %v", err)
	}
	if user.ApprovedAt != nil {
		t.Errorf("ApprovedAt = %v, want nil — nobody has let this account in yet", *user.ApprovedAt)
	}
	if user.Role != RoleViewer {
		t.Errorf("Role = %q, want %q", user.Role, RoleViewer)
	}
	if user.Username != "newcomer" {
		t.Errorf("Username = %q, want the normalized %q", user.Username, "newcomer")
	}
	if user.Email != "Newcomer@example.com" {
		t.Errorf("Email = %q, want the domain lower-cased", user.Email)
	}
	if user.PasswordHash == "" {
		t.Error("PasswordHash is empty; the password must be hashed like any other")
	}
	if user.WelcomeSeenAt != nil {
		t.Errorf("WelcomeSeenAt = %v, want nil", *user.WelcomeSeenAt)
	}
}

// TestPrepareRegistration_rejectsBadInput checks that registration is held to
// exactly the admin API's rules: the same validation runs, so nothing can be
// registered that an administrator could not have created.
func TestPrepareRegistration_rejectsBadInput(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, SessionPolicy{})
	tests := []struct {
		name    string
		in      CreateUserInput
		wantErr error
	}{
		{
			name:    "no e-mail address",
			in:      CreateUserInput{Username: "a", Password: "correct horse battery", Role: RoleViewer},
			wantErr: ErrInvalidEmail,
		},
		{
			name: "malformed e-mail address",
			in: CreateUserInput{Username: "a", Password: "correct horse battery",
				Email: "nobody@localhost", Role: RoleViewer},
			wantErr: ErrInvalidEmail,
		},
		{
			name:    "password too short",
			in:      CreateUserInput{Username: "a", Password: "short", Email: "a@example.com", Role: RoleViewer},
			wantErr: ErrPasswordTooShort,
		},
		{
			name: "username too long",
			in: CreateUserInput{
				Username: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Password: "correct horse battery", Email: "a@example.com", Role: RoleViewer,
			},
			wantErr: ErrUsernameTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := svc.prepareRegistration(tt.in); !errors.Is(err, tt.wantErr) {
				t.Errorf("prepareRegistration() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestRegisterLimiterFor_derivesTheLoginBudget checks the fallback limiter: with
// no explicit one, a client address gets exactly the login budget's worth of
// registrations before it is throttled, and an explicitly supplied limiter is
// used as it stands.
func TestRegisterLimiterFor_derivesTheLoginBudget(t *testing.T) {
	t.Parallel()

	const budget = 4
	limiter := registerLimiterFor(APIConfig{Limiter: NewLimiter(budget, time.Minute)})
	for i := range budget {
		if !limiter.Allow("198.51.100.7") {
			t.Fatalf("attempt %d was throttled, want the first %d allowed", i+1, budget)
		}
	}
	if limiter.Allow("198.51.100.7") {
		t.Errorf("attempt %d was allowed, want it throttled once the budget is spent", budget+1)
	}
	// A different address has its own budget: one noisy caller must not close
	// registration for everybody.
	if !limiter.Allow("203.0.113.9") {
		t.Error("a second address was throttled by the first address's attempts")
	}
}
