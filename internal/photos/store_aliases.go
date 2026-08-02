package photos

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PhotoprismAlias records that a PhotoPrism source photo collapsed onto a
// catalogue row holding byte-identical content under a DIFFERENT source uid. It
// is the row shape of the photoprism_aliases table (migration 0046).
type PhotoprismAlias struct {
	// PhotoprismUID is the source photo that has no row of its own.
	PhotoprismUID string `json:"photoprism_uid"`
	// PhotoUID is the catalogue photo holding its content.
	PhotoUID string `json:"photo_uid"`
	// PhotoprismFileHash is the SHA1 of the source FILE the alias was recorded
	// from, kept as provenance; it may be empty.
	PhotoprismFileHash string `json:"photoprism_file_hash"`
}

// addPhotoprismAliasSQL records (or re-points) one alias. It is idempotent by
// primary key so a re-run of the import writes the same alias again without
// erroring, and re-points one whose content moved to another row.
const addPhotoprismAliasSQL = `
INSERT INTO photoprism_aliases (photoprism_uid, photo_uid, photoprism_file_hash)
VALUES ($1, $2, nullif($3, ''))
ON CONFLICT (photoprism_uid) DO UPDATE
SET photo_uid = excluded.photo_uid, photoprism_file_hash = excluded.photoprism_file_hash`

// AddPhotoprismAlias records that the PhotoPrism photo ppUID is held by the
// catalogue photo photoUID, whose content is byte-identical to it but which
// already answers to another source uid (see migration 0046 and
// internal/ppimport). ppFileHash is the source file's SHA1 provenance and may be
// empty.
//
// It is idempotent: recording the same alias twice is a no-op, and an alias whose
// content ended up on a different row is re-pointed rather than rejected. It
// returns a wrapped error when the alias cannot be written — the caller must treat
// that as a failed photo, because an unrecorded alias is a source photo dropped in
// silence, which is exactly what this table exists to prevent.
func (s *Store) AddPhotoprismAlias(ctx context.Context, ppUID, photoUID, ppFileHash string) error {
	if _, err := s.pool.Exec(ctx, addPhotoprismAliasSQL, ppUID, photoUID, ppFileHash); err != nil {
		return fmt.Errorf("photos: recording photoprism alias %s -> %s: %w", ppUID, photoUID, err)
	}
	return nil
}

// getByPhotoprismAliasSQL resolves an aliased source uid to the photo holding its
// content. The alias is reached by a scalar subquery rather than a join, because
// photos and photoprism_aliases share three column names (photoprism_uid,
// photoprism_file_hash, created_at) and photoColumns is deliberately unqualified.
var getByPhotoprismAliasSQL = `SELECT ` + photoColumns + `
FROM photos
WHERE uid = (SELECT photo_uid FROM photoprism_aliases WHERE photoprism_uid = $1)`

// GetByPhotoprismAlias returns the catalogue photo holding the content of a
// PhotoPrism photo that collapsed onto it, or ErrPhotoNotFound when the uid is not
// aliased. It is the second half of resolving a source uid: GetByPhotoprismUID
// answers for a source photo with a row of its own, this one for a source photo
// whose content is already catalogued under another uid.
func (s *Store) GetByPhotoprismAlias(ctx context.Context, ppUID string) (Photo, error) {
	photo, err := scanPhoto(s.pool.QueryRow(ctx, getByPhotoprismAliasSQL, ppUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}

// listPhotoprismAliasesSQL lists every recorded alias, ordered for a stable read.
const listPhotoprismAliasesSQL = `
SELECT photoprism_uid, photo_uid, coalesce(photoprism_file_hash, '')
FROM photoprism_aliases
ORDER BY photoprism_uid`

// ListPhotoprismAliases returns every recorded alias. The set is small by nature
// (one row per duplicated source photo — 450 on the production library) and it is
// read whole by the import reconciliation, which has to account for every source
// uid at once.
func (s *Store) ListPhotoprismAliases(ctx context.Context) ([]PhotoprismAlias, error) {
	rows, err := s.pool.Query(ctx, listPhotoprismAliasesSQL)
	if err != nil {
		return nil, fmt.Errorf("photos: listing photoprism aliases: %w", err)
	}
	defer rows.Close()

	out := make([]PhotoprismAlias, 0)
	for rows.Next() {
		var a PhotoprismAlias
		if err := rows.Scan(&a.PhotoprismUID, &a.PhotoUID, &a.PhotoprismFileHash); err != nil {
			return nil, fmt.Errorf("photos: scanning photoprism alias: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating photoprism aliases: %w", err)
	}
	return out, nil
}
