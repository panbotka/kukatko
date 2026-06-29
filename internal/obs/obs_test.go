package obs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestParseLevel covers the recognised levels, the empty default, and an
// invalid value.
func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    slog.Level
		wantErr bool
	}{
		{name: "empty defaults to info", in: "", want: slog.LevelInfo},
		{name: "debug", in: "debug", want: slog.LevelDebug},
		{name: "info", in: "INFO", want: slog.LevelInfo},
		{name: "warn", in: "warn", want: slog.LevelWarn},
		{name: "warning alias", in: "warning", want: slog.LevelWarn},
		{name: "error", in: " error ", want: slog.LevelError},
		{name: "invalid", in: "loud", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLevel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) error = nil, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestNewLogger_redactsSecrets verifies that values whose key names a secret are
// replaced with the redaction marker and the secret never reaches the output,
// while ordinary fields pass through untouched.
func TestNewLogger_redactsSecrets(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := NewLogger(&buf, "info")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	const secret = "sup3r-s3cret-value"
	logger.Info("handling request",
		slog.String("password", secret),
		slog.String("mapy_api_key", secret),
		slog.String("s3_secret_key", secret),
		slog.String("session_cookie", secret),
		slog.String("download_token", secret),
		slog.String("Authorization", "Bearer "+secret),
		slog.String("database_dsn", secret),
		slog.String("route", "/api/v1/photos"),
	)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked into log output:\n%s", out)
	}
	if !strings.Contains(out, redactedValue) {
		t.Errorf("expected %q marker in output:\n%s", redactedValue, out)
	}
	if !strings.Contains(out, `"route":"/api/v1/photos"`) {
		t.Errorf("non-secret field was dropped:\n%s", out)
	}
}

// TestNewLogger_invalidLevel verifies construction fails for a bad level.
func TestNewLogger_invalidLevel(t *testing.T) {
	t.Parallel()

	if _, err := NewLogger(&bytes.Buffer{}, "nope"); err == nil {
		t.Fatal("NewLogger with invalid level: error = nil, want error")
	}
}

// TestIsSensitiveKey covers the substring matching for redaction decisions.
func TestIsSensitiveKey(t *testing.T) {
	t.Parallel()

	sensitive := []string{"password", "Password", "s3_secret_key", "ACCESS_KEY", "auth_token", "Cookie", "db_dsn"}
	for _, key := range sensitive {
		if !isSensitiveKey(key) {
			t.Errorf("isSensitiveKey(%q) = false, want true", key)
		}
	}
	safe := []string{"route", "method", "status", "user", "request_id", "duration_ms"}
	for _, key := range safe {
		if isSensitiveKey(key) {
			t.Errorf("isSensitiveKey(%q) = true, want false", key)
		}
	}
}

// TestSetUser_noFieldsBag verifies SetUser is a safe no-op when no fields bag is
// present in the context (for example a handler exercised directly in a test).
func TestSetUser_noFieldsBag(t *testing.T) {
	t.Parallel()

	// Must not panic.
	SetUser(context.Background(), "u1")
	if uid := userOf(context.Background()); uid != "" {
		t.Errorf("userOf without a fields bag = %q, want empty", uid)
	}
}
