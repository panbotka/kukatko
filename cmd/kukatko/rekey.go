package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/rekeyapi"
	"github.com/panbotka/kukatko/internal/rekeyjob"
)

// rekeyRewrites is what the storage move's Finish schedules its rewrites
// through: the sidecar and family-export schedulers of the configuration, so
// with the sidecar export off nothing is queued that no handler would claim.
type rekeyRewrites struct {
	exports exportSchedulers
}

// EnqueueFamilyExport schedules a rewrite of the project's genealogy export.
func (r rekeyRewrites) EnqueueFamilyExport(ctx context.Context, projectUID string) error {
	return r.exports.family.EnqueueFamilyExport(ctx, projectUID) //nolint:wrapcheck // the move wraps it.
}

// EnqueueSidecar schedules a rewrite of the photo's metadata sidecar.
func (r rekeyRewrites) EnqueueSidecar(ctx context.Context, photoUID string) error {
	return r.exports.sidecar.EnqueueSidecar(ctx, photoUID) //nolint:wrapcheck // the move wraps it.
}

// buildRekeyService assembles the storage move over the configured store and
// the database's pool. It fails when the configured backend cannot copy and
// list its objects, which both real backends can.
func buildRekeyService(cfg *config.Config, db *database.DB) (*rekey.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	moveStore, ok := store.(rekey.ObjectStore)
	if !ok {
		return nil, fmt.Errorf("storage backend %q cannot copy and list its objects", cfg.Storage.Backend)
	}
	enqueuer := jobs.NewEnqueuer(jobs.NewStore(db.Pool()))
	return rekey.New(rekey.Config{
		Pool:  db.Pool(),
		Store: moveStore,
		Jobs:  rekeyRewrites{exports: exportSchedulersFor(cfg, enqueuer)},
	}), nil
}

// buildRekeyJobService assembles the storage_rekey job handler: the storage
// move, paced at storage.rekey.per_second.
func buildRekeyJobService(cfg *config.Config, db *database.DB) (*rekeyjob.Service, error) {
	mover, err := buildRekeyService(cfg, db)
	if err != nil {
		return nil, err
	}
	return rekeyjob.New(rekeyjob.Config{Mover: mover, PerSecond: cfg.Storage.Rekey.PerSecond}), nil
}

// buildRekeyAPI assembles the system administrator's storage-move routes
// (/projects/{uid}/rekey…), every one behind authAPI's RequireMaintainer.
func buildRekeyAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) (*rekeyapi.API, error) {
	mover, err := buildRekeyService(cfg, db)
	if err != nil {
		return nil, err
	}
	return rekeyapi.NewAPI(rekeyapi.Config{Mover: mover, RequireMaintainer: authAPI.RequireMaintainer}), nil
}
