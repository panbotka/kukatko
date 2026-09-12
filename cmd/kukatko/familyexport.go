package main

import (
	"context"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/familyexportjob"
	"github.com/panbotka/kukatko/internal/jobs"
)

// buildFamilyExportServiceOrNil assembles the genealogy export service — the
// `family_export` job handler that writes the library's whole family tree to
// families.yaml at the root of the store — or nil when the sidecar export is
// switched off.
//
// It rides the same switch as the per-photo sidecars (sidecar.enabled) because it
// is the same promise: the curation survives the database. Splitting it into a
// second key would let an instance end up half-covered, which is the state
// hardest to notice and worst to discover on the day it matters.
//
// It writes through the storage layer, so the file lands beside the originals on
// both the filesystem and the R2 backends.
func buildFamilyExportServiceOrNil(
	cfg *config.Config, db *database.DB,
) (*familyexportjob.Service, error) {
	if !cfg.Sidecar.Enabled {
		return nil, nil //nolint:nilnil // a disabled export has no service, and that is not an error
	}
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	return familyexportjob.New(familyexportjob.Config{
		Families: family.NewStore(db.Pool()),
		Writer:   familyexport.NewWriter(store),
	}), nil
}

// familyExportScheduler schedules a rewrite of the library's genealogy export
// after a relation — or a subject inside one — has changed. It is the shape the
// mutating APIs take (each as its own locally-declared interface), and it is
// satisfied by jobs.Enqueuer.
type familyExportScheduler interface {
	// EnqueueFamilyExport schedules a rewrite of the genealogy export.
	EnqueueFamilyExport(ctx context.Context) error
}

// nopFamilyExportScheduler drops every enqueue. It is what the mutating APIs are
// given when the export is off.
type nopFamilyExportScheduler struct{}

// EnqueueFamilyExport does nothing and reports success.
func (nopFamilyExportScheduler) EnqueueFamilyExport(context.Context) error { return nil }

// familyExportSchedulerFor returns the scheduler the mutating APIs enqueue
// through: the real queue enqueuer when the export is on, a no-op when it is off.
//
// The switch lives here, once, for the same two reasons sidecarSchedulerFor's
// does. A nil would make every call site guard, and one that forgot would panic
// on a config nobody tests. And the enqueue must actually stop when the export is
// off: no `family_export` handler is registered then, so a job enqueued anyway
// would sit queued forever.
func familyExportSchedulerFor(cfg *config.Config, enqueuer *jobs.Enqueuer) familyExportScheduler {
	if !cfg.Sidecar.Enabled {
		return nopFamilyExportScheduler{}
	}
	return enqueuer
}

// exportSchedulers bundles what a mutating API schedules its disaster-recovery
// writes through: the per-photo metadata sidecar's queue enqueue and the
// library-wide genealogy export's. They ride the same config switch and are
// always wired together, so they travel together.
type exportSchedulers struct {
	// sidecar schedules a rewrite of one photo's metadata sidecar.
	sidecar sidecarScheduler
	// family schedules a rewrite of the library's genealogy export.
	family familyExportScheduler
}

// exportSchedulersFor returns both schedulers for cfg: the real queue enqueuer
// for each export that is on, and a no-op for each that is off.
func exportSchedulersFor(cfg *config.Config, enqueuer *jobs.Enqueuer) exportSchedulers {
	return exportSchedulers{
		sidecar: sidecarSchedulerFor(cfg, enqueuer),
		family:  familyExportSchedulerFor(cfg, enqueuer),
	}
}
