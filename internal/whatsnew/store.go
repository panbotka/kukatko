package whatsnew

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store produces the "what's new" digest over the shared pgx pool. It owns no
// connection; it borrows the pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
	gap  time.Duration
}

// NewStore returns a Store backed by pool, using [VisitGap] as the inactivity
// threshold that starts a new visit. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, gap: VisitGap}
}

// WithGap returns a copy of s that treats gap, rather than [VisitGap], as the
// inactivity threshold. It exists for tests, which cannot wait six hours to
// observe a visit rotation; production wiring uses NewStore.
func (s *Store) WithGap(gap time.Duration) *Store {
	return &Store{pool: s.pool, gap: gap}
}

// rotateVisitSQL stamps the heartbeat and, when the previous read is at least a
// gap old, rotates the reference point onto it. The whole decision is made in
// this one statement so that two tabs loading the library at the same instant
// cannot both rotate: the second waits on the row lock, then re-reads a
// last_seen_at that is already now and leaves the reference alone.
//
// $3 is the cutoff (now minus the gap) rather than an interval, so the
// comparison is a plain timestamp one and the gap stays a Go-side decision.
// RETURNING reads the post-update reference, which is exactly the "since" of the
// digest about to be built.
const rotateVisitSQL = `
UPDATE users
SET last_seen_at = $2,
    visit_reference_at = CASE
        WHEN last_seen_at IS NOT NULL AND last_seen_at <= $3 THEN last_seen_at
        ELSE visit_reference_at
    END
WHERE uid = $1
RETURNING visit_reference_at`

// rotateVisit records that userUID is here at now and returns the reference
// point of the visit this read belongs to.
//
// The returned time is zero when the account has no reference point yet, which
// is every account's first read: there is no "away" to report on. It returns
// ErrUserNotFound when no row matched, i.e. the account was deleted between
// authenticating the request and stamping the visit.
func (s *Store) rotateVisit(ctx context.Context, userUID string, now time.Time) (time.Time, error) {
	var reference *time.Time
	err := s.pool.QueryRow(ctx, rotateVisitSQL, userUID, now, now.Add(-s.gap)).Scan(&reference)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrUserNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("whatsnew: stamping visit: %w", err)
	}
	if reference == nil {
		return time.Time{}, nil
	}
	return *reference, nil
}

// countsSQL counts everything the digest reports, in one round trip. Each
// subquery is a range over a creation timestamp served by its own index
// (0053_user_visits, and idx_photos_live_created_at from 0015).
//
// The photo predicate is the library grid's own base filter — live, not hidden,
// and only the primary of a stack — so the number the panel prints is the number
// of tiles the "new photos" link actually opens. Counting raw rows here would
// promise photos that the destination then silently drops.
const countsSQL = `
SELECT
    (SELECT count(*) FROM photos
        WHERE created_at > $1
          AND archived_at IS NULL
          AND (stack_uid IS NULL OR stack_primary)
          AND NOT hidden_from_library),
    (SELECT count(*) FROM photo_comments
        WHERE created_at > $1 AND deleted_at IS NULL),
    (SELECT count(*) FROM albums
        WHERE created_at > $1 AND type = 'album'),
    (SELECT count(*) FROM subjects
        WHERE created_at > $1 AND name <> '')`

// countSince returns how many photos, comments, hand-curated albums and named
// people were created after since.
func (s *Store) countSince(ctx context.Context, since time.Time) (counts, error) {
	var c counts
	if err := s.pool.QueryRow(ctx, countsSQL, since).
		Scan(&c.photos, &c.comments, &c.albums, &c.people); err != nil {
		return counts{}, fmt.Errorf("whatsnew: counting changes: %w", err)
	}
	return c, nil
}

// listAlbumsSQL names the newest hand-curated albums created since the reference
// point. Auto-generated groupings (folder, moment, month, state) are excluded:
// an import mints them by the hundred and none of them is somebody's news.
const listAlbumsSQL = `
SELECT uid, title
FROM albums
WHERE created_at > $1 AND type = 'album'
ORDER BY created_at DESC
LIMIT $2`

// listAlbums returns up to [MaxItems] albums created after since, newest first.
func (s *Store) listAlbums(ctx context.Context, since time.Time) ([]Album, error) {
	rows, err := s.pool.Query(ctx, listAlbumsSQL, since, MaxItems)
	if err != nil {
		return nil, fmt.Errorf("whatsnew: listing new albums: %w", err)
	}
	albums, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Album, error) {
		var a Album
		if err := row.Scan(&a.UID, &a.Title); err != nil {
			return Album{}, fmt.Errorf("whatsnew: scanning album row: %w", err)
		}
		return a, nil
	})
	if err != nil {
		return nil, fmt.Errorf("whatsnew: reading new albums: %w", err)
	}
	return albums, nil
}

// listPeopleSQL names the newest named subjects created since the reference
// point. A subject with no name is recognition work in progress, not an
// announcement, so it is excluded — which is also what makes the wording
// "newly named people" true.
const listPeopleSQL = `
SELECT uid, name
FROM subjects
WHERE created_at > $1 AND name <> ''
ORDER BY created_at DESC
LIMIT $2`

// listPeople returns up to [MaxItems] named subjects created after since,
// newest first.
func (s *Store) listPeople(ctx context.Context, since time.Time) ([]Person, error) {
	rows, err := s.pool.Query(ctx, listPeopleSQL, since, MaxItems)
	if err != nil {
		return nil, fmt.Errorf("whatsnew: listing new people: %w", err)
	}
	people, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		if err := row.Scan(&p.UID, &p.Name); err != nil {
			return Person{}, fmt.Errorf("whatsnew: scanning person row: %w", err)
		}
		return p, nil
	})
	if err != nil {
		return nil, fmt.Errorf("whatsnew: reading new people: %w", err)
	}
	return people, nil
}

// Summary stamps userUID's visit at now and returns the digest of everything
// that happened since the reference point of that visit.
//
// The returned Summary has HasNews false — and nothing else set — in the two
// cases where the panel must not appear: the account's first-ever read, which
// has no reference point, and a visit that found nothing new. Reading the
// summary is what advances the visit bookkeeping, so it happens even when the
// digest turns out empty.
//
// It returns ErrUserNotFound when the account no longer exists.
func (s *Store) Summary(ctx context.Context, userUID string, now time.Time) (Summary, error) {
	since, err := s.rotateVisit(ctx, userUID, now)
	if err != nil {
		return Summary{}, err
	}
	if since.IsZero() {
		return Summary{}, nil
	}
	c, err := s.countSince(ctx, since)
	if err != nil {
		return Summary{}, err
	}
	if c.empty() {
		return Summary{}, nil
	}
	albums, people, err := s.lists(ctx, since, c)
	if err != nil {
		return Summary{}, err
	}
	return newSummary(since, c, albums, people), nil
}

// lists fetches the album and people links the digest names, skipping either
// query when its count already said there is nothing to name.
func (s *Store) lists(ctx context.Context, since time.Time, c counts) ([]Album, []Person, error) {
	var (
		albums []Album
		people []Person
		err    error
	)
	if c.albums > 0 {
		if albums, err = s.listAlbums(ctx, since); err != nil {
			return nil, nil, err
		}
	}
	if c.people > 0 {
		if people, err = s.listPeople(ctx, since); err != nil {
			return nil, nil, err
		}
	}
	return albums, people, nil
}
