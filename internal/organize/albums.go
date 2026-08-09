package organize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// albumColumns is the canonical, ordered column list for album reads, matched by
// scanAlbum.
const albumColumns = "uid, slug, title, description, type, cover_photo_uid, " +
	"private, created_by, created_at, updated_at"

// insertAlbumSQL inserts an album and returns the stored row.
const insertAlbumSQL = `
INSERT INTO albums (uid, slug, title, description, type, cover_photo_uid, private, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING ` + albumColumns

// scanAlbum reads one album row in albumColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrAlbumNotFound).
func scanAlbum(row pgx.Row) (Album, error) {
	var a Album
	if err := row.Scan(
		&a.UID, &a.Slug, &a.Title, &a.Description, &a.Type, &a.CoverPhotoUID,
		&a.Private, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Album{}, fmt.Errorf("organize: scanning album: %w", err)
	}
	return a, nil
}

// scanAlbumCount reads one album-with-count row (the albumColumns list followed
// by a photo_count column) in order, wrapping any scan error. It backs
// SearchAlbums, whose projection matches.
func scanAlbumCount(row pgx.Row) (AlbumCount, error) {
	var ac AlbumCount
	if err := row.Scan(
		&ac.UID, &ac.Slug, &ac.Title, &ac.Description, &ac.Type, &ac.CoverPhotoUID,
		&ac.Private, &ac.CreatedBy, &ac.CreatedAt, &ac.UpdatedAt, &ac.PhotoCount,
	); err != nil {
		return AlbumCount{}, fmt.Errorf("organize: scanning album count: %w", err)
	}
	return ac, nil
}

// scanAlbumSummary reads one album-index row (the albumColumns list followed by
// photo_count, the effective cover and the capture-time bounds) in order,
// wrapping any scan error. It matches listAlbumsSQL's projection.
func scanAlbumSummary(row pgx.Row) (AlbumSummary, error) {
	var as AlbumSummary
	if err := row.Scan(
		&as.UID, &as.Slug, &as.Title, &as.Description, &as.Type, &as.CoverPhotoUID,
		&as.Private, &as.CreatedBy, &as.CreatedAt, &as.UpdatedAt, &as.PhotoCount,
		&as.CoverUID, &as.CoverUIDs, &as.TakenFrom, &as.TakenTo,
	); err != nil {
		return AlbumSummary{}, fmt.Errorf("organize: scanning album summary: %w", err)
	}
	return as, nil
}

// prepareAlbumInsert validates and defaults a's type, ensures it carries a UID,
// and returns the album ready to insert together with the base slug derived from
// its title. It is shared by CreateAlbum and CreateAlbumAudited so both apply
// identical validation and slug derivation. It returns ErrInvalidType for an
// unrecognised type.
func prepareAlbumInsert(a Album) (Album, string, error) {
	if a.Type == "" {
		a.Type = AlbumManual
	}
	if !a.Type.valid() {
		return Album{}, "", fmt.Errorf("%w: %q", ErrInvalidType, a.Type)
	}
	if a.UID == "" {
		uid, err := newAlbumUID()
		if err != nil {
			return Album{}, "", err
		}
		a.UID = uid
	}
	return a, slugify(a.Title, albumFallbackSlug), nil
}

// insertAlbumRow inserts a with the given slug using q (a pool or a transaction)
// and returns the stored row. It underlies both the standalone and audited create
// paths so the insert projection lives in one place.
func insertAlbumRow(ctx context.Context, q rowQuerier, a Album, slug string) (Album, error) {
	return scanAlbum(q.QueryRow(ctx, insertAlbumSQL,
		a.UID, slug, a.Title, a.Description, a.Type, a.CoverPhotoUID, a.Private, a.CreatedBy))
}

// CreateAlbum inserts a and returns it refreshed with the generated UID, unique
// slug and timestamps. The slug is derived from a.Title and a numeric suffix is
// appended on collision. An empty type defaults to AlbumManual; an unrecognised
// type returns ErrInvalidType.
func (s *Store) CreateAlbum(ctx context.Context, a Album) (Album, error) {
	prepared, base, err := prepareAlbumInsert(a)
	if err != nil {
		return Album{}, err
	}
	return insertWithUniqueSlug(base, func(slug string) (Album, error) {
		return insertAlbumRow(ctx, s.pool, prepared, slug)
	})
}

// GetAlbumByUID returns the album with the given UID, or ErrAlbumNotFound.
func (s *Store) GetAlbumByUID(ctx context.Context, uid string) (Album, error) {
	return s.getAlbum(ctx, "uid", uid)
}

// GetAlbumBySlug returns the album with the given slug, or ErrAlbumNotFound.
func (s *Store) GetAlbumBySlug(ctx context.Context, slug string) (Album, error) {
	return s.getAlbum(ctx, "slug", slug)
}

// getAlbum fetches a single album filtered by an equality on the trusted column
// name col (an internal constant, never user input), translating pgx.ErrNoRows
// into ErrAlbumNotFound.
func (s *Store) getAlbum(ctx context.Context, col, val string) (Album, error) {
	q := "SELECT " + albumColumns + " FROM albums WHERE " + col + " = $1"
	a, err := scanAlbum(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Album{}, ErrAlbumNotFound
		}
		return Album{}, err
	}
	return a, nil
}

// updateAlbumSQL rewrites an album's editable fields (including a re-derived slug)
// and returns the refreshed row.
const updateAlbumSQL = `
UPDATE albums SET
    slug = $2, title = $3, description = $4, type = $5,
    cover_photo_uid = $6, private = $7, updated_at = now()
WHERE uid = $1
RETURNING ` + albumColumns

// prepareAlbumUpdate validates and defaults upd's type and returns it together
// with the base slug derived from the new title. It is shared by UpdateAlbum and
// UpdateAlbumAudited. It returns ErrInvalidType for an unrecognised type.
func prepareAlbumUpdate(upd AlbumUpdate) (AlbumUpdate, string, error) {
	if upd.Type == "" {
		upd.Type = AlbumManual
	}
	if !upd.Type.valid() {
		return AlbumUpdate{}, "", fmt.Errorf("%w: %q", ErrInvalidType, upd.Type)
	}
	return upd, slugify(upd.Title, albumFallbackSlug), nil
}

// updateAlbumRow rewrites the album identified by uid with upd and the given slug
// using q (a pool or a transaction), returning the refreshed row (or pgx.ErrNoRows
// when no album matches, which callers translate to ErrAlbumNotFound).
func updateAlbumRow(ctx context.Context, q rowQuerier, uid string, upd AlbumUpdate, slug string) (Album, error) {
	return scanAlbum(q.QueryRow(ctx, updateAlbumSQL,
		uid, slug, upd.Title, upd.Description, upd.Type, upd.CoverPhotoUID, upd.Private))
}

// UpdateAlbum applies upd to the album identified by uid: it re-slugs from the
// new title (kept unique) and rewrites the editable fields. An empty type
// defaults to AlbumManual. It returns ErrAlbumNotFound if no such album exists,
// or ErrInvalidType for a bad type.
func (s *Store) UpdateAlbum(ctx context.Context, uid string, upd AlbumUpdate) (Album, error) {
	prepared, base, err := prepareAlbumUpdate(upd)
	if err != nil {
		return Album{}, err
	}
	updated, err := insertWithUniqueSlug(base, func(slug string) (Album, error) {
		return updateAlbumRow(ctx, s.pool, uid, prepared, slug)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Album{}, ErrAlbumNotFound
	}
	return updated, err
}

// listAlbumsSQL reads every album with its photo count, its effective cover and
// the capture-time span of its photos, newest album first. The album columns are
// alias-qualified because the album_photos join also exposes uid-shaped columns.
//
// The ORDER BY ranks an album by MAX(p.taken_at) — the capture time of its newest
// visible photo — so the album holding the most recent photo leads the index. An
// album whose photos all lack a capture time, and an empty album, aggregate to
// NULL and sort last; uid breaks ties into a total, stable order. Deliberately no
// COALESCE onto created_at: that fallback suits a single photo, but here it would
// date an undated album by when it was uploaded and float it to the top.
//
// One join carries every derived column: photos, reached through album_photos and
// restricted to the live catalogue's visible members (not archived, and not a
// non-primary stack member). Everything else is an aggregate over that one pass.
//
//   - photo_count = COUNT(p.uid). Counting p.uid rather than the membership row
//     makes the badge agree with the grid: a hidden photo joins as a NULL row, so
//     it neither counts nor moves the range. A photo with an unknown capture time
//     drops out of MIN/MAX the same way.
//   - the fallback cover is the album's newest visible photo, with an unknown
//     capture time sorted last (such a photo becomes the cover only when nothing
//     else can) and uid breaking ties. Both keys are total, so the same album
//     returns the same cover on every request. The FILTER drops the NULL rows the
//     LEFT JOIN produces for a hidden photo or an empty album, which then yields
//     no array at all and so no cover. COALESCE lets a hand-picked cover win.
//   - cover_uids is the head of that very same ordered array — the first $1
//     candidates rather than only the first — so the index can draw a collage or
//     step past a cover a neighbouring album already used. It costs nothing
//     extra: the two projections spell the aggregate identically, so the executor
//     computes it once and the slice is a subscript on the result. COALESCE to
//     '{}' keeps an empty album an empty list rather than a NULL.
//
// The cover falling out of the same aggregation is the point, not a flourish: it
// used to be a LATERAL "ORDER BY p2.taken_at DESC LIMIT 1" correlated on a.uid,
// which the planner satisfies by walking the GLOBAL photos.taken_at order and
// probing each row for membership of that one album — so an album whose newest
// photo is old walks most of the library before its first hit, once per album.
// On the production library that was 17.3M buffer hits and 30 s against the 1 016
// buffers and 216 ms this costs; a development library of a few hundred photos
// hides the difference entirely. Never pick a per-group row with a correlated
// ORDER BY … LIMIT 1 here. See docs/PERF.md § "The album index".
const listAlbumsSQL = `
SELECT a.uid, a.slug, a.title, a.description, a.type, a.cover_photo_uid,
       a.private, a.created_by, a.created_at, a.updated_at,
       COUNT(p.uid) AS photo_count,
       COALESCE(a.cover_photo_uid,
                (array_agg(p.uid ORDER BY p.taken_at DESC NULLS LAST, p.uid)
                     FILTER (WHERE p.uid IS NOT NULL))[1]) AS cover_uid,
       COALESCE((array_agg(p.uid ORDER BY p.taken_at DESC NULLS LAST, p.uid)
                     FILTER (WHERE p.uid IS NOT NULL))[1:$1::int], '{}') AS cover_uids,
       MIN(p.taken_at) AS taken_from,
       MAX(p.taken_at) AS taken_to
FROM albums a
LEFT JOIN album_photos ap ON ap.album_uid = a.uid
LEFT JOIN photos p ON p.uid = ap.photo_uid AND p.archived_at IS NULL
    AND (p.stack_uid IS NULL OR p.stack_primary)
GROUP BY a.uid
ORDER BY MAX(p.taken_at) DESC NULLS LAST, a.uid`

// CoverCandidates is how many of an album's photos ListAlbums offers as covers
// in AlbumSummary.CoverUIDs. It is passed to listAlbumsSQL as the array-slice
// bound rather than written into the statement, so the number lives in exactly
// one place.
//
// Eight is twice what a 2×2 collage needs, which is the point: two albums built
// from the same photos can then both fill a collage without sharing a single
// picture. Raising it costs nothing per album (the candidates are a slice of an
// array the query already builds) but grows the listing's payload for every
// album, most of which never need a second candidate.
const CoverCandidates = 8

// ListAlbums returns every album together with how many photos it contains, the
// cover to render for it and the span of capture times across its photos, newest
// album first: albums are ranked by their newest photo's capture time, with
// undated and empty albums last and uid breaking ties. A store with no albums
// yields an empty slice and a nil error.
//
// Only live (non-archived) photos supply the fallback cover and the capture-time
// span, so the index describes exactly the photos the album's grid shows. A
// hand-picked cover is honoured as chosen, archived or not, because it is the
// user's own explicit answer to what the album looks like.
//
// Each summary also carries up to CoverCandidates further covers
// (AlbumSummary.CoverUIDs), so a grid of albums can render tiles that differ
// from one another instead of repeating the one photo overlapping albums share.
func (s *Store) ListAlbums(ctx context.Context) ([]AlbumSummary, error) {
	rows, err := s.pool.Query(ctx, listAlbumsSQL, CoverCandidates)
	if err != nil {
		return nil, fmt.Errorf("organize: listing albums: %w", err)
	}
	defer rows.Close()

	out := make([]AlbumSummary, 0)
	for rows.Next() {
		as, err := scanAlbumSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, as)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating albums: %w", err)
	}
	return out, nil
}

// deleteAlbumRow deletes the album identified by uid using e (a pool or a
// transaction), returning ErrAlbumNotFound when no row matched. Its album_photos
// membership rows are removed by ON DELETE CASCADE.
func deleteAlbumRow(ctx context.Context, e execer, uid string) error {
	tag, err := e.Exec(ctx, "DELETE FROM albums WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("organize: deleting album %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlbumNotFound
	}
	return nil
}

// DeleteAlbum removes the album identified by uid. Its album_photos membership
// rows are removed by ON DELETE CASCADE. It returns ErrAlbumNotFound if no such
// album exists.
func (s *Store) DeleteAlbum(ctx context.Context, uid string) error {
	return deleteAlbumRow(ctx, s.pool, uid)
}

// addPhotoSQL adds a photo to an album, ignoring the insert if it is already a
// member so the call is idempotent. Albums are presented chronologically, so a
// membership row carries no position.
const addPhotoSQL = `
INSERT INTO album_photos (album_uid, photo_uid)
VALUES ($1, $2)
ON CONFLICT (album_uid, photo_uid) DO NOTHING`

// AddPhoto adds photoUID to the album identified by albumUID; adding a photo
// that is already a member is a no-op (idempotent). It returns ErrAlbumNotFound
// or ErrPhotoNotFound when either side does not exist.
func (s *Store) AddPhoto(ctx context.Context, albumUID, photoUID string) error {
	_, err := s.pool.Exec(ctx, addPhotoSQL, albumUID, photoUID)
	if err != nil {
		return translateMembershipFK(err)
	}
	return nil
}

// removePhotoSQL removes one photo from an album's membership. It is idempotent:
// removing a photo that is not a member affects no rows.
const removePhotoSQL = "DELETE FROM album_photos WHERE album_uid = $1 AND photo_uid = $2"

// removeAlbumPhotoRow removes photoUID from albumUID using e (a pool or a
// transaction), wrapping any error. Removing a non-member is a no-op.
func removeAlbumPhotoRow(ctx context.Context, e execer, albumUID, photoUID string) error {
	if _, err := e.Exec(ctx, removePhotoSQL, albumUID, photoUID); err != nil {
		return fmt.Errorf("organize: removing photo %s from album %s: %w", photoUID, albumUID, err)
	}
	return nil
}

// RemovePhoto removes photoUID from the album identified by albumUID. It is
// idempotent: removing a photo that is not a member returns a nil error.
func (s *Store) RemovePhoto(ctx context.Context, albumUID, photoUID string) error {
	return removeAlbumPhotoRow(ctx, s.pool, albumUID, photoUID)
}

// setCoverSQL points an album's cover at a photo (or clears it) and returns the
// refreshed row.
const setCoverSQL = "UPDATE albums SET cover_photo_uid = $2, updated_at = now() " +
	"WHERE uid = $1 RETURNING " + albumColumns

// SetCover sets the cover photo of the album identified by albumUID to photoUID,
// or clears it when photoUID is nil, and returns the refreshed album. It returns
// ErrAlbumNotFound if no such album exists, or ErrPhotoNotFound if the photo does
// not exist.
func (s *Store) SetCover(ctx context.Context, albumUID string, photoUID *string) (Album, error) {
	a, err := scanAlbum(s.pool.QueryRow(ctx, setCoverSQL, albumUID, photoUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Album{}, ErrAlbumNotFound
		}
		if name, ok := isForeignKeyViolation(err); ok && strings.Contains(name, "cover") {
			return Album{}, ErrPhotoNotFound
		}
		return Album{}, err
	}
	return a, nil
}

// listAlbumPhotoUIDsSQL returns an album's photos in display order: albums are
// always chronological, oldest capture time first, with the upload (catalogue
// insertion) time standing in for photos whose capture time is unknown and the
// uid as a stable tie-break.
const listAlbumPhotoUIDsSQL = `
SELECT ap.photo_uid
FROM album_photos ap
JOIN photos p ON p.uid = ap.photo_uid
WHERE ap.album_uid = $1
ORDER BY COALESCE(p.taken_at, p.created_at), ap.photo_uid`

// ListPhotoUIDs returns the UIDs of every photo in the album identified by
// albumUID, in display order. An album with no photos yields an empty slice and a
// nil error. The caller resolves the UIDs to full photo records.
func (s *Store) ListPhotoUIDs(ctx context.Context, albumUID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, listAlbumPhotoUIDsSQL, albumUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing photos for album %s: %w", albumUID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("organize: scanning album photo uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating album photos for %s: %w", albumUID, err)
	}
	return out, nil
}

// listAlbumsForPhotoSQL selects every album a photo belongs to, newest-titled
// first by title for a stable order.
const listAlbumsForPhotoSQL = "SELECT " + albumColumns + " FROM albums a " +
	"JOIN album_photos ap ON ap.album_uid = a.uid WHERE ap.photo_uid = $1 ORDER BY a.title"

// AlbumsForPhoto returns the albums the photo identified by photoUID belongs to,
// ordered by title. It backs the photo detail view's inline album chips. An
// unknown photo simply yields an empty slice (not an error), since membership is
// the question being asked.
func (s *Store) AlbumsForPhoto(ctx context.Context, photoUID string) ([]Album, error) {
	rows, err := s.pool.Query(ctx, listAlbumsForPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing albums for photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]Album, 0)
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, album)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating albums for photo %s: %w", photoUID, err)
	}
	return out, nil
}

// listAlbumUIDsForPhotosSQL selects the album membership of a whole set of
// photos in one index scan (idx_album_photos_photo_uid). It returns uids only —
// the callers that want this ask "were these two photos in the same album?",
// which needs no titles and would otherwise pay to hydrate an album record per
// membership row.
const listAlbumUIDsForPhotosSQL = `
SELECT photo_uid, album_uid
FROM album_photos
WHERE photo_uid = ANY($1)
ORDER BY photo_uid, album_uid`

// AlbumUIDsForPhotos returns, per photo uid, the uids of the albums that photo
// belongs to. Photos with no album — and uids that name nothing — are simply
// absent from the map rather than present with an empty slice, so a caller reads
// "no albums" the same way either way. An empty input yields an empty map
// without touching the database.
func (s *Store) AlbumUIDsForPhotos(
	ctx context.Context, photoUIDs []string,
) (map[string][]string, error) {
	byPhoto := make(map[string][]string, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return byPhoto, nil
	}
	rows, err := s.pool.Query(ctx, listAlbumUIDsForPhotosSQL, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("organize: listing album membership: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var photoUID, albumUID string
		if err := rows.Scan(&photoUID, &albumUID); err != nil {
			return nil, fmt.Errorf("organize: scanning album membership: %w", err)
		}
		byPhoto[photoUID] = append(byPhoto[photoUID], albumUID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating album membership: %w", err)
	}
	return byPhoto, nil
}

// translateMembershipFK maps a foreign-key violation from an album_photos write
// to ErrAlbumNotFound or ErrPhotoNotFound by inspecting the violated constraint,
// and wraps any other error. The constraint name is matched on the referencing
// column ("photo_uid") rather than the table name, because the table name
// "album_photos" contains "album" in both constraints.
func translateMembershipFK(err error) error {
	if name, ok := isForeignKeyViolation(err); ok {
		if strings.Contains(name, "photo_uid") {
			return ErrPhotoNotFound
		}
		return ErrAlbumNotFound
	}
	return fmt.Errorf("organize: album membership write: %w", err)
}
