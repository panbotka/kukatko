// Package clusterapi exposes the face auto-clustering HTTP API for editors and
// admins: listing the clusters of unassigned faces (each with a representative
// face, examples and a suggested existing subject), assigning a whole cluster to
// a subject in one action, and removing a stray face from a cluster before it is
// named. It depends on a cluster service behaviour and a write guard, both
// injected, so it stays decoupled from the cluster package's wiring.
package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
)

// Service is the clustering backend the endpoints delegate to. It is an interface
// so clusterapi depends on the behaviour, not cluster's wiring; cluster.Service
// satisfies it.
type Service interface {
	// ListClusters returns every cluster with its representative, examples and
	// suggested subject.
	ListClusters(ctx context.Context) ([]cluster.View, error)
	// AssignCluster assigns every face in a cluster to one subject.
	AssignCluster(ctx context.Context, req cluster.AssignRequest) (cluster.AssignResult, error)
	// RemoveFace detaches one face from a cluster, returning the refreshed cluster
	// view, or deleted=true when the removal emptied (and deleted) the cluster.
	RemoveFace(ctx context.Context, clusterUID string, ref cluster.Ref) (cluster.View, bool, error)
}

// API exposes the clustering endpoints over HTTP. The write guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. All fields are required; a nil
// Service makes every endpoint answer 503.
type Config struct {
	// Service backs the clustering endpoints.
	Service Service
	// RequireWrite guards every endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the clustering endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /faces/clusters                   RequireWrite  list clusters + suggestions
//	POST /faces/clusters/{id}/assign       RequireWrite  assign whole cluster to a subject
//	POST /faces/clusters/{id}/remove-face  RequireWrite  drop a stray face from a cluster
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/faces/clusters", func(r chi.Router) {
		r.With(a.requireWrite).Get("/", a.handleList)
		r.With(a.requireWrite).Post("/{id}/assign", a.handleAssign)
		r.With(a.requireWrite).Post("/{id}/remove-face", a.handleRemoveFace)
	})
}

// clustersResponse is the JSON body of the list endpoint.
type clustersResponse struct {
	Clusters []cluster.View `json:"clusters"`
}

// handleList returns every cluster of unassigned faces with its representative,
// examples and suggested subject. It answers 503 when no cluster backend is wired.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face clustering not available")
		return
	}
	views, err := a.service.ListClusters(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing clusters failed")
		return
	}
	writeJSON(w, http.StatusOK, clustersResponse{Clusters: views})
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
