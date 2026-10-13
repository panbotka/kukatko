// Package mcpapi exposes the photo library to an AI agent as a Model Context
// Protocol server, mounted on the application's own HTTP server once per
// project at /api/v1/p/{project}/mcp (and, until the alias-removal step, at the
// unprefixed /api/v1/mcp, which addresses the bootstrap project).
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
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
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
// first, and which mistakes cost the most. instructionsFor appends the project
// the connection works in.
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
	// FindSimilar returns the project's nearest neighbours of vec by cosine
	// distance.
	FindSimilar(
		ctx context.Context, projectUID string, vec []float32, limit int, maxDistance float64,
	) ([]vectors.Match, error)
}

// EncodeReporter answers where each per-photo computation of one photo stands.
// Only the streaming encode is read from it — an agent asked "which videos still
// need preparing" cannot answer from a duration alone — and it is an interface so
// this package depends on the behaviour rather than on the processing package's
// wiring; *processing.Service satisfies it. A nil EncodeReporter leaves the field
// off the detail, which is the honest answer on an instance that does not report
// processing state.
type EncodeReporter interface {
	// Report returns the state of every step for the photo, or
	// photos.ErrPhotoNotFound.
	Report(ctx context.Context, photoUID string) ([]processing.Status, error)
}

// API exposes the MCP server over HTTP. The auth guard is injected so this
// package depends on auth's behaviour, not its wiring.
type API struct {
	enabled     bool
	requireAuth func(http.Handler) http.Handler
	handler     http.Handler

	// mu guards servers.
	mu sync.Mutex
	// servers holds the MCP servers built so far, one per write permission and
	// instructions text — the text names the project (and, through the alias,
	// carries the deprecation note), so there is one pair per project an agent
	// has connected to. A server is built on first use and then reused; a
	// renamed project gets a new pair and the old one is never asked again.
	servers map[serverKey]*mcp.Server

	photos      *photos.Store
	organize    *organize.Store
	people      *people.Store
	bulk        *bulk.Service
	similar     SimilarSearcher
	processing  EncodeReporter
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
	// Processing reports a video's streaming-encode state on the photo detail. Nil
	// leaves that one field off; nothing here can schedule an encode, which is the
	// admin surface this server deliberately does not expose (see docs/MCP.md).
	Processing EncodeReporter
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
		processing:  cfg.Processing,
		media:       cfg.Media,
		pageSize:    positiveOr(cfg.PageSize, defaultPageSize),
		maxPageSize: positiveOr(cfg.MaxPageSize, defaultMaxPageSize),
		servers:     map[serverKey]*mcp.Server{},
	}
	if !a.enabled {
		return a
	}
	a.handler = a.buildHandler()
	return a
}

// serverKey picks one MCP server: with or without the write tools, and the
// instructions it hands the agent.
type serverKey struct {
	canWrite     bool
	instructions string
}

// buildHandler builds the transport that picks, per request, the MCP server for
// the caller — read-only or with the write tools — in the request's project.
func (a *API) buildHandler() http.Handler {
	getServer := func(r *http.Request) *mcp.Server {
		ctx := r.Context()
		// RequireAuth has already run, so a principal is always present here.
		// Fall back to the read-only server if it somehow is not: an unidentified
		// caller must never reach a write tool.
		key := serverKey{canWrite: auth.ProjectRoleFromContext(ctx).CanWrite()}
		alias := auth.APIBase(ctx) == auth.APIBasePath
		if p, ok := auth.ProjectFromContext(ctx); ok {
			key.instructions = instructionsFor(p.Name, p.Slug, alias)
		} else {
			key.instructions = instructions
		}
		return a.server(key)
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

// server returns the MCP server for key, building it on first use.
func (a *API) server(key serverKey) *mcp.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.servers[key]; ok {
		return s
	}
	s := a.buildServer(key)
	a.servers[key] = s
	return s
}

// instructionsFor is the instructions text of a connection to the project name
// (slug): the general text plus a paragraph naming the project, so the agent
// knows which library its answers are about and where the others are. Through
// the transition alias (alias set) it ends with a one-line deprecation note
// pointing at the prefixed URL.
func instructionsFor(name, slug string, alias bool) string {
	text := instructions + "\n\n" +
		fmt.Sprintf("This server is the project %q (slug %q) of this Kukátko instance. ", name, slug) +
		"Every tool reads and changes that project's library only, and its uids mean nothing in " +
		"another project. The same token reaches the other projects of this instance its owner is a " +
		"member of, each at its own URL: /api/v1/p/<slug>/mcp."
	if alias {
		text += "\n\nDeprecated: this connection uses the transition alias /api/v1/mcp; " +
			"reconfigure it to /api/v1/p/" + slug + "/mcp before the alias is removed."
	}
	return text
}

// buildServer returns an MCP server carrying the read tools and, when
// key.canWrite is set, the write tools too, introducing itself with
// key.instructions. A read-only caller never sees a tool it may not call.
func (a *API) buildServer(key serverKey) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Kukátko photo library",
		Version: version.Get().Version,
	}, &mcp.ServerOptions{Instructions: key.instructions})

	a.registerSearchTools(s)
	a.registerCollectionTools(s)
	if key.canWrite {
		a.registerAlbumWriteTools(s)
		a.registerLabelWriteTools(s)
		a.registerPhotoWriteTools(s)
	}
	return s
}

// RegisterRoutes mounts the MCP endpoint onto r, which the caller has scoped
// under a project's API base path (/api/v1/p/{project}, or the bare /api/v1 of
// the transition alias):
//
//	POST /mcp  RequireAuth  Model Context Protocol (Streamable HTTP, stateless)
//
// A 401 on it advertises the endpoint's own URL in its challenge (see
// withResourceChallenge).
//
// When the server is disabled the path answers a bare 404 instead. Nothing of
// the MCP server is built or reachable — but the client is told "this server
// does not have that" in the one way every HTTP client understands, rather than
// falling into the SPA fallback and being handed 200 and index.html, which it
// would try to parse as JSON-RPC.
func (a *API) RegisterRoutes(r chi.Router) {
	if !a.enabled {
		r.Handle("/mcp", http.HandlerFunc(handleDisabled))
		return
	}
	r.With(withResourceChallenge(a.requireAuth), a.withCaller).Handle("/mcp", a.handler)
}

// handleDisabled answers the 404 of a server whose MCP endpoint is switched off.
// It is deliberately not a 403: the caller learns that the feature is absent,
// not that it exists and is being withheld.
func handleDisabled(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "mcp is not enabled")
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
