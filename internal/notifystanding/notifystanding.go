// Package notifystanding is the one answer to "may this account still receive
// a notification of this project?" — asked by every producer when it chooses
// its recipients, by internal/notifydeliver when it records and schedules a
// notification, and again by the `push_send` and `mail_send` handlers when the
// queued delivery finally runs.
//
// An account is in standing in a project when all of these hold:
//
//   - it is a member of the project and an administrator has approved that
//     membership (a pending registration learns nothing about the project);
//   - the account is enabled;
//   - the project is active (a deactivated library notifies nobody).
//
// Maintainers get no exception for being maintainers: a maintainer hears about
// a project only as its member. The one kind addressed to them as the
// instance's operators — "a registration is waiting" — reaches an enabled
// maintainer of an active project whether a member or not (ReachesMaintainers).
//
// Asking twice is deliberate. A queued delivery outlives the moment it was
// produced: an account disabled, a membership removed or a project deactivated
// after the notification was recorded must not still get the push or the mail
// minutes or hours later. Each refusal is dropped silently and counted
// (Drops), which internal/metrics exports as kukatko_notifications_dropped_total.
package notifystanding

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/notification"
)

// Querier runs the one-row standing query; *pgxpool.Pool and pgx.Tx satisfy it.
type Querier interface {
	// QueryRow runs sql with args and returns its single row.
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Addressee is whom a notification is for and what it is: the account, the
// project it is about and its kind. A queued push or e-mail of a notification
// carries it, so the handler can ask the standing question again when it runs.
type Addressee struct {
	// UserUID is the account the notification is for.
	UserUID string `json:"user_uid"`
	// ProjectUID is the project the notification is about.
	ProjectUID string `json:"project_uid"`
	// Kind is the notification's kind (notification.Kind); it decides whether
	// a maintainer who is no member is addressed too.
	Kind string `json:"kind,omitempty"`
}

// Valid reports whether a names both an account and a project, the least a
// standing question needs.
func (a Addressee) Valid() bool {
	return strings.TrimSpace(a.UserUID) != "" && strings.TrimSpace(a.ProjectUID) != ""
}

// ReachesMaintainers reports whether a notification of kind is addressed to the
// instance's maintainers as well as to the project's members: only the
// "registration waiting" notification is, because a maintainer may approve a
// registration into any project. Every other kind is news inside the library,
// for its members only.
func ReachesMaintainers(kind string) bool {
	return kind == string(notification.KindRegistrationPending)
}

// MemberSQL returns the SQL condition that holds when the account userExpr is
// in standing in the project projectExpr: an approved member of it, enabled,
// and the project active. userExpr and projectExpr are SQL expressions — a
// placeholder ("$1") or a column ("u.uid", "s.project_id") of the enclosing
// statement. Producers embed it in the query that chooses their recipients, so
// the rule is written once.
func MemberSQL(userExpr, projectExpr string) string {
	return `EXISTS (SELECT 1 FROM project_members ns_m
        JOIN users ns_u ON ns_u.uid = ns_m.user_uid
        JOIN projects ns_p ON ns_p.uid = ns_m.project_uid
        WHERE ns_m.user_uid = ` + userExpr + ` AND ns_m.project_uid = ` + projectExpr + `
          AND ns_m.approved_at IS NOT NULL AND NOT ns_u.disabled AND ns_p.active)`
}

// MaintainerSQL returns the SQL condition that holds when the account userExpr
// is an enabled maintainer and the project projectExpr is active — the second
// audience of a kind that ReachesMaintainers. Arguments as in MemberSQL.
func MaintainerSQL(userExpr, projectExpr string) string {
	return `EXISTS (SELECT 1 FROM users ns_u
        JOIN projects ns_p ON ns_p.uid = ` + projectExpr + `
        WHERE ns_u.uid = ` + userExpr + ` AND ns_u.role = 'maintainer' AND NOT ns_u.disabled AND ns_p.active)`
}

// memberStandingSQL and maintainerStandingSQL are the Go-side questions, the
// account as $1 and the project as $2 — cast, so each parameter has one type
// however many columns it is compared with.
var (
	memberStandingSQL     = `SELECT ` + MemberSQL("$1::text", "$2::text")
	maintainerStandingSQL = `SELECT ` + MemberSQL("$1::text", "$2::text") + ` OR ` +
		MaintainerSQL("$1::text", "$2::text")
)

// InStanding reports, read through q, whether a.UserUID may receive a
// notification of kind a.Kind about the project a.ProjectUID (see the package
// documentation for the rule). An addressee naming no account or no project is
// never in standing. Only a failed query is an error.
func InStanding(ctx context.Context, q Querier, a Addressee) (bool, error) {
	if !a.Valid() {
		return false, nil
	}
	query := memberStandingSQL
	if ReachesMaintainers(a.Kind) {
		query = maintainerStandingSQL
	}
	var ok bool
	if err := q.QueryRow(ctx, query, a.UserUID, a.ProjectUID).Scan(&ok); err != nil {
		return false, fmt.Errorf("notifystanding: reading the standing of %s in %s: %w", a.UserUID, a.ProjectUID, err)
	}
	return ok, nil
}

// Checker asks the standing question on its own connection pool, for the queue
// handlers that run outside any producer's transaction.
type Checker struct {
	db Querier
}

// NewChecker returns a Checker reading through db (a *pgxpool.Pool). It panics
// on a nil db, a wiring bug that should fail at startup.
func NewChecker(db Querier) *Checker {
	if db == nil {
		panic("notifystanding: NewChecker requires a database")
	}
	return &Checker{db: db}
}

// InStanding reports whether a may receive the notification, read now; see the
// package-level InStanding.
func (c *Checker) InStanding(ctx context.Context, a Addressee) (bool, error) {
	return InStanding(ctx, c.db, a)
}
