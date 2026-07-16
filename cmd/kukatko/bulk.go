package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/bulkapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// buildBulkAPI assembles the bulk metadata editing HTTP API over the shared
// pool. One POST /photos/bulk request applies an operation set (album/label
// membership, description/caption, location, archive state and the caller's
// favorite) to many photos transactionally, writing an audit-log entry
// in the same transaction. The per-request batch-size limit comes from config,
// and the write guard is supplied via authAPI so bulkapi stays decoupled from
// auth's wiring. sidecar schedules one metadata-sidecar job per photo the batch
// changed, so a 500-photo edit costs 500 small inserts rather than 500 file
// writes inside the request.
func buildBulkAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, sidecar sidecarScheduler,
) *bulkapi.API {
	service := bulk.NewService(db.Pool(), cfg.Bulk.MaxBatchSize)
	bulkLimit := ratelimit.New(cfg.RateLimit.Bulk.RatePerSec, cfg.RateLimit.Bulk.Burst)
	return bulkapi.NewAPI(bulkapi.Config{
		Service:      service,
		Sidecar:      sidecar,
		RequireWrite: authAPI.RequireWrite,
		RateLimit:    bulkLimit.Middleware,
	})
}
