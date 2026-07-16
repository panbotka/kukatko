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
	"net"
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
	// ErrInvalidWake indicates the Wake-on-LAN auto-wake settings are enabled but
	// inconsistent (missing/invalid MAC, or no destination to send the packet to).
	ErrInvalidWake = errors.New("config: invalid embedding.wake settings")
	// ErrInvalidThumbEngine indicates thumb.engine is set to an unknown value (it
	// must be empty, "go" or "vips").
	ErrInvalidThumbEngine = errors.New(`config: thumb.engine must be "go" or "vips"`)
	// ErrInvalidStorageBackend indicates storage.backend is set to an unknown value
	// (it must be empty, "fs" or "r2").
	ErrInvalidStorageBackend = errors.New(`config: storage.backend must be "fs" or "r2"`)
	// ErrIncompleteR2Config indicates storage.backend is "r2" but a required
	// storage.r2.* key is missing or storage.r2.url_ttl is non-positive. The error
	// names the offending keys and never their values.
	ErrIncompleteR2Config = errors.New("config: storage.backend is \"r2\" but its configuration is incomplete")
)

// Config is the fully resolved, typed configuration for a kukatko process.
type Config struct {
	Database   DatabaseConfig   `mapstructure:"database"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Thumb      ThumbConfig      `mapstructure:"thumb"`
	Web        WebConfig        `mapstructure:"web"`
	Embedding  EmbeddingConfig  `mapstructure:"embedding"`
	Faces      FacesConfig      `mapstructure:"faces"`
	Cluster    ClusterConfig    `mapstructure:"cluster"`
	Candidates CandidatesConfig `mapstructure:"candidates"`
	Sweep      SweepConfig      `mapstructure:"sweep"`
	Expand     ExpandConfig     `mapstructure:"expand"`
	Review     ReviewConfig     `mapstructure:"review"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Maps       MapsConfig       `mapstructure:"maps"`
	Backup     BackupConfig     `mapstructure:"backup"`
	Trash      TrashConfig      `mapstructure:"trash"`
	Duplicate  DuplicateConfig  `mapstructure:"duplicate"`
	Stacks     StacksConfig     `mapstructure:"stacks"`
	Upload     UploadConfig     `mapstructure:"upload"`
	Video      VideoConfig      `mapstructure:"video"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Bulk       BulkConfig       `mapstructure:"bulk"`
	Import     ImportConfig     `mapstructure:"import"`
	Log        LogConfig        `mapstructure:"log"`
	Metrics    MetricsConfig    `mapstructure:"metrics"`
	RateLimit  RateLimitConfig  `mapstructure:"ratelimit"`
}

// RateLimitConfig configures per-client-IP rate limiting on resource-intensive
// endpoints. Each rule is an independent token bucket; a rule with a
// non-positive RatePerSec disables limiting for that endpoint.
type RateLimitConfig struct {
	// Upload caps POST /upload (multipart ingest).
	Upload RateLimitRule `mapstructure:"upload"`
	// Bulk caps POST /photos/bulk (batch metadata edits).
	Bulk RateLimitRule `mapstructure:"bulk"`
	// Import caps the POST /import/* migration triggers.
	Import RateLimitRule `mapstructure:"import"`
	// Tiles caps GET /map/tiles/... (the mapy.com tile proxy). The geocode
	// proxy has its own credit-protecting limiter under maps.*.
	Tiles RateLimitRule `mapstructure:"tiles"`
}

// RateLimitRule is one per-client-IP token bucket: RatePerSec tokens replenish
// per second up to Burst tokens. A non-positive RatePerSec disables the rule so
// the endpoint is never throttled.
type RateLimitRule struct {
	RatePerSec float64 `mapstructure:"rate_per_sec"`
	Burst      int     `mapstructure:"burst"`
}

// LogConfig configures structured logging.
type LogConfig struct {
	// Level is the minimum slog level emitted: debug, info, warn, or error. An
	// empty value defaults to info; an invalid value fails startup.
	Level string `mapstructure:"level"`
}

// MetricsConfig configures the Prometheus metrics endpoint.
type MetricsConfig struct {
	// Enabled mounts GET /metrics and installs the request-metrics middleware
	// when true. /metrics is unauthenticated, so restrict it at the network
	// layer (bind/firewall) when exposing the server publicly.
	Enabled bool `mapstructure:"enabled"`
}

// ImportConfig groups the read-only import sources. PhotoPrism stays primary
// during the migration; its import is incremental and repeatable. PhotoSorter is
// the one-off (optionally repeatable) direct database migration from photo-sorter.
type ImportConfig struct {
	PhotoPrism  PhotoPrismConfig  `mapstructure:"photoprism"`
	PhotoSorter PhotoSorterConfig `mapstructure:"photosorter"`
}

// PhotoSorterConfig holds the read-only connection to the photo-sorter database
// for the one-off migration (internal/psimport). The DSN should come from the
// environment (KUKATKO_IMPORT_PHOTOSORTER_DSN), not a committed file. An empty
// DSN disables the migration command and its admin trigger.
type PhotoSorterConfig struct {
	// DSN is the read-only PostgreSQL connection string for the photo-sorter
	// database (empty disables the migration).
	DSN string `mapstructure:"dsn"`
	// PageSize is the photo-listing page size; a non-positive value defaults to
	// psimport.DefaultPageSize.
	PageSize int `mapstructure:"page_size"`
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
	// PageSize is the photo listing page size; the client clamps it to
	// PhotoPrism's cap (1000) and a non-positive value defaults to 1000.
	PageSize int `mapstructure:"page_size"`
}

// DatabaseConfig holds the PostgreSQL connection string and pool sizing.
type DatabaseConfig struct {
	URL          string `mapstructure:"url"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// Storage backend names accepted by StorageConfig.Backend.
const (
	// StorageBackendFS is the default backend: originals live on a local disk
	// under storage.originals_path.
	StorageBackendFS = "fs"
	// StorageBackendR2 stores originals in a private Cloudflare R2 bucket, which
	// is what lets Kukátko run on a VPS whose disk cannot hold the library.
	StorageBackendR2 = "r2"
)

// StorageConfig selects the originals backend and holds the on-disk locations
// both backends still need: the derived-artifact cache and the temp directory
// that staged uploads and materialized downloads pass through.
type StorageConfig struct {
	// Backend is "fs" (default) or "r2". An unknown value fails startup.
	Backend string `mapstructure:"backend"`
	// OriginalsPath is the local originals root, used only by the "fs" backend.
	OriginalsPath string `mapstructure:"originals_path"`
	// CachePath holds derived artifacts (thumbnails, video posters, …).
	CachePath string `mapstructure:"cache_path"`
	// TempPath is where the "r2" backend stages uploads and materializes objects
	// for the tools that need a real file. It must fit the largest single file.
	TempPath string `mapstructure:"temp_path"`
	// R2 configures the "r2" backend and is ignored by the "fs" backend.
	R2 R2Config `mapstructure:"r2"`
}

// R2Config holds the Cloudflare R2 bucket, its S3 credentials, and the settings
// for the signed URLs that let a browser fetch a private object from the edge
// Worker. SecretKey and the signing secrets are credentials: they are never
// logged, and validation reports only the names of missing keys.
type R2Config struct {
	// Endpoint is the S3 API host, for R2 <accountid>.r2.cloudflarestorage.com.
	Endpoint string `mapstructure:"endpoint"`
	// Region is the bucket region; R2 expects "auto".
	Region string `mapstructure:"region"`
	// Bucket holds originals and thumbnails.
	Bucket string `mapstructure:"bucket"`
	// AccessKey and SecretKey are the S3 credentials of an R2 API token.
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	// MediaBaseURL is the domain the edge Worker serves objects on.
	MediaBaseURL string `mapstructure:"media_base_url"`
	// URLSigningSecret signs media URLs and is shared with the Worker.
	// URLSigningSecretPrevious is additionally accepted on verification, so the
	// secret can be rotated without a window of broken URLs.
	URLSigningSecret         string `mapstructure:"url_signing_secret"`
	URLSigningSecretPrevious string `mapstructure:"url_signing_secret_previous"`
	// URLTTL is how long a signed URL stays valid.
	URLTTL time.Duration `mapstructure:"url_ttl"`
}

// Thumbnail engine names accepted by ThumbConfig.Engine.
const (
	// ThumbEngineGo is the default pure-Go, CGO-free thumbnailer.
	ThumbEngineGo = "go"
	// ThumbEngineVips shells out to the vipsthumbnail binary for JPEG/PNG/WebP
	// originals (faster and far lower-memory on large images), falling back to the
	// pure-Go engine for any other source. It keeps the binary CGO-free.
	ThumbEngineVips = "vips"
)

// ThumbConfig tunes thumbnail generation, the most CPU/memory-intensive
// per-photo work on the Pi. The defaults keep the pure-Go engine; vips is an
// opt-in acceleration for hosts with the libvips CLI installed.
type ThumbConfig struct {
	// Engine selects the rendering engine: "go" (default, pure-Go decode+resize,
	// CGO-free) or "vips" (shell out to vipsthumbnail for JPEG/PNG/WebP, which is
	// markedly faster and uses a fraction of the memory on large images). An empty
	// value means "go". The vips engine falls back to the pure-Go engine per photo
	// for any source vipsthumbnail cannot read (HEIC/RAW/video go through the
	// existing imgconvert pre-decode) or if a vips invocation fails, so it never
	// changes output behaviour — only speed.
	Engine string `mapstructure:"engine"`
	// VipsBinary overrides the vipsthumbnail executable resolved on PATH (default
	// "vipsthumbnail"). Only consulted when Engine is "vips".
	VipsBinary string `mapstructure:"vips_binary"`
	// Concurrency bounds the number of sizes encoded in parallel for a single
	// photo. A non-positive value uses GOMAXPROCS. Lower it on memory-constrained
	// hosts to cap peak thumbnail memory.
	Concurrency int `mapstructure:"concurrency"`
}

// VipsEnabled reports whether the vips engine is requested.
func (t ThumbConfig) VipsEnabled() bool {
	return t.Engine == ThumbEngineVips
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
	URL      string     `mapstructure:"url"`
	ImageDim int        `mapstructure:"image_dim"`
	FaceDim  int        `mapstructure:"face_dim"`
	Wake     WakeConfig `mapstructure:"wake"`
}

// WakeConfig optionally wakes the embeddings box via Wake-on-LAN when embedding
// jobs are waiting and the sidecar is offline. It is OFF by default; manual
// power-on of the box remains fine. Wake-on-LAN does not traverse Tailscale (an
// L3 overlay with no L2 broadcast), so the kukatko host must share the physical
// LAN with the box and send the magic packet locally.
type WakeConfig struct {
	// Enabled turns on auto-wake. When false the feature is fully inert: no
	// queue polling, no health checks, and no packets are ever sent.
	Enabled bool `mapstructure:"enabled"`
	// MAC is the box's network-card hardware address (the magic-packet target),
	// for example "aa:bb:cc:dd:ee:ff". Required when Enabled.
	MAC string `mapstructure:"mac"`
	// BroadcastAddr is the UDP broadcast destination for the magic packet, for
	// example "192.168.1.255:9". Used when Interface is empty.
	BroadcastAddr string `mapstructure:"broadcast_addr"`
	// Interface is the local NIC name to emit a raw Ethernet magic frame on (for
	// example "eth0"); it requires CAP_NET_RAW. Empty selects the UDP broadcast
	// path via BroadcastAddr instead.
	Interface string `mapstructure:"interface"`
	// MinQueue is the minimum number of pending image_embed/face_detect jobs
	// before a wake is attempted. A non-positive value defaults to 1.
	MinQueue int `mapstructure:"min_queue"`
	// Cooldown is the minimum delay between successive magic packets so a sleeping
	// box is not spammed. A non-positive value falls back to the wake package's
	// built-in default.
	Cooldown time.Duration `mapstructure:"cooldown"`
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

// CandidatesConfig tunes the "find a person among untagged photos" search: for a
// named subject it runs a kNN over unassigned faces from each of the subject's
// exemplars and surfaces the untagged faces that resemble them.
type CandidatesConfig struct {
	// MaxDistance is the default maximum cosine distance a candidate face may sit
	// from an exemplar to count. It is the fallback used when a request omits its
	// own threshold, and the baseline the vote rule scales against. A non-positive
	// value falls back to the default.
	MaxDistance float64 `mapstructure:"max_distance"`
	// SearchLimit caps how many nearest unassigned faces each exemplar's kNN returns
	// before voting merges them, bounding the per-exemplar fan-out. A non-positive
	// value falls back to the default.
	SearchLimit int `mapstructure:"search_limit"`
	// MinFacePx is the minimum face width in display pixels a candidate must have to
	// be reviewable; tiny faces in a crowd cannot be judged. It complements the
	// relative floor reused from faces.min_face_size. A non-positive value disables
	// the absolute-pixel floor.
	MinFacePx int `mapstructure:"min_face_px"`
	// Concurrency bounds how many exemplar kNN searches run at once, so searching a
	// person with hundreds of photos does not fan out unboundedly. A non-positive
	// value falls back to the default.
	Concurrency int `mapstructure:"concurrency"`
}

// SweepConfig tunes the recognition sweep — the "scan every named person for
// confident matches among unnamed faces" work list, which composes the per-subject
// candidate search across all subjects at once. It never auto-assigns: confidence
// only narrows the list, every write still needs a human.
type SweepConfig struct {
	// Concurrency bounds how many subjects are scanned at once. It stacks on top of
	// candidates.concurrency (per-subject exemplar searches), so it is kept small on a
	// RAM-constrained box. A non-positive value falls back to the default.
	Concurrency int `mapstructure:"concurrency"`
	// MaxSubjects caps how many subjects a single sweep scans. When more subjects have
	// faces than this, the sweep scans the first MaxSubjects (by name) and marks the
	// result capped rather than silently truncating. A non-positive value falls back to
	// the default.
	MaxSubjects int `mapstructure:"max_subjects"`
}

// ExpandConfig tunes the "expand a collection" search: for an album or a label it
// votes each member photo's CLIP-embedding neighbours together and surfaces the
// photos several members agree on that are not in the collection yet, so a
// half-tagged library can be finished.
type ExpandConfig struct {
	// MaxDistance is the default maximum cosine distance a candidate may sit from a
	// source photo (the UI shows this as 1 - distance, so 0.30 reads as 70 %
	// similarity). It is the fallback when a request omits its own threshold, and the
	// baseline the vote rule scales against. A non-positive value falls back to the
	// default.
	MaxDistance float64 `mapstructure:"max_distance"`
	// Limit is the default number of candidates returned when a request omits its own
	// limit. A non-positive value falls back to the default.
	Limit int `mapstructure:"limit"`
	// MaxLimit caps a request's own limit so one call cannot ask for an unbounded
	// neighbourhood. A non-positive value falls back to the default.
	MaxLimit int `mapstructure:"max_limit"`
	// SearchLimit is how many nearest photos each source photo's kNN returns before
	// voting merges them, over-fetching so the later filters do not starve. A
	// non-positive value falls back to the default.
	SearchLimit int `mapstructure:"search_limit"`
	// SourceCap bounds how many member photos are used as query vectors, so a
	// thousands-strong album is sampled (and the truncation reported) rather than
	// queried in full. A non-positive value falls back to the default.
	SourceCap int `mapstructure:"source_cap"`
	// Concurrency bounds how many per-source kNN searches run at once. A non-positive
	// value falls back to the default.
	Concurrency int `mapstructure:"concurrency"`
}

// ReviewConfig tunes the review game: one question at a time over candidates
// the system is genuinely unsure about. Confidence is 1 - cosine distance;
// only candidates inside [BandMin, BandMax) become questions — below the band
// the guess is noise, at or above BandMax the /recognition and /expand pages
// confirm in bulk instead.
type ReviewConfig struct {
	// BandMin is the inclusive lower confidence bound of the uncertainty band.
	// Out-of-range values fall back to the default pair.
	BandMin float64 `mapstructure:"band_min"`
	// BandMax is the exclusive upper confidence bound of the uncertainty band.
	// Out-of-range values fall back to the default pair.
	BandMax float64 `mapstructure:"band_max"`
	// QueueSize is the default number of questions per queue batch, sized so the
	// UI can prefetch. A non-positive value falls back to the default.
	QueueSize int `mapstructure:"queue_size"`
	// CacheTTL is how long a built queue is served from the per-user cache
	// before the expensive candidate searches run again. A non-positive value
	// falls back to the default.
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
	// MaxLabels caps how many labels one queue rebuild scans. A non-positive
	// value falls back to the default.
	MaxLabels int `mapstructure:"max_labels"`
	// LabelConcurrency bounds concurrent label-similarity searches during a
	// rebuild (each already fans out internally; the box is RAM-constrained). A
	// non-positive value falls back to the default.
	LabelConcurrency int `mapstructure:"label_concurrency"`
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
	// UserAgent is the exact User-Agent the mapy.com client sends upstream. A
	// mapy.com key can be restricted to one exact User-Agent, so this value is a
	// credential in its own right (never log it, never commit it). Empty (the
	// default) means no explicit header is sent.
	UserAgent string `mapstructure:"user_agent"`
	// BaseURL is the root of the mapy.com REST API; it is overridable mainly so
	// tests can point the proxy at a fake server.
	BaseURL string `mapstructure:"base_url"`
	// GeocodeRatePerSec caps how many reverse-geocode calls per second the
	// background `places` job sends to mapy.com, protecting the monthly credit
	// budget. <= 0 disables the throttle.
	GeocodeRatePerSec float64 `mapstructure:"geocode_rate_per_sec"`
	// GeocodeBurst is the `places` job geocode limiter's bucket size (how many
	// calls may burst before the per-second rate applies).
	GeocodeBurst int `mapstructure:"geocode_burst"`
	// TileCacheBytes is the memory budget of the server-side tile cache. Every hit
	// is one mapy.com credit not spent (the free tier bills one credit per tile),
	// so a re-visited area costs nothing. <= 0 disables the cache.
	TileCacheBytes int64 `mapstructure:"tile_cache_bytes"`
	// TileCacheTTL is how long a proxied tile stays in the server-side cache.
	// <= 0 disables the cache.
	TileCacheTTL time.Duration `mapstructure:"tile_cache_ttl"`
}

// BackupConfig holds the S3 destination, schedule and retention for in-process
// backups.
type BackupConfig struct {
	S3       S3Config `mapstructure:"s3"`
	Schedule string   `mapstructure:"schedule"`
	// Retention is how many of the most recent database dumps are kept in the
	// bucket; older dumps are pruned after a successful backup. A value <= 0
	// disables pruning (every dump is kept). Originals are never pruned.
	Retention int `mapstructure:"retention"`
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

// StacksConfig controls the stacking feature — grouping the several files of one
// shot (RAW+JPEG, exported edits, …) into one library item. Enabled is the master
// switch for the whole feature (auto-detection and manual stacking); Rules
// selects which automatic detection rules the backfill runs.
type StacksConfig struct {
	Enabled bool             `mapstructure:"enabled"`
	Rules   StackRulesConfig `mapstructure:"rules"`
}

// StackRulesConfig switches each stack-detection rule independently, because the
// rules have very different false-positive rates.
type StackRulesConfig struct {
	// BaseName groups files that share a base filename but differ in extension
	// (IMG_1234.CR2 + IMG_1234.jpg). The safest rule; on by default.
	BaseName bool `mapstructure:"base_name"`
	// SequentialCopy groups copy/edit derivatives onto their original by canonical
	// name (IMG_1234 (2).jpg, IMG_1234 copy.jpg, IMG_1234-edited.jpg); on by default.
	SequentialCopy bool `mapstructure:"sequential_copy"`
	// UniqueID groups files carrying the same EXIF ImageUniqueID / XMP InstanceID;
	// very reliable where present, on by default.
	UniqueID bool `mapstructure:"unique_id"`
	// TimeGPS groups files captured in the same second at the same GPS point. It is
	// the loosest rule and will wrongly stack burst shots taken in one second, so
	// it is OFF by default.
	TimeGPS bool `mapstructure:"time_gps"`
}

// VideoConfig tunes video playback/streaming. Videos are always served with
// HTTP range support so browsers can seek without downloading the whole file.
type VideoConfig struct {
	// Transcode enables on-the-fly transcoding of non-web-friendly codecs (for
	// example HEVC/H.265) to H.264/MP4 via ffmpeg so they play in the browser.
	// It is OFF by default: transcoding is CPU-intensive and produced on every
	// playback (no caching), and the transcoded stream cannot be seeked
	// precisely. When off, a non-web-friendly video is streamed as-is and the
	// client falls back to a download link when the browser cannot decode it.
	Transcode bool `mapstructure:"transcode"`
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

	setStorageDefaults(v)
	setThumbDefaults(v)

	v.SetDefault("web.host", "0.0.0.0")
	v.SetDefault("web.port", 8080)
	v.SetDefault("web.session_secret", "")
	v.SetDefault("web.allowed_origins", []string{})
	v.SetDefault("web.secure_cookies", false)

	v.SetDefault("embedding.url", "http://localhost:8000")
	v.SetDefault("embedding.image_dim", 768)
	v.SetDefault("embedding.face_dim", 512)
	v.SetDefault("embedding.wake.enabled", false)
	v.SetDefault("embedding.wake.mac", "")
	v.SetDefault("embedding.wake.broadcast_addr", "255.255.255.255:9")
	v.SetDefault("embedding.wake.interface", "")
	v.SetDefault("embedding.wake.min_queue", 1)
	v.SetDefault("embedding.wake.cooldown", "5m")

	v.SetDefault("faces.min_det_score", 0.5)
	v.SetDefault("faces.iou_threshold", 0.1)
	v.SetDefault("faces.suggestion_limit", 5)
	v.SetDefault("faces.suggestion_max_distance", 0.5)
	v.SetDefault("faces.min_face_size", 0.02)

	v.SetDefault("cluster.threshold", 0.4)
	v.SetDefault("cluster.min_size", 2)
	v.SetDefault("cluster.suggestion_max_distance", 0.5)

	setDiscoveryDefaults(v)

	v.SetDefault("auth.bootstrap_admin_username", "")
	v.SetDefault("auth.bootstrap_admin_password", "")
	v.SetDefault("auth.session_ttl", "168h")          // 7-day sliding idle window
	v.SetDefault("auth.session_max_lifetime", "720h") // 30-day absolute cap
	v.SetDefault("auth.login_rate_limit", 10)
	v.SetDefault("auth.login_rate_window", "15m")

	setMapsDefaults(v)

	v.SetDefault("log.level", "info")
	v.SetDefault("metrics.enabled", true)

	setOpsDefaults(v)
}

// setMapsDefaults registers the mapy.com proxy defaults: an empty API key (so the
// proxy is disabled until configured), an empty User-Agent (Go's default is sent
// until one is configured), the public REST base URL, the reverse-geocode throttle
// backing the background places job, and the server-side tile cache that keeps a
// re-browsed area from costing mapy.com credits again.
// setDiscoveryDefaults registers the defaults of the discovery features riding
// the vector indexes: per-subject candidates, the recognition sweep, collection
// expansion and the review game.
func setDiscoveryDefaults(v *viper.Viper) {
	setCandidatesDefaults(v)
	setSweepDefaults(v)
	setExpandDefaults(v)
	setReviewDefaults(v)
}

// setCandidatesDefaults registers the untagged-person candidate-search defaults:
// the fallback cosine distance, the per-exemplar kNN cap, the minimum reviewable
// face width in pixels, and the concurrency bound on exemplar searches.
func setCandidatesDefaults(v *viper.Viper) {
	v.SetDefault("candidates.max_distance", 0.5)
	v.SetDefault("candidates.search_limit", 1000)
	v.SetDefault("candidates.min_face_px", 32)
	v.SetDefault("candidates.concurrency", 8)
}

// setSweepDefaults registers the recognition-sweep defaults: a small worker pool of
// subjects scanned at once (it stacks on candidates.concurrency, so it stays low on
// this RAM-constrained box) and a cap on how many subjects one sweep scans.
func setSweepDefaults(v *viper.Viper) {
	v.SetDefault("sweep.concurrency", 4)
	v.SetDefault("sweep.max_subjects", 500)
}

// setExpandDefaults registers the "expand a collection" search defaults: the
// fallback cosine distance (0.30, shown as 70 % similarity), the default and
// maximum result counts, the per-source kNN over-fetch, the source-set cap that
// keeps a huge album from running thousands of kNN queries, and the concurrency
// bound on per-source searches.
func setExpandDefaults(v *viper.Viper) {
	v.SetDefault("expand.max_distance", 0.30)
	v.SetDefault("expand.limit", 50)
	v.SetDefault("expand.max_limit", 200)
	v.SetDefault("expand.search_limit", 200)
	v.SetDefault("expand.source_cap", 500)
	v.SetDefault("expand.concurrency", 8)
}

// setReviewDefaults registers the review game defaults: the uncertainty band
// (roughly "the system is 45–75 % sure"), the batch size the UI prefetches, the
// per-user queue cache window, and the bounds on the label fan-out.
func setReviewDefaults(v *viper.Viper) {
	v.SetDefault("review.band_min", 0.45)
	v.SetDefault("review.band_max", 0.75)
	v.SetDefault("review.queue_size", 20)
	v.SetDefault("review.cache_ttl", "60s")
	v.SetDefault("review.max_labels", 200)
	v.SetDefault("review.label_concurrency", 2)
}

func setMapsDefaults(v *viper.Viper) {
	v.SetDefault("maps.mapy_api_key", "")
	v.SetDefault("maps.user_agent", "")
	v.SetDefault("maps.base_url", "https://api.mapy.com")
	v.SetDefault("maps.geocode_rate_per_sec", 5.0)
	v.SetDefault("maps.geocode_burst", 10)
	v.SetDefault("maps.tile_cache_bytes", 64<<20) // 64 MiB ≈ a few thousand tiles
	v.SetDefault("maps.tile_cache_ttl", 24*time.Hour)
}

// setStorageDefaults registers the storage defaults: the local filesystem
// backend, its on-disk locations, and empty R2 settings. The R2 credentials have
// no sensible default and are validated only when storage.backend is "r2", so an
// "fs" deployment never has to mention them.
func setStorageDefaults(v *viper.Viper) {
	v.SetDefault("storage.backend", StorageBackendFS) // local disk default; R2 is opt-in
	v.SetDefault("storage.originals_path", "/var/lib/kukatko/originals")
	v.SetDefault("storage.cache_path", "/var/lib/kukatko/cache")
	v.SetDefault("storage.temp_path", "/var/lib/kukatko/tmp")

	v.SetDefault("storage.r2.endpoint", "")
	v.SetDefault("storage.r2.region", "auto") // R2 accepts only "auto"
	v.SetDefault("storage.r2.bucket", "")
	v.SetDefault("storage.r2.access_key", "")
	v.SetDefault("storage.r2.secret_key", "")
	v.SetDefault("storage.r2.media_base_url", "")
	v.SetDefault("storage.r2.url_signing_secret", "")
	v.SetDefault("storage.r2.url_signing_secret_previous", "")
	v.SetDefault("storage.r2.url_ttl", "1h")
}

// setThumbDefaults registers the thumbnail-engine defaults: the pure-Go engine,
// the vipsthumbnail binary name (used only when the engine is "vips"), and an
// unbounded (GOMAXPROCS) per-photo encode concurrency.
func setThumbDefaults(v *viper.Viper) {
	v.SetDefault("thumb.engine", ThumbEngineGo) // pure-Go default; vips is opt-in
	v.SetDefault("thumb.vips_binary", "vipsthumbnail")
	v.SetDefault("thumb.concurrency", 0) // non-positive falls back to GOMAXPROCS
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
	v.SetDefault("backup.retention", 7)

	v.SetDefault("trash.retention_days", 30)

	v.SetDefault("duplicate.enabled", true)
	v.SetDefault("duplicate.phash_max_diff", 8)
	v.SetDefault("duplicate.embedding_max_dist", 0.05)

	v.SetDefault("stacks.enabled", true)
	v.SetDefault("stacks.rules.base_name", true)
	v.SetDefault("stacks.rules.sequential_copy", true)
	v.SetDefault("stacks.rules.unique_id", true)
	// Off by default: the same-second+GPS rule wrongly stacks burst shots.
	v.SetDefault("stacks.rules.time_gps", false)

	v.SetDefault("upload.max_file_size_mb", 0) // 0 = unlimited

	v.SetDefault("video.transcode", false) // on-the-fly HEVC→H.264 transcode is opt-in

	v.SetDefault("worker.count", 2)
	v.SetDefault("worker.poll_interval", "2s")
	v.SetDefault("worker.stale_after", "5m")
	v.SetDefault("worker.stale_scan_interval", "1m")

	v.SetDefault("bulk.max_batch_size", 1000)

	v.SetDefault("import.photoprism.base_url", "")
	v.SetDefault("import.photoprism.token", "")
	v.SetDefault("import.photoprism.page_size", 1000)

	v.SetDefault("import.photosorter.dsn", "")
	v.SetDefault("import.photosorter.page_size", 500)

	// Per-client-IP rate limits on heavy endpoints. Defaults are generous enough
	// for normal human/UI use and only bite under abusive flooding. Set
	// rate_per_sec to 0 to disable any individual rule.
	v.SetDefault("ratelimit.upload.rate_per_sec", 5)
	v.SetDefault("ratelimit.upload.burst", 30)
	v.SetDefault("ratelimit.bulk.rate_per_sec", 2)
	v.SetDefault("ratelimit.bulk.burst", 10)
	v.SetDefault("ratelimit.import.rate_per_sec", 1)
	v.SetDefault("ratelimit.import.burst", 3)
	v.SetDefault("ratelimit.tiles.rate_per_sec", 50)
	v.SetDefault("ratelimit.tiles.burst", 200)
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
	if err := c.Embedding.Wake.validate(); err != nil {
		return err
	}
	if err := c.Thumb.validate(); err != nil {
		return err
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	return c.Auth.validate()
}

// validate checks the storage backend selection and, when the R2 backend is
// selected, that every key it needs is present. An "fs" deployment is always
// valid: it never looks at the R2 settings.
func (s StorageConfig) validate() error {
	switch s.Backend {
	case "", StorageBackendFS:
		return nil
	case StorageBackendR2:
		return s.R2.validate(s.TempPath)
	default:
		return fmt.Errorf("%w: got %q", ErrInvalidStorageBackend, s.Backend)
	}
}

// validate reports every missing R2 key at once, so a misconfigured deployment
// fails startup with one actionable message rather than one key per restart. Only
// key names are reported; a credential's value never reaches the error.
func (r R2Config) validate(tempPath string) error {
	required := []struct{ key, value string }{
		{"storage.r2.endpoint", r.Endpoint},
		{"storage.r2.bucket", r.Bucket},
		{"storage.r2.access_key", r.AccessKey},
		{"storage.r2.secret_key", r.SecretKey},
		{"storage.r2.media_base_url", r.MediaBaseURL},
		{"storage.r2.url_signing_secret", r.URLSigningSecret},
		{"storage.temp_path", tempPath},
	}
	missing := make([]string, 0, len(required))
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing %s", ErrIncompleteR2Config, strings.Join(missing, ", "))
	}
	if r.URLTTL <= 0 {
		return fmt.Errorf("%w: storage.r2.url_ttl must be positive, got %s", ErrIncompleteR2Config, r.URLTTL)
	}
	return nil
}

// validate checks the Wake-on-LAN settings when auto-wake is enabled: a parseable
// MAC must be present and at least one destination (broadcast address or
// interface) must be configured. A disabled wake config is always valid.
func (w *WakeConfig) validate() error {
	if !w.Enabled {
		return nil
	}
	if w.MAC == "" {
		return fmt.Errorf("%w: embedding.wake.mac is required when wake is enabled", ErrInvalidWake)
	}
	if _, err := net.ParseMAC(w.MAC); err != nil {
		return fmt.Errorf("%w: embedding.wake.mac %q: %w", ErrInvalidWake, w.MAC, err)
	}
	if w.BroadcastAddr == "" && w.Interface == "" {
		return fmt.Errorf("%w: set embedding.wake.broadcast_addr or embedding.wake.interface", ErrInvalidWake)
	}
	return nil
}

// validate checks the thumbnail engine selection: it must be empty (treated as
// the pure-Go default), "go" or "vips". An unknown engine fails startup so a
// typo cannot silently leave thumbnails on the default engine.
func (t ThumbConfig) validate() error {
	switch t.Engine {
	case "", ThumbEngineGo, ThumbEngineVips:
		return nil
	default:
		return fmt.Errorf("%w: got %q", ErrInvalidThumbEngine, t.Engine)
	}
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
