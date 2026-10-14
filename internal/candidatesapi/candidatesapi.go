// Package candidatesapi exposes the "find a person among untagged photos" search
// over HTTP for curators and above. POST /subjects/{uid}/candidates runs the
// untagged-face candidate search for a subject and returns the resembling faces,
// each tagged with the action confirming it would take. It is read-only: confirming
// a candidate goes through the existing POST /photos/{uid}/faces/assign path, so
// this package adds no second write path. It depends on a search behaviour and a
// curator guard, both injected, so it stays decoupled from the candidates package's
// wiring.
package candidatesapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
)

// subjectNotFound is the 404 message for a subject the request's project does
// not hold, whether it exists in another project or nowhere.
const subjectNotFound = "subject not found"

// maxBodyBytes caps the request body. The body is two numbers, so a tight limit
// guards against oversized payloads.
const maxBodyBytes = 64 << 10

// Service is the search backend the endpoint delegates to. It is an interface so
// candidatesapi depends on the behaviour, not the candidates package's wiring;
// *candidates.Service satisfies it.
type Service interface {
	// Find returns the untagged-face candidates for the subject inside the
	// project, or people.ErrSubjectNotFound when the project holds no such
	// subject (one of another project included).
	Find(ctx context.Context, projectUID, subjectUID string, req candidates.Request) (candidates.Result, error)
}

// API exposes the candidate search over HTTP. The curator guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	service        Service
	requireCurator func(http.Handler) http.Handler
	rateLimit      func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service makes the endpoint
// answer 503.
type Config struct {
	// Service backs the candidate search.
	Service Service
	// RequireCurator guards the endpoint for curators and above.
	RequireCurator func(http.Handler) http.Handler
	// RateLimit throttles the candidate search, mounted inside the curator guard so it
	// can key on the caller and the project (cmd/kukatko's vector throttle: one
	// bucket per user, then one per project). Nil leaves the route unthrottled.
	RateLimit func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	limit := cfg.RateLimit
	if limit == nil {
		limit = passthrough
	}
	return &API{service: cfg.Service, requireCurator: cfg.RequireCurator, rateLimit: limit}
}

// RegisterRoutes mounts the candidate endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST /subjects/{uid}/candidates  RequireCurator  untagged-face candidates for a subject
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireCurator, a.rateLimit).Post("/subjects/{uid}/candidates", a.handleFind)
}

// handleFind runs the candidate search for the path subject inside the request's
// project. An absent backend answers 503, an unparsable or negative-valued body
// 400, an unknown subject 404 — and so does a subject of another project, with the
// same body, so the two cannot be told apart.
func (a *API) handleFind(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "candidate search not available")
		return
	}
	req, err := decodeRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.Find(r.Context(), auth.ProjectUIDFromContext(r.Context()), chi.URLParam(r, "uid"), req)
	if err != nil {
		if errors.Is(err, people.ErrSubjectNotFound) {
			// A fixed message: the wrapped error names the uid, and the answer
			// about a foreign subject must be byte-identical to a missing one's.
			writeError(w, http.StatusNotFound, subjectNotFound)
			return
		}
		writeError(w, http.StatusInternalServerError, "candidate search failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeRequest reads the optional search body. An empty body is valid and yields a
// zero Request (all defaults). Unknown fields, an oversized body, or a negative
// threshold or limit are rejected with an error safe to surface to the client.
func decodeRequest(r *http.Request) (candidates.Request, error) {
	var req candidates.Request
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return candidates.Request{}, errors.New("invalid request body: " + err.Error())
	}
	if req.Threshold < 0 {
		return candidates.Request{}, errors.New("threshold must not be negative")
	}
	if req.Limit < 0 {
		return candidates.Request{}, errors.New("limit must not be negative")
	}
	return req, nil
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("candidatesapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// passthrough is the no-op middleware standing in for an unset rate limit.
func passthrough(next http.Handler) http.Handler { return next }
