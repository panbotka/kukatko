// Package server implements the kukatko HTTP server: chi routing, request
// handlers, and the server lifecycle (start and graceful shutdown).
//
// The listen address is supplied by the caller (resolved from the config
// subsystem in the serve command); timeouts remain hardcoded for now.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/version"
	"github.com/panbotka/kukatko/internal/web"
)

const (
	// DefaultAddr is the default TCP address the server listens on until
	// configuration support is added in a later milestone.
	DefaultAddr = ":8080"

	// readHeaderTimeout bounds how long the server waits for request headers,
	// mitigating slow-client (Slowloris) attacks.
	readHeaderTimeout = 10 * time.Second

	// shutdownTimeout bounds graceful shutdown: in-flight requests get this
	// long to finish before the server is forcibly closed.
	shutdownTimeout = 15 * time.Second
)

// Server wraps an http.Server together with the chi router that defines the
// kukatko HTTP API.
type Server struct {
	httpServer     *http.Server
	router         chi.Router
	apiGroups      []func(chi.Router)
	middlewares    []func(http.Handler) http.Handler
	metricsHandler http.Handler
}

// Option customises a Server during construction.
type Option func(*Server)

// WithAPI registers a function that mounts routes onto the /api/v1 router group.
// Multiple WithAPI options compose; each register runs against the same
// versioned subrouter, so callers (for example the auth subsystem) add their
// endpoints under /api/v1.
func WithAPI(register func(r chi.Router)) Option {
	return func(s *Server) {
		s.apiGroups = append(s.apiGroups, register)
	}
}

// WithMiddleware appends observability (or other) middlewares applied to every
// route after the built-in RequestID/RealIP and before Recoverer. They run in
// the order given; pass the metrics middleware and the structured access logger
// here. Multiple WithMiddleware options compose.
func WithMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.middlewares = append(s.middlewares, mw...)
	}
}

// WithMetricsHandler mounts h at GET /metrics (outside /api/v1, so Prometheus
// scrapes without authenticating). A nil handler leaves /metrics unmounted.
func WithMetricsHandler(h http.Handler) Option {
	return func(s *Server) {
		s.metricsHandler = h
	}
}

// New constructs a Server listening on addr with all routes and middleware
// registered. If addr is empty, DefaultAddr is used. Options (for example
// WithAPI) extend the server with additional route groups.
func New(addr string, opts ...Option) *Server {
	if addr == "" {
		addr = DefaultAddr
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)

	srv := &Server{
		router: router,
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           router,
			ReadHeaderTimeout: readHeaderTimeout,
		},
	}
	for _, opt := range opts {
		opt(srv)
	}
	// Observability middlewares (metrics, structured access log) are injected via
	// WithMiddleware so they see the RequestID/RealIP already set; Recoverer runs
	// innermost so the access log records the recovered 500.
	for _, mw := range srv.middlewares {
		router.Use(mw)
	}
	router.Use(middleware.Recoverer)
	srv.routes()
	return srv
}

// Handler returns the underlying HTTP handler, primarily so tests can exercise
// the routes without binding a TCP socket.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Addr returns the TCP address the server is configured to listen on.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// routes registers all HTTP routes on the server's router. API endpoints are
// registered explicitly; the embedded SPA handler is installed as the not-found
// handler so it serves unmatched paths (client-side routes, static assets)
// while leaving method-not-allowed responses on real API routes intact.
func (s *Server) routes() {
	s.router.Get("/healthz", handleHealthz)
	if s.metricsHandler != nil {
		s.router.Method(http.MethodGet, "/metrics", s.metricsHandler)
	}
	if len(s.apiGroups) > 0 {
		s.router.Route("/api/v1", func(r chi.Router) {
			for _, register := range s.apiGroups {
				register(r)
			}
		})
	}
	s.router.NotFound(web.Handler().ServeHTTP)
}

// Run starts the HTTP server and blocks until ctx is canceled (for example on
// SIGINT/SIGTERM), after which it performs a graceful shutdown bounded by
// shutdownTimeout. It returns an error if the server fails to start or does not
// shut down cleanly; a normal shutdown returns nil.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		err := s.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen and serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.shutdown()
	}
}

// shutdown gracefully stops the HTTP server, allowing in-flight requests up to
// shutdownTimeout to complete.
func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}

// healthResponse is the JSON body returned by the health-check endpoint.
type healthResponse struct {
	Status  string       `json:"status"`
	Version version.Info `json:"version"`
}

// handleHealthz responds with HTTP 200 and a JSON body reporting service health
// and build version information.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: version.Get(),
	})
}

// writeJSON writes payload as an indent-free JSON response with the given status
// code and the appropriate Content-Type header. Because the status line is
// flushed before the body is encoded, an encoding failure can only be logged,
// not turned into a different HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("server: encoding JSON response: %v", err)
	}
}
