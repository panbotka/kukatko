package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importapi"
	"github.com/panbotka/kukatko/internal/importer"
)

// buildImportAPI assembles the read-only HTTP API for imports: the run-history
// and per-photo/per-file failure listings, both fed by `kukatko import dir` (and
// by the finished PhotoPrism/photo-sorter migration, whose rows are kept as the
// catalogue's provenance record). Both read over the shared pool. The maintainer
// guard is supplied via authAPI so importapi stays decoupled from auth's wiring;
// imports are an operations capability at the top of the ladder.
func buildImportAPI(db *database.DB, authAPI *auth.API) *importapi.API {
	return importapi.NewAPI(importapi.Config{
		Runs:              importer.NewStore(db.Pool()),
		Failures:          importer.NewStore(db.Pool()),
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
