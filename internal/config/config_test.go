package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		{"thumb.engine", cfg.Thumb.Engine, ThumbEngineGo},
		{"thumb.vips_binary", cfg.Thumb.VipsBinary, "vipsthumbnail"},
		{"thumb.concurrency", cfg.Thumb.Concurrency, 0},
		{"web.host", cfg.Web.Host, "0.0.0.0"},
		{"web.port", cfg.Web.Port, 8080},
		{"embedding.url", cfg.Embedding.URL, "http://localhost:8000"},
		{"embedding.image_dim", cfg.Embedding.ImageDim, 768},
		{"embedding.face_dim", cfg.Embedding.FaceDim, 512},
		{"embedding.wake.enabled", cfg.Embedding.Wake.Enabled, false},
		{"embedding.wake.broadcast_addr", cfg.Embedding.Wake.BroadcastAddr, "255.255.255.255:9"},
		{"embedding.wake.min_queue", cfg.Embedding.Wake.MinQueue, 1},
		{"embedding.wake.cooldown", cfg.Embedding.Wake.Cooldown, 5 * time.Minute},
		{"faces.min_det_score", cfg.Faces.MinDetScore, 0.5},
		{"faces.iou_threshold", cfg.Faces.IoUThreshold, 0.1},
		{"faces.suggestion_limit", cfg.Faces.SuggestionLimit, 5},
		{"faces.suggestion_max_distance", cfg.Faces.SuggestionMaxDistance, 0.5},
		{"faces.min_face_size", cfg.Faces.MinFaceSize, 0.02},
		{"cluster.threshold", cfg.Cluster.Threshold, 0.4},
		{"cluster.min_size", cfg.Cluster.MinSize, 2},
		{"cluster.suggestion_max_distance", cfg.Cluster.SuggestionMaxDistance, 0.5},
		{"trash.retention_days", cfg.Trash.RetentionDays, 30},
		{"duplicate.enabled", cfg.Duplicate.Enabled, true},
		{"duplicate.phash_max_diff", cfg.Duplicate.PhashMaxDiff, 8},
		{"duplicate.embedding_max_dist", cfg.Duplicate.EmbeddingMaxDist, 0.05},
		{"upload.max_file_size_mb", cfg.Upload.MaxFileSizeMB, 0},
		{"video.transcode", cfg.Video.Transcode, false},
		{"worker.count", cfg.Worker.Count, 2},
		{"worker.poll_interval", cfg.Worker.PollInterval, 2 * time.Second},
		{"worker.stale_after", cfg.Worker.StaleAfter, 5 * time.Minute},
		{"worker.stale_scan_interval", cfg.Worker.StaleScanInterval, time.Minute},
		{"bulk.max_batch_size", cfg.Bulk.MaxBatchSize, 1000},
		{"maps.base_url", cfg.Maps.BaseURL, "https://api.mapy.com"},
		{"maps.geocode_rate_per_sec", cfg.Maps.GeocodeRatePerSec, 5.0},
		{"maps.geocode_burst", cfg.Maps.GeocodeBurst, 10},
		{"backup.s3.path_style", cfg.Backup.S3.PathStyle, false},
		{"backup.retention", cfg.Backup.Retention, 7},
		{"web.secure_cookies", cfg.Web.SecureCookies, false},
		{"auth.session_ttl", cfg.Auth.SessionTTL, 168 * time.Hour},
		{"auth.session_max_lifetime", cfg.Auth.SessionMaxLifetime, 720 * time.Hour},
		{"auth.login_rate_limit", cfg.Auth.LoginRateLimit, 10},
		{"auth.login_rate_window", cfg.Auth.LoginRateWindow, 15 * time.Minute},
		{"ratelimit.upload.rate_per_sec", cfg.RateLimit.Upload.RatePerSec, 5.0},
		{"ratelimit.upload.burst", cfg.RateLimit.Upload.Burst, 30},
		{"ratelimit.bulk.rate_per_sec", cfg.RateLimit.Bulk.RatePerSec, 2.0},
		{"ratelimit.bulk.burst", cfg.RateLimit.Bulk.Burst, 10},
		{"ratelimit.import.rate_per_sec", cfg.RateLimit.Import.RatePerSec, 1.0},
		{"ratelimit.import.burst", cfg.RateLimit.Import.Burst, 3},
		{"ratelimit.tiles.rate_per_sec", cfg.RateLimit.Tiles.RatePerSec, 50.0},
		{"ratelimit.tiles.burst", cfg.RateLimit.Tiles.Burst, 200},
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

// TestMaxFileSizeBytes verifies the mebibyte-to-byte conversion and the
// unlimited (0/negative) cases.
func TestMaxFileSizeBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mb   int
		want int64
	}{
		{"unlimited zero", 0, 0},
		{"unlimited negative", -5, 0},
		{"one mebibyte", 1, 1024 * 1024},
		{"two hundred mebibytes", 200, 200 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UploadConfig{MaxFileSizeMB: tt.mb}.MaxFileSizeBytes()
			if got != tt.want {
				t.Errorf("MaxFileSizeBytes(%d) = %d, want %d", tt.mb, got, tt.want)
			}
		})
	}
}

// TestLoad_uploadEnvOverride verifies the upload size cap parses from the
// environment.
func TestLoad_uploadEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_UPLOAD_MAX_FILE_SIZE_MB", "512")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Upload.MaxFileSizeMB != 512 {
		t.Errorf("upload.max_file_size_mb = %d, want 512", cfg.Upload.MaxFileSizeMB)
	}
	if got := cfg.Upload.MaxFileSizeBytes(); got != 512*1024*1024 {
		t.Errorf("MaxFileSizeBytes = %d, want %d", got, 512*1024*1024)
	}
}

// TestLoad_rateLimitEnvOverride verifies the per-endpoint rate-limit rules parse
// from the environment, including the float rate and the disable sentinel (0).
func TestLoad_rateLimitEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC", "12.5")
	t.Setenv("KUKATKO_RATELIMIT_UPLOAD_BURST", "60")
	t.Setenv("KUKATKO_RATELIMIT_TILES_RATE_PER_SEC", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.RateLimit.Upload.RatePerSec != 12.5 {
		t.Errorf("ratelimit.upload.rate_per_sec = %v, want 12.5", cfg.RateLimit.Upload.RatePerSec)
	}
	if cfg.RateLimit.Upload.Burst != 60 {
		t.Errorf("ratelimit.upload.burst = %d, want 60", cfg.RateLimit.Upload.Burst)
	}
	if cfg.RateLimit.Tiles.RatePerSec != 0 {
		t.Errorf("ratelimit.tiles.rate_per_sec = %v, want 0 (disabled)", cfg.RateLimit.Tiles.RatePerSec)
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

// TestLoad_wakeEnvOverride verifies the Wake-on-LAN settings parse from the
// environment, including a valid MAC and a custom cooldown.
func TestLoad_wakeEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_EMBEDDING_WAKE_ENABLED", "true")
	t.Setenv("KUKATKO_EMBEDDING_WAKE_MAC", "aa:bb:cc:dd:ee:ff")
	t.Setenv("KUKATKO_EMBEDDING_WAKE_BROADCAST_ADDR", "192.168.1.255:9")
	t.Setenv("KUKATKO_EMBEDDING_WAKE_MIN_QUEUE", "4")
	t.Setenv("KUKATKO_EMBEDDING_WAKE_COOLDOWN", "10m")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	w := cfg.Embedding.Wake
	if !w.Enabled || w.MAC != "aa:bb:cc:dd:ee:ff" || w.BroadcastAddr != "192.168.1.255:9" {
		t.Errorf("wake basics = %+v, want enabled with mac/broadcast set", w)
	}
	if w.MinQueue != 4 || w.Cooldown != 10*time.Minute {
		t.Errorf("wake thresholds = min_queue %d cooldown %s, want 4/10m", w.MinQueue, w.Cooldown)
	}
}

// TestLoad_wakeValidation verifies that an enabled wake config with a
// missing/invalid MAC (or no destination) fails validation, while a disabled
// config tolerates any MAC value.
func TestLoad_wakeValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{
			name:    "enabled without mac",
			env:     map[string]string{"KUKATKO_EMBEDDING_WAKE_ENABLED": "true"},
			wantErr: true,
		},
		{
			name: "enabled with invalid mac",
			env: map[string]string{
				"KUKATKO_EMBEDDING_WAKE_ENABLED": "true",
				"KUKATKO_EMBEDDING_WAKE_MAC":     "not-a-mac",
			},
			wantErr: true,
		},
		{
			name: "enabled with valid mac and default broadcast",
			env: map[string]string{
				"KUKATKO_EMBEDDING_WAKE_ENABLED": "true",
				"KUKATKO_EMBEDDING_WAKE_MAC":     "aa:bb:cc:dd:ee:ff",
			},
			wantErr: false,
		},
		{
			name: "disabled tolerates garbage mac",
			env: map[string]string{
				"KUKATKO_EMBEDDING_WAKE_ENABLED": "false",
				"KUKATKO_EMBEDDING_WAKE_MAC":     "garbage",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMinimalEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := Load("")
			if tt.wantErr && !errors.Is(err, ErrInvalidWake) {
				t.Fatalf("Load error = %v, want ErrInvalidWake", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}
		})
	}
}

// TestLoad_thumbEngine verifies thumb.engine is overridable and validated: the
// accepted values load, an unknown engine fails startup with ErrInvalidThumbEngine.
func TestLoad_thumbEngine(t *testing.T) {
	tests := []struct {
		name       string
		engine     string
		wantErr    bool
		wantEngine string
	}{
		{name: "empty defaults to go", engine: "", wantErr: false, wantEngine: ThumbEngineGo},
		{name: "go accepted", engine: "go", wantErr: false, wantEngine: ThumbEngineGo},
		{name: "vips accepted", engine: "vips", wantErr: false, wantEngine: ThumbEngineVips},
		{name: "unknown rejected", engine: "imagemagick", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMinimalEnv(t)
			if tt.engine != "" {
				t.Setenv("KUKATKO_THUMB_ENGINE", tt.engine)
			}
			t.Setenv("KUKATKO_THUMB_CONCURRENCY", "3")
			cfg, err := Load("")
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidThumbEngine) {
					t.Fatalf("Load error = %v, want ErrInvalidThumbEngine", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}
			if cfg.Thumb.Engine != tt.wantEngine {
				t.Errorf("thumb.engine = %q, want %q", cfg.Thumb.Engine, tt.wantEngine)
			}
			if cfg.Thumb.VipsEnabled() != (tt.wantEngine == ThumbEngineVips) {
				t.Errorf("VipsEnabled() = %v, inconsistent with engine %q", cfg.Thumb.VipsEnabled(), cfg.Thumb.Engine)
			}
			if cfg.Thumb.Concurrency != 3 {
				t.Errorf("thumb.concurrency = %d, want 3", cfg.Thumb.Concurrency)
			}
		})
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

// TestLoad_authDurationEnvOverride verifies Go-duration auth keys parse from
// environment variables (via viper's string-to-duration decode hook).
func TestLoad_authDurationEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_AUTH_SESSION_TTL", "1h")
	t.Setenv("KUKATKO_AUTH_SESSION_MAX_LIFETIME", "24h")
	t.Setenv("KUKATKO_AUTH_LOGIN_RATE_LIMIT", "3")
	t.Setenv("KUKATKO_AUTH_LOGIN_RATE_WINDOW", "30s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Auth.SessionTTL != time.Hour {
		t.Errorf("auth.session_ttl = %s, want 1h", cfg.Auth.SessionTTL)
	}
	if cfg.Auth.SessionMaxLifetime != 24*time.Hour {
		t.Errorf("auth.session_max_lifetime = %s, want 24h", cfg.Auth.SessionMaxLifetime)
	}
	if cfg.Auth.LoginRateLimit != 3 {
		t.Errorf("auth.login_rate_limit = %d, want 3", cfg.Auth.LoginRateLimit)
	}
	if cfg.Auth.LoginRateWindow != 30*time.Second {
		t.Errorf("auth.login_rate_window = %s, want 30s", cfg.Auth.LoginRateWindow)
	}
}

// TestLoad_invalidSessionLifetime verifies a max lifetime shorter than the
// sliding TTL fails validation.
func TestLoad_invalidSessionLifetime(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_AUTH_SESSION_TTL", "48h")
	t.Setenv("KUKATKO_AUTH_SESSION_MAX_LIFETIME", "24h")

	_, err := Load("")
	if !errors.Is(err, ErrInvalidSessionLifetime) {
		t.Fatalf("Load error = %v, want ErrInvalidSessionLifetime", err)
	}
}

// TestLoad_invalidLoginRateLimit verifies a non-positive attempt count fails
// validation.
func TestLoad_invalidLoginRateLimit(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_AUTH_LOGIN_RATE_LIMIT", "0")

	_, err := Load("")
	if !errors.Is(err, ErrInvalidLoginRateLimit) {
		t.Fatalf("Load error = %v, want ErrInvalidLoginRateLimit", err)
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

// TestLoad_importPhotoPrismDefaults verifies the import.photoprism keys default
// to empty (import disabled).
func TestLoad_importPhotoPrismDefaults(t *testing.T) {
	setMinimalEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Import.PhotoPrism.BaseURL != "" || cfg.Import.PhotoPrism.Token != "" {
		t.Errorf("import.photoprism defaults = %+v, want empty", cfg.Import.PhotoPrism)
	}
	if cfg.Import.PhotoPrism.PageSize != 1000 {
		t.Errorf("import.photoprism.page_size default = %d, want 1000", cfg.Import.PhotoPrism.PageSize)
	}
}

// TestLoad_importPhotoPrismEnvOverride verifies the import.photoprism keys can be
// supplied via the KUKATKO_ environment (secret token included).
func TestLoad_importPhotoPrismEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_IMPORT_PHOTOPRISM_BASE_URL", "https://photos.example")
	t.Setenv("KUKATKO_IMPORT_PHOTOPRISM_TOKEN", "secret-app-token")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Import.PhotoPrism.BaseURL != "https://photos.example" {
		t.Errorf("base_url = %q", cfg.Import.PhotoPrism.BaseURL)
	}
	if cfg.Import.PhotoPrism.Token != "secret-app-token" {
		t.Errorf("token = %q", cfg.Import.PhotoPrism.Token)
	}
}
