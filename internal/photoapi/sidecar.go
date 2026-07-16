package photoapi

import (
	"context"
	"log"
)

// SidecarEnqueuer schedules a rewrite of a photo's metadata sidecar — the YAML
// file in storage holding its metadata and curation, which is what lets the
// catalogue be rebuilt without the database. It is satisfied by jobs.Enqueuer.
//
// A nil SidecarEnqueuer disables the scheduling: the export is off, and an edit
// simply does not schedule one.
type SidecarEnqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// enqueueSidecar schedules a rewrite of the photo's sidecar after a mutation, and
// is best-effort by design: a failure is logged and swallowed, never returned.
//
// Two rules are load-bearing here. It must run *after* the mutation has
// committed, because the job re-reads the photo and enqueuing earlier would
// serialise the old value. And it must never fail the user's edit: the edit is
// safely in Postgres either way, the sidecar is a second copy of it, and refusing
// a save because a copy could not be scheduled would make the safety net the
// thing that drops you. What a lost enqueue actually costs is a stale file until
// the backfill next runs, which is the whole reason the backfill exists.
func (a *API) enqueueSidecar(ctx context.Context, photoUID string) {
	if a.sidecar == nil || photoUID == "" {
		return
	}
	if err := a.sidecar.EnqueueSidecar(ctx, photoUID); err != nil {
		log.Printf("photoapi: enqueuing sidecar for %s: %v", photoUID, err)
	}
}

// enqueueSidecars schedules a sidecar rewrite for each of photoUIDs. It is the
// bulk form of enqueueSidecar, for the endpoints that mutate a selection.
func (a *API) enqueueSidecars(ctx context.Context, photoUIDs []string) {
	for _, uid := range photoUIDs {
		a.enqueueSidecar(ctx, uid)
	}
}
