package organizeapi

import (
	"context"
	"log"
)

// enqueueSidecar schedules a rewrite of the photo's sidecar after its curation
// changed, and is best-effort: a failure is logged and swallowed, never returned.
//
// It runs after the mutation has committed, because the job re-reads the photo
// and enqueuing earlier would serialise the membership as it was. And it never
// fails the request: the change is safely in Postgres, the sidecar is a second
// copy of it, and a copy that could not be scheduled must not cost the user their
// edit. A lost enqueue costs a stale file until the backfill next runs.
func (a *API) enqueueSidecar(ctx context.Context, photoUID string) {
	if a.sidecar == nil || photoUID == "" {
		return
	}
	if err := a.sidecar.EnqueueSidecar(ctx, photoUID); err != nil {
		log.Printf("organizeapi: enqueuing sidecar for %s: %v", photoUID, err)
	}
}

// enqueueSidecars schedules a sidecar rewrite for each of photoUIDs. It is the
// bulk form, for the album-membership endpoints that take a selection.
func (a *API) enqueueSidecars(ctx context.Context, photoUIDs []string) {
	for _, uid := range photoUIDs {
		a.enqueueSidecar(ctx, uid)
	}
}
