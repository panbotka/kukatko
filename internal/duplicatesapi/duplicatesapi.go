// Package duplicatesapi exposes the editor/admin HTTP endpoints for reviewing and
// resolving groups of likely-duplicate photos. GET /duplicates lists the groups;
// POST /duplicates/merge resolves one by merging the redundant copies into a
// chosen keeper and archiving them. Both depend only on injected behaviours (a
// list Service, a merge Service) and a write guard, so the package stays
// decoupled from the duplicates/dupmerge and auth wiring. A nil list Service
// answers 503 on GET (so the route mounts even when detection is disabled by
// config); the merge itself is transactional and lives in internal/dupmerge.
package duplicatesapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/dupmerge"
)

// maxBodyBytes caps the merge request body: a keeper UID plus a short list of
// member UIDs is tiny, so 256 KiB is generous while guarding against abuse.
const maxBodyBytes = 256 << 10

// Service is the duplicates listing behaviour the API drives. It is satisfied by
// *duplicates.Service; a nil Service makes the list endpoint answer 503.
type Service interface {
	// FindGroups returns one page of duplicate groups.
	FindGroups(ctx context.Context, limit, offset int) (duplicates.Result, error)
}

// MergeService resolves a duplicate group by merging the redundant copies into a
// chosen keeper. It is satisfied by *dupmerge.Service; a nil MergeService makes
// the merge endpoint answer 503.
type MergeService interface {
	// Merge performs the transactional merge + archive and returns what it did.
	Merge(ctx context.Context, in dupmerge.Input) (dupmerge.Result, error)
	// Preview computes what Merge would do without changing anything.
	Preview(ctx context.Context, in dupmerge.Input) (dupmerge.Result, error)
}

// API exposes the duplicates endpoints over HTTP behind a write guard.
type API struct {
	service      Service
	merge        MergeService
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service is valid (the list
// endpoint answers 503) as is a nil Merge (the merge endpoint answers 503);
// RequireWrite is required.
type Config struct {
	// Service finds duplicate groups; nil means detection is not configured.
	Service Service
	// Merge resolves a group into its keeper; nil means merging is unavailable.
	Merge MergeService
	// RequireWrite guards the endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, merge: cfg.Merge, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the duplicates endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /duplicates        RequireWrite  list duplicate groups (query: limit, offset)
//	POST /duplicates/merge  RequireWrite  resolve a group by merging into its keeper
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/duplicates", a.handleList)
	r.With(a.requireWrite).Post("/duplicates/merge", a.handleMerge)
}

// handleList returns a page of duplicate groups. It answers 503 when detection is
// not configured, 400 for an invalid limit/offset, and 500 when the scan fails.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "duplicate detection not available")
		return
	}
	limit, offset, err := parsePaging(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.FindGroups(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "finding duplicates failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// mergeRequest is the JSON body of POST /duplicates/merge: the chosen keeper, the
// full group membership (including the keeper), and whether to only preview.
type mergeRequest struct {
	// KeeperUID is the member to keep; the rest are merged into it and archived.
	KeeperUID string `json:"keeper_uid"`
	// MemberUIDs is the full group membership, including the keeper.
	MemberUIDs []string `json:"member_uids"`
	// DryRun, when true, previews the merge (nothing is changed) so the client can
	// show what would move before asking the user to confirm.
	DryRun bool `json:"dry_run"`
}

// handleMerge resolves a duplicate group by merging its redundant copies into the
// chosen keeper (or previews that when dry_run is set). It answers 503 when
// merging is not configured, 400 for a malformed request or an invalid group, 404
// when the keeper does not exist, and 500 when the merge fails.
func (a *API) handleMerge(w http.ResponseWriter, r *http.Request) {
	if a.merge == nil {
		writeError(w, http.StatusServiceUnavailable, "duplicate resolution not available")
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	var req mergeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := dupmerge.Input{KeeperUID: req.KeeperUID, MemberUIDs: req.MemberUIDs, ActorUID: user.UID}
	var result dupmerge.Result
	var err error
	if req.DryRun {
		result, err = a.merge.Preview(r.Context(), in)
	} else {
		result, err = a.merge.Merge(r.Context(), in)
	}
	if err != nil {
		status, msg := mergeStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// mergeStatus maps a merge error to an HTTP status and client message.
func mergeStatus(err error) (int, string) {
	switch {
	case errors.Is(err, dupmerge.ErrNoKeeper),
		errors.Is(err, dupmerge.ErrTooFewMembers),
		errors.Is(err, dupmerge.ErrKeeperNotInGroup):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, dupmerge.ErrKeeperNotFound):
		return http.StatusNotFound, err.Error()
	default:
		return http.StatusInternalServerError, "merge failed"
	}
}

// decodeJSON decodes the request body into dst, rejecting unknown fields and
// bodies larger than maxBodyBytes.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// parsePaging reads the optional limit and offset query parameters, returning a
// descriptive error when either is present but not a non-negative integer. Absent
// parameters yield zero, which the service treats as "default".
func parsePaging(r *http.Request) (limit, offset int, err error) {
	limit, err = parseNonNegative(r, "limit")
	if err != nil {
		return 0, 0, err
	}
	offset, err = parseNonNegative(r, "offset")
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// parseNonNegative parses query parameter name as a non-negative integer,
// returning zero when it is absent and an error when it is malformed or negative.
func parseNonNegative(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, invalidParamError(name)
	}
	return n, nil
}

// invalidParamError builds the 400 error for a bad pagination parameter.
func invalidParamError(name string) error {
	return &paramError{name: name}
}

// paramError reports an invalid query parameter by name.
type paramError struct {
	name string
}

// Error implements error for paramError.
func (e *paramError) Error() string {
	return "invalid " + e.name + " parameter"
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
		log.Printf("duplicatesapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
