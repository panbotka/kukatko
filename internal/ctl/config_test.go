package ctl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestConfig_Find verifies context lookup by name.
func TestConfig_Find(t *testing.T) {
	t.Parallel()

	cfg := &Config{Contexts: []Context{{Name: "prod", Server: "https://a"}, {Name: "dev", Server: "http://b"}}}
	got, ok := cfg.Find("dev")
	if !ok || got.Server != "http://b" {
		t.Errorf("Find(dev) = %+v, %v; want the dev context", got, ok)
	}
	if _, ok := cfg.Find("nope"); ok {
		t.Error("Find(nope) reported a hit")
	}
}

// TestConfig_Set verifies that Set inserts a new context and replaces an
// existing one of the same name without touching the current context.
func TestConfig_Set(t *testing.T) {
	t.Parallel()

	cfg := &Config{CurrentContext: "prod", Contexts: []Context{{Name: "prod", Server: "https://a"}}}
	cfg.Set(Context{Name: "dev", Server: "http://b"})
	if len(cfg.Contexts) != 2 {
		t.Fatalf("after inserting dev, len(Contexts) = %d, want 2", len(cfg.Contexts))
	}
	cfg.Set(Context{Name: "prod", Server: "https://c", Token: "kkt_x_y"})
	if len(cfg.Contexts) != 2 {
		t.Fatalf("after replacing prod, len(Contexts) = %d, want 2", len(cfg.Contexts))
	}
	prod, _ := cfg.Find("prod")
	if prod.Server != "https://c" || prod.Token != "kkt_x_y" {
		t.Errorf("prod = %+v, want the replacement", prod)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("CurrentContext = %q, want it untouched", cfg.CurrentContext)
	}
}

// TestConfig_Set_normalizesServer verifies a trailing slash is canonicalised away
// on the way in, so the stored file and `config list` show one spelling.
func TestConfig_Set_normalizesServer(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.Set(Context{Name: "prod", Server: "https://kukatko.example.com/"})
	prod, _ := cfg.Find("prod")
	if prod.Server != "https://kukatko.example.com" {
		t.Errorf("stored server = %q, want it normalized", prod.Server)
	}
}

// TestConfig_Use verifies switching the current context and the error for an
// unknown name.
func TestConfig_Use(t *testing.T) {
	t.Parallel()

	cfg := &Config{Contexts: []Context{{Name: "prod", Server: "https://a"}}}
	if err := cfg.Use("prod"); err != nil {
		t.Fatalf("Use(prod) returned %v", err)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("CurrentContext = %q, want prod", cfg.CurrentContext)
	}
	if err := cfg.Use("nope"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("Use(nope) error = %v, want ErrContextNotFound", err)
	}
}

// TestResolve verifies context selection and the per-field environment
// overrides: KUKATKO_SERVER and KUKATKO_TOKEN each replace only their own field,
// so a stored context can be re-credentialed by the environment alone.
func TestResolve(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		CurrentContext: "prod",
		Contexts: []Context{
			{Name: "prod", Server: "https://prod.example.com/", Token: "kkt_p_secret"},
			{Name: "dev", Server: "http://localhost:8080", Token: "kkt_d_secret"},
		},
	}

	tests := []struct {
		name        string
		cfg         *Config
		contextName string
		env         Env
		wantServer  string
		wantToken   string
		wantContext string
		wantErr     error
	}{
		{
			name: "current context is used by default", cfg: cfg,
			wantServer: "https://prod.example.com", wantToken: "kkt_p_secret", wantContext: "prod",
		},
		{
			name: "named context overrides the current one", cfg: cfg, contextName: "dev",
			wantServer: "http://localhost:8080", wantToken: "kkt_d_secret", wantContext: "dev",
		},
		{
			name: "env server overrides the context server only", cfg: cfg,
			env:        Env{Server: "https://staging.example.com"},
			wantServer: "https://staging.example.com", wantToken: "kkt_p_secret", wantContext: "prod",
		},
		{
			name: "env token overrides the context token only", cfg: cfg,
			env:        Env{Token: "kkt_e_env"},
			wantServer: "https://prod.example.com", wantToken: "kkt_e_env", wantContext: "prod",
		},
		{
			name: "env overrides both", cfg: cfg,
			env:        Env{Server: "https://x.example.com/", Token: "kkt_e_env"},
			wantServer: "https://x.example.com", wantToken: "kkt_e_env", wantContext: "prod",
		},
		{
			name: "env alone works without any config", cfg: &Config{},
			env:        Env{Server: "https://x.example.com", Token: "kkt_e_env"},
			wantServer: "https://x.example.com", wantToken: "kkt_e_env",
		},
		{
			name: "nil config with env", cfg: nil,
			env:        Env{Server: "https://x.example.com"},
			wantServer: "https://x.example.com",
		},
		{
			name: "unknown named context", cfg: cfg, contextName: "nope",
			wantErr: ErrContextNotFound,
		},
		{
			name: "dangling current context", cfg: &Config{CurrentContext: "ghost"},
			wantErr: ErrContextNotFound,
		},
		{
			name: "no server anywhere", cfg: &Config{},
			wantErr: ErrNoServer,
		},
		{
			name: "context without a server and no env", cfg: &Config{
				CurrentContext: "bare", Contexts: []Context{{Name: "bare"}},
			},
			wantErr: ErrNoServer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(tt.cfg, tt.contextName, tt.env)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Resolve error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve returned %v", err)
			}
			if got.Server != tt.wantServer {
				t.Errorf("Server = %q, want %q", got.Server, tt.wantServer)
			}
			if got.Token != tt.wantToken {
				t.Errorf("Token = %q, want %q", got.Token, tt.wantToken)
			}
			if got.ContextName != tt.wantContext {
				t.Errorf("ContextName = %q, want %q", got.ContextName, tt.wantContext)
			}
		})
	}
}

// TestEnvFromOS verifies the two environment overrides are read from the process
// environment under their documented names.
func TestEnvFromOS(t *testing.T) {
	// t.Setenv forbids t.Parallel; the environment is restored afterwards.
	t.Setenv(EnvServer, "https://env.example.com")
	t.Setenv(EnvToken, "kkt_env_secret")

	got := EnvFromOS()
	if got.Server != "https://env.example.com" || got.Token != "kkt_env_secret" {
		t.Errorf("EnvFromOS() = %+v, want the environment values", got)
	}
}

// TestEnvFromOS_unset verifies unset variables yield empty overrides, which
// Resolve treats as "keep the context's own value".
func TestEnvFromOS_unset(t *testing.T) {
	t.Setenv(EnvServer, "")
	t.Setenv(EnvToken, "")

	if got := EnvFromOS(); got != (Env{}) {
		t.Errorf("EnvFromOS() = %+v, want the zero Env", got)
	}
}

// TestLoad_missingFile verifies a missing context file is an empty config, not an
// error: a first run driven purely by the environment needs no file.
func TestLoad_missingFile(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join(t.TempDir(), "absent", "ctl.yaml"))
	if err != nil {
		t.Fatalf("Load of a missing file returned %v", err)
	}
	if cfg == nil || len(cfg.Contexts) != 0 || cfg.CurrentContext != "" {
		t.Errorf("Load of a missing file = %+v, want an empty config", cfg)
	}
}

// TestLoad_invalidYAML verifies a corrupt context file surfaces as an error.
func TestLoad_invalidYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ctl.yaml")
	if err := os.WriteFile(path, []byte("contexts: [oops"), 0o600); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load of invalid YAML returned no error")
	}
}

// TestSave_roundTrip verifies a saved config loads back identically.
func TestSave_roundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kukatko", "ctl.yaml")
	want := &Config{
		CurrentContext: "prod",
		Contexts:       []Context{{Name: "prod", Server: "https://prod.example.com", Token: "kkt_p_secret"}},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save returned %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if got.CurrentContext != want.CurrentContext || len(got.Contexts) != 1 || got.Contexts[0] != want.Contexts[0] {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestSave_fileIsOwnerOnly verifies the context file — which holds bearer tokens
// — is never left readable by anyone but its owner, and that its directory is
// owner-only too.
func TestSave_fileIsOwnerOnly(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "kukatko")
	path := filepath.Join(dir, "ctl.yaml")
	cfg := &Config{Contexts: []Context{{Name: "prod", Server: "https://a", Token: "kkt_p_secret"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != configFileMode {
		t.Errorf("config file mode = %o, want %o", got, configFileMode)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != configDirMode {
		t.Errorf("config dir mode = %o, want %o", got, configDirMode)
	}
}

// TestSave_tightensLooseMode verifies Save re-restricts a pre-existing
// world-readable context file rather than writing a token into it as it stands.
func TestSave_tightensLooseMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ctl.yaml")
	// Deliberately world-readable: Save must tighten it rather than reuse it.
	if err := os.WriteFile(path, []byte("contexts: []\n"), 0o644); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	cfg := &Config{Contexts: []Context{{Name: "prod", Server: "https://a", Token: "kkt_p_secret"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != configFileMode {
		t.Errorf("config file mode = %o, want %o", got, configFileMode)
	}
}

// TestSave_leavesNoTempFile verifies the atomic rename leaves nothing behind.
func TestSave_leavesNoTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.yaml")
	if err := Save(path, &Config{Contexts: []Context{{Name: "a", Server: "https://a"}}}); err != nil {
		t.Fatalf("Save returned %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "ctl.yaml" {
		t.Errorf("directory holds %d entries, want just ctl.yaml", len(entries))
	}
}

// TestDefaultConfigPath verifies the client config lives under the user config
// directory, which honours XDG_CONFIG_HOME on Linux.
func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath returned %v", err)
	}
	if want := "/tmp/xdg/kukatko/ctl.yaml"; got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

// TestNormalizeServer verifies trailing slashes and surrounding space are removed
// so the API base path can be concatenated directly.
func TestNormalizeServer(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://a.example.com":    "https://a.example.com",
		"https://a.example.com/":   "https://a.example.com",
		"https://a.example.com///": "https://a.example.com",
		"  http://localhost:8080 ": "http://localhost:8080",
	}
	for in, want := range tests {
		if got := NormalizeServer(in); got != want {
			t.Errorf("NormalizeServer(%q) = %q, want %q", in, got, want)
		}
	}
}
