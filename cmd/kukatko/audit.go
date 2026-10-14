package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auditapi"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
)

// buildAuditAPI assembles the audit-log HTTP API over the shared pool. On the
// instance, GET /audit lists the whole durable trail with filters (a project's
// slug among them) and pagination for a maintainer, GET /audit/mine the same
// listing narrowed to the caller's own actions for any signed-in account (its
// user and project parameters ignored, so it is no username or slug oracle); under
// a project, the same two routes list only that project's entries, for its
// admin (its user filter resolved among the project's people only) and for a
// member. Entries themselves are written within mutation
// transactions elsewhere, so this API is read-only. The guards are supplied via
// authAPI so auditapi stays decoupled from auth's wiring, and the user and
// project filters are resolved through lookup functions for the same reason —
// auditapi reads accounts and projects, it does not own them.
func buildAuditAPI(db *database.DB, authAPI *auth.API) *auditapi.API {
	store := audit.NewStore(db.Pool())
	return auditapi.NewAPI(auditapi.Config{
		Store:              store,
		ResolveUser:        resolveAuditUser(auth.NewStore(db.Pool())),
		ResolveProjectUser: resolveProjectAuditUser(store),
		ResolveProject:     resolveAuditProject(project.NewStore(db.Pool())),
		RequireMaintainer:  authAPI.RequireMaintainer,
		RequireAdmin:       authAPI.RequireAdmin,
		RequireAuth:        authAPI.RequireAuth,
		RequireSignedIn:    authAPI.RequireSignedIn,
	})
}

// resolveProjectAuditUser resolves a project admin's user filter among that
// project's people only (audit.Store.ProjectPerson), so an account of another
// tenant and one that does not exist are the same ErrUnknownUser — and the
// listing answers both with the same empty page.
func resolveProjectAuditUser(store *audit.Store) auditapi.ResolveProjectUser {
	return func(ctx context.Context, projectUID, value string) (string, error) {
		uid, err := store.ProjectPerson(ctx, projectUID, value)
		if errors.Is(err, audit.ErrNoProjectPerson) {
			return "", auditapi.ErrUnknownUser
		}
		if err != nil {
			return "", fmt.Errorf("resolving project audit user %q: %w", value, err)
		}
		return uid, nil
	}
}

// resolveAuditProject resolves the instance audit listing's project filter — a
// slug, the name a maintainer sees in the project's URL — against the project
// store. A deactivated project resolves too: its history is still history.
func resolveAuditProject(store *project.Store) auditapi.ResolveProject {
	return func(ctx context.Context, slug string) (string, error) {
		p, err := store.GetBySlug(ctx, slug)
		if errors.Is(err, project.ErrNotFound) {
			return "", auditapi.ErrUnknownProject
		}
		if err != nil {
			return "", fmt.Errorf("resolving audit project %q: %w", slug, err)
		}
		return p.UID, nil
	}
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
