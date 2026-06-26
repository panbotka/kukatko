//go:build integration

package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

const (
	testTTL         = time.Hour
	testMaxLifetime = 3 * time.Hour
	testPassword    = "initial-password"
)

// testEnv bundles a fresh store, a service with a controllable clock, and the
// mutable "now" the clock reads.
type testEnv struct {
	db    *database.DB
	store *auth.Store
	svc   *auth.Service
	now   *time.Time
}

// newTestEnv builds an auth service over the integration database with a clock
// the test drives by writing through the returned pointer.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{TTL: testTTL, MaxLifetime: testMaxLifetime}).
		WithClock(clock)
	return &testEnv{db: db, store: store, svc: svc, now: &now}
}

// createUser is a helper that creates a user with the given role and the shared
// test password.
func (e *testEnv) createUser(t *testing.T, username string, role auth.Role) auth.User {
	t.Helper()
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username,
		Password: testPassword,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return user
}

func TestLogin_successAndFailures(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "alice", auth.RoleEditor)

	t.Run("success", func(t *testing.T) {
		sess, user, err := env.svc.Login(t.Context(), "alice", testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if user.Username != "alice" || sess.Token == "" || sess.DownloadToken == "" {
			t.Fatalf("unexpected login result: user=%+v session=%+v", user, sess)
		}
		if sess.Token == sess.DownloadToken {
			t.Error("session token and download token must differ")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		if _, _, err := env.svc.Login(t.Context(), "alice", "nope"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login wrong password error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		if _, _, err := env.svc.Login(t.Context(), "ghost", testPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login unknown user error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("case-insensitive username", func(t *testing.T) {
		if _, _, err := env.svc.Login(t.Context(), "ALICE", testPassword); err != nil {
			t.Errorf("Login with upper-case username: %v", err)
		}
	})

	t.Run("disabled user", func(t *testing.T) {
		user := env.createUser(t, "bob", auth.RoleViewer)
		if _, err := env.svc.SetUserDisabled(t.Context(), user.UID, true); err != nil {
			t.Fatalf("SetUserDisabled: %v", err)
		}
		if _, _, err := env.svc.Login(t.Context(), "bob", testPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login disabled user error = %v, want ErrInvalidCredentials", err)
		}
	})
}

func TestLogin_recordsLastLogin(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "carol", auth.RoleViewer)
	if user.LastLoginAt != nil {
		t.Fatal("new user should have nil last_login_at")
	}
	if _, _, err := env.svc.Login(t.Context(), "carol", testPassword); err != nil {
		t.Fatalf("Login: %v", err)
	}
	got, err := env.store.GetUserByUID(t.Context(), user.UID)
	if err != nil {
		t.Fatalf("GetUserByUID: %v", err)
	}
	if got.LastLoginAt == nil {
		t.Error("last_login_at should be set after login")
	}
}

func TestSession_slidingExtensionCapAndExpiry(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "dave", auth.RoleEditor)
	base := *env.now

	sess, _, err := env.svc.Login(t.Context(), "dave", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !sess.ExpiresAt.Equal(base.Add(testTTL)) {
		t.Fatalf("initial expiry = %s, want %s", sess.ExpiresAt, base.Add(testTTL))
	}

	// Activity inside the window slides the expiry forward to now+TTL. Each
	// re-authentication happens before the previous expiry so the session stays
	// alive while it walks toward the max-lifetime cap.
	*env.now = base.Add(30 * time.Minute)
	_, slid, err := env.svc.Authenticate(t.Context(), sess.Token)
	if err != nil {
		t.Fatalf("Authenticate (slide): %v", err)
	}
	wantSlid := base.Add(30*time.Minute + testTTL) // expires base+1h30m
	if !slid.ExpiresAt.Equal(wantSlid) {
		t.Errorf("slid expiry = %s, want %s", slid.ExpiresAt, wantSlid)
	}
	assertStoredExpiry(t, env, sess.Token, wantSlid)

	// Another refresh before base+1h30m pushes the expiry out again.
	*env.now = base.Add(80 * time.Minute)
	if _, _, err := env.svc.Authenticate(t.Context(), sess.Token); err != nil {
		t.Fatalf("Authenticate (second slide): %v", err)
	}

	// A refresh where now+TTL would exceed the max lifetime caps the expiry at
	// created+MaxLifetime (base+3h) rather than now+TTL.
	*env.now = base.Add(130 * time.Minute) // before the prior expiry base+2h20m
	_, capped, err := env.svc.Authenticate(t.Context(), sess.Token)
	if err != nil {
		t.Fatalf("Authenticate (cap): %v", err)
	}
	wantCap := base.Add(testMaxLifetime)
	if !capped.ExpiresAt.Equal(wantCap) {
		t.Errorf("capped expiry = %s, want %s", capped.ExpiresAt, wantCap)
	}

	// Past the capped expiry: the session is rejected and the row removed.
	*env.now = base.Add(testMaxLifetime + time.Minute)
	if _, _, err := env.svc.Authenticate(t.Context(), sess.Token); !errors.Is(err, auth.ErrSessionExpired) {
		t.Errorf("Authenticate (expired) error = %v, want ErrSessionExpired", err)
	}
	if _, err := env.store.GetSessionByToken(t.Context(), sess.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("expired session still present: err = %v, want ErrSessionNotFound", err)
	}
}

// assertStoredExpiry checks the persisted session expiry for token equals want.
func assertStoredExpiry(t *testing.T, env *testEnv, token string, want time.Time) {
	t.Helper()
	stored, err := env.store.GetSessionByToken(t.Context(), token)
	if err != nil {
		t.Fatalf("GetSessionByToken: %v", err)
	}
	if !stored.ExpiresAt.Equal(want) {
		t.Errorf("stored expiry = %s, want %s", stored.ExpiresAt, want)
	}
}

func TestCleanupExpiredSessions_removesOnlyExpired(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "erin", auth.RoleViewer)
	base := *env.now

	sess, _, err := env.svc.Login(t.Context(), "erin", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Before expiry: cleanup removes nothing.
	*env.now = base.Add(30 * time.Minute)
	if n, err := env.svc.CleanupExpiredSessions(t.Context()); err != nil || n != 0 {
		t.Fatalf("Cleanup before expiry: n=%d err=%v, want n=0 nil", n, err)
	}

	// After expiry: cleanup removes the session.
	*env.now = base.Add(2 * time.Hour)
	n, err := env.svc.CleanupExpiredSessions(t.Context())
	if err != nil {
		t.Fatalf("Cleanup after expiry: %v", err)
	}
	if n != 1 {
		t.Errorf("cleanup removed %d sessions, want 1", n)
	}
	if _, err := env.store.GetSessionByToken(t.Context(), sess.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session survived cleanup: err = %v, want ErrSessionNotFound", err)
	}
}

func TestChangePassword_invalidatesOtherSessions(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "frank", auth.RoleEditor)

	keep, _, err := env.svc.Login(t.Context(), "frank", testPassword)
	if err != nil {
		t.Fatalf("Login keep: %v", err)
	}
	other, _, err := env.svc.Login(t.Context(), "frank", testPassword)
	if err != nil {
		t.Fatalf("Login other: %v", err)
	}

	const newPassword = "brand-new-password"
	if err := env.svc.ChangePassword(t.Context(), user.UID, keep.Token, testPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// The current (kept) session survives; the other session is invalidated.
	if _, _, err := env.svc.Authenticate(t.Context(), keep.Token); err != nil {
		t.Errorf("kept session should remain valid: %v", err)
	}
	if _, _, err := env.svc.Authenticate(t.Context(), other.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("other session error = %v, want ErrSessionNotFound", err)
	}

	// Old password no longer works; the new one does.
	if _, _, err := env.svc.Login(t.Context(), "frank", testPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("login with old password error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := env.svc.Login(t.Context(), "frank", newPassword); err != nil {
		t.Errorf("login with new password: %v", err)
	}
}

func TestChangePassword_wrongCurrent(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "grace", auth.RoleViewer)
	sess, _, err := env.svc.Login(t.Context(), "grace", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	err = env.svc.ChangePassword(t.Context(), user.UID, sess.Token, "wrong-current", "a-new-password")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("ChangePassword wrong current error = %v, want ErrInvalidCredentials", err)
	}
}

func TestResetPassword_invalidatesAllSessions(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "heidi", auth.RoleViewer)
	sess, _, err := env.svc.Login(t.Context(), "heidi", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	const resetTo = "admin-reset-password"
	if err := env.svc.ResetPassword(t.Context(), user.UID, resetTo); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if _, _, err := env.svc.Authenticate(t.Context(), sess.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session after reset error = %v, want ErrSessionNotFound", err)
	}
	if _, _, err := env.svc.Login(t.Context(), "heidi", resetTo); err != nil {
		t.Errorf("login with reset password: %v", err)
	}
}

func TestCreateUser_duplicateAndInvalidRole(t *testing.T) {
	env := newTestEnv(t)
	env.createUser(t, "ivan", auth.RoleViewer)

	_, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "ivan", Password: testPassword, Role: auth.RoleEditor,
	})
	if !errors.Is(err, auth.ErrUsernameTaken) {
		t.Errorf("duplicate username error = %v, want ErrUsernameTaken", err)
	}

	_, err = env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "judy", Password: testPassword, Role: auth.Role("superuser"),
	})
	if !errors.Is(err, auth.ErrInvalidRole) {
		t.Errorf("invalid role error = %v, want ErrInvalidRole", err)
	}
}

func TestUpdateUser_disableInvalidatesSessions(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "karl", auth.RoleEditor)
	sess, _, err := env.svc.Login(t.Context(), "karl", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	updated, err := env.svc.UpdateUser(t.Context(), user.UID, auth.UpdateUserInput{
		DisplayName: "Karl K", Email: "karl@example.com", Role: auth.RoleViewer, Disabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Role != auth.RoleViewer || !updated.Disabled || updated.DisplayName != "Karl K" {
		t.Errorf("update not applied: %+v", updated)
	}
	if _, _, err := env.svc.Authenticate(t.Context(), sess.Token); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("session after disable error = %v, want ErrSessionNotFound", err)
	}
}

func TestBootstrap_outcomes(t *testing.T) {
	env := newTestEnv(t)

	t.Run("no credentials on empty table", func(t *testing.T) {
		outcome, err := env.svc.Bootstrap(t.Context(), "", "")
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if outcome != auth.BootstrapSkippedNoCredentials {
			t.Errorf("outcome = %v, want BootstrapSkippedNoCredentials", outcome)
		}
	})

	t.Run("creates admin on empty table", func(t *testing.T) {
		outcome, err := env.svc.Bootstrap(t.Context(), "root", "bootstrap-password")
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if outcome != auth.BootstrapCreated {
			t.Fatalf("outcome = %v, want BootstrapCreated", outcome)
		}
		user, err := env.store.GetUserByUsername(t.Context(), "root")
		if err != nil {
			t.Fatalf("GetUserByUsername: %v", err)
		}
		if user.Role != auth.RoleAdmin {
			t.Errorf("bootstrap user role = %q, want admin", user.Role)
		}
		if _, _, err := env.svc.Login(t.Context(), "root", "bootstrap-password"); err != nil {
			t.Errorf("login as bootstrap admin: %v", err)
		}
	})

	t.Run("skips when users already exist", func(t *testing.T) {
		outcome, err := env.svc.Bootstrap(t.Context(), "another", "bootstrap-password")
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if outcome != auth.BootstrapSkippedHasUsers {
			t.Errorf("outcome = %v, want BootstrapSkippedHasUsers", outcome)
		}
	})
}

// TestAuthenticateDownloadToken covers the media download-token validation: a
// live token resolves to its user and session, an unknown token is reported as
// ErrSessionNotFound, and an expired session is rejected and pruned.
func TestAuthenticateDownloadToken(t *testing.T) {
	env := newTestEnv(t)
	user := env.createUser(t, "dora", auth.RoleViewer)

	sess, _, err := env.svc.Login(t.Context(), "dora", testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	t.Run("valid token resolves user and session", func(t *testing.T) {
		gotUser, gotSess, err := env.svc.AuthenticateDownloadToken(t.Context(), sess.DownloadToken)
		if err != nil {
			t.Fatalf("AuthenticateDownloadToken: %v", err)
		}
		if gotUser.UID != user.UID || gotSess.ID != sess.ID {
			t.Errorf("resolved user=%s session=%s, want %s/%s", gotUser.UID, gotSess.ID, user.UID, sess.ID)
		}
		if gotSess.Role != auth.RoleViewer {
			t.Errorf("session role = %s, want viewer", gotSess.Role)
		}
	})

	t.Run("unknown token is ErrSessionNotFound", func(t *testing.T) {
		if _, _, err := env.svc.AuthenticateDownloadToken(t.Context(), "nope"); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Errorf("error = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("expired session is rejected and deleted", func(t *testing.T) {
		*env.now = env.now.Add(testMaxLifetime + time.Minute)
		if _, _, err := env.svc.AuthenticateDownloadToken(t.Context(), sess.DownloadToken); !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("error = %v, want ErrSessionExpired", err)
		}
		// The row is gone, so a retry now reports it as not found.
		if _, _, err := env.svc.AuthenticateDownloadToken(t.Context(), sess.DownloadToken); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Errorf("post-expiry error = %v, want ErrSessionNotFound", err)
		}
	})
}
