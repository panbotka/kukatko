package main

import (
	"context"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/sidecarjob"
)

// buildSidecarServiceOrNil assembles the metadata sidecar export service — the
// `sidecar` job handler that writes a photo's metadata and curation to a YAML
// file in storage, and the backfill behind POST /process/sidecars — or nil when
// the export is switched off.
//
// It writes through the storage layer, so sidecars land beside the originals on
// both the filesystem and the R2 backends. The returned service exposes Handle
// (for the worker registry), BackfillSidecars (for processapi) and Remove (for
// the purge path).
func buildSidecarServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
) (*sidecarjob.Service, error) {
	if !cfg.Sidecar.Enabled {
		return nil, nil //nolint:nilnil // a disabled export has no service, and that is not an error
	}
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	photoStore := photos.NewStore(db.Pool())
	return sidecarjob.New(sidecarjob.Config{
		Photos:   photoStore,
		Organize: organize.NewStore(db.Pool()),
		People:   people.NewStore(db.Pool()),
		Places:   places.NewStore(db.Pool()),
		Users:    auth.NewStore(db.Pool()),
		Writer:   sidecarexport.NewWriter(store),
		Lister:   photoStore,
		Enqueuer: enqueuer,
	}), nil
}

// sidecarBackfillerOrNil returns svc as a processapi.SidecarBackfiller, or a nil
// interface (not a typed-nil pointer, so processapi's == nil check fires and
// disables /process/sidecars) when the export is off.
func sidecarBackfillerOrNil(svc *sidecarjob.Service) processapi.SidecarBackfiller {
	if svc == nil {
		return nil
	}
	return svc
}

// sidecarScheduler schedules a rewrite of a photo's metadata sidecar after its
// metadata or curation has changed. It is the shape every mutating API takes (as
// its own locally-declared interface), and it is satisfied by jobs.Enqueuer.
type sidecarScheduler interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// nopSidecarScheduler drops every sidecar enqueue. It is what the mutating APIs
// are given when the export is off.
type nopSidecarScheduler struct{}

// EnqueueSidecar does nothing and reports success.
func (nopSidecarScheduler) EnqueueSidecar(context.Context, string) error { return nil }

// sidecarSchedulerFor returns the scheduler the mutating APIs enqueue through: the
// real queue enqueuer when the export is on, a no-op when it is off.
//
// The switch lives here, once, rather than in each mutating API, and it returns a
// working no-op rather than a nil interface for two reasons. A nil would make
// every call site guard, and one that forgot would panic on a config nobody
// tests. And the enqueue must actually stop when the export is off: no `sidecar`
// handler is registered then, so a job enqueued anyway would sit queued forever,
// and the queue would silently fill with work that can never drain.
func sidecarSchedulerFor(cfg *config.Config, enqueuer *jobs.Enqueuer) sidecarScheduler {
	if !cfg.Sidecar.Enabled {
		return nopSidecarScheduler{}
	}
	return enqueuer
}
