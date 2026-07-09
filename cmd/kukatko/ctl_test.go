package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/ctl"
)

// TestImpliesCtl verifies the ctl level is implied only for the kukatkoctl
// program name, whatever directory the symlink lives in.
func TestImpliesCtl(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"kukatkoctl":                true,
		"/usr/local/bin/kukatkoctl": true,
		"./kukatkoctl":              true,
		"kukatkoctl.exe":            true,
		"kukatko":                   false,
		"/usr/local/bin/kukatko":    false,
		"kukatkoctl-dev":            false,
		"ctl":                       false,
		"":                          false,
	}
	for argv0, want := range tests {
		if got := impliesCtl(argv0); got != want {
			t.Errorf("impliesCtl(%q) = %v, want %v", argv0, got, want)
		}
	}
}

// TestRootCmd_ctlSymlinkImpliesCtl verifies that, invoked as kukatkoctl, the ctl
// subtree becomes the root: `kukatkoctl photos list` works and the server-side
// subcommands are gone.
func TestRootCmd_ctlSymlinkImpliesCtl(t *testing.T) {
	t.Parallel()

	root := newRootCmd("/usr/local/bin/kukatkoctl")
	if root.Use != ctlProgramName {
		t.Errorf("root.Use = %q, want %q", root.Use, ctlProgramName)
	}
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"photos", "config"} {
		if !names[want] {
			t.Errorf("kukatkoctl root has no %q subcommand", want)
		}
	}
	if names["serve"] || names["ctl"] {
		t.Errorf("kukatkoctl root exposes server subcommands: %v", names)
	}
}

// ctlServer starts an httptest server and returns the path of a ctl config file
// pointing at it, so a command can be driven end to end against a real HTTP
// endpoint. The server and the temp directory are cleaned up with the test.
func ctlServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	configPath := filepath.Join(t.TempDir(), "ctl.yaml")
	cfg := &ctl.Config{
		CurrentContext: "test",
		Contexts:       []ctl.Context{{Name: "test", Server: srv.URL, Token: "kkt_abc_supersecret"}},
	}
	if err := ctl.Save(configPath, cfg); err != nil {
		t.Fatalf("seeding ctl config: %v", err)
	}
	return configPath
}

// runCtl executes the command tree with the ambient environment overrides
// cleared, returning the captured output. stdin feeds --token-stdin.
func runCtl(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	t.Setenv(ctl.EnvServer, "")
	t.Setenv(ctl.EnvToken, "")

	cmd := newRootCmd("kukatko")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// listBody is a two-row /photos envelope, exactly as internal/photoapi shapes it.
const listBody = `{"photos":[{"uid":"pht01","file_name":"a.jpg","file_size":2097152,` +
	`"taken_at":"2024-05-01T10:22:33Z","title":"Lake","is_favorite":true},` +
	`{"uid":"pht02","file_name":"b.mp4","file_size":10485760,"title":""}],` +
	`"total":42,"limit":2,"offset":0,"next_offset":2}`

// TestCtlPhotosList_table verifies the default output is a compact table and that
// the filters reach the API as query parameters.
func TestCtlPhotosList_table(t *testing.T) {
	var gotQuery, gotAuth string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(listBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "list", "--year", "2024", "--limit", "2")
	if err != nil {
		t.Fatalf("photos list returned %v", err)
	}
	if gotAuth != "Bearer kkt_abc_supersecret" {
		t.Errorf("Authorization = %q, want the stored bearer token", gotAuth)
	}
	if !strings.Contains(gotQuery, "limit=2") || !strings.Contains(gotQuery, "taken_after=2024-01-01") {
		t.Errorf("query = %q, want the limit and the year range", gotQuery)
	}
	for _, want := range []string{"UID", "pht01", "Lake", "a.jpg", "2.0 MiB", "2 of 42 photos", "next offset 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlPhotosList_json verifies -o json echoes the API's own bytes unchanged,
// which is what a machine consumer parses.
func TestCtlPhotosList_json(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(listBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "photos", "list")
	if err != nil {
		t.Fatalf("photos list -o json returned %v", err)
	}
	if out != listBody+"\n" {
		t.Errorf("json output was not passed through unchanged:\ngot  %q\nwant %q", out, listBody+"\n")
	}
}

// TestCtlPhotosList_empty verifies an empty result set prints one line in table
// form and an untouched empty envelope in JSON form.
func TestCtlPhotosList_empty(t *testing.T) {
	const emptyBody = `{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(emptyBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "list")
	if err != nil {
		t.Fatalf("photos list returned %v", err)
	}
	if out != "no photos found\n" {
		t.Errorf("empty table output = %q, want a single no-photos line", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "photos", "list")
	if err != nil {
		t.Fatalf("photos list -o json returned %v", err)
	}
	if out != emptyBody+"\n" {
		t.Errorf("empty json output = %q, want the envelope unchanged", out)
	}
}

// TestCtlPhotosGet verifies the detail command renders a key/value table and
// requests the right path.
func TestCtlPhotosGet(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"uid":"pht01","title":"Lake","file_name":"a.jpg","file_size":1536,
			"albums":[{"uid":"alb1","title":"Trip"}],"labels":[],"files":[]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "get", "pht01")
	if err != nil {
		t.Fatalf("photos get returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01" {
		t.Errorf("path = %q, want /api/v1/photos/pht01", gotPath)
	}
	for _, want := range []string{"UID", "pht01", "TITLE", "Lake", "ALBUMS", "Trip"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlPhotosGet_notFound verifies a 404 surfaces the server's own message and
// a non-nil error, which main maps to a non-zero exit code.
func TestCtlPhotosGet_notFound(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"photo not found"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "get", "ghost")
	if err == nil {
		t.Fatal("photos get of a missing photo returned no error")
	}
	if !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), "photo not found") {
		t.Errorf("error = %q, want the server's 404 message", err)
	}
}

// TestCtlPhotosSearch verifies the search command hits /search, defaults to the
// hybrid mode, and reports a degraded fallback in the summary line.
func TestCtlPhotosSearch(t *testing.T) {
	var gotPath, gotMode, gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMode, gotQuery = r.URL.Path, r.URL.Query().Get("mode"), r.URL.Query().Get("q")
		w.Write([]byte(`{"photos":[{"uid":"pht01","file_name":"a.jpg"}],"total":1,` +
			`"limit":100,"offset":0,"next_offset":null,"mode":"fulltext","degraded":true}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "search", "jezero")
	if err != nil {
		t.Fatalf("photos search returned %v", err)
	}
	if gotPath != "/api/v1/search" || gotQuery != "jezero" {
		t.Errorf("request = %s?q=%s, want /api/v1/search?q=jezero", gotPath, gotQuery)
	}
	if gotMode != ctl.SearchHybrid {
		t.Errorf("mode = %q, want the hybrid default", gotMode)
	}
	if !strings.Contains(out, "mode fulltext") || !strings.Contains(out, "degraded") {
		t.Errorf("search output does not report the degraded fallback:\n%s", out)
	}
}

// TestCtlPhotosSearch_invalidMode verifies an unknown mode is caught before a
// round trip is spent on it.
func TestCtlPhotosSearch_invalidMode(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with an invalid mode")
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "search", "x", "--mode", "magic")
	if !errors.Is(err, ctl.ErrInvalidSearchMode) {
		t.Errorf("search --mode magic error = %v, want ErrInvalidSearchMode", err)
	}
}

// TestCtlPhotosSearch_noFavoriteFlag verifies search does not offer --favorite.
// GET /search never reads the parameter, so accepting the flag would silently
// return unfiltered results; list, whose endpoint does honour it, keeps it.
func TestCtlPhotosSearch_noFavoriteFlag(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite an unknown flag")
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "search", "x", "--favorite")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("search --favorite error = %v, want an unknown flag error", err)
	}
}

// TestCtlPhotosList_favoriteFlag verifies list still forwards --favorite, the
// filter GET /photos does implement.
func TestCtlPhotosList_favoriteFlag(t *testing.T) {
	var gotFavorite string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotFavorite = r.URL.Query().Get("favorite")
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"photos", "list", "--favorite"); err != nil {
		t.Fatalf("photos list --favorite returned %v", err)
	}
	if gotFavorite != "true" {
		t.Errorf("favorite = %q, want %q", gotFavorite, "true")
	}
}

// TestCtlPhotos_unauthorized verifies a 401 produces a short actionable message —
// not a stack trace, not a body dump, and never the token itself.
func TestCtlPhotos_unauthorized(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"authentication required"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "list")
	if err == nil {
		t.Fatal("a 401 produced no error")
	}
	var unauthorized *ctl.UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("error = %v (%T), want *ctl.UnauthorizedError", err, err)
	}
	msg := err.Error()
	for _, want := range []string{"401", "missing, expired, or revoked", "/auth/tokens", "KUKATKO_TOKEN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("401 message %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "supersecret") {
		t.Errorf("401 message leaks the token: %q", msg)
	}
}

// TestCtl_envOverridesContext verifies KUKATKO_SERVER and KUKATKO_TOKEN win over
// the stored context.
func TestCtl_envOverridesContext(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	}))
	defer srv.Close()

	configPath := filepath.Join(t.TempDir(), "ctl.yaml")
	stored := &ctl.Config{
		CurrentContext: "test",
		Contexts:       []ctl.Context{{Name: "test", Server: "https://wrong.example.com", Token: "kkt_old_x"}},
	}
	if err := ctl.Save(configPath, stored); err != nil {
		t.Fatalf("seeding ctl config: %v", err)
	}

	t.Setenv(ctl.EnvServer, srv.URL)
	t.Setenv(ctl.EnvToken, "kkt_new_y")

	cmd := newRootCmd("kukatko")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"ctl", "--ctl-config", configPath, "photos", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("photos list returned %v", err)
	}
	if gotAuth != "Bearer kkt_new_y" {
		t.Errorf("Authorization = %q, want the environment token", gotAuth)
	}
}

// TestCtl_noServerConfigured verifies an unconfigured client fails with an
// actionable message rather than dialling nothing.
func TestCtl_noServerConfigured(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "list")
	if !errors.Is(err, ctl.ErrNoServer) {
		t.Errorf("error = %v, want ctl.ErrNoServer", err)
	}
}

// TestCtl_invalidOutputFormat verifies -o yaml is rejected: this CLI emits only
// a table or the API's own JSON.
func TestCtl_invalidOutputFormat(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with an invalid output format")
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "yaml", "photos", "list")
	if !errors.Is(err, ctl.ErrInvalidFormat) {
		t.Errorf("error = %v, want ctl.ErrInvalidFormat", err)
	}
}

// TestCtlConfigSetContext verifies a context is created from stdin, becomes
// current as the first one, and lands in a file only its owner can read.
func TestCtlConfigSetContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "kukatko", "ctl.yaml")

	out, err := runCtl(t, "kkt_abc_supersecret\n", "ctl", "--ctl-config", configPath,
		"config", "set-context", "prod", "--server", "https://kukatko.example.com", "--token-stdin")
	if err != nil {
		t.Fatalf("config set-context returned %v", err)
	}
	if strings.Contains(out, "supersecret") {
		t.Errorf("set-context echoed the token:\n%s", out)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("ctl.yaml mode = %o, want 600", got)
	}

	cfg, err := ctl.Load(configPath)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("CurrentContext = %q, want the first context to become current", cfg.CurrentContext)
	}
	prod, ok := cfg.Find("prod")
	if !ok || prod.Server != "https://kukatko.example.com" || prod.Token != "kkt_abc_supersecret" {
		t.Errorf("stored context = %+v, want the server and token", prod)
	}
}

// TestCtlConfigSetContext_updateKeepsToken verifies updating only the server URL
// leaves the stored token in place, and that a second context does not steal the
// current one unless asked.
func TestCtlConfigSetContext_updateKeepsToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")

	if _, err := runCtl(t, "kkt_a_secret\n", "ctl", "--ctl-config", configPath,
		"config", "set-context", "prod", "--server", "https://a.example.com", "--token-stdin"); err != nil {
		t.Fatalf("creating prod: %v", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"config", "set-context", "prod", "--server", "https://b.example.com"); err != nil {
		t.Fatalf("updating prod: %v", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"config", "set-context", "dev", "--server", "http://localhost:8080"); err != nil {
		t.Fatalf("creating dev: %v", err)
	}

	cfg, err := ctl.Load(configPath)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	prod, _ := cfg.Find("prod")
	if prod.Server != "https://b.example.com" || prod.Token != "kkt_a_secret" {
		t.Errorf("prod = %+v, want the new server and the kept token", prod)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("CurrentContext = %q, want prod to stay current", cfg.CurrentContext)
	}
}

// TestCtlConfigSetContext_errors verifies the guardrails: a new context needs a
// server, and the two token flags are mutually exclusive.
func TestCtlConfigSetContext_errors(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "set-context", "prod")
	if !errors.Is(err, ctl.ErrServerRequired) {
		t.Errorf("set-context without --server error = %v, want ErrServerRequired", err)
	}

	_, err = runCtl(t, "kkt_a_b\n", "ctl", "--ctl-config", configPath, "config", "set-context", "prod",
		"--server", "https://a", "--token", "kkt_c_d", "--token-stdin")
	if !errors.Is(err, errTokenFlagConflict) {
		t.Errorf("both token flags error = %v, want errTokenFlagConflict", err)
	}

	_, err = runCtl(t, "   \n", "ctl", "--ctl-config", configPath, "config", "set-context", "prod",
		"--server", "https://a", "--token-stdin")
	if !errors.Is(err, errEmptyTokenStdin) {
		t.Errorf("blank stdin error = %v, want errEmptyTokenStdin", err)
	}
}

// TestCtlConfigList verifies the context table marks the current context and
// never prints a token.
func TestCtlConfigList(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "list")
	if err != nil {
		t.Fatalf("config list returned %v", err)
	}
	if strings.Contains(out, "supersecret") {
		t.Errorf("config list leaked the token:\n%s", out)
	}
	for _, want := range []string{"CURRENT", "NAME", "SERVER", "TOKEN", "test", "stored"} {
		if !strings.Contains(out, want) {
			t.Errorf("config list output does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlConfigList_empty verifies an unconfigured client says so instead of
// failing.
func TestCtlConfigList_empty(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "list")
	if err != nil {
		t.Fatalf("config list returned %v", err)
	}
	if out != "no contexts configured\n" {
		t.Errorf("config list output = %q", out)
	}
}

// TestCtlConfigUseContext verifies switching the current context and the error
// for an unknown name.
func TestCtlConfigUseContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")
	cfg := &ctl.Config{
		CurrentContext: "prod",
		Contexts: []ctl.Context{
			{Name: "prod", Server: "https://a"},
			{Name: "dev", Server: "http://b"},
		},
	}
	if err := ctl.Save(configPath, cfg); err != nil {
		t.Fatalf("seeding ctl config: %v", err)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "use-context", "dev"); err != nil {
		t.Fatalf("config use-context returned %v", err)
	}
	reloaded, err := ctl.Load(configPath)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if reloaded.CurrentContext != "dev" {
		t.Errorf("CurrentContext = %q, want dev", reloaded.CurrentContext)
	}

	_, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "use-context", "ghost")
	if !errors.Is(err, ctl.ErrContextNotFound) {
		t.Errorf("use-context ghost error = %v, want ErrContextNotFound", err)
	}
}

// TestKukatkoctl_photosList verifies the symlinked invocation reaches the same
// command without the ctl level.
func TestKukatkoctl_photosList(t *testing.T) {
	t.Setenv(ctl.EnvServer, "")
	t.Setenv(ctl.EnvToken, "")

	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	})

	cmd := newRootCmd("kukatkoctl")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--ctl-config", configPath, "photos", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("kukatkoctl photos list returned %v", err)
	}
	if got := buf.String(); got != "no photos found\n" {
		t.Errorf("kukatkoctl photos list output = %q", got)
	}
}
