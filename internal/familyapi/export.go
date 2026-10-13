package familyapi

import (
	"context"
	"log"
)

// ExportEnqueuer schedules a rewrite of a project's genealogy export — the
// families.yaml under the project's storage prefix that holds its whole family
// tree, which is what lets the tree be rebuilt without the database. It is
// satisfied by jobs.Enqueuer.
//
// It takes the project and nothing else because there is one file per project
// and the job re-reads every family of it when it runs. A nil ExportEnqueuer
// disables the scheduling: the export is off, and a relation change simply does
// not schedule one.
type ExportEnqueuer interface {
	// EnqueueFamilyExport schedules a rewrite of the project's genealogy export.
	EnqueueFamilyExport(ctx context.Context, projectUID string) error
}

// enqueueExport schedules a rewrite of the genealogy export after a mutation, and
// is best-effort by design: a failure is logged and swallowed, never returned.
//
// Two rules are load-bearing here, the same two the sidecar enqueues follow. It
// must run *after* the mutation has committed, because the job re-reads the
// genealogy and enqueuing earlier would serialise the old state. Only the file
// of the project the mutation happened in is rewritten. And it must
// never fail the user's edit: the relation is safely in Postgres either way, the
// file is a second copy of it, and refusing a save because a copy could not be
// scheduled would make the safety net the thing that drops you.
func (a *API) enqueueExport(ctx context.Context, projectUID string) {
	if a.export == nil {
		return
	}
	if err := a.export.EnqueueFamilyExport(ctx, projectUID); err != nil {
		log.Printf("familyapi: enqueuing the family tree export: %v", err)
	}
}
