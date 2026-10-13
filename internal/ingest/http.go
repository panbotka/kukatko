package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// API exposes the ingest pipeline over HTTP. It mounts the multipart upload
// endpoint behind a sign-in guard supplied by the auth subsystem, so the
// ingest package depends on auth only for the caller's identity, not its wiring.
type API struct {
	svc         *Service
	requireAuth func(http.Handler) http.Handler
	rateLimit   func(http.Handler) http.Handler
}

// NewAPI returns an API that runs uploads through svc and protects the route
// with requireAuth (typically auth.API.RequireAuth, allowing every signed-in
// role, viewers included). requireAuth must not be nil. rateLimit is an optional
// per-client-IP throttle applied *behind* the auth check, so it can read who the
// caller is; a nil value disables throttling.
func NewAPI(
	svc *Service,
	requireAuth func(http.Handler) http.Handler,
	rateLimit func(http.Handler) http.Handler,
) *API {
	if rateLimit == nil {
		rateLimit = passthroughMiddleware
	}
	return &API{svc: svc, requireAuth: requireAuth, rateLimit: rateLimit}
}

// passthroughMiddleware is a no-op middleware used when no rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// RegisterRoutes mounts the upload endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST /upload   RequireAuth + rate limit   multipart/form-data, one or more files
//
// The rate limiter runs *inside* the sign-in guard, not ahead of it: the throttle
// keys on the client IP for everybody, but an API token an admin marked
// unlimited is exempt from it (see auth.RateLimitExempt), and that is knowable
// only once the caller has been authenticated. Nothing expensive becomes
// reachable by the swap — an unauthenticated flood now pays one indexed
// credential lookup before its 401, and the upload itself is still behind the
// guard.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth, a.rateLimit).Post("/upload", a.handleUpload)
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
// (including 409 duplicate semantics and the 413 limit refusals) live in each
// result's Status field. A request that is not multipart, or carries no file
// parts, is a 400. A project whose quota is already used up is answered 413
// {"error":"quota_exceeded",…} before a byte of the body is read: every file in
// it would be refused.
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data upload")
		return
	}

	// The files go into the request's project — the one the sign-in guard just
	// let the caller into — under that project's storage prefix. A request with
	// none (a wiring bug) has every file refused by the pipeline (ErrNoProject).
	proj, _ := auth.ProjectFromContext(r.Context())
	if refusal, full := QuotaRefusal(r.Context(), a.svc, proj); full {
		writeJSON(w, http.StatusRequestEntityTooLarge, refusal)
		return
	}
	uploadedBy := uploaderUID(r)
	results, err := a.ingestParts(r, reader, proj, uploadedBy)
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
// filename into the project proj and skipping plain form fields — except a
// SizeField, which declares the size of the file part right after it. It
// returns the per-file results in order, or an error if the multipart stream
// itself is malformed. A file refused for a limit is skipped (its remaining
// bytes discarded, never stored) and the files after it still run.
func (a *API) ingestParts(
	r *http.Request, reader *multipart.Reader, proj *project.Project, uploadedBy string,
) ([]FileResult, error) {
	var results []FileResult
	var declared int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return results, nil
		}
		if err != nil {
			return nil, fmt.Errorf("ingest: reading multipart part: %w", err)
		}
		if part.FileName() == "" {
			if part.FormName() == SizeField {
				declared = ReadSizeField(part)
			}
			_ = part.Close()
			continue
		}
		results = append(results, a.svc.IngestFile(r.Context(), proj, part, Request{
			Filename: part.FileName(), UploadedBy: uploadedBy, DeclaredSize: PartSize(part, declared),
		}))
		declared = 0
		_ = part.Close()
	}
}

// QuotaChecker answers whether a project can take more originals. It is
// satisfied by *Service.
type QuotaChecker interface {
	// CheckQuota returns a *LimitError when proj cannot take incoming more bytes
	// (0 or less: a file of unknown size); see Service.CheckQuota.
	CheckQuota(ctx context.Context, proj *project.Project, incoming int64) error
}

// QuotaRefusal answers whether proj's quota is already used up, so that a
// request's body need not be read: when it is, it returns the 413 body to send
// and true. A failed usage read is not a refusal here — each file is checked
// again, and refused on that error, by the pipeline.
func QuotaRefusal(ctx context.Context, quota QuotaChecker, proj *project.Project) (*LimitError, bool) {
	var limitErr *LimitError
	if err := quota.CheckQuota(ctx, proj, 0); errors.As(err, &limitErr) {
		return limitErr, true
	}
	return nil, false
}

// uploaderUID returns the authenticated user's UID for attribution, or the
// empty string when no user is on the context (the sign-in guard should prevent
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
