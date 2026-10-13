//go:build integration

package mcpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mcpapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// isolationServer mounts the MCP endpoint the way cmd/kukatko does: under
// /api/v1/p/{project} only, behind auth.WithProject.
func isolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := mcpapi.NewAPI(mcpapi.Config{
		Enabled:     true,
		Photos:      photos.NewStore(pool),
		Organize:    organize.NewStore(pool),
		People:      people.NewStore(pool),
		Bulk:        bulk.NewService(pool, 100),
		RequireAuth: authAPI.RequireAuth,
	})
	projects := project.NewStore(pool)
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterInstanceRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// mcpCall posts one JSON-RPC request to the MCP endpoint at path as client and
// returns the status, the headers and the raw body.
func mcpCall(
	t *testing.T, client *http.Client, server *httptest.Server, path, method string, params any,
) (int, http.Header, []byte) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// instructionsAt runs initialize at path and returns the server's instructions.
func instructionsAt(t *testing.T, client *http.Client, server *httptest.Server, path string) string {
	t.Helper()
	status, _, raw := mcpCall(t, client, server, path, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1"},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize at %s = %d: %s", path, status, raw)
	}
	var reply struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("decode initialize: %v (%s)", err, raw)
	}
	return reply.Result.Instructions
}

// TestMCP_projectPrefixNeverLeaksTheOtherProject calls every read tool through
// /api/v1/p/<B>/mcp as B's member: no answer names one of A's uids, A's photo
// is not found by uid, and B's own photo is.
func TestMCP_projectPrefixNeverLeaksTheOtherProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := isolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	path := f.PathFor(projecttest.SideB, "mcp")

	for _, tool := range []struct {
		name string
		args map[string]any
	}{
		{"search_photos", map[string]any{}},
		{"search_photos", map[string]any{"query": f.A.Token}},
		{"list_albums", map[string]any{}},
		{"list_labels", map[string]any{}},
		{"list_subjects", map[string]any{}},
		{"library_stats", map[string]any{}},
	} {
		status, _, raw := mcpCall(t, client, server, path, "tools/call",
			map[string]any{"name": tool.name, "arguments": tool.args})
		if status != http.StatusOK {
			t.Fatalf("%s through B = %d: %s", tool.name, status, raw)
		}
		for _, uid := range f.A.UIDs() {
			if strings.Contains(string(raw), uid) {
				t.Errorf("%s through B names A's %s: %s", tool.name, uid, raw)
			}
		}
	}

	for _, c := range []struct {
		uid       string
		wantFound bool
	}{{f.A.PhotoUID, false}, {f.B.PhotoUID, true}} {
		_, _, raw := mcpCall(t, client, server, path, "tools/call",
			map[string]any{"name": "get_photo", "arguments": map[string]any{"uid": c.uid}})
		var reply struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatalf("decode get_photo: %v (%s)", err, raw)
		}
		if reply.Result.IsError == c.wantFound {
			t.Errorf("get_photo(%s) through B: isError = %v, want found = %v (%s)",
				c.uid, reply.Result.IsError, c.wantFound, raw)
		}
	}
}

// TestMCP_instructionsNameTheProject verifies the server introduces the
// project the URL named, and only that one.
func TestMCP_instructionsNameTheProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := isolationServer(t, pool)

	clientB := f.ClientFor(t, server, projecttest.SideB)
	prefixed := instructionsAt(t, clientB, server, f.PathFor(projecttest.SideB, "mcp"))
	if !strings.Contains(prefixed, `"`+f.B.Slug+`"`) || strings.Contains(prefixed, "Deprecated") {
		t.Errorf("B's instructions do not name B alone:\n%s", prefixed)
	}

	clientA := f.ClientFor(t, server, projecttest.SideA)
	inA := instructionsAt(t, clientA, server, f.PathFor(projecttest.SideA, "mcp"))
	if !strings.Contains(inA, `"`+f.A.Slug+`"`) || strings.Contains(inA, `"`+f.B.Slug+`"`) ||
		strings.Contains(inA, "Deprecated") {
		t.Errorf("A's instructions do not name A alone:\n%s", inA)
	}
}

// TestMCP_challengeAdvertisesThePrefixedURL verifies an unauthenticated call
// to a project's endpoint is told that endpoint's URL in its 401 challenge.
func TestMCP_challengeAdvertisesThePrefixedURL(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := isolationServer(t, pool)

	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		path := f.PathFor(side, "mcp")
		status, header, raw := mcpCall(t, apitest.Client(), server, path, "tools/list", nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401: %s", path, status, raw)
		}
		want := `Bearer realm="` + server.URL + path + `"`
		if got := header.Get("WWW-Authenticate"); got != want {
			t.Errorf("WWW-Authenticate at %s = %q, want %q", path, got, want)
		}
	}
	// The unprefixed path of the transition is gone: nothing answers there.
	status, _, raw := mcpCall(t, apitest.Client(), server, auth.APIBasePath+"/mcp", "tools/list", nil)
	if status != http.StatusNotFound {
		t.Errorf("anonymous %s/mcp = %d, want 404: %s", auth.APIBasePath, status, raw)
	}
}
