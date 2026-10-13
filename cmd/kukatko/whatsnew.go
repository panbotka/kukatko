package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/whatsnew"
	"github.com/panbotka/kukatko/internal/whatsnewapi"
)

// buildWhatsNewAPI assembles GET /whats-new over the shared pool: the digest of
// what happened in the library since the caller's previous visit, readable by
// every signed-in role. The read guard is supplied via authAPI so whatsnewapi
// stays decoupled from auth's wiring, and the store keeps the per-account visit
// bookkeeping the digest is measured from. The task store supplies the "tasks
// wait on you" line, so the digest and the queue's badge count the same thing.
func buildWhatsNewAPI(db *database.DB, authAPI *auth.API) *whatsnewapi.API {
	return whatsnewapi.NewAPI(whatsnewapi.Config{
		Store:       whatsnew.NewStore(db.Pool(), requestTaskCounter{tasks: phototask.NewStore(db.Pool())}),
		RequireAuth: authAPI.RequireAuth,
	})
}

// requestTaskCounter is the task store as the digest's caller-only
// whatsnew.TaskCounter: it counts the tasks waiting on the caller in the
// request's project, which auth.WithProject put on the context — the very
// number the queue's badge shows in that project. It bridges the digest, which
// is scoped by project in a later step, to the already scoped task store.
type requestTaskCounter struct {
	tasks *phototask.Store
}

// WaitingOnMe returns how many open tasks of the request's project wait on
// callerUID. A context without a project is phototask.ErrNoProject.
func (c requestTaskCounter) WaitingOnMe(ctx context.Context, callerUID string) (int, error) {
	n, err := c.tasks.WaitingOnMe(ctx, auth.ProjectUIDFromContext(ctx), callerUID)
	if err != nil {
		return 0, fmt.Errorf("counting the tasks waiting on %s: %w", callerUID, err)
	}
	return n, nil
}
