package photos

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// Store is the database access layer for the photo catalogue. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// photoInsertColumns is the canonical, ordered list of columns written on
// insert. The order is shared by insertPhotoSQL's VALUES list and by the
// argument order in Create, so the three must be kept in lockstep.
var photoInsertColumns = []string{
	"uid", "file_hash", "file_path", "file_name", "file_size", "file_mime",
	"file_width", "file_height", "file_orientation", "media_type", "duration_ms",
	"video_codec", "audio_codec", "has_audio", "fps", "taken_at", "taken_at_source",
	"title", "description", "notes", "lat", "lng", "altitude", "camera_make",
	"camera_model", "lens_model", "iso", "aperture", "exposure", "focal_length",
	"exif", "private", "archived_at", "uploaded_by", "photoprism_uid",
	"photoprism_file_hash", "photosorter_uid",
}

// photoColumns is the canonical, ordered column list for photo reads (the insert
// columns plus the database-managed timestamps), matched by scanPhoto.
var photoColumns = strings.Join(photoInsertColumns, ", ") + ", created_at, updated_at"

// insertPhotoSQL is the INSERT … RETURNING statement used by Create, built once
// from photoInsertColumns so the column list and placeholders cannot drift.
var insertPhotoSQL = "INSERT INTO photos (" + strings.Join(photoInsertColumns, ", ") +
	") VALUES (" + placeholders(len(photoInsertColumns)) + ") RETURNING " + photoColumns

// placeholders returns a comma-separated list of PostgreSQL positional
// parameter placeholders "$1, $2, …, $n". It returns an empty string for n <= 0.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			sb.WriteString(", ")
		}
		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(i))
	}
	return sb.String()
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation and, if so, the name of the violated constraint.
func isUniqueViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// scanPhoto reads one photo row in photoColumns order from a pgx.Row (a
// single-row QueryRow result or a row during iteration), returning a wrapped
// error on failure.
func scanPhoto(row pgx.Row) (Photo, error) {
	var p Photo
	var exif []byte
	if err := row.Scan(
		&p.UID, &p.FileHash, &p.FilePath, &p.FileName, &p.FileSize, &p.FileMime,
		&p.FileWidth, &p.FileHeight, &p.FileOrientation, &p.MediaType, &p.DurationMs,
		&p.VideoCodec, &p.AudioCodec, &p.HasAudio, &p.FPS, &p.TakenAt, &p.TakenAtSource,
		&p.Title, &p.Description, &p.Notes, &p.Lat, &p.Lng, &p.Altitude, &p.CameraMake,
		&p.CameraModel, &p.LensModel, &p.ISO, &p.Aperture, &p.Exposure, &p.FocalLength,
		&exif, &p.Private, &p.ArchivedAt, &p.UploadedBy, &p.PhotoprismUID,
		&p.PhotoprismFileHash, &p.PhotosorterUID, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return Photo{}, fmt.Errorf("photos: scanning photo: %w", err)
	}
	p.Exif = exif
	return p, nil
}

// Create inserts p and returns it refreshed with the database-assigned values
// (a generated UID when p.UID is empty, plus created_at/updated_at). It returns
// ErrFileHashTaken if an identical original is already catalogued, or a wrapped
// error otherwise.
func (s *Store) Create(ctx context.Context, p Photo) (Photo, error) {
	if p.UID == "" {
		uid, err := newPhotoUID()
		if err != nil {
			return Photo{}, err
		}
		p.UID = uid
	}
	if p.MediaType == "" {
		p.MediaType = MediaImage
	}
	args := []any{
		p.UID, p.FileHash, p.FilePath, p.FileName, p.FileSize, p.FileMime,
		p.FileWidth, p.FileHeight, p.FileOrientation, p.MediaType, p.DurationMs,
		p.VideoCodec, p.AudioCodec, p.HasAudio, p.FPS, p.TakenAt, p.TakenAtSource,
		p.Title, p.Description, p.Notes, p.Lat, p.Lng, p.Altitude, p.CameraMake,
		p.CameraModel, p.LensModel, p.ISO, p.Aperture, p.Exposure, p.FocalLength,
		nilIfEmptyJSON(p.Exif), p.Private, p.ArchivedAt, p.UploadedBy, p.PhotoprismUID,
		p.PhotoprismFileHash, p.PhotosorterUID,
	}
	created, err := scanPhoto(s.pool.QueryRow(ctx, insertPhotoSQL, args...))
	if err != nil {
		if name, ok := isUniqueViolation(err); ok && strings.Contains(name, "file_hash") {
			return Photo{}, ErrFileHashTaken
		}
		return Photo{}, err
	}
	return created, nil
}

// GetByUID returns the photo with the given UID, or ErrPhotoNotFound.
func (s *Store) GetByUID(ctx context.Context, uid string) (Photo, error) {
	return s.getPhoto(ctx, "uid", uid)
}

// GetByFileHash returns the photo with the given SHA256 file hash, or
// ErrPhotoNotFound. It is the primary dedup lookup.
func (s *Store) GetByFileHash(ctx context.Context, hash string) (Photo, error) {
	return s.getPhoto(ctx, "file_hash", hash)
}

// GetByPhotoprismUID returns the photo imported from the given PhotoPrism UID,
// or ErrPhotoNotFound.
func (s *Store) GetByPhotoprismUID(ctx context.Context, ppUID string) (Photo, error) {
	return s.getPhoto(ctx, "photoprism_uid", ppUID)
}

// GetByPhotosorterUID returns the photo migrated from the given photo-sorter
// UID, or ErrPhotoNotFound.
func (s *Store) GetByPhotosorterUID(ctx context.Context, psUID string) (Photo, error) {
	return s.getPhoto(ctx, "photosorter_uid", psUID)
}

// SetPhotoprismRef stamps the PhotoPrism external identifiers (the photo's
// PhotoPrism UID and its SHA1 file hash) onto the photo identified by uid and
// returns the refreshed photo. The PhotoPrism import uses it to backfill these
// references onto a photo whose content was deduplicated by SHA256 against an
// already-catalogued file (for example one uploaded directly or migrated from
// photo-sorter), so subsequent incremental runs short-circuit on the
// photoprism_uid lookup instead of re-downloading the original. It returns
// ErrPhotoNotFound if no such photo exists.
func (s *Store) SetPhotoprismRef(ctx context.Context, uid, ppUID, ppFileHash string) (Photo, error) {
	q := `UPDATE photos SET photoprism_uid = $2, photoprism_file_hash = $3, updated_at = now()
		WHERE uid = $1 RETURNING ` + photoColumns
	photo, err := scanPhoto(s.pool.QueryRow(ctx, q, uid, ppUID, ppFileHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}

// getPhoto fetches a single photo filtered by an equality on the trusted column
// name col (an internal constant, never user input), translating pgx.ErrNoRows
// into ErrPhotoNotFound.
func (s *Store) getPhoto(ctx context.Context, col, val string) (Photo, error) {
	q := "SELECT " + photoColumns + " FROM photos WHERE " + col + " = $1"
	photo, err := scanPhoto(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}

// ListByUIDs returns the photos whose uid is in uids, in unspecified order. It
// is a batch lookup for callers that already hold a set of uids (for example the
// similar-photos endpoint, which resolves vector matches to photo records) and
// want to avoid issuing one query per uid. Missing uids are simply absent from
// the result; an empty input returns an empty slice without querying.
func (s *Store) ListByUIDs(ctx context.Context, uids []string) ([]Photo, error) {
	if len(uids) == 0 {
		return []Photo{}, nil
	}
	q := "SELECT " + photoColumns + " FROM photos WHERE uid = ANY($1)"
	rows, err := s.pool.Query(ctx, q, uids)
	if err != nil {
		return nil, fmt.Errorf("photos: listing by uids: %w", err)
	}
	defer rows.Close()

	out := make([]Photo, 0, len(uids))
	for rows.Next() {
		photo, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating by uids: %w", err)
	}
	return out, nil
}

// UpdateMetadata applies the user-editable metadata in m to the photo identified
// by uid, bumps updated_at, and returns the refreshed photo. It returns
// ErrPhotoNotFound if no such photo exists.
func (s *Store) UpdateMetadata(ctx context.Context, uid string, m MetadataUpdate) (Photo, error) {
	q := `UPDATE photos SET
		title = $2, description = $3, notes = $4, taken_at = $5, taken_at_source = $6,
		lat = $7, lng = $8, altitude = $9, private = $10, updated_at = now()
		WHERE uid = $1 RETURNING ` + photoColumns
	photo, err := scanPhoto(s.pool.QueryRow(ctx, q, uid,
		m.Title, m.Description, m.Notes, m.TakenAt, m.TakenAtSource,
		m.Lat, m.Lng, m.Altitude, m.Private))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}

// Archive marks the photo identified by uid archived (sets archived_at to now)
// and returns the refreshed photo, or ErrPhotoNotFound. Archiving an
// already-archived photo refreshes its archived_at.
func (s *Store) Archive(ctx context.Context, uid string) (Photo, error) {
	return s.setArchived(ctx, uid, true)
}

// Unarchive clears the archived state of the photo identified by uid and returns
// the refreshed photo, or ErrPhotoNotFound.
func (s *Store) Unarchive(ctx context.Context, uid string) (Photo, error) {
	return s.setArchived(ctx, uid, false)
}

// setArchived sets or clears archived_at for the photo identified by uid and
// returns the refreshed photo, translating pgx.ErrNoRows into ErrPhotoNotFound.
func (s *Store) setArchived(ctx context.Context, uid string, archived bool) (Photo, error) {
	expr := "NULL"
	if archived {
		expr = "now()"
	}
	q := "UPDATE photos SET archived_at = " + expr + ", updated_at = now() " +
		"WHERE uid = $1 RETURNING " + photoColumns
	photo, err := scanPhoto(s.pool.QueryRow(ctx, q, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}

// Delete removes the photo identified by uid. Its photo_files, photo_phashes and
// photo_edits rows are removed by ON DELETE CASCADE. It returns ErrPhotoNotFound
// if no such photo exists.
func (s *Store) Delete(ctx context.Context, uid string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM photos WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("photos: deleting photo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPhotoNotFound
	}
	return nil
}

// nilIfEmptyJSON returns nil for an empty raw JSON value so it is stored as SQL
// NULL, and the value itself otherwise.
func nilIfEmptyJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
