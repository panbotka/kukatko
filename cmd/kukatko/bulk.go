package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/bulkapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
)

// buildBulkAPI assembles the bulk metadata editing HTTP API over the shared
// pool. One POST /photos/bulk request applies an operation set (album/label
// membership, description/caption, location, private flag, archive state and the
// caller's favorite) to many photos transactionally, writing an audit-log entry
// in the same transaction. The per-request batch-size limit comes from config,
// and the write guard is supplied via authAPI so bulkapi stays decoupled from
// auth's wiring.
func buildBulkAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *bulkapi.API {
	service := bulk.NewService(db.Pool(), cfg.Bulk.MaxBatchSize)
	return bulkapi.NewAPI(bulkapi.Config{
		Service:      service,
		RequireWrite: authAPI.RequireWrite,
	})
}
