package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auditapi"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
)

// buildAuditAPI assembles the audit-log HTTP API over the shared pool. GET
// /audit lists the durable audit trail with filters and pagination for an admin,
// GET /audit/mine the same listing narrowed to the caller's own actions for any
// signed-in user; entries themselves are written within mutation transactions
// elsewhere, so this API is read-only. Both guards are supplied via authAPI so
// auditapi stays decoupled from auth's wiring, and the user filter is resolved
// through a lookup function for the same reason — auditapi reads accounts, it
// does not own them.
func buildAuditAPI(db *database.DB, authAPI *auth.API) *auditapi.API {
	return auditapi.NewAPI(auditapi.Config{
		Store:        audit.NewStore(db.Pool()),
		ResolveUser:  resolveAuditUser(auth.NewStore(db.Pool())),
		RequireAdmin: authAPI.RequireAdmin,
		RequireAuth:  authAPI.RequireAuth,
	})
}

// resolveAuditUser resolves the audit listing's user filter against the account
// store, accepting either an account UID or a username — a human filtering the
// trail reaches for the name they know, and a name that resolves to nothing has
// to be refused rather than answered with an empty page. The UID is tried first
// so a UID always means the account it names, whatever usernames exist.
func resolveAuditUser(store *auth.Store) auditapi.ResolveUser {
	return func(ctx context.Context, value string) (string, error) {
		user, err := store.GetUserByUIDOrUsername(ctx, value)
		if errors.Is(err, auth.ErrUserNotFound) {
			return "", auditapi.ErrUnknownUser
		}
		if err != nil {
			return "", fmt.Errorf("resolving audit user %q: %w", value, err)
		}
		return user.UID, nil
	}
}
