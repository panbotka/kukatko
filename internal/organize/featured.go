package organize

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// ErrNotFeaturable indicates an attempt to feature an album that is not a
// hand-made one: only type "album" may appear on the homepage (the machine-made
// folders, months, moments and places are not something one points the whole
// family at). Migration 0094 enforces the same rule with a CHECK.
var ErrNotFeaturable = errors.New("organize: only a hand-made album can be featured")

// FeaturedAlbum is one album on the homepage's "Doporučujeme" strip: the album
// (whose FeaturedUntil is always set here, since only active features are
// listed), when it was featured, how many photos it holds and the photo that
// stands for it.
//
// The cover is resolved exactly as AlbumCovers resolves it — the hand-picked one,
// else the newest visible member — and carries the photo's file hash and
// BlurHash so the caller can mint a thumbnail URL and paint a placeholder
// without fetching the photo.
type FeaturedAlbum struct {
	Album
	// FeaturedAt is when the running feature began. Extending a feature keeps it;
	// it orders the strip, newest first.
	FeaturedAt time.Time `json:"featured_at"`
	// PhotoCount is the number of photos in the album, counted as the album
	// index counts them (live photos, one per stack).
	PhotoCount int `json:"photo_count"`
	// CoverUID is the photo standing for the album, nil for an album with
	// nothing to show.
	CoverUID *string `json:"cover_uid,omitempty"`
	// CoverFileHash is the cover's SHA256, the thumbnail cache key. It never
	// leaves the server: the API turns it into a thumbnail URL.
	CoverFileHash string `json:"-"`
	// CoverBlurhash is the cover's blurred placeholder, nil when it has none.
	CoverBlurhash *string `json:"cover_blurhash,omitempty"`
}

// featureAlbumSQL starts or extends an album's feature. A feature still running
// keeps its featured_at, so prolonging it does not move the album ahead of ones
// featured later; a lapsed or absent one starts afresh at now(). featured_by
// always records the latest hand on it.
const featureAlbumSQL = `
UPDATE albums SET
    featured_at = CASE WHEN featured_until > now() THEN featured_at ELSE now() END,
    featured_until = $2,
    featured_by = $3
WHERE uid = $1
RETURNING ` + albumColumns

// unfeatureAlbumSQL ends an album's feature, clearing all three columns so the
// row reads exactly like a never-featured album.
const unfeatureAlbumSQL = `
UPDATE albums SET featured_until = NULL, featured_at = NULL, featured_by = NULL
WHERE uid = $1
RETURNING ` + albumColumns

// FeatureAlbumAudited features the album identified by uid on the homepage until
// the given instant — or extends/shortens a running feature to it — on behalf
// of the user byUID (empty for none), and writes entry in the same transaction.
// entry's TargetUID defaults to uid. It returns ErrAlbumNotFound for a missing
// album and ErrNotFeaturable for one that is not a hand-made album; either way
// no audit row is written. Validating until (in the future, not too far) is the
// caller's job: the store records whatever instant it is given.
func (s *Store) FeatureAlbumAudited(
	ctx context.Context, uid string, until time.Time, byUID string, entry audit.Entry,
) (Album, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	var out Album
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		if err := requireFeaturable(ctx, tx, uid); err != nil {
			return err
		}
		a, err := scanAlbum(tx.QueryRow(ctx, featureAlbumSQL, uid, until, optionalUID(byUID)))
		if err != nil {
			return fmt.Errorf("organize: featuring album %s: %w", uid, err)
		}
		out = a
		return nil
	})
	if err != nil {
		return Album{}, err
	}
	return out, nil
}

// optionalUID maps an empty user uid to NULL, so a request without an acting
// user (a pass-through guard in a test) records no featured_by rather than
// violating the users foreign key with "".
func optionalUID(uid string) *string {
	if uid == "" {
		return nil
	}
	return &uid
}

// requireFeaturable locks the album row for the rest of tx and reports whether
// it may be featured: ErrAlbumNotFound when no album has the uid, and
// ErrNotFeaturable when it is not a hand-made one. Asking first rather than
// letting the CHECK fire is what gives the caller a typed error to map.
func requireFeaturable(ctx context.Context, tx pgx.Tx, uid string) error {
	var typ AlbumType
	err := tx.QueryRow(ctx, "SELECT type FROM albums WHERE uid = $1 FOR UPDATE", uid).Scan(&typ)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlbumNotFound
	}
	if err != nil {
		return fmt.Errorf("organize: reading album %s: %w", uid, err)
	}
	if typ != AlbumManual {
		return fmt.Errorf("%w: type %q", ErrNotFeaturable, typ)
	}
	return nil
}

// UnfeatureAlbumAudited takes the album identified by uid off the homepage and
// writes entry in the same transaction. entry's TargetUID defaults to uid.
// Unfeaturing an album that is not featured is a no-op that still succeeds (the
// request said "make sure it is not featured", and it is not). A missing album
// returns ErrAlbumNotFound and writes no audit row.
func (s *Store) UnfeatureAlbumAudited(ctx context.Context, uid string, entry audit.Entry) (Album, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	var out Album
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		a, err := scanAlbum(tx.QueryRow(ctx, unfeatureAlbumSQL, uid))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlbumNotFound
		}
		if err != nil {
			return fmt.Errorf("organize: unfeaturing album %s: %w", uid, err)
		}
		out = a
		return nil
	})
	if err != nil {
		return Album{}, err
	}
	return out, nil
}

// listFeaturedAlbumsSQL reads every album featured right now, newest feature
// first, with its photo count and its cover.
//
// The featured CTE is the whole selection — active, hand-made, and private only
// when $1 allows it — and everything after it is restricted to that handful of
// albums up front: the count and the derived cover each scan the membership of
// the featured albums only, never the library. The derived cover is the same
// DISTINCT ON albumCoversSQL uses (never a correlated ORDER BY … LIMIT 1, see
// docs/PERF.md), and a hand-picked cover wins over it. The count agrees with the
// album index's badge: live photos, one per stack.
const listFeaturedAlbumsSQL = `
WITH featured AS (
    SELECT uid FROM albums
    WHERE featured_until > now() AND type = 'album' AND ($1::boolean OR NOT private)
),
counts AS (
    SELECT ap.album_uid, COUNT(*) AS photo_count
    FROM album_photos ap
    JOIN photos p ON p.uid = ap.photo_uid
    WHERE ap.album_uid IN (SELECT uid FROM featured)
      AND p.archived_at IS NULL AND (p.stack_uid IS NULL OR p.stack_primary)
    GROUP BY ap.album_uid
),
derived AS (
    SELECT DISTINCT ON (ap.album_uid) ap.album_uid, p.uid AS photo_uid, p.file_hash, p.blurhash
    FROM album_photos ap
    JOIN photos p ON p.uid = ap.photo_uid
    WHERE ap.album_uid IN (SELECT uid FROM featured) AND ` + coverVisible + `
    ORDER BY ap.album_uid, ` + coverOrder + `
)
SELECT a.uid, a.slug, a.title, a.description, a.type, a.cover_photo_uid,
       a.private, a.created_by, a.created_at, a.updated_at, a.featured_until,
       COALESCE(a.featured_at, a.updated_at),
       COALESCE(c.photo_count, 0),
       COALESCE(picked.uid, d.photo_uid),
       COALESCE(picked.file_hash, d.file_hash),
       CASE WHEN picked.uid IS NOT NULL THEN picked.blurhash ELSE d.blurhash END
FROM albums a
JOIN featured f ON f.uid = a.uid
LEFT JOIN counts c ON c.album_uid = a.uid
LEFT JOIN photos picked ON picked.uid = a.cover_photo_uid
LEFT JOIN derived d ON d.album_uid = a.uid
ORDER BY a.featured_at DESC NULLS LAST, a.uid`

// ListFeaturedAlbums returns the albums featured on the homepage right now,
// newest feature first. An album whose feature has lapsed is simply absent —
// expiry is read through the clock, nothing has to clear it. Private albums are
// included only when includePrivate is true (the caller decides who may see
// them). Nothing featured yields an empty slice and a nil error.
func (s *Store) ListFeaturedAlbums(ctx context.Context, includePrivate bool) ([]FeaturedAlbum, error) {
	rows, err := s.pool.Query(ctx, listFeaturedAlbumsSQL, includePrivate)
	if err != nil {
		return nil, fmt.Errorf("organize: listing featured albums: %w", err)
	}
	defer rows.Close()

	out := make([]FeaturedAlbum, 0)
	for rows.Next() {
		var fa FeaturedAlbum
		var fileHash *string
		if err := rows.Scan(
			&fa.UID, &fa.Slug, &fa.Title, &fa.Description, &fa.Type, &fa.CoverPhotoUID,
			&fa.Private, &fa.CreatedBy, &fa.CreatedAt, &fa.UpdatedAt, &fa.FeaturedUntil, &fa.FeaturedAt,
			&fa.PhotoCount, &fa.CoverUID, &fileHash, &fa.CoverBlurhash,
		); err != nil {
			return nil, fmt.Errorf("organize: scanning featured album: %w", err)
		}
		fa.CoverFileHash = deref(fileHash)
		out = append(out, fa)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating featured albums: %w", err)
	}
	return out, nil
}
