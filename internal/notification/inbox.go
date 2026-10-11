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

// inboxWhere is the predicate of every inbox read: the owner's listed rows,
// optionally only the unread ones. $1 is the owner, $2 the unread switch.
const inboxWhere = `n.user_uid = $1 AND n.in_app AND (NOT $2 OR n.read_at IS NULL)`

// listSQL reads one page of an inbox, newest first; the uid breaks a tie in
// created_at so a page boundary never repeats or skips a row.
const listSQL = `SELECT ` + selectColumns + `
FROM notifications n
WHERE ` + inboxWhere + `
ORDER BY n.created_at DESC, n.uid DESC
LIMIT $3 OFFSET $4`

// countSQL counts the selection a listSQL page is taken from.
const countSQL = `SELECT count(*) FROM notifications n WHERE ` + inboxWhere

// List returns one page of ownerUID's inbox — the notifications delivered
// in-app, newest first — and how many the selection holds. A record that only
// backs a push or an e-mail (InApp false) is never listed. An account with an
// empty inbox, or no account at all, yields an empty, non-nil page.
func (s *Store) List(ctx context.Context, ownerUID string, opts ListOptions) (Page, error) {
	opts = opts.normalized()
	page := Page{Notifications: []Notification{}, Limit: opts.Limit, Offset: opts.Offset}
	if err := s.pool.QueryRow(ctx, countSQL, ownerUID, opts.Unread).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("notification: counting the inbox of %s: %w", ownerUID, err)
	}
	rows, err := s.pool.Query(ctx, listSQL, ownerUID, opts.Unread, opts.Limit, opts.Offset)
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

// UnreadCount returns how many notifications in ownerUID's inbox are unread —
// the number on the bell. Records outside the inbox do not count.
func (s *Store) UnreadCount(ctx context.Context, ownerUID string) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, countSQL, ownerUID, true).Scan(&count); err != nil {
		return 0, fmt.Errorf("notification: counting the unread of %s: %w", ownerUID, err)
	}
	return count, nil
}

// markAllReadSQL stamps every unread inbox notification of the owner read.
const markAllReadSQL = `
UPDATE notifications n SET read_at = now()
WHERE ` + inboxWhere

// MarkAllRead marks every unread notification in ownerUID's inbox read and
// returns how many it marked. Already-read ones keep the moment they were
// first read, and records outside the inbox are left alone — "read everything"
// is about the list the owner is looking at.
func (s *Store) MarkAllRead(ctx context.Context, ownerUID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, markAllReadSQL, ownerUID, true)
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
