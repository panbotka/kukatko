// Package obs provides kukatko's structured logging and the request-scoped
// plumbing that ties log lines to HTTP requests.
//
// Logging is slog with a JSON handler at a configurable level. A redacting
// ReplaceAttr hook scrubs values whose key names a secret (password, token,
// API key, S3 credentials, session cookie, database DSN, …) so secrets can
// never leak into a log line, even if a caller accidentally logs one.
//
// The AccessLog middleware emits one structured line per HTTP request with a
// consistent field set — request id, method, path, route pattern, status,
// duration, bytes, remote IP, and the authenticated user when known. The
// request id comes from chi's RequestID middleware, so logs, the X-Request-Id
// response header, and (via the shared route label) metrics all line up.
package obs

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// DefaultLevel is the log level used when configuration leaves it empty.
const DefaultLevel = "info"

// redactedValue replaces the value of any attribute whose key names a secret.
const redactedValue = "[REDACTED]"

// sensitiveKeySubstrings are the lowercased substrings that mark an attribute
// key as carrying a secret. A match redacts the value regardless of its type.
var sensitiveKeySubstrings = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key",
	"access_key", "secret_key", "authorization", "cookie", "credential", "dsn",
}

// ParseLevel converts a textual level ("debug", "info", "warn", "error",
// case-insensitive) to an slog.Level. An empty string yields DefaultLevel.
// It returns an error for any other value so a typo in configuration surfaces
// at startup rather than silently selecting the wrong verbosity.
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "":
		return ParseLevel(DefaultLevel)
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("obs: unknown log level %q", level)
	}
}

// NewLogger builds a JSON slog.Logger writing to w at the given level with the
// secret-redacting ReplaceAttr hook installed. It returns an error if level is
// invalid.
func NewLogger(w io.Writer, level string) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: redactAttr,
	})
	return slog.New(handler), nil
}

// Setup builds the logger with NewLogger and installs it as the slog default,
// returning it for callers that want to log through an explicit handle. It
// returns an error if level is invalid.
func Setup(w io.Writer, level string) (*slog.Logger, error) {
	logger, err := NewLogger(w, level)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return logger, nil
}

// redactAttr is the slog ReplaceAttr hook: it replaces the value of any
// attribute whose key names a secret with redactedValue, leaving every other
// attribute untouched. It is applied to attributes inside groups too.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		a.Value = slog.StringValue(redactedValue)
	}
	return a
}

// isSensitiveKey reports whether key (matched case-insensitively against
// sensitiveKeySubstrings) names a secret that must be redacted.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}
