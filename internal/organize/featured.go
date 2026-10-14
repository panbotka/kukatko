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

// ErrNotFeatured indicates an album that is not featured right now — its
// feature lapsed, was switched off, or never was — or no album at all.
var ErrNotFeatured = errors.New("organize: album is not featured")

// Featuring is one album newly put on the homepage: the album and the account
// that featured it (empty for a system caller).
type Featuring struct {
	// AlbumUID is the album that was featured.
	AlbumUID string
	// ActorUID is the account that featured it, or empty for none.
	ActorUID string
}

// FeatureObserver hears that an album was freshly featured — put on the
// homepage while it was not there — inside the transaction that featured it,
// so whatever it records commits with the feature or not at all. Prolonging
// or shortening a running feature is not reported: the album is already on
// the strip and nobody should hear about it twice. It is how the
// "a new album is featured" notification (internal/albumfeaturedjob) learns
// about a feature without the HTTP handler having to remember it.
//
// An error it returns fails the feature.
type FeatureObserver interface {
	// Featured hears that f.AlbumUID is now on the homepage.
	Featured(ctx context.Context, tx pgx.Tx, f Featuring) error
}

// WithFeatureObserver returns a copy of s that reports every fresh feature to
// observer, within the transaction of the change. The copy shares the pool; s
// itself is unchanged.
func (s *Store) WithFeatureObserver(observer FeatureObserver) *Store {
	out := *s
	out.features = observer
	return &out
}

// FeaturedAlbum is one album on the homepage's "Doporučujeme" strip: the album
// (whose FeaturedUntil is always set here, since only active features are
// listed), when it was featured, how many photos it holds and what to draw for
// it.
//
// What to draw is exactly what the album index offers for the same album
// (AlbumSummary): the cover candidates in the index's order, from which the
// frontend draws the same 2 × 2 collage as the album's tile, and the single
// cover — the hand-picked one, else the first candidate — with the photo's
// file hash and BlurHash so the caller can mint a thumbnail URL and paint a
// placeholder without fetching the photo.
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
	// CoverUIDs are the album's first CoverCandidates visible photos, in the
	// album index's order (coverCandidateOrder) — the same list
	// AlbumSummary.CoverUIDs carries, and like it never holding a hand-picked
	// cover. Empty for an album with no visible photo.
	CoverUIDs []string `json:"cover_uids,omitempty"`
	// CoverFileHash is the cover's SHA256, the thumbnail cache key. It never
	// leaves the server: the API turns it into a thumbnail URL.
	CoverFileHash string `json:"-"`
	// CoverFilePath is the cover's original key, whose storage prefix is the
	// prefix of its thumbnails. It never leaves the server either.
	CoverFilePath string `json:"-"`
	// CoverBlurhash is the cover's blurred placeholder, nil when it has none.
	CoverBlurhash *string `json:"cover_blurhash,omitempty"`
	// FeaturedBy is the account that last featured the album, nil when it is
	// not known (a system caller, or the account was deleted).
	FeaturedBy *string `json:"featured_by,omitempty"`
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
WHERE uid = $1 AND project_id = $4
RETURNING ` + albumColumns

// unfeatureAlbumSQL ends an album's feature, clearing all three columns so the
// row reads exactly like a never-featured album.
const unfeatureAlbumSQL = `
UPDATE albums SET featured_until = NULL, featured_at = NULL, featured_by = NULL
WHERE uid = $1 AND project_id = $2
RETURNING ` + albumColumns

// FeatureAlbumAudited features the album of the project identified by uid on the
// project's homepage until the given instant — or extends/shortens a running
// feature to it — on behalf of the user byUID (empty for none), and writes entry
// in the same transaction. entry's TargetUID defaults to uid and its ProjectUID
// to the project. It returns ErrAlbumNotFound for a missing album (or one of
// another project) and ErrNotFeaturable for one that is not a hand-made album;
// either way no audit row is written. Validating until (in the future, not too far) is the
// caller's job: the store records whatever instant it is given. A feature
// observer (WithFeatureObserver) hears the change only when the album was not
// featured already — re-dating a running feature is silent.
func (s *Store) FeatureAlbumAudited(
	ctx context.Context, projectUID, uid string, until time.Time, byUID string, entry audit.Entry,
) (Album, error) {
	if err := requireProject(projectUID); err != nil {
		return Album{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	var out Album
	err := s.inAuditedTx(ctx, withProject(entry, projectUID), func(tx pgx.Tx) error {
		wasFeatured, err := requireFeaturable(ctx, tx, projectUID, uid)
		if err != nil {
			return err
		}
		a, err := scanAlbum(tx.QueryRow(ctx, featureAlbumSQL, uid, until, optionalUID(byUID), projectUID))
		if err != nil {
			return fmt.Errorf("organize: featuring album %s: %w", uid, err)
		}
		out = a
		if wasFeatured || s.features == nil {
			return nil
		}
		if err := s.features.Featured(ctx, tx, Featuring{AlbumUID: uid, ActorUID: byUID}); err != nil {
			return fmt.Errorf("organize: reporting the feature of album %s: %w", uid, err)
		}
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
// it may be featured: ErrAlbumNotFound when no album of the project has the uid,
// and
// ErrNotFeaturable when it is not a hand-made one. Asking first rather than
// letting the CHECK fire is what gives the caller a typed error to map. It also
// reports whether the album is featured right now, read under the same lock,
// so the caller knows whether the feature it is about to write is a fresh one.
func requireFeaturable(ctx context.Context, tx pgx.Tx, projectUID, uid string) (bool, error) {
	var typ AlbumType
	var featured bool
	err := tx.QueryRow(ctx,
		"SELECT type, COALESCE(featured_until > now(), false) FROM albums "+
			"WHERE uid = $1 AND project_id = $2 FOR UPDATE", uid, projectUID,
	).Scan(&typ, &featured)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrAlbumNotFound
	}
	if err != nil {
		return false, fmt.Errorf("organize: reading album %s: %w", uid, err)
	}
	if typ != AlbumManual {
		return false, fmt.Errorf("%w: type %q", ErrNotFeaturable, typ)
	}
	return featured, nil
}

// UnfeatureAlbumAudited takes the album of the project identified by uid off the
// homepage and writes entry in the same transaction. entry's TargetUID defaults
// to uid and its ProjectUID to the project. Unfeaturing an album that is not
// featured is a no-op that still succeeds (the request said "make sure it is not
// featured", and it is not). A missing (or foreign) album returns
// ErrAlbumNotFound and writes no audit row.
func (s *Store) UnfeatureAlbumAudited(
	ctx context.Context, projectUID, uid string, entry audit.Entry,
) (Album, error) {
	if err := requireProject(projectUID); err != nil {
		return Album{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	var out Album
	err := s.inAuditedTx(ctx, withProject(entry, projectUID), func(tx pgx.Tx) error {
		a, err := scanAlbum(tx.QueryRow(ctx, unfeatureAlbumSQL, uid, projectUID))
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

// listFeaturedAlbumsSQL reads every album of the project $4 featured right now —
// or, with $3 set, just the one with that uid, whatever its project ($4 NULL:
// the job loader) — newest feature first, with its photo count, its cover
// candidates and its cover. Every photo it counts or draws is one of the album's
// own project.
//
// The featured CTE is the whole selection — active, hand-made, private only
// when $1 allows it, and one uid when $3 names it — and everything after it is restricted to that handful of
// albums up front: one aggregation over the membership of the featured albums
// only, never the library, yields both the count and the candidates. The count
// agrees with the album index's badge (live photos, one per stack). The
// candidates are the album index's own — the same array_agg in the same
// coverCandidateOrder, cut to $2 — narrowed by coverVisible, so a photo hidden
// from the library never headlines the homepage. The derived cover is the first
// candidate (an aggregate, never a correlated ORDER BY … LIMIT 1, see
// docs/PERF.md), and a hand-picked cover wins over it.
const listFeaturedAlbumsSQL = `
WITH featured AS (
    SELECT uid, project_id FROM albums
    WHERE featured_until > now() AND type = 'album' AND ($1::boolean OR NOT private)
      AND ($3::text IS NULL OR uid = $3)
      AND ($4::text IS NULL OR project_id = $4)
),
members AS (
    SELECT ap.album_uid,
           COUNT(*) AS photo_count,
           (array_agg(p.uid ORDER BY ` + coverCandidateOrder + `)
                FILTER (WHERE ` + coverVisible + `))[1:$2::int] AS cover_uids
    FROM album_photos ap
    JOIN featured f ON f.uid = ap.album_uid
    JOIN photos p ON p.uid = ap.photo_uid AND p.project_id = f.project_id
    WHERE p.archived_at IS NULL AND (p.stack_uid IS NULL OR p.stack_primary)
    GROUP BY ap.album_uid
)
SELECT a.uid, a.slug, a.title, a.description, a.type, a.cover_photo_uid,
       a.private, a.created_by, a.created_at, a.updated_at, a.featured_until,
       COALESCE(a.featured_at, a.updated_at),
       COALESCE(m.photo_count, 0),
       COALESCE(m.cover_uids, '{}'),
       COALESCE(picked.uid, derived.uid),
       COALESCE(picked.file_hash, derived.file_hash),
       COALESCE(picked.file_path, derived.file_path),
       CASE WHEN picked.uid IS NOT NULL THEN picked.blurhash ELSE derived.blurhash END,
       a.featured_by
FROM albums a
JOIN featured f ON f.uid = a.uid
LEFT JOIN members m ON m.album_uid = a.uid
LEFT JOIN photos picked ON picked.uid = a.cover_photo_uid AND picked.project_id = a.project_id
LEFT JOIN photos derived ON derived.uid = m.cover_uids[1] AND derived.project_id = a.project_id
ORDER BY a.featured_at DESC NULLS LAST, a.uid`

// ListFeaturedAlbums returns the albums of the project featured on its homepage
// right now, newest feature first. An album whose feature has lapsed is simply absent —
// expiry is read through the clock, nothing has to clear it. Private albums are
// included only when includePrivate is true (the caller decides who may see
// them). Nothing featured yields an empty slice and a nil error.
func (s *Store) ListFeaturedAlbums(
	ctx context.Context, projectUID string, includePrivate bool,
) ([]FeaturedAlbum, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return listFeatured(ctx, s.pool, includePrivate, nil, &projectUID)
}

// FeaturedAlbumTx reads, on tx, the album identified by uid as the homepage
// strip would list it — the same record ListFeaturedAlbums returns, private or
// not — and ErrNotFeatured when the strip would not list it: the feature
// lapsed or was switched off, or no album has the uid. It is for a caller
// acting on a feature after the fact (the album_featured notification job)
// that must see the album as it stands in its own transaction.
//
// It is the explicitly unscoped job loader of the package: a job has no request
// and so no project, and the album uid its payload carries — written by the
// feature's own transaction — is the whole key. The album's photos are still
// read only from its own project.
func (s *Store) FeaturedAlbumTx(ctx context.Context, tx pgx.Tx, uid string) (FeaturedAlbum, error) {
	list, err := listFeatured(ctx, tx, true, &uid, nil)
	if err != nil {
		return FeaturedAlbum{}, err
	}
	if len(list) == 0 {
		return FeaturedAlbum{}, ErrNotFeatured
	}
	return list[0], nil
}

// listFeatured runs listFeaturedAlbumsSQL on q: every featured album of the
// project projectUID names, or only the one uid names, private ones only when
// includePrivate is set.
func listFeatured(
	ctx context.Context, q querier, includePrivate bool, uid, projectUID *string,
) ([]FeaturedAlbum, error) {
	rows, err := q.Query(ctx, listFeaturedAlbumsSQL, includePrivate, CoverCandidates, uid, projectUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing featured albums: %w", err)
	}
	defer rows.Close()

	out := make([]FeaturedAlbum, 0)
	for rows.Next() {
		var fa FeaturedAlbum
		var fileHash, filePath *string
		if err := rows.Scan(
			&fa.UID, &fa.Slug, &fa.Title, &fa.Description, &fa.Type, &fa.CoverPhotoUID,
			&fa.Private, &fa.CreatedBy, &fa.CreatedAt, &fa.UpdatedAt, &fa.FeaturedUntil, &fa.FeaturedAt,
			&fa.PhotoCount, &fa.CoverUIDs, &fa.CoverUID, &fileHash, &filePath, &fa.CoverBlurhash, &fa.FeaturedBy,
		); err != nil {
			return nil, fmt.Errorf("organize: scanning featured album: %w", err)
		}
		fa.CoverFileHash = deref(fileHash)
		fa.CoverFilePath = deref(filePath)
		out = append(out, fa)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating featured albums: %w", err)
	}
	return out, nil
}
