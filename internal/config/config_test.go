package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setMinimalEnv sets just the required database URL so Load passes validation
// when a test only cares about other behaviour.
func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KUKATKO_DATABASE_URL", "postgres://u:p@localhost:5432/kukatko")
}

// TestLoad_defaults verifies that, with only the required database URL provided
// and no config file, every documented default is applied.
func TestLoad_defaults(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"database.max_open_conns", cfg.Database.MaxOpenConns, 25},
		{"database.max_idle_conns", cfg.Database.MaxIdleConns, 5},
		{"storage.originals_path", cfg.Storage.OriginalsPath, "/var/lib/kukatko/originals"},
		{"storage.cache_path", cfg.Storage.CachePath, "/var/lib/kukatko/cache"},
		{"web.host", cfg.Web.Host, "0.0.0.0"},
		{"web.port", cfg.Web.Port, 8080},
		{"embedding.url", cfg.Embedding.URL, "http://localhost:8000"},
		{"embedding.image_dim", cfg.Embedding.ImageDim, 768},
		{"embedding.face_dim", cfg.Embedding.FaceDim, 512},
		{"trash.retention_days", cfg.Trash.RetentionDays, 30},
		{"duplicate.enabled", cfg.Duplicate.Enabled, true},
		{"duplicate.phash_max_diff", cfg.Duplicate.PhashMaxDiff, 8},
		{"duplicate.embedding_max_dist", cfg.Duplicate.EmbeddingMaxDist, 0.05},
		{"backup.s3.path_style", cfg.Backup.S3.PathStyle, false},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestLoad_envOverridesDefaults verifies env variables override the built-in
// defaults across nested keys and varied scalar types.
func TestLoad_envOverridesDefaults(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_WEB_PORT", "9999")
	t.Setenv("KUKATKO_WEB_HOST", "127.0.0.1")
	t.Setenv("KUKATKO_DATABASE_MAX_OPEN_CONNS", "50")
	t.Setenv("KUKATKO_EMBEDDING_URL", "http://box:9000")
	t.Setenv("KUKATKO_DUPLICATE_ENABLED", "false")
	t.Setenv("KUKATKO_DUPLICATE_EMBEDDING_MAX_DIST", "0.1")
	t.Setenv("KUKATKO_BACKUP_S3_PATH_STYLE", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Web.Port != 9999 {
		t.Errorf("web.port = %d, want 9999", cfg.Web.Port)
	}
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("web.host = %q, want 127.0.0.1", cfg.Web.Host)
	}
	if cfg.Database.MaxOpenConns != 50 {
		t.Errorf("database.max_open_conns = %d, want 50", cfg.Database.MaxOpenConns)
	}
	if cfg.Embedding.URL != "http://box:9000" {
		t.Errorf("embedding.url = %q, want http://box:9000", cfg.Embedding.URL)
	}
	if cfg.Duplicate.Enabled {
		t.Error("duplicate.enabled = true, want false")
	}
	if cfg.Duplicate.EmbeddingMaxDist != 0.1 {
		t.Errorf("duplicate.embedding_max_dist = %v, want 0.1", cfg.Duplicate.EmbeddingMaxDist)
	}
	if !cfg.Backup.S3.PathStyle {
		t.Error("backup.s3.path_style = false, want true")
	}
}

// TestLoad_envOverridesYAMLFile verifies that an env variable wins over a value
// set in the YAML file (env always takes precedence).
func TestLoad_envOverridesYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "database:\n  url: postgres://from-file/db\nweb:\n  port: 7000\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	t.Setenv("KUKATKO_WEB_PORT", "8181")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Database.URL != "postgres://from-file/db" {
		t.Errorf("database.url = %q, want value from file", cfg.Database.URL)
	}
	if cfg.Web.Port != 8181 {
		t.Errorf("web.port = %d, want 8181 (env overrides file)", cfg.Web.Port)
	}
}

// TestLoad_nestedKeyMapping verifies KUKATKO_-prefixed env vars map onto nested
// struct fields, and that the unprefixed MAPY_API_KEY binds to maps.mapy_api_key.
func TestLoad_nestedKeyMapping(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME", "admin")
	t.Setenv("KUKATKO_BACKUP_S3_BUCKET", "kukatko-backups")
	t.Setenv("KUKATKO_WEB_ALLOWED_ORIGINS", "https://a.example,https://b.example")
	t.Setenv("MAPY_API_KEY", "mapy-secret")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Auth.BootstrapAdminUsername != "admin" {
		t.Errorf("auth.bootstrap_admin_username = %q, want admin", cfg.Auth.BootstrapAdminUsername)
	}
	if cfg.Backup.S3.Bucket != "kukatko-backups" {
		t.Errorf("backup.s3.bucket = %q, want kukatko-backups", cfg.Backup.S3.Bucket)
	}
	if cfg.Maps.MapyAPIKey != "mapy-secret" {
		t.Errorf("maps.mapy_api_key = %q, want mapy-secret", cfg.Maps.MapyAPIKey)
	}
	wantOrigins := []string{"https://a.example", "https://b.example"}
	if len(cfg.Web.AllowedOrigins) != len(wantOrigins) {
		t.Fatalf("web.allowed_origins = %v, want %v", cfg.Web.AllowedOrigins, wantOrigins)
	}
	for i, want := range wantOrigins {
		if cfg.Web.AllowedOrigins[i] != want {
			t.Errorf("web.allowed_origins[%d] = %q, want %q", i, cfg.Web.AllowedOrigins[i], want)
		}
	}
}

// TestLoad_missingDatabaseURL verifies the required-field validation triggers
// when no database URL is supplied.
func TestLoad_missingDatabaseURL(t *testing.T) {
	// Ensure no ambient value leaks in from the environment.
	t.Setenv("KUKATKO_DATABASE_URL", "")

	_, err := Load("")
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Fatalf("Load error = %v, want ErrMissingDatabaseURL", err)
	}
}

// TestLoad_invalidWebPort verifies an out-of-range port fails validation.
func TestLoad_invalidWebPort(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_WEB_PORT", "70000")

	_, err := Load("")
	if !errors.Is(err, ErrInvalidWebPort) {
		t.Fatalf("Load error = %v, want ErrInvalidWebPort", err)
	}
}

// TestLoad_invalidPoolSize verifies max_idle_conns may not exceed max_open_conns.
func TestLoad_invalidPoolSize(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_DATABASE_MAX_OPEN_CONNS", "5")
	t.Setenv("KUKATKO_DATABASE_MAX_IDLE_CONNS", "10")

	_, err := Load("")
	if !errors.Is(err, ErrInvalidPoolSize) {
		t.Fatalf("Load error = %v, want ErrInvalidPoolSize", err)
	}
}

// TestLoad_malformedYAML verifies a syntactically invalid config file surfaces
// as an error rather than being silently ignored.
func TestLoad_malformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("web:\n  port: : :\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

// TestResolveConfigPath verifies precedence: explicit path, then KUKATKO_CONFIG,
// then the default.
func TestResolveConfigPath(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		t.Setenv(envConfigPath, "from-env.yaml")
		if got := resolveConfigPath("explicit.yaml"); got != "explicit.yaml" {
			t.Errorf("resolveConfigPath = %q, want explicit.yaml", got)
		}
	})
	t.Run("env used when no explicit path", func(t *testing.T) {
		t.Setenv(envConfigPath, "from-env.yaml")
		if got := resolveConfigPath(""); got != "from-env.yaml" {
			t.Errorf("resolveConfigPath = %q, want from-env.yaml", got)
		}
	})
	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv(envConfigPath, "")
		if got := resolveConfigPath(""); got != defaultConfigPath {
			t.Errorf("resolveConfigPath = %q, want %q", got, defaultConfigPath)
		}
	})
}
