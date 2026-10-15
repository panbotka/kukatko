package notification

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Inbox paging bounds. The bell's dropdown asks for a handful, the inbox page
// for a screenful at a time.
const (
	// DefaultListLimit is the page size when the caller names none.
	DefaultListLimit = 30
	// MaxListLimit is the largest page a caller may ask for.
	MaxListLimit = 100
)

// ListOptions selects one page of an account's inbox.
type ListOptions struct {
	// Unread keeps only notifications not read yet.
	Unread bool
	// Limit is the page size; non-positive means DefaultListLimit, anything
	// over MaxListLimit is clamped to it.
	Limit int
	// Offset is how many notifications to skip; a negative offset reads as 0.
	Offset int
}

// normalized returns o with Limit and Offset forced into their bounds.
func (o ListOptions) normalized() ListOptions {
	switch {
	case o.Limit <= 0:
		o.Limit = DefaultListLimit
	case o.Limit > MaxListLimit:
		o.Limit = MaxListLimit
	}
	o.Offset = max(o.Offset, 0)
	return o
}

// Page is one page of an inbox and the size of the whole selection.
type Page struct {
	// Notifications is the page, newest first.
	Notifications []Notification
	// Total is how many notifications the selection holds across all pages.
	Total int
	// Limit and Offset are the bounds the page was actually read with.
	Limit  int
	Offset int
}

// inboxWhere is the predicate of every inbox read: the owner's listed rows in
// one project, optionally only the unread ones. $1 is the owner, $2 the unread
// switch, $3 the project.
const inboxWhere = `n.user_uid = $1 AND n.project_id = $3 AND n.in_app AND (NOT $2 OR n.read_at IS NULL)`

// listSQL reads one page of an inbox, newest first; the uid breaks a tie in
// created_at so a page boundary never repeats or skips a row.
const listSQL = `SELECT ` + selectColumns + `
FROM notifications n
WHERE ` + inboxWhere + `
ORDER BY n.created_at DESC, n.uid DESC
LIMIT $4 OFFSET $5`

// countSQL counts the selection a listSQL page is taken from.
const countSQL = `SELECT count(*) FROM notifications n WHERE ` + inboxWhere

// List returns one page of ownerUID's inbox in the project — the notifications
// of that project delivered in-app, newest first — and how many the selection
// holds; another project's notifications are never listed nor counted. A record that only
// backs a push or an e-mail (InApp false) is never listed. An account with an
// empty inbox, or no account at all, yields an empty, non-nil page.
func (s *Store) List(ctx context.Context, projectUID, ownerUID string, opts ListOptions) (Page, error) {
	if projectUID == "" {
		return Page{}, ErrNoProject
	}
	opts = opts.normalized()
	page := Page{Notifications: []Notification{}, Limit: opts.Limit, Offset: opts.Offset}
	if err := s.pool.QueryRow(ctx, countSQL, ownerUID, opts.Unread, projectUID).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("notification: counting the inbox of %s: %w", ownerUID, err)
	}
	rows, err := s.pool.Query(ctx, listSQL, ownerUID, opts.Unread, projectUID, opts.Limit, opts.Offset)
	if err != nil {
		return Page{}, fmt.Errorf("notification: listing the inbox of %s: %w", ownerUID, err)
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return Page{}, err
		}
		page.Notifications = append(page.Notifications, n)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("notification: listing the inbox of %s: %w", ownerUID, err)
	}
	return page, nil
}

// UnreadCount returns how many notifications in ownerUID's inbox in the project
// are unread — the number on the bell. Records outside the inbox, and another
// project's notifications, do not count.
func (s *Store) UnreadCount(ctx context.Context, projectUID, ownerUID string) (int, error) {
	if projectUID == "" {
		return 0, ErrNoProject
	}
	var count int
	if err := s.pool.QueryRow(ctx, countSQL, ownerUID, true, projectUID).Scan(&count); err != nil {
		return 0, fmt.Errorf("notification: counting the unread of %s: %w", ownerUID, err)
	}
	return count, nil
}

// StandingSQL builds the SQL condition that holds when the account userExpr
// may read notifications of the project projectExpr, both SQL expressions of
// the enclosing statement. It is internal/notifystanding's MemberSQL, passed
// in by the caller because that package imports this one for the kinds and
// cannot be imported back.
type StandingSQL func(userExpr, projectExpr string) string

// ProjectUnread is how many unread inbox notifications one project holds for
// an account.
type ProjectUnread struct {
	// ProjectUID is the project the notifications are about.
	ProjectUID string
	// Unread is how many of them are unread; always positive.
	Unread int
}

// unreadByProjectSQL counts an owner's unread inbox notifications ($1) per
// project, keeping only the projects standing lets through: every project at
// once, so the one tenancy predicate is the standing condition's membership,
// not a project parameter.
func unreadByProjectSQL(standing StandingSQL) string {
	return `SELECT n.project_id, count(*)
FROM notifications n
WHERE n.user_uid = $1 AND n.in_app AND n.read_at IS NULL
  AND ` + standing("n.user_uid", "n.project_id") + `
GROUP BY n.project_id
ORDER BY n.project_id`
}

// UnreadByProject returns, for every project in which ownerUID has unread inbox
// notifications and which standing lets through, how many — ordered by project
// uid, a project with nothing unread left out. It is the one inbox read that
// spans projects: the switcher marks the projects with news the reader is not
// working in. A project whose membership was removed, is still pending or
// withheld, or which was deactivated is not in standing, so its notifications
// are neither counted nor is its existence told. A nil standing is an error:
// an unfiltered count would name every project the account was ever notified
// about.
func (s *Store) UnreadByProject(ctx context.Context, ownerUID string, standing StandingSQL) ([]ProjectUnread, error) {
	if standing == nil {
		return nil, ErrNoStanding
	}
	rows, err := s.pool.Query(ctx, unreadByProjectSQL(standing), ownerUID)
	if err != nil {
		return nil, fmt.Errorf("notification: counting the unread of %s per project: %w", ownerUID, err)
	}
	defer rows.Close()
	out := []ProjectUnread{}
	for rows.Next() {
		var p ProjectUnread
		if err := rows.Scan(&p.ProjectUID, &p.Unread); err != nil {
			return nil, fmt.Errorf("notification: scanning the unread of %s per project: %w", ownerUID, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notification: counting the unread of %s per project: %w", ownerUID, err)
	}
	return out, nil
}

// markAllReadSQL stamps every unread inbox notification of the owner read.
const markAllReadSQL = `
UPDATE notifications n SET read_at = now()
WHERE ` + inboxWhere

// MarkAllRead marks every unread notification in ownerUID's inbox in the
// project read and returns how many it marked. Already-read ones keep the
// moment they were first read, and records outside the inbox — or of another
// project — are left alone: "read everything" is about the list the owner is
// looking at.
func (s *Store) MarkAllRead(ctx context.Context, projectUID, ownerUID string) (int64, error) {
	if projectUID == "" {
		return 0, ErrNoProject
	}
	tag, err := s.pool.Exec(ctx, markAllReadSQL, ownerUID, true, projectUID)
	if err != nil {
		return 0, fmt.Errorf("notification: marking the inbox of %s read: %w", ownerUID, err)
	}
	return tag.RowsAffected(), nil
}

// WantedTx returns, read on the caller's open transaction tx, every channel
// through which userUID wants notifications of kind — the stored choice where
// the account made one, the channel's default otherwise — in display order. An
// account that wants none yields an empty slice. An unknown kind is
// ErrUnknownKind.
func (s *Store) WantedTx(ctx context.Context, tx pgx.Tx, userUID string, kind Kind) ([]Channel, error) {
	if !kind.Known() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	prefs, err := readPrefs(ctx, tx, userUID)
	if err != nil {
		return nil, err
	}
	out := []Channel{}
	for _, pref := range prefs {
		if pref.Kind == kind && pref.Enabled {
			out = append(out, pref.Channel)
		}
	}
	return out, nil
}
