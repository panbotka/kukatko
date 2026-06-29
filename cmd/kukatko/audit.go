package main

import (
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auditapi"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
)

// buildAuditAPI assembles the admin-only audit-log HTTP API over the shared
// pool. GET /audit lists the durable audit trail with filters and pagination;
// entries themselves are written within mutation transactions elsewhere, so this
// API is read-only. The admin guard is supplied via authAPI so auditapi stays
// decoupled from auth's wiring.
func buildAuditAPI(db *database.DB, authAPI *auth.API) *auditapi.API {
	return auditapi.NewAPI(auditapi.Config{
		Store:        audit.NewStore(db.Pool()),
		RequireAdmin: authAPI.RequireAdmin,
	})
}
