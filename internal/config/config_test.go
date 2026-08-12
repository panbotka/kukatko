package config

import (
	"errors"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/expand"
)

// setMinimalEnv clears every variable Load reads and then sets just the required
// database URL, so a test that asserts a default is asserting the default rather
// than whatever this shell happens to export.
//
// The clearing is the point: a developer who sources .secrets/db.env — which is
// what running the dev server and the integration tests both want — otherwise
// carries a real deployment's KUKATKO_STORAGE_*, KUKATKO_IMPORT_* and
// KUKATKO_WORKER_COUNT into `make check`, and the defaults tests fail for reasons
// that have nothing to do with the change under test.
func setMinimalEnv(t *testing.T) {
	t.Helper()
	clearConfigEnv(t)
	t.Setenv("KUKATKO_DATABASE_URL", "postgres://u:p@localhost:5432/kukatko")
}

// clearConfigEnv unsets every KUKATKO_-prefixed variable plus the unprefixed
// MAPY_API_KEY, restoring the process environment when the test ends.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if !found || (!strings.HasPrefix(key, envPrefix+"_") && key != "MAPY_API_KEY") {
			continue
		}
		// t.Setenv cannot unset, so restore by hand — with the value captured now,
		// before anything in this test has had a chance to change it.
		t.Cleanup(func() {
			if err := os.Setenv(key, value); err != nil {
				t.Errorf("restoring %s: %v", key, err)
			}
		})
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetting %s: %v", key, err)
		}
	}
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
		{"storage.backend", cfg.Storage.Backend, StorageBackendFS},
		{"storage.originals_path", cfg.Storage.OriginalsPath, "/var/lib/kukatko/originals"},
		{"storage.cache_path", cfg.Storage.CachePath, "/var/lib/kukatko/cache"},
		{"storage.temp_path", cfg.Storage.TempPath, "/var/lib/kukatko/tmp"},
		{"storage.r2.region", cfg.Storage.R2.Region, "auto"},
		{"storage.r2.endpoint", cfg.Storage.R2.Endpoint, ""},
		{"storage.r2.bucket", cfg.Storage.R2.Bucket, ""},
		{"storage.r2.url_ttl", cfg.Storage.R2.URLTTL, time.Hour},
		{"thumb.engine", cfg.Thumb.Engine, ThumbEngineGo},
		{"thumb.vips_binary", cfg.Thumb.VipsBinary, "vipsthumbnail"},
		{"thumb.concurrency", cfg.Thumb.Concurrency, 0},
		{"thumb.max_pixels", cfg.Thumb.MaxPixels, int64(200_000_000)},
		{"web.host", cfg.Web.Host, "0.0.0.0"},
		{"web.port", cfg.Web.Port, 8080},
		{"embedding.url", cfg.Embedding.URL, "http://localhost:8000"},
		{"embedding.image_dim", cfg.Embedding.ImageDim, 1152},
		{"embedding.face_dim", cfg.Embedding.FaceDim, 512},
		{"embedding.dial_timeout", cfg.Embedding.DialTimeout, 3 * time.Second},
		{"embedding.request_timeout", cfg.Embedding.RequestTimeout, 60 * time.Second},
		{"embedding.text_timeout", cfg.Embedding.TextTimeout, 5 * time.Second},
		{"embedding.wake.enabled", cfg.Embedding.Wake.Enabled, false},
		{"embedding.wake.broadcast_addr", cfg.Embedding.Wake.BroadcastAddr, "255.255.255.255:9"},
		{"embedding.wake.min_queue", cfg.Embedding.Wake.MinQueue, 1},
		{"embedding.wake.cooldown", cfg.Embedding.Wake.Cooldown, 5 * time.Minute},
		{"embedding.ocr.enabled", cfg.Embedding.OCR.Enabled, true},
		{"embedding.ocr.min_confidence", cfg.Embedding.OCR.MinConfidence, 0.5},
		{"embedding.ocr.preview_size", cfg.Embedding.OCR.PreviewSize, "fit_1920"},
		{"faces.min_det_score", cfg.Faces.MinDetScore, 0.5},
		{"faces.iou_threshold", cfg.Faces.IoUThreshold, 0.1},
		{"faces.suggestion_limit", cfg.Faces.SuggestionLimit, 5},
		{"faces.suggestion_max_distance", cfg.Faces.SuggestionMaxDistance, 0.5},
		{"faces.min_face_size", cfg.Faces.MinFaceSize, 0.02},
		{"cluster.threshold", cfg.Cluster.Threshold, 0.4},
		{"cluster.min_size", cfg.Cluster.MinSize, 2},
		{"cluster.suggestion_max_distance", cfg.Cluster.SuggestionMaxDistance, 0.5},
		{"candidates.max_distance", cfg.Candidates.MaxDistance, 0.5},
		{"candidates.search_limit", cfg.Candidates.SearchLimit, 1000},
		{"candidates.min_face_px", cfg.Candidates.MinFacePx, 32},
		{"candidates.concurrency", cfg.Candidates.Concurrency, 8},
		{"candidates.max_exemplars", cfg.Candidates.MaxExemplars, 500},
		{"candidates.max_candidates", cfg.Candidates.MaxCandidates, 500},
		{"sweep.concurrency", cfg.Sweep.Concurrency, 4},
		{"sweep.max_subjects", cfg.Sweep.MaxSubjects, 500},
		{"expand.max_distance", cfg.Expand.MaxDistance, 0.20},
		{"expand.limit", cfg.Expand.Limit, 50},
		{"expand.max_limit", cfg.Expand.MaxLimit, 200},
		{"expand.search_limit", cfg.Expand.SearchLimit, 200},
		{"expand.source_cap", cfg.Expand.SourceCap, 500},
		{"expand.concurrency", cfg.Expand.Concurrency, 8},
		{"review.band_min", cfg.Review.BandMin, 0.45},
		{"review.band_max", cfg.Review.BandMax, 0.75},
		{"review.sure_min", cfg.Review.SureMin, 0.80},
		{"review.sure_share", cfg.Review.SureShare, 0.70},
		{"review.queue_size", cfg.Review.QueueSize, 20},
		{"review.round_size", cfg.Review.RoundSize, 10},
		{"review.round_max_per_entity", cfg.Review.RoundMaxPerEntity, 3},
		{"review.cache_ttl", cfg.Review.CacheTTL, 60 * time.Second},
		{"review.max_labels", cfg.Review.MaxLabels, 200},
		{"review.label_concurrency", cfg.Review.LabelConcurrency, 2},
		// The variety rule: a batch of 20 questions therefore draws on at least
		// five different people or labels instead of interrogating one of them.
		{"review.max_per_entity", cfg.Review.MaxPerEntity, 4},
		{"trash.retention_days", cfg.Trash.RetentionDays, 365},
		{"duplicate.enabled", cfg.Duplicate.Enabled, true},
		{"duplicate.phash_max_diff", cfg.Duplicate.PhashMaxDiff, 8},
		// Model-specific: re-derived when the image tower became SigLIP 2, see
		// docs/THRESHOLDS.md. It was 0.05 while the embeddings were CLIP ViT-L-14.
		{"duplicate.embedding_max_dist", cfg.Duplicate.EmbeddingMaxDist, 0.028},
		{"stacks.enabled", cfg.Stacks.Enabled, true},
		{"stacks.rules.base_name", cfg.Stacks.Rules.BaseName, true},
		{"stacks.rules.sequential_copy", cfg.Stacks.Rules.SequentialCopy, true},
		{"stacks.rules.unique_id", cfg.Stacks.Rules.UniqueID, true},
		{"stacks.rules.time_gps", cfg.Stacks.Rules.TimeGPS, false},
		// The MCP server hands an AI agent the library, so its default is the
		// off switch — this pins that, not just the key's existence.
		{"mcp.enabled", cfg.MCP.Enabled, false},
		{"mcp.page_size", cfg.MCP.PageSize, 25},
		{"mcp.max_page_size", cfg.MCP.MaxPageSize, 100},
		// The sidecar export is the "curation survives losing the database" mechanism,
		// and one switched on after the disaster is no mechanism at all — so this pins
		// the default is on, not merely that the key exists.
		{"sidecar.enabled", cfg.Sidecar.Enabled, true},
		{"location_estimate.enabled", cfg.LocationEstimate.Enabled, true},
		{"location_estimate.window", cfg.LocationEstimate.Window, 6 * time.Hour},
		{"location_estimate.radius_meters", cfg.LocationEstimate.RadiusMeters, 5000.0},
		{"upload.max_file_size_mb", cfg.Upload.MaxFileSizeMB, 0},
		{"video.transcode", cfg.Video.Transcode, false},
		{"worker.count", cfg.Worker.Count, 2},
		{"worker.poll_interval", cfg.Worker.PollInterval, 2 * time.Second},
		{"worker.stale_after", cfg.Worker.StaleAfter, 5 * time.Minute},
		{"worker.stale_scan_interval", cfg.Worker.StaleScanInterval, time.Minute},
		{"bulk.max_batch_size", cfg.Bulk.MaxBatchSize, 1000},
		{"maps.base_url", cfg.Maps.BaseURL, "https://api.mapy.com"},
		{"maps.user_agent", cfg.Maps.UserAgent, ""},
		{"maps.geocode_rate_per_sec", cfg.Maps.GeocodeRatePerSec, 5.0},
		{"maps.geocode_burst", cfg.Maps.GeocodeBurst, 10},
		// The rate limiter alone bounds only how fast credits are spent. The budget
		// is what stops a full import from draining the whole quota, so a default
		// of "no cap" would be a silently missing guard rail.
		{"maps.geocode_budget", cfg.Maps.GeocodeBudget, 1000},
		{"maps.geocode_budget_window", cfg.Maps.GeocodeBudgetWindow, 24 * time.Hour},
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
		{"ratelimit.comment.rate_per_sec", cfg.RateLimit.Comment.RatePerSec, 0.5},
		{"ratelimit.comment.burst", cfg.RateLimit.Comment.Burst, 10},
		{"ratelimit.tiles.rate_per_sec", cfg.RateLimit.Tiles.RatePerSec, 50.0},
		{"ratelimit.tiles.burst", cfg.RateLimit.Tiles.Burst, 200},
		{"metrics.enabled", cfg.Metrics.Enabled, true},
		// The library gauges aggregate over the largest tables there are and
		// Prometheus scrapes forever, so a default of "no memoisation" would make
		// /metrics itself the load it exists to report on.
		{"metrics.library_ttl", cfg.Metrics.LibraryTTL, time.Minute},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	// A map does not belong in the comparable table above. This one is worth
	// pinning anyway: the sidecar-bound job types must be serialised out of the
	// box, because they hit the single-request-at-a-time embeddings box.
	wantTypes := map[string]int{"image_embed": 1, "face_detect": 1, "ocr": 1}
	if !maps.Equal(cfg.Worker.TypeCount, wantTypes) {
		t.Errorf("worker.type_count = %v, want %v", cfg.Worker.TypeCount, wantTypes)
	}
}

// TestLoad_workerTypeCountFromYAML verifies a per-job-type worker override is
// read from the config file. A YAML block replaces the default map rather than
// merging into it, so the sidecar-bound caps are re-applied by internal/worker,
// not by this key — see TestEffectiveTypeConcurrency there.
func TestLoad_workerTypeCountFromYAML(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "database:\n  url: postgres://localhost/db\n" +
		"worker:\n  count: 6\n  type_count:\n    thumbnail: 4\n    image_embed: 2\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Worker.Count != 6 {
		t.Errorf("worker.count = %d, want 6", cfg.Worker.Count)
	}
	want := map[string]int{"thumbnail": 4, "image_embed": 2}
	if !maps.Equal(cfg.Worker.TypeCount, want) {
		t.Errorf("worker.type_count = %v, want %v", cfg.Worker.TypeCount, want)
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
	t.Setenv("KUKATKO_EMBEDDING_TEXT_TIMEOUT", "2s")
	t.Setenv("KUKATKO_DUPLICATE_ENABLED", "false")
	t.Setenv("KUKATKO_SIDECAR_ENABLED", "false")
	t.Setenv("KUKATKO_DUPLICATE_EMBEDDING_MAX_DIST", "0.1")
	t.Setenv("KUKATKO_BACKUP_S3_PATH_STYLE", "true")
	t.Setenv("KUKATKO_MCP_ENABLED", "true")
	t.Setenv("KUKATKO_MCP_PAGE_SIZE", "5")
	t.Setenv("KUKATKO_THUMB_MAX_PIXELS", "500000000")
	t.Setenv("KUKATKO_MAPS_GEOCODE_BUDGET", "250")
	t.Setenv("KUKATKO_MAPS_GEOCODE_BUDGET_WINDOW", "6h")
	t.Setenv("KUKATKO_EMBEDDING_OCR_ENABLED", "false")
	t.Setenv("KUKATKO_EMBEDDING_OCR_MIN_CONFIDENCE", "0.8")

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
	// The search-facing timeout is the one an operator tunes when the box is slow
	// but reachable, so it must be reachable from the environment too.
	if cfg.Embedding.TextTimeout != 2*time.Second {
		t.Errorf("embedding.text_timeout = %v, want 2s", cfg.Embedding.TextTimeout)
	}
	if cfg.Duplicate.Enabled {
		t.Error("duplicate.enabled = true, want false")
	}
	if cfg.Sidecar.Enabled {
		t.Error("sidecar.enabled = true, want false")
	}
	// Text recognition is the switch an operator flips when the box is busy or
	// the readings are noisy, so both of its knobs must be reachable by env.
	if cfg.Embedding.OCR.Enabled {
		t.Error("embedding.ocr.enabled = true, want false")
	}
	if cfg.Embedding.OCR.MinConfidence != 0.8 {
		t.Errorf("embedding.ocr.min_confidence = %v, want 0.8", cfg.Embedding.OCR.MinConfidence)
	}
	if cfg.Duplicate.EmbeddingMaxDist != 0.1 {
		t.Errorf("duplicate.embedding_max_dist = %v, want 0.1", cfg.Duplicate.EmbeddingMaxDist)
	}
	if !cfg.Backup.S3.PathStyle {
		t.Error("backup.s3.path_style = false, want true")
	}
	// Turning the MCP server on is the one thing an operator does to this key,
	// and a key with no registered default would silently ignore its env var.
	if !cfg.MCP.Enabled {
		t.Error("mcp.enabled = false, want true from KUKATKO_MCP_ENABLED")
	}
	if cfg.MCP.PageSize != 5 {
		t.Errorf("mcp.page_size = %d, want 5", cfg.MCP.PageSize)
	}
	// A large int64 key: confirm the env string decodes into the int64 field so
	// operators can raise the decode cap for a genuinely huge-panorama library.
	if cfg.Thumb.MaxPixels != 500_000_000 {
		t.Errorf("thumb.max_pixels = %d, want 500000000", cfg.Thumb.MaxPixels)
	}
	// The geocode budget is the guard on metered mapy.com credits: an operator
	// tightens it from the environment before a big import, so both halves of it
	// must actually decode.
	if cfg.Maps.GeocodeBudget != 250 {
		t.Errorf("maps.geocode_budget = %d, want 250", cfg.Maps.GeocodeBudget)
	}
	if cfg.Maps.GeocodeBudgetWindow != 6*time.Hour {
		t.Errorf("maps.geocode_budget_window = %s, want 6h", cfg.Maps.GeocodeBudgetWindow)
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
	clearConfigEnv(t)
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

// TestLoad_reviewTierEnvOverrides verifies the two tier-mix keys can be tuned
// from the environment like everything else, since they are the dial an operator
// reaches for when the game feels too hard or too mindless.
func TestLoad_reviewTierEnvOverrides(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_REVIEW_SURE_MIN", "0.85")
	t.Setenv("KUKATKO_REVIEW_SURE_SHARE", "0.6")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Review.SureMin != 0.85 {
		t.Errorf("review.sure_min = %v, want 0.85", cfg.Review.SureMin)
	}
	if cfg.Review.SureShare != 0.6 {
		t.Errorf("review.sure_share = %v, want 0.6", cfg.Review.SureShare)
	}
}

// TestLoad_reviewRoundEnvOverrides covers the two round keys, which are the
// dials an operator reaches for when a session feels too long or too repetitive.
func TestLoad_reviewRoundEnvOverrides(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_REVIEW_ROUND_SIZE", "6")
	t.Setenv("KUKATKO_REVIEW_ROUND_MAX_PER_ENTITY", "2")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Review.RoundSize != 6 {
		t.Errorf("review.round_size = %d, want 6", cfg.Review.RoundSize)
	}
	if cfg.Review.RoundMaxPerEntity != 2 {
		t.Errorf("review.round_max_per_entity = %d, want 2", cfg.Review.RoundMaxPerEntity)
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
	t.Setenv("KUKATKO_MAPS_USER_AGENT", "Kukatko/1.0 (token)")

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
	if want := "Kukatko/1.0 (token)"; cfg.Maps.UserAgent != want {
		t.Errorf("maps.user_agent = %q, want %q", cfg.Maps.UserAgent, want)
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

// TestLoad_trustedProxiesDefault pins the shipped default: a reverse proxy is
// on the same host or the same private network, so those two ranges — and
// nothing else — may rename the client in a forwarding header.
func TestLoad_trustedProxiesDefault(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := []string{"loopback", "private"}
	if !slices.Equal(cfg.Web.TrustedProxies, want) {
		t.Fatalf("web.trusted_proxies = %v, want %v", cfg.Web.TrustedProxies, want)
	}
	set, err := cfg.Web.TrustedProxySet()
	if err != nil {
		t.Fatalf("TrustedProxySet returned error: %v", err)
	}
	if !set.Contains(netip.MustParseAddr("172.18.0.2")) {
		t.Error("the default set does not trust a Docker-network proxy")
	}
	if set.Contains(netip.MustParseAddr("203.0.113.7")) {
		t.Error("the default set trusts a public address; forwarding headers would be forgeable")
	}
}

// TestLoad_trustedProxiesEnvOverride verifies the list parses from a
// comma-separated environment variable, the form a deployment sets it in.
func TestLoad_trustedProxiesEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_WEB_TRUSTED_PROXIES", "loopback,10.8.0.0/24")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	set, err := cfg.Web.TrustedProxySet()
	if err != nil {
		t.Fatalf("TrustedProxySet returned error: %v", err)
	}
	if !set.Contains(netip.MustParseAddr("10.8.0.9")) {
		t.Error("the configured block is not trusted")
	}
	if set.Contains(netip.MustParseAddr("192.168.1.1")) {
		t.Error("an override still trusts the built-in private ranges; it must replace them, not extend them")
	}
}

// TestLoad_trustedProxiesEmptyTrustsNothing verifies an explicitly empty list is
// honoured — the strictest setting, for an instance exposed directly. It has to
// be written in YAML: viper reads an empty environment variable as "unset", so
// KUKATKO_WEB_TRUSTED_PROXIES="" leaves the default in place.
func TestLoad_trustedProxiesEmptyTrustsNothing(t *testing.T) {
	setMinimalEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("web:\n  trusted_proxies: []\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	set, err := cfg.Web.TrustedProxySet()
	if err != nil {
		t.Fatalf("TrustedProxySet returned error: %v", err)
	}
	if !set.Empty() {
		t.Errorf("web.trusted_proxies = %v, want an empty set", cfg.Web.TrustedProxies)
	}
}

// TestLoad_invalidTrustedProxy verifies a typo fails startup instead of silently
// trusting nothing (or, worse, something else).
func TestLoad_invalidTrustedProxy(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_WEB_TRUSTED_PROXIES", "loopback,proxy.example.com")

	_, err := Load("")
	if !errors.Is(err, ErrInvalidTrustedProxy) {
		t.Fatalf("Load error = %v, want ErrInvalidTrustedProxy", err)
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

// setR2Env sets every storage.r2 key an "r2" deployment requires, so a test can
// blank exactly the one it is about.
func setR2Env(t *testing.T) {
	t.Helper()
	t.Setenv("KUKATKO_STORAGE_BACKEND", StorageBackendR2)
	t.Setenv("KUKATKO_STORAGE_TEMP_PATH", "/var/lib/kukatko/tmp")
	t.Setenv("KUKATKO_STORAGE_R2_ENDPOINT", "https://account.r2.cloudflarestorage.com")
	t.Setenv("KUKATKO_STORAGE_R2_BUCKET", "kukatko")
	t.Setenv("KUKATKO_STORAGE_R2_ACCESS_KEY", "access")
	t.Setenv("KUKATKO_STORAGE_R2_SECRET_KEY", "secret")
	t.Setenv("KUKATKO_STORAGE_R2_MEDIA_BASE_URL", "https://media.example.com")
	t.Setenv("KUKATKO_STORAGE_R2_URL_SIGNING_SECRET", "signing-secret")
}

// TestLoad_storageBackend verifies storage.backend is overridable and validated:
// "fs" and "r2" load, an unknown backend fails startup.
func TestLoad_storageBackend(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		wantErr bool
	}{
		{name: "empty defaults to fs", backend: ""},
		{name: "fs accepted", backend: StorageBackendFS},
		{name: "r2 accepted", backend: StorageBackendR2},
		{name: "unknown rejected", backend: "s3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMinimalEnv(t)
			if tt.backend == StorageBackendR2 {
				setR2Env(t)
			} else if tt.backend != "" {
				t.Setenv("KUKATKO_STORAGE_BACKEND", tt.backend)
			}
			cfg, err := Load("")
			if tt.backend == "s3" {
				if !errors.Is(err, ErrInvalidStorageBackend) {
					t.Fatalf("Load error = %v, want ErrInvalidStorageBackend", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}
			want := tt.backend
			if want == "" {
				want = StorageBackendFS
			}
			if cfg.Storage.Backend != want {
				t.Errorf("storage.backend = %q, want %q", cfg.Storage.Backend, want)
			}
		})
	}
}

// TestLoad_r2EnvOverride verifies every storage.r2 key maps onto its KUKATKO_
// environment variable, including the duration and the previous signing secret.
func TestLoad_r2EnvOverride(t *testing.T) {
	setMinimalEnv(t)
	setR2Env(t)
	t.Setenv("KUKATKO_STORAGE_R2_REGION", "auto")
	t.Setenv("KUKATKO_STORAGE_R2_URL_SIGNING_SECRET_PREVIOUS", "old-secret")
	t.Setenv("KUKATKO_STORAGE_R2_URL_TTL", "15m")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"storage.backend", cfg.Storage.Backend, StorageBackendR2},
		{"storage.temp_path", cfg.Storage.TempPath, "/var/lib/kukatko/tmp"},
		{"storage.r2.endpoint", cfg.Storage.R2.Endpoint, "https://account.r2.cloudflarestorage.com"},
		{"storage.r2.region", cfg.Storage.R2.Region, "auto"},
		{"storage.r2.bucket", cfg.Storage.R2.Bucket, "kukatko"},
		{"storage.r2.access_key", cfg.Storage.R2.AccessKey, "access"},
		{"storage.r2.secret_key", cfg.Storage.R2.SecretKey, "secret"},
		{"storage.r2.media_base_url", cfg.Storage.R2.MediaBaseURL, "https://media.example.com"},
		{"storage.r2.url_signing_secret", cfg.Storage.R2.URLSigningSecret, "signing-secret"},
		{"storage.r2.url_signing_secret_previous", cfg.Storage.R2.URLSigningSecretPrevious, "old-secret"},
		{"storage.r2.url_ttl", cfg.Storage.R2.URLTTL, 15 * time.Minute},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
}

// TestLoad_r2MissingKeyFailsFast verifies that selecting the r2 backend without a
// required key fails startup rather than surfacing later as a runtime error, and
// that the message names the missing key without leaking any credential value.
func TestLoad_r2MissingKeyFailsFast(t *testing.T) {
	tests := []struct {
		env string
		key string
	}{
		{env: "KUKATKO_STORAGE_R2_ENDPOINT", key: "storage.r2.endpoint"},
		{env: "KUKATKO_STORAGE_R2_BUCKET", key: "storage.r2.bucket"},
		{env: "KUKATKO_STORAGE_R2_ACCESS_KEY", key: "storage.r2.access_key"},
		{env: "KUKATKO_STORAGE_R2_SECRET_KEY", key: "storage.r2.secret_key"},
		{env: "KUKATKO_STORAGE_R2_MEDIA_BASE_URL", key: "storage.r2.media_base_url"},
		{env: "KUKATKO_STORAGE_R2_URL_SIGNING_SECRET", key: "storage.r2.url_signing_secret"},
	}
	for _, tt := range tests {
		t.Run("missing "+tt.key, func(t *testing.T) {
			setMinimalEnv(t)
			setR2Env(t)
			t.Setenv(tt.env, "")

			_, err := Load("")
			if !errors.Is(err, ErrIncompleteR2Config) {
				t.Fatalf("Load error = %v, want ErrIncompleteR2Config", err)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("Load error %q does not name the missing key %q", err, tt.key)
			}
			for _, secret := range []string{"secret", "signing-secret", "access"} {
				if strings.Contains(err.Error(), `"`+secret+`"`) {
					t.Errorf("Load error leaks a credential value: %v", err)
				}
			}
		})
	}
}

// TestLoad_r2EmptyTempPathFailsFast verifies that blanking storage.temp_path in
// the YAML file fails startup for the r2 backend, which stages every upload and
// download through it. The key has a default, so only a file can empty it: viper
// treats an empty environment variable as unset.
func TestLoad_r2EmptyTempPathFailsFast(t *testing.T) {
	setMinimalEnv(t)
	setR2Env(t)
	os.Unsetenv("KUKATKO_STORAGE_TEMP_PATH")

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("storage:\n  temp_path: \"\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, err := Load(path)
	if !errors.Is(err, ErrIncompleteR2Config) {
		t.Fatalf("Load error = %v, want ErrIncompleteR2Config", err)
	}
	if !strings.Contains(err.Error(), "storage.temp_path") {
		t.Errorf("Load error %q does not name storage.temp_path", err)
	}
}

// TestLoad_r2InvalidTTL verifies a non-positive signed-URL TTL fails startup: it
// would mint URLs that are expired the moment they are handed out.
func TestLoad_r2InvalidTTL(t *testing.T) {
	setMinimalEnv(t)
	setR2Env(t)
	t.Setenv("KUKATKO_STORAGE_R2_URL_TTL", "0s")

	_, err := Load("")
	if !errors.Is(err, ErrIncompleteR2Config) {
		t.Fatalf("Load error = %v, want ErrIncompleteR2Config", err)
	}
}

// TestLoad_r2WithoutSignedURLs verifies the shape a bucket with no edge Worker in
// front of it takes: the r2 backend with neither media_base_url nor
// url_signing_secret loads, and the TTL is not checked because nothing is signed.
// That is the configuration development runs against a local MinIO.
func TestLoad_r2WithoutSignedURLs(t *testing.T) {
	setMinimalEnv(t)
	setR2Env(t)
	t.Setenv("KUKATKO_STORAGE_R2_MEDIA_BASE_URL", "")
	t.Setenv("KUKATKO_STORAGE_R2_URL_SIGNING_SECRET", "")
	t.Setenv("KUKATKO_STORAGE_R2_URL_TTL", "0s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Storage.R2.MediaBaseURL != "" {
		t.Errorf("storage.r2.media_base_url = %q, want empty", cfg.Storage.R2.MediaBaseURL)
	}
	if cfg.Storage.R2.Bucket != "kukatko" {
		t.Errorf("storage.r2.bucket = %q, want %q", cfg.Storage.R2.Bucket, "kukatko")
	}
}

// TestLoad_r2SigningSecretWithoutBaseURL verifies that half a signed-URL pair
// fails startup: a signing secret with no base URL reads like signing is on while
// nothing would ever be signed.
func TestLoad_r2SigningSecretWithoutBaseURL(t *testing.T) {
	setMinimalEnv(t)
	setR2Env(t)
	t.Setenv("KUKATKO_STORAGE_R2_MEDIA_BASE_URL", "")

	_, err := Load("")
	if !errors.Is(err, ErrIncompleteR2Config) {
		t.Fatalf("Load error = %v, want ErrIncompleteR2Config", err)
	}
	for _, key := range []string{"storage.r2.media_base_url", "storage.r2.url_signing_secret"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("Load error %q does not name %q", err, key)
		}
	}
	if strings.Contains(err.Error(), `"signing-secret"`) {
		t.Errorf("Load error leaks a credential value: %v", err)
	}
}

// TestLoad_fsBackendIgnoresR2 verifies the default backend never looks at the R2
// settings, so an existing deployment is unaffected by the new keys.
func TestLoad_fsBackendIgnoresR2(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_STORAGE_BACKEND", StorageBackendFS)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Storage.R2.Endpoint != "" {
		t.Errorf("storage.r2.endpoint = %q, want empty", cfg.Storage.R2.Endpoint)
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

// TestLoad_retiredImportSectionsAreIgnored pins what happens to a config file
// written before the PhotoPrism/photo-sorter migration was removed: the
// import.photoprism, import.photosorter and ratelimit.import blocks no longer
// map onto anything, and Load must ignore them rather than fail. Deployed
// config.yaml files in production still carry them, so a strict decode here
// would refuse to start the server on an untouched machine. Viper unmarshals
// without mapstructure's ErrorUnused, which is what makes this hold.
func TestLoad_retiredImportSectionsAreIgnored(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "database:\n  url: postgres://localhost/db\n" +
		"import:\n" +
		"  photoprism:\n    base_url: https://photos.example\n    token: secret-app-token\n    page_size: 1000\n" +
		"  photosorter:\n    dsn: postgres://localhost/sorter\n    base_url: https://sorter.example\n" +
		"    token: psat_secret\n    page_size: 500\n" +
		"ratelimit:\n  import:\n    rate_per_sec: 1\n    burst: 3\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load with retired import sections returned error: %v", err)
	}
	if cfg.Database.URL != "postgres://localhost/db" {
		t.Errorf("database.url = %q, want the value from the file", cfg.Database.URL)
	}
}

// TestLoad_retiredImportEnvIsIgnored is the same guarantee for the environment:
// a deployment unit still exporting KUKATKO_IMPORT_PHOTOPRISM_* /
// KUKATKO_IMPORT_PHOTOSORTER_* must start, not fail.
func TestLoad_retiredImportEnvIsIgnored(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_IMPORT_PHOTOPRISM_BASE_URL", "https://photos.example")
	t.Setenv("KUKATKO_IMPORT_PHOTOPRISM_TOKEN", "secret-app-token")
	t.Setenv("KUKATKO_IMPORT_PHOTOSORTER_DSN", "postgres://localhost/sorter")
	t.Setenv("KUKATKO_IMPORT_PHOTOSORTER_TOKEN", "psat_secret")
	if _, err := Load(""); err != nil {
		t.Fatalf("Load with retired import environment returned error: %v", err)
	}
}

// TestLoad_locationEstimateEnvOverride verifies the location_estimate keys can be
// supplied via the KUKATKO_ environment, including switching the feature off.
func TestLoad_locationEstimateEnvOverride(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("KUKATKO_LOCATION_ESTIMATE_ENABLED", "false")
	t.Setenv("KUKATKO_LOCATION_ESTIMATE_WINDOW", "90m")
	t.Setenv("KUKATKO_LOCATION_ESTIMATE_RADIUS_METERS", "1500.5")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LocationEstimate.Enabled {
		t.Errorf("location_estimate.enabled = true, want false")
	}
	if cfg.LocationEstimate.Window != 90*time.Minute {
		t.Errorf("location_estimate.window = %v, want 90m", cfg.LocationEstimate.Window)
	}
	if cfg.LocationEstimate.RadiusMeters != 1500.5 {
		t.Errorf("location_estimate.radius_meters = %v, want 1500.5", cfg.LocationEstimate.RadiusMeters)
	}
}

// TestLoad_locationEstimateValidation checks that an enabled estimator with a
// nonsensical window or radius fails startup rather than silently never
// producing an estimate, while a disabled one is left unchecked.
func TestLoad_locationEstimateValidation(t *testing.T) {
	tests := []struct {
		name    string
		enabled string
		window  string
		radius  string
		wantErr bool
	}{
		{name: "defaults are valid", enabled: "true", window: "6h", radius: "5000", wantErr: false},
		{name: "zero window rejected", enabled: "true", window: "0s", radius: "5000", wantErr: true},
		{name: "negative window rejected", enabled: "true", window: "-1h", radius: "5000", wantErr: true},
		{name: "zero radius rejected", enabled: "true", window: "6h", radius: "0", wantErr: true},
		{name: "negative radius rejected", enabled: "true", window: "6h", radius: "-5", wantErr: true},
		// A switched-off estimator never reads either value, so nonsense in them is
		// not a reason to refuse to boot.
		{name: "disabled ignores nonsense", enabled: "false", window: "0s", radius: "0", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv("KUKATKO_LOCATION_ESTIMATE_ENABLED", tt.enabled)
			t.Setenv("KUKATKO_LOCATION_ESTIMATE_WINDOW", tt.window)
			t.Setenv("KUKATKO_LOCATION_ESTIMATE_RADIUS_METERS", tt.radius)

			_, err := Load("")
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidLocationEstimate) {
					t.Fatalf("Load error = %v, want ErrInvalidLocationEstimate", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
		})
	}
}

// TestLoad_embeddingThresholdsMatchPackageDefaults pins the two image-embedding
// distance defaults to the package-level fallbacks that stand in for them when a
// config value is non-positive. The pair only makes sense together: they are the
// same threshold reached by two routes, and a model change has to move both (the
// derivation lives in docs/THRESHOLDS.md).
func TestLoad_embeddingThresholdsMatchPackageDefaults(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Expand.MaxDistance != expand.DefaultMaxDistance {
		t.Errorf("expand.max_distance = %v, want expand.DefaultMaxDistance (%v)",
			cfg.Expand.MaxDistance, expand.DefaultMaxDistance)
	}
	// The duplicate threshold has no package-level twin, but it must stay well
	// inside the expand one: "the same photo twice" is a strictly tighter claim
	// than "belongs in the same album", whatever the model's scale.
	if cfg.Duplicate.EmbeddingMaxDist >= cfg.Expand.MaxDistance {
		t.Errorf("duplicate.embedding_max_dist = %v, want below expand.max_distance (%v)",
			cfg.Duplicate.EmbeddingMaxDist, cfg.Expand.MaxDistance)
	}
}
