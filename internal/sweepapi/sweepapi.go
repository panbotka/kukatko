// Package sweepapi exposes the recognition sweep over HTTP for editors and admins.
// GET /faces/sweep scans every named subject for confident matches among unnamed
// faces and streams the result as newline-delimited JSON (application/x-ndjson): a
// progress line per subject, a person line for each subject with actionable
// candidates, and a final summary. Streaming keeps the wait bearable — results appear
// person by person as they arrive rather than after the whole (slow) scan.
//
// It is read-only and never auto-assigns: confirming a candidate still goes through
// the existing POST /photos/{uid}/faces/assign path. It depends on a sweep behaviour
// and a write guard, both injected, so it stays decoupled from the sweep package's
// wiring.
package sweepapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/sweep"
)

// defaultConfidencePercent is the confidence used when the request omits it. It
// matches the frontend slider's default and maps to a tight 0.25 cosine distance.
const defaultConfidencePercent = 75

// minDistance is the tightest cosine distance a confidence maps to. It keeps a very
// high confidence (near 100 %) from collapsing to 0, which the candidate search would
// misread as "use the default distance".
const minDistance = 0.01

// Service is the sweep backend the endpoint delegates to. It is an interface so
// sweepapi depends on the behaviour, not the sweep package's wiring; *sweep.Service
// satisfies it.
type Service interface {
	// Sweep scans the named subjects and calls emit for each streamed event. emit is
	// called serially, so the handler can write it straight to the response.
	Sweep(ctx context.Context, params sweep.Params, emit func(sweep.Event) error) error
}

// API exposes the recognition sweep over HTTP. The write guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service makes the endpoint answer
// 503.
type Config struct {
	// Service backs the sweep.
	Service Service
	// RequireWrite guards the endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg. A nil RequireWrite is replaced with a pass-through,
// so the endpoint still mounts (unguarded) rather than panicking.
func NewAPI(cfg Config) *API {
	guard := cfg.RequireWrite
	if guard == nil {
		guard = func(next http.Handler) http.Handler { return next }
	}
	return &API{service: cfg.Service, requireWrite: guard}
}

// RegisterRoutes mounts the sweep endpoint onto r, which the caller has scoped under
// the API base path (for example /api/v1):
//
//	GET /faces/sweep  RequireWrite  streamed recognition sweep across all subjects
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/faces/sweep", a.handleSweep)
}

// handleSweep parses the confidence and per-person limit, then streams the sweep as
// newline-delimited JSON. An absent backend answers 503; an unparsable parameter 400;
// a failure before the first line is written becomes a 500, while a mid-stream failure
// (the client is already receiving 200 OK) can only be logged.
func (a *API) handleSweep(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "recognition sweep not available")
		return
	}
	params, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stream := newStream(w)
	if err := a.service.Sweep(r.Context(), params, stream.emit); err != nil {
		stream.fail(err)
	}
}

// parseParams reads the confidence and limit query parameters into sweep.Params.
func parseParams(r *http.Request) (sweep.Params, error) {
	threshold, err := parseConfidence(r.URL.Query().Get("confidence"))
	if err != nil {
		return sweep.Params{}, err
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return sweep.Params{}, err
	}
	return sweep.Params{Threshold: threshold, Limit: limit}, nil
}

// parseConfidence turns the confidence query value into a maximum cosine distance. It
// accepts either a similarity percentage (a value above 1, e.g. 75 → distance 0.25)
// or a raw cosine distance (0..1). An empty value defaults to defaultConfidencePercent.
// A negative, non-numeric, or too-large value is rejected.
func parseConfidence(raw string) (float64, error) {
	if raw == "" {
		return percentToDistance(defaultConfidencePercent), nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, errors.New("confidence must be a number")
	}
	switch {
	case value < 0:
		return 0, errors.New("confidence must not be negative")
	case value <= 1:
		return value, nil // a raw cosine distance
	case value <= 100:
		return percentToDistance(value), nil // a similarity percentage
	default:
		return 0, errors.New("confidence must be a percentage (<=100) or a cosine distance (<=1)")
	}
}

// percentToDistance maps a similarity percentage to the complementary cosine distance
// (distance = 1 - percent/100), floored so a near-100 % confidence stays a positive
// distance rather than collapsing to the search's default.
func percentToDistance(percent float64) float64 {
	distance := 1 - percent/100
	if distance < minDistance {
		return minDistance
	}
	return distance
}

// parseLimit reads the per-person candidate cap. An empty value means all (0); a
// negative value is rejected.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if value < 0 {
		return 0, errors.New("limit must not be negative")
	}
	return value, nil
}

// stream writes sweep events as newline-delimited JSON, setting the streaming headers
// lazily on the first line so a failure before any output can still become a normal
// error status. It flushes after every line when the writer supports it, so the
// client sees progress as it happens.
type stream struct {
	w       http.ResponseWriter
	enc     *json.Encoder
	flusher http.Flusher
	started bool
}

// newStream wraps w for NDJSON streaming.
func newStream(w http.ResponseWriter) *stream {
	flusher, _ := w.(http.Flusher)
	return &stream{w: w, enc: json.NewEncoder(w), flusher: flusher}
}

// emit writes one event as a JSON line, sending the 200 status and streaming headers
// on the first call.
func (s *stream) emit(ev sweep.Event) error {
	if !s.started {
		s.w.Header().Set("Content-Type", "application/x-ndjson")
		s.w.Header().Set("Cache-Control", "no-store")
		s.w.Header().Set("X-Content-Type-Options", "nosniff")
		s.w.WriteHeader(http.StatusOK)
		s.started = true
	}
	if err := s.enc.Encode(ev); err != nil {
		return fmt.Errorf("encoding sweep event: %w", err)
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// fail turns a sweep failure into a 500 when nothing has streamed yet; once the 200
// status and some lines are out, it can only be logged.
func (s *stream) fail(err error) {
	if s.started {
		log.Printf("sweepapi: sweep failed mid-stream: %v", err)
		return
	}
	writeError(s.w, http.StatusInternalServerError, "recognition sweep failed")
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorBody{Error: message}); err != nil {
		log.Printf("sweepapi: encoding error response: %v", err)
	}
}
