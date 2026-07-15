package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/outlierapi"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildOutlierAPI assembles the per-subject face outlier detection HTTP API over
// the shared pool: the outlier service (subject's faces ranked by distance from
// their trimmed embedding centroid, with user-confirmed faces excluded via the
// feedback store) behind its editor/admin endpoint. The write guard is supplied
// via authAPI so the outlierapi package stays decoupled from auth's wiring.
func buildOutlierAPI(db *database.DB, authAPI *auth.API) *outlierapi.API {
	svc := outliers.New(outliers.Config{
		Faces:    vectors.NewStore(db.Pool()),
		People:   people.NewStore(db.Pool()),
		Feedback: feedback.NewStore(db.Pool()),
	})
	return outlierapi.NewAPI(outlierapi.Config{
		Service:      svc,
		RequireWrite: authAPI.RequireWrite,
	})
}
