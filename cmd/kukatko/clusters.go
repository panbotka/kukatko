package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/clusterapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildClusterAPI assembles the face auto-clustering subsystem over the shared
// pool: the cluster service (which reuses the shared face-matching service to name
// a whole cluster) and its editor/admin HTTP API. It returns both the HTTP API
// (mounted under /api/v1) and the service, which the processing API reuses to
// expose the admin recluster trigger. The write guard is supplied via authAPI so
// the clusterapi package stays decoupled from auth's wiring.
func buildClusterAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, faceSvc *facematch.Service,
) (*clusterapi.API, *cluster.Service) {
	clusterSvc := cluster.New(cluster.Config{
		Store:                 cluster.NewStore(db.Pool()),
		Faces:                 vectors.NewStore(db.Pool()),
		Assigner:              faceSvc,
		Threshold:             cfg.Cluster.Threshold,
		MinSize:               cfg.Cluster.MinSize,
		SuggestionMaxDistance: cfg.Cluster.SuggestionMaxDistance,
	})
	api := clusterapi.NewAPI(clusterapi.Config{
		Service:      clusterSvc,
		RequireWrite: authAPI.RequireWrite,
	})
	return api, clusterSvc
}
