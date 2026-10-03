// Package uploadlinkapi exposes upload links over HTTP: the curator's management
// routes (create, list, extend, revoke) and the two public routes behind a short
// link — the page's description of where the photos go, and the upload itself,
// which runs every file through the ordinary ingest pipeline and files the
// resulting photo into the link's albums and labels.
//
// It also implements auth.UploadLinkGate, which lets a live link stand in for
// the shared registration secret and hands the photos an anonymous uploader just
// sent to the account they register.
//
// The store, the ingest pipeline and the auth guards are injected, so the
// handlers are unit-testable with fakes.
package uploadlinkapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// Store is the subset of uploadlink.Store the endpoints need.
type Store interface {
	// Create stores a link and returns it with its plaintext code.
	Create(ctx context.Context, in uploadlink.NewLink, entry audit.Entry) (uploadlink.Link, string, error)
	// Get returns one link by UID, or uploadlink.ErrNotFound.
	Get(ctx context.Context, uid string) (uploadlink.Link, error)
	// ByCode returns the link a short code names, or uploadlink.ErrNotFound.
	ByCode(ctx context.Context, code string) (uploadlink.Link, error)
	// List returns the links creatorUID made, or every link for "".
	List(ctx context.Context, creatorUID string) ([]uploadlink.Link, error)
	// Extend moves a link's expiry.
	Extend(ctx context.Context, uid string, expiresAt time.Time, entry audit.Entry) (uploadlink.Link, error)
	// Revoke revokes a link for good.
	Revoke(ctx context.Context, uid string, entry audit.Entry) (uploadlink.Link, error)
	// RecordUpload records one file that came through a link and files its photo.
	RecordUpload(ctx context.Context, up uploadlink.Upload, entry audit.Entry) error
}

// Ingester runs one file through the upload pipeline. It is satisfied by
// *ingest.Service.
type Ingester interface {
	// IngestFile ingests the file read from src and reports its outcome.
	IngestFile(ctx context.Context, src io.Reader, req ingest.Request) ingest.FileResult
}

// SidecarEnqueuer schedules a rewrite of a photo's metadata sidecar, which a
// photo filed into new albums and labels needs. It is satisfied by jobs.Enqueuer.
type SidecarEnqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies and limits of NewAPI.
type Config struct {
	// Store backs every route (required).
	Store Store
	// Ingest runs the uploaded files through the pipeline (required).
	Ingest Ingester
	// Sidecar reschedules the sidecar of every photo an upload filed; nil
	// schedules none.
	Sidecar SidecarEnqueuer
	// RequireCurator guards the management routes (required).
	RequireCurator func(http.Handler) http.Handler
	// OptionalAuth puts a signed-in caller on the context of the public upload
	// without refusing anybody else (required).
	OptionalAuth func(http.Handler) http.Handler
	// CurrentUser resolves the caller from the request; nil uses
	// auth.UserFromContext. Tests inject a fake.
	CurrentUser func(r *http.Request) (auth.User, bool)
	// IPLimit throttles the public routes per client IP; nil throttles nothing.
	IPLimit *ratelimit.Limiter
	// LinkLimit throttles the uploads through one link, keyed by the link; nil
	// throttles nothing.
	LinkLimit *ratelimit.Limiter
	// MaxFileSize caps one uploaded file in bytes on top of the pipeline's own
	// cap; 0 sets none.
	MaxFileSize int64
	// MaxUploadsPerLink caps the files one link accepts; 0 sets none.
	MaxUploadsPerLink int
	// DefaultDays and MaxDays are the validity the create form offers and the
	// longest one a link may have, in days. Non-positive values fall back to 30
	// and 365.
	DefaultDays int
	MaxDays     int
	// SecureCookies marks the anonymous uploader's session cookie Secure.
	SecureCookies bool
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
	// Logger records the failures a handler swallows; nil uses slog.Default().
	Logger *slog.Logger
}

// Fallback validity bounds when the configuration supplies none.
const (
	fallbackDefaultDays = 30
	fallbackMaxDays     = 365
)

// API exposes the upload-link routes.
type API struct {
	store          Store
	ingest         Ingester
	sidecar        SidecarEnqueuer
	requireCurator func(http.Handler) http.Handler
	optionalAuth   func(http.Handler) http.Handler
	currentUser    func(r *http.Request) (auth.User, bool)
	ipLimit        *ratelimit.Limiter
	linkLimit      *ratelimit.Limiter
	maxFileSize    int64
	maxUploads     int
	defaultDays    int
	maxDays        int
	secureCookies  bool
	now            func() time.Time
	log            *slog.Logger
}

// NewAPI returns an API from cfg, filling the optional collaborators with their
// defaults. A nil limiter becomes a disabled one, so the routes never branch on
// whether throttling is configured.
func NewAPI(cfg Config) *API {
	a := &API{
		store: cfg.Store, ingest: cfg.Ingest, sidecar: cfg.Sidecar,
		requireCurator: cfg.RequireCurator, optionalAuth: cfg.OptionalAuth,
		currentUser: cfg.CurrentUser, ipLimit: cfg.IPLimit, linkLimit: cfg.LinkLimit,
		maxFileSize: cfg.MaxFileSize, maxUploads: cfg.MaxUploadsPerLink,
		defaultDays: cfg.DefaultDays, maxDays: cfg.MaxDays,
		secureCookies: cfg.SecureCookies, now: cfg.Now, log: cfg.Logger,
	}
	if a.currentUser == nil {
		a.currentUser = func(r *http.Request) (auth.User, bool) { return auth.UserFromContext(r.Context()) }
	}
	if a.ipLimit == nil {
		a.ipLimit = ratelimit.New(0, 0)
	}
	if a.linkLimit == nil {
		a.linkLimit = ratelimit.New(0, 0)
	}
	if a.maxDays <= 0 {
		a.maxDays = fallbackMaxDays
	}
	if a.defaultDays <= 0 || a.defaultDays > a.maxDays {
		a.defaultDays = min(fallbackDefaultDays, a.maxDays)
	}
	if a.now == nil {
		a.now = time.Now
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	return a
}

// RegisterRoutes mounts the routes onto r, which the caller has scoped under the
// API base path (for example /api/v1):
//
//	GET  /upload-links               RequireCurator   own links (an admin's: all)
//	POST /upload-links               RequireCurator   create; the code is in this response only
//	POST /upload-links/{uid}/extend  RequireCurator   creator or admin
//	POST /upload-links/{uid}/revoke  RequireCurator   creator or admin
//	GET  /u/{code}                   public           title, note, target names, expiry; starts the anonymous session
//	POST /u/{code}/upload            public           multipart upload into the targets
//
// The public pair is throttled per client IP ahead of everything else, so an
// unknown code costs a guesser their budget before it costs a lookup; the
// upload is additionally throttled per link once the link is known.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/upload-links", func(r chi.Router) {
		r.Use(a.requireCurator)
		r.Get("/", a.handleList)
		r.Post("/", a.handleCreate)
		r.Post("/{uid}/extend", a.handleExtend)
		r.Post("/{uid}/revoke", a.handleRevoke)
	})
	r.Route("/u/{code}", func(r chi.Router) {
		r.Use(a.ipLimit.Middleware)
		r.With(a.optionalAuth).Get("/", a.handlePublic)
		r.With(a.optionalAuth).Post("/upload", a.handleUpload)
	})
}
