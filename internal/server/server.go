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
	httpServer *http.Server
	router     chi.Router
}

// New constructs a Server listening on addr with all routes and middleware
// registered. If addr is empty, DefaultAddr is used.
func New(addr string) *Server {
	if addr == "" {
		addr = DefaultAddr
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	srv := &Server{
		router: router,
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           router,
			ReadHeaderTimeout: readHeaderTimeout,
		},
	}
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

// routes registers all HTTP routes on the server's router.
func (s *Server) routes() {
	s.router.Get("/healthz", handleHealthz)
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
