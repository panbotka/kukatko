package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/clusterapi"
	"github.com/panbotka/kukatko/internal/clusterjob"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildClusterAPI assembles the face auto-clustering subsystem over the shared
// pool: the cluster service (which reuses the shared face-matching service to name
// a whole cluster), the job service that runs the grouping and the preparation of
// the cached listing summaries in the background, and the editor/admin HTTP API.
// It returns the HTTP API (mounted under /api/v1) and the job service, which the
// worker registers as the `face_cluster` handler and the processing API reuses to
// expose the admin recluster trigger. The write guard is supplied via authAPI so
// the clusterapi package stays decoupled from auth's wiring.
func buildClusterAPI(
	cfg *config.Config, db *database.DB, store *jobs.Store, authAPI *auth.API, faceSvc *facematch.Service,
) (*clusterapi.API, *clusterjob.Service) {
	clusterSvc := cluster.New(cluster.Config{
		Store:                 cluster.NewStore(db.Pool()),
		Faces:                 vectors.NewStore(db.Pool()),
		Assigner:              faceSvc,
		Threshold:             cfg.Cluster.Threshold,
		MinSize:               cfg.Cluster.MinSize,
		SuggestionMaxDistance: cfg.Cluster.SuggestionMaxDistance,
	})
	jobSvc := clusterjob.New(clusterSvc, store, 0, nil)
	api := clusterapi.NewAPI(clusterapi.Config{
		Service: clusterSvc,
		// The listing schedules the preparation of the groups it could not show yet,
		// so opening the page is what starts the work that fills it in.
		Preparer:     jobSvc,
		RequireWrite: authAPI.RequireWrite,
	})
	return api, jobSvc
}
