package photos

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// addPhotoprismAliasSQL records (or re-points) one alias. It is idempotent by
// primary key so recording the same alias again writes it without erroring,
// and re-points one whose content moved to another row. The alias belongs to
// the project $5, and is written only when the photo $4 holding its content is
// one of that project's; otherwise nothing is inserted. That photo's uid is
// bound twice ($2 and $4) because one placeholder used for the VARCHAR
// photo_uid column and the TEXT photos.uid fails type deduction (42P08).
const addPhotoprismAliasSQL = `
INSERT INTO photoprism_aliases (project_id, photoprism_uid, photo_uid, photoprism_file_hash)
SELECT p.project_id, $1, $2, nullif($3, '') FROM photos p WHERE p.uid = $4 AND p.project_id = $5
ON CONFLICT (project_id, photoprism_uid) DO UPDATE
SET photo_uid = excluded.photo_uid, photoprism_file_hash = excluded.photoprism_file_hash`

// AddPhotoprismAlias records that the PhotoPrism photo ppUID is held by the
// project's catalogue photo photoUID, whose content is byte-identical to it but
// which already answers to another source uid (see migration 0046). ppFileHash
// is the source file's SHA1 provenance and may be empty.
//
// It is idempotent: recording the same alias twice is a no-op, and an alias whose
// content ended up on a different row is re-pointed rather than rejected. It
// returns ErrPhotoNotFound when photoUID is not a photo of the project,
// ErrNoProject for an empty projectUID and a wrapped error when the alias cannot
// be written. Nothing in production records aliases any more (the PhotoPrism
// import that did was removed); the recorded ones stay live data behind uid
// search, and the tests seed them through this.
func (s *Store) AddPhotoprismAlias(ctx context.Context, projectUID, ppUID, photoUID, ppFileHash string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, addPhotoprismAliasSQL, ppUID, photoUID, ppFileHash, photoUID, projectUID)
	if err != nil {
		return fmt.Errorf("photos: recording photoprism alias %s -> %s: %w", ppUID, photoUID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrPhotoNotFound, photoUID)
	}
	return nil
}

// getByPhotoprismAliasSQL resolves an aliased source uid to the photo holding its
// content. The alias is reached by a scalar subquery rather than a join, because
// photos and photoprism_aliases share three column names (photoprism_uid,
// photoprism_file_hash, created_at) and photoColumns is deliberately unqualified.
//
// Both halves are confined to the project ($1): an alias is keyed by
// (project_id, photoprism_uid), so the same source uid may be recorded once per
// project, and only this project's record — pointing at this project's photo —
// may answer.
var getByPhotoprismAliasSQL = `SELECT ` + photoColumns + `
FROM photos
WHERE photos.project_id = $1
  AND uid = (SELECT photo_uid FROM photoprism_aliases WHERE project_id = $1 AND photoprism_uid = $2)`

// GetByPhotoprismAlias returns the catalogue photo holding the content of a
// PhotoPrism photo that collapsed onto it, or ErrPhotoNotFound when the uid is not
// aliased. It is the second half of resolving a source uid: GetByPhotoprismUID
// answers for a source photo with a row of its own, this one for a source photo
// whose content is already catalogued under another uid. The lookup is confined
// to the project; an empty projectUID is ErrNoProject.
func (s *Store) GetByPhotoprismAlias(ctx context.Context, projectUID, ppUID string) (Photo, error) {
	if err := requireProject(projectUID); err != nil {
		return Photo{}, err
	}
	photo, err := scanPhoto(s.pool.QueryRow(ctx, getByPhotoprismAliasSQL, projectUID, ppUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Photo{}, ErrPhotoNotFound
		}
		return Photo{}, err
	}
	return photo, nil
}
