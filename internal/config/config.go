// Package config defines kukatko's typed configuration and loads it from a YAML
// file with environment-variable overrides.
//
// Configuration is layered: built-in defaults < YAML file < environment
// variables (env always wins). Environment variables use the KUKATKO_ prefix
// and map onto nested keys by replacing dots with underscores, for example
// KUKATKO_DATABASE_URL -> database.url and KUKATKO_WEB_PORT -> web.port. The
// mapy.com API key is the one exception: it is read from the unprefixed
// MAPY_API_KEY so it can be shared with other tooling.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	// envPrefix is the prefix for environment variables that override config
	// keys, for example KUKATKO_WEB_PORT overrides web.port.
	envPrefix = "KUKATKO"
	// envConfigPath is the environment variable that points at the YAML config
	// file when no explicit path is given.
	envConfigPath = "KUKATKO_CONFIG"
	// defaultConfigPath is the YAML file consulted when neither an explicit path
	// nor envConfigPath is provided. A missing file is not an error.
	defaultConfigPath = "config.yaml"
	// maxPort is the highest valid TCP port number.
	maxPort = 65535
)

// Sentinel validation errors returned by Validate so callers can match them
// with errors.Is.
var (
	// ErrMissingDatabaseURL indicates database.url was left empty.
	ErrMissingDatabaseURL = errors.New("config: database.url is required (set KUKATKO_DATABASE_URL)")
	// ErrInvalidWebPort indicates web.port is outside the valid TCP range.
	ErrInvalidWebPort = errors.New("config: web.port must be between 1 and 65535")
	// ErrInvalidPoolSize indicates the connection-pool sizes are inconsistent.
	ErrInvalidPoolSize = errors.New("config: invalid database connection-pool sizing")
	// ErrInvalidEmbeddingDim indicates an embedding dimension is not positive.
	ErrInvalidEmbeddingDim = errors.New("config: embedding image_dim and face_dim must be positive")
	// ErrInvalidSessionLifetime indicates the session TTL or max-lifetime is
	// non-positive, or the max lifetime is shorter than the sliding TTL.
	ErrInvalidSessionLifetime = errors.New(
		"config: auth.session_ttl and auth.session_max_lifetime must be positive with max >= ttl")
	// ErrInvalidLoginRateLimit indicates the login rate-limit attempt count or
	// window is non-positive.
	ErrInvalidLoginRateLimit = errors.New(
		"config: auth.login_rate_limit and auth.login_rate_window must be positive")
)

// Config is the fully resolved, typed configuration for a kukatko process.
type Config struct {
	Database  DatabaseConfig  `mapstructure:"database"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Web       WebConfig       `mapstructure:"web"`
	Embedding EmbeddingConfig `mapstructure:"embedding"`
	Faces     FacesConfig     `mapstructure:"faces"`
	Cluster   ClusterConfig   `mapstructure:"cluster"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Maps      MapsConfig      `mapstructure:"maps"`
	Backup    BackupConfig    `mapstructure:"backup"`
	Trash     TrashConfig     `mapstructure:"trash"`
	Duplicate DuplicateConfig `mapstructure:"duplicate"`
	Upload    UploadConfig    `mapstructure:"upload"`
	Worker    WorkerConfig    `mapstructure:"worker"`
	Bulk      BulkConfig      `mapstructure:"bulk"`
	Import    ImportConfig    `mapstructure:"import"`
}

// ImportConfig groups the read-only import sources. PhotoPrism stays primary
// during the migration; its import is incremental and repeatable.
type ImportConfig struct {
	PhotoPrism PhotoPrismConfig `mapstructure:"photoprism"`
}

// PhotoPrismConfig holds the connection details for the read-only PhotoPrism API
// client (internal/photoprism). The token is a long-lived app password / access
// token and should come from the environment
// (KUKATKO_IMPORT_PHOTOPRISM_TOKEN), not a committed file.
type PhotoPrismConfig struct {
	// BaseURL is the root of the PhotoPrism instance (empty disables the import).
	BaseURL string `mapstructure:"base_url"`
	// Token is the Bearer app password / access token used for every request.
	Token string `mapstructure:"token"`
}

// DatabaseConfig holds the PostgreSQL connection string and pool sizing.
type DatabaseConfig struct {
	URL          string `mapstructure:"url"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// StorageConfig holds on-disk locations for original media and derived caches.
type StorageConfig struct {
	OriginalsPath string `mapstructure:"originals_path"`
	CachePath     string `mapstructure:"cache_path"`
}

// WebConfig holds the HTTP listener and session/CORS settings.
type WebConfig struct {
	Host           string   `mapstructure:"host"`
	Port           int      `mapstructure:"port"`
	SessionSecret  string   `mapstructure:"session_secret"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	// SecureCookies marks session cookies as Secure (HTTPS-only). Leave false
	// for plain-HTTP local development; enable behind a TLS-terminating proxy.
	SecureCookies bool `mapstructure:"secure_cookies"`
}

// EmbeddingConfig points at the external embedding service and records the
// vector dimensions it produces.
type EmbeddingConfig struct {
	URL      string `mapstructure:"url"`
	ImageDim int    `mapstructure:"image_dim"`
	FaceDim  int    `mapstructure:"face_dim"`
}

// FacesConfig tunes the face_detect job and the face↔marker matching and
// suggestion logic.
type FacesConfig struct {
	// MinDetScore is the minimum detector confidence (det_score) a detected face
	// must have to be stored; lower-confidence detections are dropped. The sidecar
	// applies its own detection threshold, so this is a second, configurable floor.
	// A non-positive value disables the filter (stores every detection).
	MinDetScore float64 `mapstructure:"min_det_score"`
	// IoUThreshold is the minimum Intersection-over-Union a detected face's box must
	// share with an existing marker for the two to be considered the same region.
	// Mirrors photo-sorter's 0.1 default.
	IoUThreshold float64 `mapstructure:"iou_threshold"`
	// SuggestionLimit caps how many likely subjects are suggested for an unnamed
	// face.
	SuggestionLimit int `mapstructure:"suggestion_limit"`
	// SuggestionMaxDistance is the cosine-distance cutoff for the primary suggestion
	// search; neighbouring faces farther than this are ignored before the threshold
	// fallback widens the search. A non-positive value disables the primary cutoff.
	SuggestionMaxDistance float64 `mapstructure:"suggestion_max_distance"`
	// MinFaceSize is the minimum normalised width (0..1) a neighbouring face must
	// have to contribute a suggestion, so tiny background faces do not drive
	// identity guesses. A non-positive value disables the size filter.
	MinFaceSize float64 `mapstructure:"min_face_size"`
}

// ClusterConfig tunes face auto-clustering, which groups unassigned faces of the
// same person so a whole group can be named at once.
type ClusterConfig struct {
	// Threshold is the maximum cosine distance between two faces for them to be
	// linked as the same person during clustering. Lower is stricter (fewer, purer
	// clusters); higher is looser. A non-positive value falls back to the default.
	Threshold float64 `mapstructure:"threshold"`
	// MinSize is the smallest connected component that becomes a cluster; smaller
	// groups stay unclustered so single stray faces are not surfaced. A
	// non-positive value falls back to the default.
	MinSize int `mapstructure:"min_size"`
	// SuggestionMaxDistance is the cosine-distance cutoff for suggesting an existing
	// subject for a cluster: named neighbours farther than this from the cluster's
	// centroid produce no suggestion. A non-positive value falls back to the default.
	SuggestionMaxDistance float64 `mapstructure:"suggestion_max_distance"`
}

// AuthConfig holds the credentials used to bootstrap the initial admin account
// plus the session and login rate-limiting policy.
type AuthConfig struct {
	BootstrapAdminUsername string `mapstructure:"bootstrap_admin_username"`
	BootstrapAdminPassword string `mapstructure:"bootstrap_admin_password"`
	// SessionTTL is the sliding idle window: each authenticated request extends a
	// session's expiry to now+SessionTTL, so an active user never gets logged out.
	SessionTTL time.Duration `mapstructure:"session_ttl"`
	// SessionMaxLifetime caps the absolute age of a session regardless of
	// activity; sliding extension never pushes expiry beyond created_at+this.
	SessionMaxLifetime time.Duration `mapstructure:"session_max_lifetime"`
	// LoginRateLimit is the maximum number of failed login attempts allowed per
	// username+IP within LoginRateWindow before further attempts return 429.
	LoginRateLimit int `mapstructure:"login_rate_limit"`
	// LoginRateWindow is the trailing window over which LoginRateLimit applies.
	LoginRateWindow time.Duration `mapstructure:"login_rate_window"`
}

// MapsConfig holds the server-side mapy.com API key (kept off the client) and
// the base URL of the mapy.com REST API the tile and reverse-geocode proxies
// call.
type MapsConfig struct {
	MapyAPIKey string `mapstructure:"mapy_api_key"`
	// BaseURL is the root of the mapy.com REST API; it is overridable mainly so
	// tests can point the proxy at a fake server.
	BaseURL string `mapstructure:"base_url"`
}

// BackupConfig holds the S3 destination and schedule for in-process backups.
type BackupConfig struct {
	S3       S3Config `mapstructure:"s3"`
	Schedule string   `mapstructure:"schedule"`
}

// S3Config holds connection details for an S3-compatible backup endpoint.
type S3Config struct {
	Endpoint  string `mapstructure:"endpoint"`
	Region    string `mapstructure:"region"`
	Bucket    string `mapstructure:"bucket"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	PathStyle bool   `mapstructure:"path_style"`
}

// TrashConfig holds the soft-delete retention policy.
type TrashConfig struct {
	RetentionDays int `mapstructure:"retention_days"`
}

// DuplicateConfig holds thresholds for duplicate detection.
type DuplicateConfig struct {
	Enabled          bool    `mapstructure:"enabled"`
	PhashMaxDiff     int     `mapstructure:"phash_max_diff"`
	EmbeddingMaxDist float64 `mapstructure:"embedding_max_dist"`
}

// UploadConfig holds limits for the upload/ingest endpoint.
type UploadConfig struct {
	// MaxFileSizeMB caps a single uploaded file in mebibytes. 0 disables the cap
	// (uploads are streamed and never buffered whole in memory regardless).
	MaxFileSizeMB int `mapstructure:"max_file_size_mb"`
}

// WorkerConfig tunes the in-process background worker that drains the job queue.
type WorkerConfig struct {
	// Count is the number of jobs processed in parallel. <= 0 uses the worker's
	// built-in default.
	Count int `mapstructure:"count"`
	// PollInterval is how long an idle worker waits before polling the queue
	// again when it is empty.
	PollInterval time.Duration `mapstructure:"poll_interval"`
	// StaleAfter is the lock age past which a running job is presumed abandoned
	// (its worker died) and recovered for retry.
	StaleAfter time.Duration `mapstructure:"stale_after"`
	// StaleScanInterval is how often stale-lock recovery runs.
	StaleScanInterval time.Duration `mapstructure:"stale_scan_interval"`
}

// BulkConfig limits the bulk metadata editing endpoint.
type BulkConfig struct {
	// MaxBatchSize caps how many photo UIDs one bulk request may target. A
	// request exceeding it is rejected with a clear error before any change. A
	// non-positive value falls back to the bulk package's built-in default.
	MaxBatchSize int `mapstructure:"max_batch_size"`
}

// MaxFileSizeBytes returns the per-file upload cap in bytes, or 0 for no cap.
func (u UploadConfig) MaxFileSizeBytes() int64 {
	if u.MaxFileSizeMB <= 0 {
		return 0
	}
	return int64(u.MaxFileSizeMB) * 1024 * 1024
}

// Load reads configuration from the resolved YAML file (if present) overlaid
// with KUKATKO_-prefixed environment variables, validates it, and returns the
// typed result. configPath, when non-empty, takes precedence over the
// KUKATKO_CONFIG environment variable and the default config.yaml. A missing
// config file is not an error: defaults plus environment variables are used.
func Load(configPath string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigType("yaml")
	v.SetConfigFile(resolveConfigPath(configPath))

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	// MAPY_API_KEY is shared with other tooling and lives outside the KUKATKO_
	// prefix, so it must be bound explicitly.
	if err := v.BindEnv("maps.mapy_api_key", "MAPY_API_KEY"); err != nil {
		return nil, fmt.Errorf("config: binding MAPY_API_KEY: %w", err)
	}

	if err := readConfigFile(v); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: decoding configuration: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// readConfigFile loads the configured YAML file into v, treating a missing file
// as a non-error (env-only operation is supported); any other read or parse
// failure is returned wrapped.
func readConfigFile(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: reading %s: %w", v.ConfigFileUsed(), err)
	}
	return nil
}

// resolveConfigPath selects the YAML file path: an explicit configPath wins,
// then the KUKATKO_CONFIG environment variable, then defaultConfigPath.
func resolveConfigPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	if envPath := os.Getenv(envConfigPath); envPath != "" {
		return envPath
	}
	return defaultConfigPath
}

// setDefaults registers the built-in default for every configuration key. Every
// key gets a default so that AutomaticEnv overrides are picked up on Unmarshal
// even when no YAML file is present.
func setDefaults(v *viper.Viper) {
	v.SetDefault("database.url", "")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)

	v.SetDefault("storage.originals_path", "/var/lib/kukatko/originals")
	v.SetDefault("storage.cache_path", "/var/lib/kukatko/cache")

	v.SetDefault("web.host", "0.0.0.0")
	v.SetDefault("web.port", 8080)
	v.SetDefault("web.session_secret", "")
	v.SetDefault("web.allowed_origins", []string{})
	v.SetDefault("web.secure_cookies", false)

	v.SetDefault("embedding.url", "http://localhost:8000")
	v.SetDefault("embedding.image_dim", 768)
	v.SetDefault("embedding.face_dim", 512)

	v.SetDefault("faces.min_det_score", 0.5)
	v.SetDefault("faces.iou_threshold", 0.1)
	v.SetDefault("faces.suggestion_limit", 5)
	v.SetDefault("faces.suggestion_max_distance", 0.5)
	v.SetDefault("faces.min_face_size", 0.02)

	v.SetDefault("cluster.threshold", 0.4)
	v.SetDefault("cluster.min_size", 2)
	v.SetDefault("cluster.suggestion_max_distance", 0.5)

	v.SetDefault("auth.bootstrap_admin_username", "")
	v.SetDefault("auth.bootstrap_admin_password", "")
	v.SetDefault("auth.session_ttl", "168h")          // 7-day sliding idle window
	v.SetDefault("auth.session_max_lifetime", "720h") // 30-day absolute cap
	v.SetDefault("auth.login_rate_limit", 10)
	v.SetDefault("auth.login_rate_window", "15m")

	v.SetDefault("maps.mapy_api_key", "")
	v.SetDefault("maps.base_url", "https://api.mapy.com")

	setOpsDefaults(v)
}

// setOpsDefaults registers defaults for the backup, trash, duplicate, upload and
// worker subsystems. It is split out of setDefaults to keep each function focused
// and within the length budget.
func setOpsDefaults(v *viper.Viper) {
	v.SetDefault("backup.s3.endpoint", "")
	v.SetDefault("backup.s3.region", "")
	v.SetDefault("backup.s3.bucket", "")
	v.SetDefault("backup.s3.access_key", "")
	v.SetDefault("backup.s3.secret_key", "")
	v.SetDefault("backup.s3.path_style", false)
	v.SetDefault("backup.schedule", "")

	v.SetDefault("trash.retention_days", 30)

	v.SetDefault("duplicate.enabled", true)
	v.SetDefault("duplicate.phash_max_diff", 8)
	v.SetDefault("duplicate.embedding_max_dist", 0.05)

	v.SetDefault("upload.max_file_size_mb", 0) // 0 = unlimited

	v.SetDefault("worker.count", 2)
	v.SetDefault("worker.poll_interval", "2s")
	v.SetDefault("worker.stale_after", "5m")
	v.SetDefault("worker.stale_scan_interval", "1m")

	v.SetDefault("bulk.max_batch_size", 1000)

	v.SetDefault("import.photoprism.base_url", "")
	v.SetDefault("import.photoprism.token", "")
}

// Validate checks that required fields are present and inter-field invariants
// hold, returning one of the package's sentinel errors (matchable with
// errors.Is) wrapped with offending values where useful.
func (c *Config) Validate() error {
	if c.Database.URL == "" {
		return ErrMissingDatabaseURL
	}
	if c.Web.Port < 1 || c.Web.Port > maxPort {
		return fmt.Errorf("%w: got %d", ErrInvalidWebPort, c.Web.Port)
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("%w: max_idle_conns (%d) exceeds max_open_conns (%d)",
			ErrInvalidPoolSize, c.Database.MaxIdleConns, c.Database.MaxOpenConns)
	}
	if c.Embedding.ImageDim < 1 || c.Embedding.FaceDim < 1 {
		return ErrInvalidEmbeddingDim
	}
	return c.Auth.validate()
}

// validate checks the auth session and rate-limit invariants, returning one of
// the package's sentinel errors wrapped with the offending values.
func (a *AuthConfig) validate() error {
	if a.SessionTTL <= 0 || a.SessionMaxLifetime <= 0 || a.SessionMaxLifetime < a.SessionTTL {
		return fmt.Errorf("%w: ttl=%s max=%s", ErrInvalidSessionLifetime, a.SessionTTL, a.SessionMaxLifetime)
	}
	if a.LoginRateLimit < 1 || a.LoginRateWindow <= 0 {
		return fmt.Errorf("%w: limit=%d window=%s", ErrInvalidLoginRateLimit, a.LoginRateLimit, a.LoginRateWindow)
	}
	return nil
}
