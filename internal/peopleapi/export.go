package peopleapi

import (
	"context"
	"log"
)

// FamilyExportEnqueuer schedules a rewrite of a project's genealogy export —
// the families.yaml under the project's storage prefix that holds its whole
// family tree. It is satisfied by jobs.Enqueuer.
//
// A subject mutation is a relation change more often than it looks. Renaming
// somebody rewrites what the tree says their name is; deleting them cascades
// away every family row they were a partner or a child in; a merge moves one
// person's place in the tree onto another. All three would otherwise leave the
// file describing people and families that no longer exist that way — and a
// rebuild reading it would restore them.
//
// A nil enqueuer disables the scheduling: the export is off, and a subject edit
// simply does not schedule one.
type FamilyExportEnqueuer interface {
	// EnqueueFamilyExport schedules a rewrite of the project's genealogy export.
	EnqueueFamilyExport(ctx context.Context, projectUID string) error
}

// enqueueFamilyExport schedules the rewrite after a mutation has committed. Like
// every other export enqueue it is best-effort: a failure is logged and
// swallowed, because the change is safely in Postgres and refusing an edit
// because a second copy could not be scheduled would make the safety net the
// thing that drops you. Only the file of the project the subject belongs to is
// rewritten.
func (a *API) enqueueFamilyExport(ctx context.Context, projectUID string) {
	if a.familyExport == nil {
		return
	}
	if err := a.familyExport.EnqueueFamilyExport(ctx, projectUID); err != nil {
		log.Printf("peopleapi: enqueuing the family tree export: %v", err)
	}
}
