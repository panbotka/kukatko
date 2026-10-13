package mcpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/auth"
)

// refuse is a guard that answers every request the way auth's guards answer a
// missing credential: the bare Bearer challenge and 401.
func refuse(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", auth.Challenge)
		w.WriteHeader(http.StatusUnauthorized)
	})
}

// TestResourceChallenge_namesThePrefixedEndpoint pins the 401 an MCP client
// reads: the Bearer scheme with the URL of the very endpoint it called — the
// project's prefixed one — and the scheme the caller used.
func TestResourceChallenge_namesThePrefixedEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		base  string
		https bool
		proxy string
		want  string
	}{
		{name: "project prefix", base: "/api/v1/p/rodina",
			want: `Bearer realm="http://kukatko.test/api/v1/p/rodina/mcp"`},
		{name: "tls", base: "/api/v1/p/v", https: true,
			want: `Bearer realm="https://kukatko.test/api/v1/p/v/mcp"`},
		{name: "behind a proxy", base: "/api/v1/p/v", proxy: "https",
			want: `Bearer realm="https://kukatko.test/api/v1/p/v/mcp"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://kukatko.test/x", nil)
			if tt.base != "" {
				r = r.WithContext(auth.ContextWithAPIBase(r.Context(), tt.base))
			}
			if tt.https {
				r.TLS = &tls.ConnectionState{}
			}
			if tt.proxy != "" {
				r.Header.Set("X-Forwarded-Proto", tt.proxy)
			}
			if got := resourceChallenge(r); got != tt.want {
				t.Errorf("resourceChallenge = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResourceChallenge_quotesCannotEscape verifies a hostile Host header
// cannot break out of the quoted realm.
func TestResourceChallenge_quotesCannotEscape(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://kukatko.test/x", nil)
	r.Host = `evil" scope="all\`
	got := resourceChallenge(r)
	if strings.Count(got, `"`) != 2 || strings.Contains(got, `\`) {
		t.Errorf("resourceChallenge = %q, want exactly one quoted realm", got)
	}
}

// TestWithResourceChallenge verifies the guard's 401 carries the endpoint's
// challenge, and that a request the guard lets through reaches the handler
// with the original writer, its own statuses untouched.
func TestWithResourceChallenge(t *testing.T) {
	t.Parallel()

	ctx := auth.ContextWithAPIBase(t.Context(), "/api/v1/p/rodina")
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://kukatko.test/api/v1/p/rodina/mcp", nil)

	rec := httptest.NewRecorder()
	withResourceChallenge(refuse)(http.NotFoundHandler()).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="http://kukatko.test/api/v1/p/rodina/mcp"` {
		t.Errorf("WWW-Authenticate = %q, want the prefixed endpoint", got)
	}

	rec = httptest.NewRecorder()
	var reached http.ResponseWriter
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = w
		w.WriteHeader(http.StatusUnauthorized)
	})
	withResourceChallenge(passthrough)(next).ServeHTTP(rec, r)
	if reached != http.ResponseWriter(rec) {
		t.Errorf("the handler got %T, want the original writer", reached)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("a handler's own 401 got WWW-Authenticate %q, want it untouched", got)
	}
}

// TestInstructionsFor verifies the text keeps the general instructions and
// names the project, and carries no deprecation note: there is no alias left
// to deprecate.
func TestInstructionsFor(t *testing.T) {
	t.Parallel()

	text := instructionsFor("Rodina Nováků", "rodina")
	if !strings.HasPrefix(text, instructions) {
		t.Error("the project text does not keep the general instructions")
	}
	if !strings.Contains(text, `"Rodina Nováků"`) || !strings.Contains(text, `"rodina"`) {
		t.Errorf("instructions do not name the project:\n%s", text)
	}
	if strings.Contains(text, "Deprecated") {
		t.Errorf("the endpoint carries a deprecation note:\n%s", text)
	}
}

// TestServer_cachedPerKey verifies a server is built once per permission and
// instructions text and reused after.
func TestServer_cachedPerKey(t *testing.T) {
	t.Parallel()

	a := &API{servers: map[serverKey]*mcp.Server{}, pageSize: 1, maxPageSize: 1}
	one := a.server(serverKey{instructions: "a"})
	if a.server(serverKey{instructions: "a"}) != one {
		t.Error("the same key built a second server")
	}
	if a.server(serverKey{instructions: "b"}) == one || a.server(serverKey{canWrite: true, instructions: "a"}) == one {
		t.Error("a different key reused the server")
	}
}
