package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
)

// API exposes the ingest pipeline over HTTP. It mounts the multipart upload
// endpoint behind a write-access guard supplied by the auth subsystem, so the
// ingest package depends on auth only for the caller's identity, not its wiring.
type API struct {
	svc          *Service
	requireWrite func(http.Handler) http.Handler
	rateLimit    func(http.Handler) http.Handler
}

// NewAPI returns an API that runs uploads through svc and protects the route
// with requireWrite (typically auth.API.RequireWrite, allowing editors and
// admins). requireWrite must not be nil. rateLimit is an optional per-client-IP
// throttle applied ahead of the auth check; a nil value disables throttling.
func NewAPI(
	svc *Service,
	requireWrite func(http.Handler) http.Handler,
	rateLimit func(http.Handler) http.Handler,
) *API {
	if rateLimit == nil {
		rateLimit = passthroughMiddleware
	}
	return &API{svc: svc, requireWrite: requireWrite, rateLimit: rateLimit}
}

// passthroughMiddleware is a no-op middleware used when no rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// RegisterRoutes mounts the upload endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST /upload   rate limit + RequireWrite   multipart/form-data, one or more files
//
// The rate limiter runs outermost so a flood is rejected by client IP before the
// auth lookup, capping the work a single noisy uploader can impose.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.rateLimit, a.requireWrite).Post("/upload", a.handleUpload)
}

// uploadResponse is the JSON body returned by the upload endpoint: one result
// per uploaded file, in upload order.
type uploadResponse struct {
	Results []FileResult `json:"results"`
}

// handleUpload streams a multipart upload part by part — never buffering whole
// files in memory — and ingests each file, returning a per-file result list.
// The overall response is 200 whenever the request was a well-formed multipart
// body carrying at least one file; individual created/duplicate/error outcomes
// (including 409 duplicate semantics) live in each result's Status field. A
// request that is not multipart, or carries no file parts, is a 400.
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data upload")
		return
	}

	uploadedBy := uploaderUID(r)
	results, err := a.ingestParts(r, reader, uploadedBy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "malformed multipart upload")
		return
	}
	if len(results) == 0 {
		writeError(w, http.StatusBadRequest, "no files in upload")
		return
	}
	writeJSON(w, http.StatusOK, uploadResponse{Results: results})
}

// ingestParts walks the multipart stream, ingesting every part that carries a
// filename and skipping plain form fields. It returns the per-file results in
// order, or an error if the multipart stream itself is malformed.
func (a *API) ingestParts(
	r *http.Request, reader *multipart.Reader, uploadedBy string,
) ([]FileResult, error) {
	var results []FileResult
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return results, nil
		}
		if err != nil {
			return nil, fmt.Errorf("ingest: reading multipart part: %w", err)
		}
		if part.FileName() == "" {
			_ = part.Close()
			continue
		}
		results = append(results, a.svc.Ingest(r.Context(), part, part.FileName(), uploadedBy))
		_ = part.Close()
	}
}

// uploaderUID returns the authenticated user's UID for attribution, or the
// empty string when no user is on the context (the write guard should prevent
// this, but ingest tolerates anonymous attribution).
func uploaderUID(r *http.Request) string {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		return user.UID
	}
	return ""
}

// errorBody is the JSON body returned for request-level errors.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("ingest: encoding JSON response: %v", err)
	}
}

// writeError writes a request-level error response (distinct from a per-file
// error, which is carried inside a FileResult).
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
