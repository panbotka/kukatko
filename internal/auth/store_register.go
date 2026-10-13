package auth

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/project"
)

// listApprovalRecipientsQuery reads the accounts that are told somebody
// registered: every enabled maintainer and every enabled, approved admin of the
// project $1 (the project the registration joins), by username so the order is
// stable between runs.
//
// It deliberately does not filter on the address. An account with a placeholder
// address in the reserved .invalid domain is skipped later by the mail enqueuer,
// which is the one place that decision is made (see internal/mailjob) — a second
// copy of the rule here would drift from it.
const listApprovalRecipientsQuery = `SELECT ` + userColumns + `
	FROM users
	WHERE disabled = false AND (role = $2 OR EXISTS (
		SELECT 1 FROM project_members m
		WHERE m.user_uid = users.uid AND m.project_uid = $1 AND m.role = $3 AND m.approved_at IS NOT NULL))
	ORDER BY username`

// ListApprovalRecipients returns every enabled account that may approve a
// registration into projectUID — its admins and the maintainers — so each can
// be told somebody is waiting. An empty projectUID is ErrNoProject.
// The slice is empty (not nil) when there is nobody: a project whose only
// administrator is disabled still accepts registrations, it just has nobody to
// notify.
func (s *Store) ListApprovalRecipients(ctx context.Context, projectUID string) ([]User, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	rows, err := s.pool.Query(ctx, listApprovalRecipientsQuery,
		projectUID, string(RoleMaintainer), string(project.RoleAdmin))
	if err != nil {
		return nil, fmt.Errorf("auth: querying approval recipients: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating approval recipients: %w", err)
	}
	return users, nil
}
