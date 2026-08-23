package auth

import (
	"context"
	"fmt"
)

// listApprovalRecipientsQuery reads the accounts that are told somebody
// registered: every enabled admin and maintainer, oldest username first so the
// order is stable between runs.
//
// It deliberately does not filter on the address. An account with a placeholder
// address in the reserved .invalid domain is skipped later by the mail enqueuer,
// which is the one place that decision is made (see internal/mailjob) — a second
// copy of the rule here would drift from it.
const listApprovalRecipientsQuery = `SELECT ` + userColumns + `
	FROM users
	WHERE disabled = false AND role = ANY($1)
	ORDER BY username`

// ListApprovalRecipients returns every enabled account that may approve a
// registration — the admins and the maintainers — so each can be told somebody
// is waiting. The slice is empty (not nil) when there is nobody: an instance
// whose only administrator is disabled still accepts registrations, it just has
// nobody to notify.
func (s *Store) ListApprovalRecipients(ctx context.Context) ([]User, error) {
	roles := []string{string(RoleAdmin), string(RoleMaintainer)}
	rows, err := s.pool.Query(ctx, listApprovalRecipientsQuery, roles)
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
