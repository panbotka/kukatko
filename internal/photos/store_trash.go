package photos

import (
	"context"
	"fmt"
	"time"
)

// ListArchivedUIDs returns the UIDs of archived photos (archived_at IS NOT NULL)
// ordered oldest-archived first, limited and offset for batched purging.
//
// When before is non-nil only photos whose archived_at is at or before it are
// returned — the retention cutoff used by the scheduled purge. When before is
// nil every archived photo qualifies, which backs the "empty trash" action. A
// non-positive limit falls back to defaultListLimit. The slice is empty (not
// nil) when nothing matches.
func (s *Store) ListArchivedUIDs(ctx context.Context, before *time.Time, limit, offset int) ([]string, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if offset < 0 {
		offset = 0
	}

	q := "SELECT uid FROM photos WHERE archived_at IS NOT NULL"
	args := []any{}
	if before != nil {
		args = append(args, *before)
		q += fmt.Sprintf(" AND archived_at <= $%d", len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY archived_at, uid LIMIT $%d", len(args))
	args = append(args, offset)
	q += fmt.Sprintf(" OFFSET $%d", len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: querying archived photos: %w", err)
	}
	defer rows.Close()

	uids := make([]string, 0, limit)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("photos: scanning archived uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating archived uids: %w", err)
	}
	return uids, nil
}
