package photos

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// fileColumns is the canonical, ordered column list for photo_files reads,
// matched by scanFile.
const fileColumns = `id, photo_uid, file_path, file_hash, file_size, file_mime,
	is_primary, role, created_at`

// scanFile reads one photo_files row in fileColumns order from a pgx.Row.
func scanFile(row pgx.Row) (PhotoFile, error) {
	var f PhotoFile
	if err := row.Scan(
		&f.ID, &f.PhotoUID, &f.FilePath, &f.FileHash, &f.FileSize, &f.FileMime,
		&f.IsPrimary, &f.Role, &f.CreatedAt,
	); err != nil {
		return PhotoFile{}, fmt.Errorf("photos: scanning file: %w", err)
	}
	return f, nil
}

// CreateFile inserts f and returns it refreshed with the database-assigned id
// and created_at. It returns ErrFilePathTaken if the photo already has a file at
// that path, ErrPrimaryFileExists if f.IsPrimary collides with an existing
// primary file, or a wrapped error otherwise.
func (s *Store) CreateFile(ctx context.Context, f PhotoFile) (PhotoFile, error) {
	const q = `INSERT INTO photo_files
		(photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + fileColumns
	created, err := scanFile(s.pool.QueryRow(ctx, q,
		f.PhotoUID, f.FilePath, f.FileHash, f.FileSize, f.FileMime, f.IsPrimary, f.Role))
	if err != nil {
		if name, ok := isUniqueViolation(err); ok {
			if strings.Contains(name, "one_primary") {
				return PhotoFile{}, ErrPrimaryFileExists
			}
			return PhotoFile{}, ErrFilePathTaken
		}
		return PhotoFile{}, err
	}
	return created, nil
}

// ListFiles returns every file belonging to the photo identified by photoUID,
// primary first then by id. The slice is empty (not nil) when the photo has no
// files.
func (s *Store) ListFiles(ctx context.Context, photoUID string) ([]PhotoFile, error) {
	q := "SELECT " + fileColumns + " FROM photo_files WHERE photo_uid = $1 " +
		"ORDER BY is_primary DESC, id"
	rows, err := s.pool.Query(ctx, q, photoUID)
	if err != nil {
		return nil, fmt.Errorf("photos: querying files: %w", err)
	}
	defer rows.Close()

	files := make([]PhotoFile, 0)
	for rows.Next() {
		file, scanErr := scanFile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating files: %w", err)
	}
	return files, nil
}

// SetPhash upserts the perceptual hashes for p.PhotoUID, replacing any existing
// row. It returns a wrapped error on failure.
func (s *Store) SetPhash(ctx context.Context, p Phash) error {
	const q = `INSERT INTO photo_phashes (photo_uid, phash, dhash)
		VALUES ($1, $2, $3)
		ON CONFLICT (photo_uid) DO UPDATE SET phash = EXCLUDED.phash, dhash = EXCLUDED.dhash`
	if _, err := s.pool.Exec(ctx, q, p.PhotoUID, p.Phash, p.Dhash); err != nil {
		return fmt.Errorf("photos: upserting phash: %w", err)
	}
	return nil
}

// GetPhash returns the perceptual hashes for the photo identified by photoUID,
// or ErrPhashNotFound.
func (s *Store) GetPhash(ctx context.Context, photoUID string) (Phash, error) {
	const q = `SELECT photo_uid, phash, dhash, created_at FROM photo_phashes WHERE photo_uid = $1`
	var p Phash
	err := s.pool.QueryRow(ctx, q, photoUID).Scan(&p.PhotoUID, &p.Phash, &p.Dhash, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Phash{}, ErrPhashNotFound
		}
		return Phash{}, fmt.Errorf("photos: scanning phash: %w", err)
	}
	return p, nil
}

// NearestPhash returns the photo UID whose stored perceptual hash is closest to
// phash, together with that Hamming distance (number of differing bits over the
// 64-bit pHash). It powers the near-duplicate warning at ingest time. Only the
// pHash is compared; the gradient dHash is stored for future use. It returns
// ErrPhashNotFound when no photo_phashes rows exist yet (the first upload).
//
// The distance is computed in PostgreSQL: each bigint hash is reinterpreted as
// bit(64), XORed, and its set bits counted with bit_count.
func (s *Store) NearestPhash(ctx context.Context, phash int64) (uid string, distance int, err error) {
	const q = `SELECT photo_uid, bit_count((phash::bit(64)) # ($1::bigint::bit(64))) AS dist
		FROM photo_phashes
		ORDER BY dist ASC, photo_uid
		LIMIT 1`
	if err := s.pool.QueryRow(ctx, q, phash).Scan(&uid, &distance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, ErrPhashNotFound
		}
		return "", 0, fmt.Errorf("photos: querying nearest phash: %w", err)
	}
	return uid, distance, nil
}

// ListActivePhashes returns the perceptual hashes of every visible photo, for the
// duplicate-detection scan. Archived (trashed) photos and the non-primary members
// of a stack are excluded so they are never grouped as duplicates — this is how
// same-stack pairs (a RAW and its JPEG are a textbook near-duplicate) are kept out
// of the duplicates page: a non-primary member never becomes a node, so it can
// pair with nothing. The result is ordered by photo_uid for deterministic
// grouping and is empty (not nil) when no visible photo has a hash.
func (s *Store) ListActivePhashes(ctx context.Context) ([]Phash, error) {
	const q = `SELECT ph.photo_uid, ph.phash, ph.dhash, ph.created_at
		FROM photo_phashes ph
		JOIN photos p ON p.uid = ph.photo_uid
		WHERE p.archived_at IS NULL AND (p.stack_uid IS NULL OR p.stack_primary)
		ORDER BY ph.photo_uid`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("photos: querying active phashes: %w", err)
	}
	defer rows.Close()

	hashes := make([]Phash, 0)
	for rows.Next() {
		var p Phash
		if err := rows.Scan(&p.PhotoUID, &p.Phash, &p.Dhash, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("photos: scanning active phash: %w", err)
		}
		hashes = append(hashes, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating active phashes: %w", err)
	}
	return hashes, nil
}

// SetEdit upserts the non-destructive edits for e.PhotoUID, replacing any
// existing row and bumping updated_at. The all-or-nothing crop and the rotation
// allow-list are enforced by SQL CHECK constraints. It returns a wrapped error
// on failure.
func (s *Store) SetEdit(ctx context.Context, e Edit) error {
	const q = `INSERT INTO photo_edits
		(photo_uid, crop_x, crop_y, crop_w, crop_h, rotation, brightness, contrast, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (photo_uid) DO UPDATE SET
			crop_x = EXCLUDED.crop_x, crop_y = EXCLUDED.crop_y,
			crop_w = EXCLUDED.crop_w, crop_h = EXCLUDED.crop_h,
			rotation = EXCLUDED.rotation, brightness = EXCLUDED.brightness,
			contrast = EXCLUDED.contrast, updated_at = now()`
	if _, err := s.pool.Exec(ctx, q,
		e.PhotoUID, e.CropX, e.CropY, e.CropW, e.CropH, e.Rotation, e.Brightness, e.Contrast,
	); err != nil {
		return fmt.Errorf("photos: upserting edit: %w", err)
	}
	return nil
}

// GetEdit returns the non-destructive edits for the photo identified by
// photoUID, or ErrEditNotFound.
func (s *Store) GetEdit(ctx context.Context, photoUID string) (Edit, error) {
	const q = `SELECT photo_uid, crop_x, crop_y, crop_w, crop_h, rotation,
		brightness, contrast, updated_at FROM photo_edits WHERE photo_uid = $1`
	var e Edit
	err := s.pool.QueryRow(ctx, q, photoUID).Scan(
		&e.PhotoUID, &e.CropX, &e.CropY, &e.CropW, &e.CropH, &e.Rotation,
		&e.Brightness, &e.Contrast, &e.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Edit{}, ErrEditNotFound
		}
		return Edit{}, fmt.Errorf("photos: scanning edit: %w", err)
	}
	return e, nil
}
