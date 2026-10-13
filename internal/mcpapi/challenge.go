package mcpapi

import (
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/auth"
)

// mcpPath is the endpoint's path below a project's API base path.
const mcpPath = "/mcp"

// resourceURL returns the absolute URL of the MCP endpoint the request reached:
// the request's own origin plus the API base path it came through — so a call
// to /api/v1/p/<slug>/mcp is told that URL. The scheme follows the TLS state or a proxy's
// X-Forwarded-Proto; either way the answer goes back only to the caller that
// sent the headers.
func resourceURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + auth.APIBase(r.Context()) + mcpPath
}

// resourceChallenge returns the WWW-Authenticate value of a 401 on the endpoint:
// the Bearer scheme of auth.Challenge with the endpoint's resource URL as its
// realm. It is deliberately not RFC 9728's resource_metadata — Kukátko publishes
// no protected-resource metadata document and no OAuth, and a client given
// resource_metadata would go looking for an authorization server that does not
// exist (see docs/MCP.md). The realm names which project's endpoint refused the
// call; characters that could break out of the quoted string are dropped.
func resourceChallenge(r *http.Request) string {
	realm := strings.NewReplacer(`"`, "", `\`, "").Replace(resourceURL(r))
	return auth.Challenge + ` realm="` + realm + `"`
}

// withResourceChallenge wraps the auth guard so that a 401 it writes carries
// resourceChallenge instead of the bare scheme. Only the guard's own answer
// passes through the rewriting writer: a request it lets through reaches next
// with the original ResponseWriter, so the MCP transport sees an unwrapped
// writer (flushing and all) and its own statuses are never touched.
func withResourceChallenge(guard func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pass := http.HandlerFunc(func(_ http.ResponseWriter, inner *http.Request) {
				next.ServeHTTP(w, inner)
			})
			guard(pass).ServeHTTP(&challengeWriter{ResponseWriter: w, challenge: resourceChallenge(r)}, r)
		})
	}
}

// challengeWriter replaces the WWW-Authenticate header of a 401 written
// through it with challenge.
type challengeWriter struct {
	http.ResponseWriter

	challenge string
}

// WriteHeader sets the challenge on a 401 and writes the status.
func (c *challengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized {
		c.Header().Set("WWW-Authenticate", c.challenge)
	}
	c.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the wrapped writer, for http.ResponseController.
func (c *challengeWriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}
