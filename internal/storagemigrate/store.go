package storagemigrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Item is one photo the migration has yet to move: everything needed to write
// its original into the object store, and nothing more. FileMIME may be empty
// for a row imported before media types were recorded.
type Item struct {
	// UID is the photo's primary key, and the order the migration walks in.
	UID string
	// FilePath is the original's path relative to the storage root, which is also
	// its object key: the layouts are identical, so nothing is re-keyed.
	FilePath string
	// FileHash is the SHA256 the catalogue holds for the original's content.
	FileHash string
	// FileSize is the original's size in bytes.
	FileSize int64
	// FileMIME is the media type the object is served as.
	FileMIME string
}

// Progress is the catalogue-wide state of the move, read straight out of the
// photos table. It is what makes a second invocation of the command a status
// report as much as a resume: Pending and PendingBytes are exactly the work a
// run would still have to do.
type Progress struct {
	// Total is the number of photos in the catalogue.
	Total int64
	// Migrated is how many of them are confirmed present in the object store.
	Migrated int64
	// Pending is Total minus Migrated.
	Pending int64
	// PendingBytes is the summed original size of the pending photos. Thumbnails
	// are not counted: how many of them exist is a question only the local cache
	// can answer, and it is answered per photo as the run reaches it.
	PendingBytes int64
}

// Store is the catalogue side of the migration, backed by the shared pgx pool.
// It owns no connection.
type Store struct {
	pool *pgxpool.Pool
}

// compile-time assertion that *Store satisfies the Catalogue the Migrator wants.
var _ Catalogue = (*Store)(nil)

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// progressSQL tallies the whole catalogue in one pass: how many photos there
// are, how many are already in the object store, and how many bytes of originals
// the rest still hold.
const progressSQL = `
SELECT count(*),
       count(*) FILTER (WHERE storage_migrated_at IS NOT NULL),
       coalesce(sum(file_size) FILTER (WHERE storage_migrated_at IS NULL), 0)
FROM photos`

// Progress returns the catalogue-wide state of the migration.
func (s *Store) Progress(ctx context.Context) (Progress, error) {
	var progress Progress
	err := s.pool.QueryRow(ctx, progressSQL).
		Scan(&progress.Total, &progress.Migrated, &progress.PendingBytes)
	if err != nil {
		return Progress{}, fmt.Errorf("storagemigrate: reading progress: %w", err)
	}
	progress.Pending = progress.Total - progress.Migrated
	return progress, nil
}

// pendingBatchSQL reads the next page of photos that are not known to be in the
// object store. The uid cursor is what makes the paging safe while the run is
// stamping rows behind it: without it a photo that failed would be handed out
// again by the very next batch, forever.
const pendingBatchSQL = `
SELECT uid, file_path, file_hash, file_size, file_mime
FROM photos
WHERE storage_migrated_at IS NULL AND uid > $1
ORDER BY uid
LIMIT $2`

// PendingBatch returns up to limit photos that are not yet confirmed in the
// object store, in uid order, starting strictly after cursor (pass "" for the
// first page). A non-positive limit means DefaultBatchSize. An exhausted work
// list yields a non-nil, empty slice.
func (s *Store) PendingBatch(ctx context.Context, cursor string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = DefaultBatchSize
	}
	rows, err := s.pool.Query(ctx, pendingBatchSQL, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: listing pending photos: %w", err)
	}
	defer rows.Close()

	items := make([]Item, 0, limit)
	for rows.Next() {
		var item Item
		if err := rows.Scan(
			&item.UID, &item.FilePath, &item.FileHash, &item.FileSize, &item.FileMIME,
		); err != nil {
			return nil, fmt.Errorf("storagemigrate: scanning pending photo: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storagemigrate: iterating pending photos: %w", err)
	}
	return items, nil
}

// markMigratedSQL stamps a photo as present in the object store. The predicate
// makes a second stamp a no-op rather than a lie about when it landed.
const markMigratedSQL = `
UPDATE photos SET storage_migrated_at = now()
WHERE uid = $1 AND storage_migrated_at IS NULL`

// MarkMigrated records that every object of the photo identified by uid is in
// the object store, verified. It is the commit the local original may only be
// deleted after, and it is idempotent: stamping an already-stamped photo, or one
// that vanished from the catalogue mid-run, is not an error.
func (s *Store) MarkMigrated(ctx context.Context, uid string) error {
	if _, err := s.pool.Exec(ctx, markMigratedSQL, uid); err != nil {
		return fmt.Errorf("storagemigrate: marking photo %s migrated: %w", uid, err)
	}
	return nil
}
