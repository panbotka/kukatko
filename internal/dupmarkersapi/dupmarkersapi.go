// Package dupmarkersapi exposes the curation page for "one person marked more
// than once on the same photo" over HTTP.
//
// GET /duplicate-markers lists the findings (any authenticated user may look);
// the two repairs behind it are editor/admin only. Neither repair is new
// behaviour: "keep this one" drives the existing face-assignment state machine
// (internal/facematch) once per marker it detaches, and "no face here" flips the
// existing marker invalid flag through its audited store method. Nothing is ever
// deleted or merged, and nothing happens without a click — the listing itself
// never writes.
//
// The third choice a curator has, "leave it be", is a persisted opinion rather
// than a repair, so it lives with the rest of them in internal/feedbackapi
// (POST/DELETE /feedback/duplicate-marker-dismissals), exactly as the "not
// duplicates" decision does for /duplicates.
//
// Every dependency is an injected interface, so the package stays decoupled from
// the dupmarkers/people/facematch and auth wiring and is unit-testable with fakes.
// A nil Service answers 503 on the listing, so the routes mount even when the
// feature is not wired.
package dupmarkersapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
)

// Service lists the repeated-marker findings. It is satisfied by
// *dupmarkers.Service; a nil Service makes the listing answer 503.
type Service interface {
	// FindGroups returns one page of findings, worst first.
	FindGroups(ctx context.Context, limit, offset int) (dupmarkers.Result, error)
}

// MarkerStore is the marker access the repairs need: reading a photo's markers to
// resolve a group server-side, and the audited invalid-flag write. It is satisfied
// by *people.Store.
type MarkerStore interface {
	// ListMarkersByPhoto returns every marker on a photo.
	ListMarkersByPhoto(ctx context.Context, photoUID string) ([]people.Marker, error)
	// GetMarkerByUID returns one marker, or people.ErrMarkerNotFound.
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
	// SetMarkerInvalidAudited flips a marker's invalid flag, audited in the same
	// transaction.
	SetMarkerInvalidAudited(
		ctx context.Context, uid string, invalid bool, entry audit.Entry,
	) (people.Marker, error)
}

// Assigner applies one face-assignment transition. It is the existing write path
// the "keep this one" repair detaches the losing markers through, so unassigning
// here behaves exactly as it does everywhere else (subject cleared, reviewed
// cleared, faces cache refreshed, audited). *facematch.Service satisfies it.
type Assigner interface {
	// Apply performs one assignment-state transition, audited.
	Apply(ctx context.Context, req facematch.AssignRequest, meta audit.Meta) (facematch.AssignResult, error)
}

// API exposes the repeated-marker endpoints over HTTP.
type API struct {
	service      Service
	markers      MarkerStore
	assigner     Assigner
	requireAuth  func(http.Handler) http.Handler
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service is valid (the listing
// answers 503); nil Markers or Assigner make the matching repair answer 503. Both
// guards are required.
type Config struct {
	// Service finds the repeated-marker groups; nil means the listing is off.
	Service Service
	// Markers backs the marker reads and the invalid-flag write.
	Markers MarkerStore
	// Assigner detaches the losing markers of a group.
	Assigner Assigner
	// RequireAuth guards the read-only listing for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards the repairs for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		service:      cfg.Service,
		markers:      cfg.Markers,
		assigner:     cfg.Assigner,
		requireAuth:  cfg.RequireAuth,
		requireWrite: cfg.RequireWrite,
	}
}

// RegisterRoutes mounts the repeated-marker endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /duplicate-markers          RequireAuth   list the findings (query: limit, offset)
//	POST /duplicate-markers/keep     RequireWrite  keep one marker, detach the rest of the group
//	POST /duplicate-markers/invalid  RequireWrite  flag one marker as "no face in this box"
//
// The third decision — "leave it be" — is a durable opinion and lives with the
// other persisted feedback, at POST/DELETE /feedback/duplicate-marker-dismissals.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/duplicate-markers", a.handleList)
	r.With(a.requireWrite).Post("/duplicate-markers/keep", a.handleKeep)
	r.With(a.requireWrite).Post("/duplicate-markers/invalid", a.handleInvalid)
}

// handleList returns a page of findings. It answers 503 when the service is not
// wired, 400 for an invalid limit/offset, and 500 when the scan fails.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "repeated-marker review not available")
		return
	}
	limit, offset, err := parsePaging(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.FindGroups(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing repeated markers failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// keepResponse reports what "keep this one" did: which marker survived and which
// ones lost their subject. The detached uids are listed rather than counted so the
// client can name them in an undo hint and a reviewer can follow the audit trail.
type keepResponse struct {
	PhotoUID      string   `json:"photo_uid"`
	SubjectUID    string   `json:"subject_uid"`
	KeepMarkerUID string   `json:"keep_marker_uid"`
	Detached      []string `json:"detached"`
}

// handleKeep resolves one finding by keeping the named marker and detaching every
// other valid face marker the same subject has on the same photo: each loses its
// subject and its reviewed flag but survives as a region, because on a group shot
// the box almost always belongs to somebody else and is worth re-tagging rather
// than throwing away.
//
// The group is resolved server-side from (photo, subject) rather than taken from
// the request, so a stale client list cannot detach a marker that has meanwhile
// been re-tagged. It answers 503 without a backend, 400 for a malformed body, 404
// when the kept marker is not one of that person's valid face markers on that
// photo, and 500 when a detach fails.
func (a *API) handleKeep(w http.ResponseWriter, r *http.Request) {
	if a.markers == nil || a.assigner == nil {
		writeError(w, http.StatusServiceUnavailable, "repeated-marker repair not available")
		return
	}
	in, err := decodeKeep(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	group, err := a.groupMarkers(r.Context(), in.PhotoUID, in.SubjectUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading markers failed")
		return
	}
	if !containsMarker(group, in.KeepMarkerUID) {
		writeError(w, http.StatusNotFound, "marker is not assigned to that person on that photo")
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	meta := audit.FromRequest(r, user.UID)
	detached := make([]string, 0, len(group))
	for _, marker := range group {
		if marker.UID == in.KeepMarkerUID {
			continue
		}
		if err := a.detach(r.Context(), in.PhotoUID, marker.UID, meta); err != nil {
			writeError(w, http.StatusInternalServerError, "detaching marker failed")
			return
		}
		detached = append(detached, marker.UID)
	}
	writeJSON(w, http.StatusOK, keepResponse{
		PhotoUID:      in.PhotoUID,
		SubjectUID:    in.SubjectUID,
		KeepMarkerUID: in.KeepMarkerUID,
		Detached:      detached,
	})
}

// detach clears one marker's subject through the shared face-assignment state
// machine, so the write is identical to unassigning the face from anywhere else in
// the app — including its audit entry.
func (a *API) detach(ctx context.Context, photoUID, markerUID string, meta audit.Meta) error {
	if _, err := a.assigner.Apply(ctx, facematch.AssignRequest{
		Action:    facematch.ActionUnassignPerson,
		PhotoUID:  photoUID,
		MarkerUID: markerUID,
	}, meta); err != nil {
		return fmt.Errorf("dupmarkersapi: detaching marker %s: %w", markerUID, err)
	}
	return nil
}

// groupMarkers returns the subject's valid face markers on the photo — the group a
// repair acts on — in the store's own stable order (oldest first, then by uid). The
// order only decides how `detached` reads back; which markers are detached does not
// depend on it.
func (a *API) groupMarkers(ctx context.Context, photoUID, subjectUID string) ([]people.Marker, error) {
	all, err := a.markers.ListMarkersByPhoto(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("dupmarkersapi: listing markers of %s: %w", photoUID, err)
	}
	group := make([]people.Marker, 0, len(all))
	for _, marker := range all {
		if marker.Invalid || marker.Type != people.MarkerFace {
			continue
		}
		if marker.SubjectUID == nil || *marker.SubjectUID != subjectUID {
			continue
		}
		group = append(group, marker)
	}
	return group, nil
}

// containsMarker reports whether uid names one of the group's markers.
func containsMarker(group []people.Marker, uid string) bool {
	for _, marker := range group {
		if marker.UID == uid {
			return true
		}
	}
	return false
}

// handleInvalid flags one marker as "there is no face in this box at all" and
// answers 204. The marker row survives and keeps its subject; only the flag
// changes, which is enough for every listing that means "a real face" to skip it —
// including this page's own, so the group shrinks and, at one marker, disappears.
//
// It answers 503 without a backend, 400 for a malformed body, and 404 for an
// unknown marker.
func (a *API) handleInvalid(w http.ResponseWriter, r *http.Request) {
	if a.markers == nil {
		writeError(w, http.StatusServiceUnavailable, "repeated-marker repair not available")
		return
	}
	in, err := decodeInvalid(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := a.markers.GetMarkerByUID(r.Context(), in.MarkerUID)
	if err != nil {
		status, msg := markerStatus(err)
		writeError(w, status, msg)
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := audit.FromRequest(r, user.UID).
		Entry(audit.ActionMarkerInvalidate, "markers", existing.UID, invalidDetails(existing))
	if _, err := a.markers.SetMarkerInvalidAudited(r.Context(), in.MarkerUID, true, entry); err != nil {
		status, msg := markerStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// invalidDetails builds the audit details for flagging a marker invalid: the photo
// it sits on and, when it had one, the subject it was assigned to — so the trail
// says whose face count the flag changed without a second lookup.
func invalidDetails(marker people.Marker) map[string]any {
	details := map[string]any{"photo_uid": marker.PhotoUID, "invalid": true}
	if marker.SubjectUID != nil {
		details["subject_uid"] = *marker.SubjectUID
	}
	return details
}

// markerStatus maps a marker error to an HTTP status and client message.
func markerStatus(err error) (int, string) {
	if errors.Is(err, people.ErrMarkerNotFound) {
		return http.StatusNotFound, "marker not found"
	}
	return http.StatusInternalServerError, "marker update failed"
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
		return 0, &paramError{name: name}
	}
	return n, nil
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
		log.Printf("dupmarkersapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
