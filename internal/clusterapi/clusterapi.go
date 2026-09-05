// Package clusterapi exposes the face auto-clustering HTTP API for editors and
// admins: listing the clusters of unassigned faces (each with a representative
// face, examples and a suggested existing subject), assigning a whole cluster to
// a subject in one action, and removing a stray face from a cluster before it is
// named. It depends on a cluster service behaviour and a write guard, both
// injected, so it stays decoupled from the cluster package's wiring.
//
// The listing is paginated and reads only what has been prepared in the
// background: a page carries the clusters whose cached summary exists, together
// with how many are still being prepared, so a library with thousands of groups
// answers in one indexed query instead of a vector search per group.
package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
)

// Service is the clustering backend the endpoints delegate to. It is an interface
// so clusterapi depends on the behaviour, not cluster's wiring; cluster.Service
// satisfies it.
type Service interface {
	// ListPage returns one page of the clusters that are ready to be shown, with
	// how many are ready in total and how many are still being prepared.
	ListPage(ctx context.Context, req cluster.PageRequest) (cluster.Listing, error)
	// AssignCluster assigns every face in a cluster to one subject.
	AssignCluster(ctx context.Context, req cluster.AssignRequest) (cluster.AssignResult, error)
	// RemoveFace detaches one face from a cluster, returning the refreshed cluster
	// view, or deleted=true when the removal emptied (and deleted) the cluster.
	RemoveFace(ctx context.Context, clusterUID string, ref cluster.Ref) (cluster.View, bool, error)
}

// Preparer schedules the background pass the library's groups are missing: the
// grouping of unassigned faces on a library that has none, or the cached
// summaries each group is listed from. It is satisfied by clusterjob.Service and
// is optional: with none wired the listing still serves whatever is prepared, it
// just never asks for more.
type Preparer interface {
	// EnsureGrouping queues the pass the library is missing, unless one is already
	// waiting or running, and reports whether a pass is in flight afterwards. It
	// groups only unassigned faces and reassigns nobody.
	EnsureGrouping(ctx context.Context) (bool, error)
}

// API exposes the clustering endpoints over HTTP. The write guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	service      Service
	preparer     Preparer
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Service and RequireWrite are
// required (a nil Service makes every endpoint answer 503); Preparer is
// optional.
type Config struct {
	// Service backs the clustering endpoints.
	Service Service
	// Preparer schedules the background preparation of pending clusters.
	Preparer Preparer
	// RequireWrite guards every endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, preparer: cfg.Preparer, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the clustering endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /faces/clusters?limit&offset      RequireWrite  one page of clusters + suggestions
//	POST /faces/clusters/{id}/assign       RequireWrite  assign whole cluster to a subject
//	POST /faces/clusters/{id}/remove-face  RequireWrite  drop a stray face from a cluster
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/faces/clusters", func(r chi.Router) {
		r.With(a.requireWrite).Get("/", a.handleList)
		r.With(a.requireWrite).Post("/{id}/assign", a.handleAssign)
		r.With(a.requireWrite).Post("/{id}/remove-face", a.handleRemoveFace)
	})
}

// listResponse is the listing page plus whether a grouping pass is queued or
// running. The flag is what lets an empty page say "the groups are being worked
// out" instead of "there are none", which on a library that had never been
// grouped was the one thing the page could not tell its reader.
type listResponse struct {
	cluster.Listing
	Grouping bool `json:"grouping"`
}

// handleList returns one page of the clusters of unassigned faces, each with its
// representative, examples and suggested subject, plus how many clusters are
// ready in total, how many are still being prepared in the background, and
// whether a pass is running.
//
// The page is served entirely from the cached summaries, so it costs two indexed
// queries and no vector search. Opening it is also what starts the background
// work that fills it in: the scheduler groups the unassigned faces of a library
// that has no groups, and prepares the summaries of the groups that have none.
// It never regroups a named face — browsing changes who a face belongs to
// nowhere in this API — and a failure to schedule is not a failure to answer:
// the reader gets the groups that are ready either way.
//
// It answers 400 for a malformed limit/offset and 503 when no cluster backend is
// wired.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face clustering not available")
		return
	}
	req, err := pageRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	listing, err := a.service.ListPage(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing clusters failed")
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Listing: listing, Grouping: a.ensureGrouping(r)})
}

// ensureGrouping asks the scheduler for the pass the library is missing and
// reports whether one is queued or running. With no preparer wired, or when the
// scheduler fails, it reports no pass: the listing is still served, it just
// cannot promise work it did not manage to queue.
func (a *API) ensureGrouping(r *http.Request) bool {
	if a.preparer == nil {
		return false
	}
	grouping, err := a.preparer.EnsureGrouping(r.Context())
	if err != nil {
		log.Printf("clusterapi: scheduling the face-grouping pass: %v", err)
		return false
	}
	return grouping
}

// pageRequest reads the limit and offset query parameters. Both are optional; a
// value that is not a non-negative integer is a bad request rather than a
// silently ignored one, so a broken client is told so. The service clamps the
// accepted values.
func pageRequest(r *http.Request) (cluster.PageRequest, error) {
	limit, err := intParam(r, "limit")
	if err != nil {
		return cluster.PageRequest{}, err
	}
	offset, err := intParam(r, "offset")
	if err != nil {
		return cluster.PageRequest{}, err
	}
	return cluster.PageRequest{Limit: limit, Offset: offset}, nil
}

// intParam reads one optional non-negative integer query parameter, returning 0
// when it is absent or empty.
func intParam(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

// handleAssign assigns every face in the cluster named in the path to the subject
// named in the JSON body (by uid or name). Validation problems answer 400, an
// unknown cluster or subject 404, and a missing backend 503.
func (a *API) handleAssign(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face clustering not available")
		return
	}
	var req cluster.AssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ClusterUID = chi.URLParam(r, "id")
	result, err := a.service.AssignCluster(r.Context(), req)
	if err != nil {
		writeClusterError(w, err, "assigning cluster failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// removeFaceRequest is the JSON body of the remove-face endpoint: the face to
// detach from the cluster.
type removeFaceRequest struct {
	PhotoUID  string `json:"photo_uid"`
	FaceIndex int    `json:"face_index"`
}

// removeFaceResponse is the JSON body of the remove-face endpoint: the refreshed
// cluster, or null when the removal emptied (and deleted) the cluster.
type removeFaceResponse struct {
	Cluster *cluster.View `json:"cluster"`
}

// handleRemoveFace detaches one face (named in the JSON body) from the cluster
// named in the path, so a stray face does not pollute the name before assignment.
// A missing cluster or non-member face answers 404, and a missing backend 503.
func (a *API) handleRemoveFace(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face clustering not available")
		return
	}
	var req removeFaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, deleted, err := a.service.RemoveFace(r.Context(), chi.URLParam(r, "id"),
		cluster.Ref{PhotoUID: req.PhotoUID, FaceIndex: req.FaceIndex})
	if err != nil {
		writeClusterError(w, err, "removing face from cluster failed")
		return
	}
	resp := removeFaceResponse{}
	if !deleted {
		resp.Cluster = &view
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeClusterError maps a cluster-service error to an HTTP response: 400 for
// invalid requests, 404 for a missing cluster/subject/face, 409 for an empty
// cluster, otherwise 500 with failMsg.
func writeClusterError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, cluster.ErrMissingSubject),
		errors.Is(err, facematch.ErrMissingBBox),
		errors.Is(err, facematch.ErrInvalidAction),
		errors.Is(err, people.ErrInvalidBounds),
		errors.Is(err, people.ErrInvalidType):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cluster.ErrClusterNotFound),
		errors.Is(err, cluster.ErrFaceNotInCluster),
		errors.Is(err, people.ErrSubjectNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cluster.ErrEmptyCluster):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, failMsg)
	}
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
		log.Printf("clusterapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
