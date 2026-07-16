// Package mcpapi exposes the photo library to an AI agent as a Model Context
// Protocol server, mounted on the application's own HTTP server at /api/v1/mcp.
// It exists because "find every photo of grandma from the sixties and put them in
// an album" is how this library is actually maintained, and that workflow needs an
// agent that can search, read and organise — not a human clicking a grid.
//
// Three rules shape the package:
//
// It adds no auth. The endpoint sits behind the same RequireAuth middleware and
// the same RBAC as every other route: an agent authenticates with a long-lived
// `kkt_` API token, and its user's role decides what it may do. A viewer token is
// not merely refused on write — the write tools are not registered on the server
// it talks to, so they are invisible in tools/list. The role check is repeated
// inside every write handler, so the boundary holds even if that wiring changes.
//
// It calls the service layer in process. The tools use the same stores the HTTP
// handlers use, so mutations keep their transaction boundaries and every one of
// them writes an audit row in the same transaction, exactly like a human's click.
//
// It is compact by construction. An agent's context window, not the database, is
// the scarce resource: list tools return a handful of fields per photo (never the
// raw EXIF blob), page by default, and report how many more rows exist so the
// agent can decide whether to ask for them.
//
// Nothing destructive is exposed — no purge, no trash, no restore, no backup, no
// user administration. See docs/MCP.md; that omission is deliberate and permanent.
package mcpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/version"
)

// serverName identifies this server to a connecting client. It is the logical
// name clients key their configuration on, so it must stay stable.
const serverName = "kukatko"

// defaultPageSize and defaultMaxPageSize bound a list tool's result when the
// configuration leaves them unset. They are deliberately small: every returned
// row costs the calling agent context.
const (
	defaultPageSize    = 25
	defaultMaxPageSize = 100
)

// instructions is handed to the agent on initialize, before it has seen a single
// tool. It buys the tool descriptions their context: which tool to reach for
// first, and which mistakes cost the most.
const instructions = `Kukátko is a personal photo and video library.

Start with search_photos: it takes free text and a filter query language
(person:babicka year:1965 album:dovolena, - to exclude), and returns compact
photo summaries. Use library_stats to answer "how many"; use list_albums,
list_labels and list_subjects to turn a name a human used into the uid the
other tools want. Photo details (albums, labels, people, camera) come from
get_photo, one photo at a time — search deliberately does not return them.

To organise many photos at once use bulk_edit_photos rather than looping over
the single-photo tools: it applies the whole batch in one transaction, so a
change cannot end up half-applied.

Nothing here can delete a photo, empty the trash or touch users; those
operations are not exposed. Every change you make is recorded in the library's
audit trail against the token you are using.`

// SimilarSearcher is the vector-search backend behind find_similar_photos. It is
// an interface so this package depends on the behaviour rather than on the
// vectors package's wiring; *vectors.Store satisfies it. A nil SimilarSearcher
// makes the tool report that similarity search is unavailable, which is the
// honest answer on an instance with no embeddings.
type SimilarSearcher interface {
	// GetEmbedding returns a photo's image embedding, or vectors.ErrEmbeddingNotFound.
	GetEmbedding(ctx context.Context, photoUID string) (vectors.Embedding, error)
	// FindSimilar returns the nearest neighbours of vec by cosine distance.
	FindSimilar(ctx context.Context, vec []float32, limit int, maxDistance float64) ([]vectors.Match, error)
}

// API exposes the MCP server over HTTP. The auth guard is injected so this
// package depends on auth's behaviour, not its wiring.
type API struct {
	enabled     bool
	requireAuth func(http.Handler) http.Handler
	handler     http.Handler

	photos      *photos.Store
	organize    *organize.Store
	people      *people.Store
	bulk        *bulk.Service
	similar     SimilarSearcher
	media       *mediaurl.Builder
	pageSize    int
	maxPageSize int
}

// Config bundles the dependencies of NewAPI. Enabled false leaves every other
// field unread and mounts no route at all.
type Config struct {
	// Enabled mounts the endpoint. When false RegisterRoutes registers nothing,
	// so /api/v1/mcp does not exist rather than answering 403 — the server is a
	// new attack surface and stays off until it is asked for.
	Enabled bool
	// Photos is the catalog core backing search, details and metadata edits.
	Photos *photos.Store
	// Organize backs albums, labels, favourites and ratings.
	Organize *organize.Store
	// People backs the subject (people and animals) tools.
	People *people.Store
	// Bulk backs bulk_edit_photos.
	Bulk *bulk.Service
	// Similar backs find_similar_photos. Nil disables that one tool.
	Similar SimilarSearcher
	// Media stamps thumbnail URLs onto returned photos. Nil falls back to this
	// application's own media routes.
	Media *mediaurl.Builder
	// RequireAuth guards the endpoint; the per-tool role check happens inside.
	RequireAuth func(http.Handler) http.Handler
	// PageSize is a list tool's default limit; non-positive falls back to 25.
	PageSize int
	// MaxPageSize caps a list tool's limit; non-positive falls back to 100.
	MaxPageSize int
}

// NewAPI returns an API from cfg. When cfg.Enabled is false it returns an API
// that mounts nothing; the MCP servers are not even built.
func NewAPI(cfg Config) *API {
	a := &API{
		enabled:     cfg.Enabled,
		requireAuth: cfg.RequireAuth,
		photos:      cfg.Photos,
		organize:    cfg.Organize,
		people:      cfg.People,
		bulk:        cfg.Bulk,
		similar:     cfg.Similar,
		media:       cfg.Media,
		pageSize:    positiveOr(cfg.PageSize, defaultPageSize),
		maxPageSize: positiveOr(cfg.MaxPageSize, defaultMaxPageSize),
	}
	if !a.enabled {
		return a
	}
	a.handler = a.buildHandler()
	return a
}

// buildHandler builds the two MCP servers — one read-only, one with the write
// tools — and the transport that picks between them per request. Both are built
// once at startup; getServer only chooses.
func (a *API) buildHandler() http.Handler {
	readOnly := a.buildServer(false)
	writable := a.buildServer(true)

	getServer := func(r *http.Request) *mcp.Server {
		// RequireAuth has already run, so a principal is always present here.
		// Fall back to the read-only server if it somehow is not: an unidentified
		// caller must never reach a write tool.
		if user, ok := auth.UserFromContext(r.Context()); ok && user.Role.CanWrite() {
			return writable
		}
		return readOnly
	}

	// Stateless: each POST is self-contained, so the endpoint stays a plain
	// authenticated route with no session state to hijack or expire, and the
	// request's context — carrying the principal and the audit metadata — reaches
	// the tool handlers. JSONResponse: these tools answer in one shot and never
	// stream, so a plain application/json body is the honest reply.
	//
	// DisableLocalhostProtection: the SDK's DNS-rebinding guard rejects a request
	// that arrives over loopback with a non-loopback Host header, which is exactly
	// what a reverse proxy in front of this server produces. The guard protects
	// unauthenticated local servers; this endpoint requires a valid principal, so
	// it would only break real deployments.
	return mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
	})
}

// buildServer returns an MCP server carrying the read tools and, when canWrite is
// set, the write tools too. A read-only caller never sees a tool it may not call.
func (a *API) buildServer(canWrite bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Kukátko photo library",
		Version: version.Get().Version,
	}, &mcp.ServerOptions{Instructions: instructions})

	a.registerSearchTools(s)
	a.registerCollectionTools(s)
	if canWrite {
		a.registerAlbumWriteTools(s)
		a.registerLabelWriteTools(s)
		a.registerPhotoWriteTools(s)
	}
	return s
}

// RegisterRoutes mounts the MCP endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST /mcp  RequireAuth  Model Context Protocol (Streamable HTTP, stateless)
//
// When the server is disabled nothing is mounted: the path falls through to the
// SPA handler like any other unknown route, rather than existing and refusing.
func (a *API) RegisterRoutes(r chi.Router) {
	if !a.enabled {
		return
	}
	r.With(a.requireAuth, a.withCaller).Handle("/mcp", a.handler)
}

// positiveOr returns v when it is positive, and fallback otherwise, so a
// misconfigured bound degrades to the package default instead of returning
// nothing.
func positiveOr(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
