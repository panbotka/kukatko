// Package uploadlinkapi exposes upload links over HTTP: the curator's management
// routes (create, list, extend, revoke, restore or replace the code) and the two public routes behind a short
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
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// Store is the subset of uploadlink.Store the endpoints need.
type Store interface {
	// Create stores a link in a project and returns it with its plaintext code.
	Create(
		ctx context.Context, projectUID string, in uploadlink.NewLink, entry audit.Entry,
	) (uploadlink.Link, string, error)
	// Get returns one of the project's links by UID, or uploadlink.ErrNotFound.
	Get(ctx context.Context, projectUID, uid string) (uploadlink.Link, error)
	// ByCode returns the link a short code names, in whichever project, or
	// uploadlink.ErrNotFound.
	ByCode(ctx context.Context, code string) (uploadlink.Link, error)
	// List returns the project's links creatorUID made, or all of them for "".
	List(ctx context.Context, projectUID, creatorUID string) ([]uploadlink.Link, error)
	// Extend moves a link's expiry.
	Extend(ctx context.Context, projectUID, uid string, expiresAt time.Time, entry audit.Entry) (uploadlink.Link, error)
	// Revoke revokes a link for good.
	Revoke(ctx context.Context, projectUID, uid string, entry audit.Entry) (uploadlink.Link, error)
	// RestoreCode stores a pre-0091 link's code once it matches the stored hash.
	RestoreCode(ctx context.Context, projectUID, uid, code string, entry audit.Entry) (uploadlink.Link, error)
	// RotateCode replaces a link's code with a fresh one.
	RotateCode(ctx context.Context, projectUID, uid string, entry audit.Entry) (uploadlink.Link, error)
	// ReserveUpload takes one slot of a link's file cap while the link is live,
	// atomically; it refuses with uploadlink.ErrFull, ErrRevoked, ErrExpired or
	// ErrNotFound.
	ReserveUpload(ctx context.Context, uid string, maxUploads int, now time.Time) error
	// ReleaseUpload gives back a slot whose file was not recorded.
	ReleaseUpload(ctx context.Context, uid string) error
	// RecordUpload records one file that came through a link and files its photo.
	RecordUpload(ctx context.Context, up uploadlink.Upload, entry audit.Entry) error
}

// Ingester runs one file through the upload pipeline. It is satisfied by
// *ingest.Service.
type Ingester interface {
	// IngestFile ingests the file read from src and reports its outcome.
	IngestFile(ctx context.Context, src io.Reader, req ingest.Request) ingest.FileResult
}

// ProjectResolver reads the project a link files into. It is satisfied by
// *project.Store.
type ProjectResolver interface {
	// Get returns the project with uid, or project.ErrNotFound.
	Get(ctx context.Context, uid string) (*project.Project, error)
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
	// Projects resolves the project of a link the public routes found by its
	// code: the upload is ingested into that project under its storage prefix,
	// and a link of a deactivated project answers like an unknown one. Nil
	// ingests into the link's project with no prefix (tests with fakes).
	Projects ProjectResolver
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
	// CurrentRole resolves the caller's role in the request's project; nil uses
	// auth.ProjectRoleFromContext. Tests inject a fake.
	CurrentRole func(r *http.Request) project.Role
	// CurrentProject resolves the uid of the request's project, which every
	// management route works in; nil uses auth.ProjectUIDFromContext. Tests
	// inject a fake.
	CurrentProject func(r *http.Request) string
	// IPLimit throttles the public routes per client IP; nil throttles nothing.
	IPLimit *ratelimit.Limiter
	// LinkLimit throttles the uploads through one link, keyed by the link; nil
	// throttles nothing.
	LinkLimit *ratelimit.Limiter
	// MaxFileSize caps one uploaded file in bytes on top of the pipeline's own
	// cap; 0 sets none.
	MaxFileSize int64
	// MaxUploadsPerLink caps the files one link accepts over its life, enforced
	// per file in the database; 0 sets none.
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
	projects       ProjectResolver
	sidecar        SidecarEnqueuer
	requireCurator func(http.Handler) http.Handler
	optionalAuth   func(http.Handler) http.Handler
	currentUser    func(r *http.Request) (auth.User, bool)
	currentRole    func(r *http.Request) project.Role
	currentProject func(r *http.Request) string
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
		store: cfg.Store, ingest: cfg.Ingest, projects: cfg.Projects, sidecar: cfg.Sidecar,
		requireCurator: cfg.RequireCurator, optionalAuth: cfg.OptionalAuth,
		currentUser: cfg.CurrentUser, currentRole: cfg.CurrentRole, currentProject: cfg.CurrentProject,
		ipLimit: cfg.IPLimit, linkLimit: cfg.LinkLimit,
		maxFileSize: cfg.MaxFileSize, maxUploads: cfg.MaxUploadsPerLink,
		defaultDays: cfg.DefaultDays, maxDays: cfg.MaxDays,
		secureCookies: cfg.SecureCookies, now: cfg.Now, log: cfg.Logger,
	}
	a.defaultCallerResolvers()
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

// defaultCallerResolvers fills the caller resolvers cfg left nil with the
// ones reading the request's auth context.
func (a *API) defaultCallerResolvers() {
	if a.currentUser == nil {
		a.currentUser = func(r *http.Request) (auth.User, bool) { return auth.UserFromContext(r.Context()) }
	}
	if a.currentRole == nil {
		a.currentRole = func(r *http.Request) project.Role { return auth.ProjectRoleFromContext(r.Context()) }
	}
	if a.currentProject == nil {
		a.currentProject = func(r *http.Request) string { return auth.ProjectUIDFromContext(r.Context()) }
	}
}

// RegisterRoutes mounts the routes onto r, which the caller has scoped under the
// API base path (for example /api/v1):
//
//	GET  /upload-links                     RequireCurator   own links of the project (an admin's: all), with codes
//	POST /upload-links                     RequireCurator   create
//	POST /upload-links/{uid}/extend        RequireCurator   creator or admin
//	POST /upload-links/{uid}/revoke        RequireCurator   creator or admin
//	POST /upload-links/{uid}/restore-code  RequireCurator   creator or admin; the original code only
//	POST /upload-links/{uid}/new-code      RequireCurator   creator or admin; the old URL dies
//	GET  /u/{code}                   public           title, note, target names, expiry; starts the anonymous session
//	POST /u/{code}/upload            public           multipart upload into the targets
//
// The public pair is throttled per client IP ahead of everything else, so an
// unknown code costs a guesser their budget before it costs a lookup; the
// upload is additionally throttled per link once the link is known.
//
// The server mounts the two halves apart: RegisterManagementRoutes with the
// project-scoped API (under /api/v1/p/{project} and its transition alias),
// where every route works in the request's project and another project's link
// is a 404; RegisterPublicRoutes once, instance-level — a public link resolves
// its own project from the link row (upload_links.project_id) and files the
// upload into it, under that project's storage prefix.
func (a *API) RegisterRoutes(r chi.Router) {
	a.RegisterManagementRoutes(r)
	a.RegisterPublicRoutes(r)
}

// RegisterManagementRoutes mounts the curators' /upload-links half of
// RegisterRoutes.
func (a *API) RegisterManagementRoutes(r chi.Router) {
	r.Route("/upload-links", func(r chi.Router) {
		r.Use(a.requireCurator)
		r.Get("/", a.handleList)
		r.Post("/", a.handleCreate)
		r.Post("/{uid}/extend", a.handleExtend)
		r.Post("/{uid}/revoke", a.handleRevoke)
		r.Post("/{uid}/restore-code", a.handleRestoreCode)
		r.Post("/{uid}/new-code", a.handleNewCode)
	})
}

// RegisterPublicRoutes mounts the public /u/{code} half of RegisterRoutes: the
// page and the upload behind a short link, with no session required.
func (a *API) RegisterPublicRoutes(r chi.Router) {
	r.Route("/u/{code}", func(r chi.Router) {
		r.Use(a.ipLimit.Middleware)
		r.With(a.optionalAuth).Get("/", a.handlePublic)
		r.With(a.optionalAuth).Post("/upload", a.handleUpload)
	})
}
