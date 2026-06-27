package photosorter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Phash returns the perceptual hashes stored for photoUID. The boolean is false
// when photo-sorter has no photo_phashes row for the photo.
func (r *Reader) Phash(ctx context.Context, photoUID string) (Phash, bool, error) {
	const q = `SELECT photo_uid, phash, dhash FROM photo_phashes WHERE photo_uid = $1`
	var p Phash
	err := r.pool.QueryRow(ctx, q, photoUID).Scan(&p.PhotoUID, &p.Phash, &p.Dhash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Phash{}, false, nil
	}
	if err != nil {
		return Phash{}, false, fmt.Errorf("photosorter: reading phash for %s: %w", photoUID, err)
	}
	return p, true, nil
}

// Edit returns the non-destructive edits stored for photoUID. The boolean is
// false when photo-sorter has no photo_edits row for the photo (no edits).
func (r *Reader) Edit(ctx context.Context, photoUID string) (Edit, bool, error) {
	const q = `SELECT photo_uid, crop_x, crop_y, crop_w, crop_h, rotation, brightness, contrast
		FROM photo_edits WHERE photo_uid = $1`
	var e Edit
	err := r.pool.QueryRow(ctx, q, photoUID).Scan(
		&e.PhotoUID, &e.CropX, &e.CropY, &e.CropW, &e.CropH,
		&e.Rotation, &e.Brightness, &e.Contrast)
	if errors.Is(err, pgx.ErrNoRows) {
		return Edit{}, false, nil
	}
	if err != nil {
		return Edit{}, false, fmt.Errorf("photosorter: reading edit for %s: %w", photoUID, err)
	}
	return e, true, nil
}
