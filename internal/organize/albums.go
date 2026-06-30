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
	"private, order_by, created_by, created_at, updated_at"

// insertAlbumSQL inserts an album and returns the stored row.
const insertAlbumSQL = `
INSERT INTO albums (uid, slug, title, description, type, cover_photo_uid, private, order_by, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING ` + albumColumns

// scanAlbum reads one album row in albumColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrAlbumNotFound).
func scanAlbum(row pgx.Row) (Album, error) {
	var a Album
	if err := row.Scan(
		&a.UID, &a.Slug, &a.Title, &a.Description, &a.Type, &a.CoverPhotoUID,
		&a.Private, &a.OrderBy, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Album{}, fmt.Errorf("organize: scanning album: %w", err)
	}
	return a, nil
}

// CreateAlbum inserts a and returns it refreshed with the generated UID, unique
// slug and timestamps. The slug is derived from a.Title and a numeric suffix is
// appended on collision. An empty type defaults to AlbumManual and an empty
// order_by to "added"; an unrecognised type returns ErrInvalidType.
func (s *Store) CreateAlbum(ctx context.Context, a Album) (Album, error) {
	if a.Type == "" {
		a.Type = AlbumManual
	}
	if !a.Type.valid() {
		return Album{}, fmt.Errorf("%w: %q", ErrInvalidType, a.Type)
	}
	if a.OrderBy == "" {
		a.OrderBy = "added"
	}
	if a.UID == "" {
		uid, err := newAlbumUID()
		if err != nil {
			return Album{}, err
		}
		a.UID = uid
	}
	base := slugify(a.Title, albumFallbackSlug)
	return insertWithUniqueSlug(base, func(slug string) (Album, error) {
		a.Slug = slug
		return scanAlbum(s.pool.QueryRow(ctx, insertAlbumSQL,
			a.UID, a.Slug, a.Title, a.Description, a.Type, a.CoverPhotoUID,
			a.Private, a.OrderBy, a.CreatedBy))
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
    cover_photo_uid = $6, private = $7, order_by = $8, updated_at = now()
WHERE uid = $1
RETURNING ` + albumColumns

// UpdateAlbum applies upd to the album identified by uid: it re-slugs from the
// new title (kept unique) and rewrites the editable fields. An empty type
// defaults to AlbumManual and an empty order_by to "added". It returns
// ErrAlbumNotFound if no such album exists, or ErrInvalidType for a bad type.
func (s *Store) UpdateAlbum(ctx context.Context, uid string, upd AlbumUpdate) (Album, error) {
	if upd.Type == "" {
		upd.Type = AlbumManual
	}
	if !upd.Type.valid() {
		return Album{}, fmt.Errorf("%w: %q", ErrInvalidType, upd.Type)
	}
	if upd.OrderBy == "" {
		upd.OrderBy = "added"
	}
	base := slugify(upd.Title, albumFallbackSlug)
	updated, err := insertWithUniqueSlug(base, func(slug string) (Album, error) {
		return scanAlbum(s.pool.QueryRow(ctx, updateAlbumSQL,
			uid, slug, upd.Title, upd.Description, upd.Type,
			upd.CoverPhotoUID, upd.Private, upd.OrderBy))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Album{}, ErrAlbumNotFound
	}
	return updated, err
}

// listAlbumsSQL reads every album with its photo count, ordered by title then uid
// for a stable index display. The album columns are alias-qualified because the
// album_photos join also exposes uid-shaped columns.
const listAlbumsSQL = `
SELECT a.uid, a.slug, a.title, a.description, a.type, a.cover_photo_uid,
       a.private, a.order_by, a.created_by, a.created_at, a.updated_at,
       COUNT(ap.photo_uid) AS photo_count
FROM albums a
LEFT JOIN album_photos ap ON ap.album_uid = a.uid
GROUP BY a.uid
ORDER BY a.title, a.uid`

// ListAlbums returns every album together with how many photos it contains,
// ordered by title then uid. A store with no albums yields an empty slice and a
// nil error.
func (s *Store) ListAlbums(ctx context.Context) ([]AlbumCount, error) {
	rows, err := s.pool.Query(ctx, listAlbumsSQL)
	if err != nil {
		return nil, fmt.Errorf("organize: listing albums: %w", err)
	}
	defer rows.Close()

	out := make([]AlbumCount, 0)
	for rows.Next() {
		var ac AlbumCount
		if err := rows.Scan(
			&ac.UID, &ac.Slug, &ac.Title, &ac.Description, &ac.Type, &ac.CoverPhotoUID,
			&ac.Private, &ac.OrderBy, &ac.CreatedBy, &ac.CreatedAt, &ac.UpdatedAt, &ac.PhotoCount,
		); err != nil {
			return nil, fmt.Errorf("organize: scanning album count: %w", err)
		}
		out = append(out, ac)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating albums: %w", err)
	}
	return out, nil
}

// DeleteAlbum removes the album identified by uid. Its album_photos membership
// rows are removed by ON DELETE CASCADE. It returns ErrAlbumNotFound if no such
// album exists.
func (s *Store) DeleteAlbum(ctx context.Context, uid string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM albums WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("organize: deleting album %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlbumNotFound
	}
	return nil
}

// addPhotoSQL adds a photo to an album, updating its position if it is already a
// member so the call is idempotent.
const addPhotoSQL = `
INSERT INTO album_photos (album_uid, photo_uid, sort_order)
VALUES ($1, $2, $3)
ON CONFLICT (album_uid, photo_uid) DO UPDATE SET sort_order = EXCLUDED.sort_order`

// AddPhoto adds photoUID to the album identified by albumUID at the given
// sortOrder, updating the position if the photo is already a member (idempotent).
// It returns ErrAlbumNotFound or ErrPhotoNotFound when either side does not exist.
func (s *Store) AddPhoto(ctx context.Context, albumUID, photoUID string, sortOrder int) error {
	_, err := s.pool.Exec(ctx, addPhotoSQL, albumUID, photoUID, sortOrder)
	if err != nil {
		return translateMembershipFK(err)
	}
	return nil
}

// RemovePhoto removes photoUID from the album identified by albumUID. It is
// idempotent: removing a photo that is not a member returns a nil error.
func (s *Store) RemovePhoto(ctx context.Context, albumUID, photoUID string) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM album_photos WHERE album_uid = $1 AND photo_uid = $2", albumUID, photoUID)
	if err != nil {
		return fmt.Errorf("organize: removing photo %s from album %s: %w", photoUID, albumUID, err)
	}
	return nil
}

// ReorderPhotos sets each photo's sort_order to its index in orderedPhotoUIDs,
// applied atomically so the album never shows a half-reordered state. Photos in
// the list that are not members of the album are ignored. It returns
// ErrAlbumNotFound if no such album exists.
func (s *Store) ReorderPhotos(ctx context.Context, albumUID string, orderedPhotoUIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("organize: begin reorder album %s: %w", albumUID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := albumExists(ctx, tx, albumUID); err != nil {
		return err
	}
	for i, photoUID := range orderedPhotoUIDs {
		if _, err := tx.Exec(ctx,
			"UPDATE album_photos SET sort_order = $3 WHERE album_uid = $1 AND photo_uid = $2",
			albumUID, photoUID, i,
		); err != nil {
			return fmt.Errorf("organize: reordering photo %s in album %s: %w", photoUID, albumUID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("organize: commit reorder album %s: %w", albumUID, err)
	}
	return nil
}

// albumExists returns ErrAlbumNotFound if no album with uid exists within tx, and
// nil otherwise. It guards membership mutations that would otherwise silently
// no-op on a missing album.
func albumExists(ctx context.Context, tx pgx.Tx, uid string) error {
	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM albums WHERE uid = $1)", uid).Scan(&exists); err != nil {
		return fmt.Errorf("organize: checking album %s: %w", uid, err)
	}
	if !exists {
		return ErrAlbumNotFound
	}
	return nil
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

// listAlbumPhotoUIDsSQL returns an album's photos in display order: by sort_order,
// then by the time they were added, then by uid for a stable tie-break.
const listAlbumPhotoUIDsSQL = `
SELECT photo_uid FROM album_photos
WHERE album_uid = $1
ORDER BY sort_order, added_at, photo_uid`

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
