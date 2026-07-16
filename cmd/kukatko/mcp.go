package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mcpapi"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildMCPAPI assembles the Model Context Protocol server that lets an AI agent
// drive the library — search it, read it, organise it — over the existing HTTP
// server at POST /api/v1/mcp. It calls the same stores the HTTP handlers call, so
// its mutations keep their transaction boundaries and their audit rows.
//
// The endpoint is off unless mcp.enabled is set: when it is false the API mounts
// no route at all, so the path does not exist rather than existing and refusing.
// The auth guard comes from authAPI, and the role check on each write tool from
// the caller's own role, so mcpapi stays decoupled from auth's wiring.
func buildMCPAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *mcpapi.API {
	return mcpapi.NewAPI(mcpapi.Config{
		Enabled:     cfg.MCP.Enabled,
		Photos:      photos.NewStore(db.Pool()),
		Organize:    organize.NewStore(db.Pool()),
		People:      people.NewStore(db.Pool()),
		Bulk:        bulk.NewService(db.Pool(), cfg.Bulk.MaxBatchSize),
		Similar:     vectors.NewStore(db.Pool()),
		Media:       mediaurl.NewBuilder(mediaStore),
		RequireAuth: authAPI.RequireAuth,
		PageSize:    cfg.MCP.PageSize,
		MaxPageSize: cfg.MCP.MaxPageSize,
	})
}
