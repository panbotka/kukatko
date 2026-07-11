package organize

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// setRatingUpsertSQL inserts or updates only the rating column of a user's rating
// row, leaving the flag at its existing value (or its 'none' default for a new
// row) and stamping updated_at.
const setRatingUpsertSQL = `
INSERT INTO user_ratings (user_uid, photo_uid, rating)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, photo_uid) DO UPDATE SET rating = EXCLUDED.rating, updated_at = now()`

// setFlagUpsertSQL inserts or updates only the flag column of a user's rating
// row, leaving the rating at its existing value (or its 0 default for a new row)
// and stamping updated_at.
const setFlagUpsertSQL = `
INSERT INTO user_ratings (user_uid, photo_uid, flag)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, photo_uid) DO UPDATE SET flag = EXCLUDED.flag, updated_at = now()`

// pruneDefaultRatingSQL deletes a rating row that has fallen back to all
// defaults (rating 0 and flag 'none'), keeping the table sparse so a never-rated
// photo simply has no row.
const pruneDefaultRatingSQL = `
DELETE FROM user_ratings
WHERE user_uid = $1 AND photo_uid = $2 AND rating = 0 AND flag = 'none'`

// SetRating sets the star rating photoUID has for the user identified by userUID,
// leaving any existing flag untouched. It is an idempotent upsert; when the
// resulting row would be all-defaults (rating 0 and flag 'none') the row is
// deleted instead, so a never-rated photo keeps no row. It returns
// ErrInvalidRating for a rating outside 0–5, or ErrUserNotFound / ErrPhotoNotFound
// when either side does not exist.
func (s *Store) SetRating(ctx context.Context, userUID, photoUID string, rating int) error {
	if rating < ratingMin || rating > ratingMax {
		return ErrInvalidRating
	}
	return s.upsertRating(ctx, setRatingUpsertSQL, userUID, photoUID, rating, "rating write")
}

// SetFlag sets the personal-marking flag photoUID has for the user identified by
// userUID, leaving any existing rating untouched. It is an idempotent upsert;
// when the resulting row would be all-defaults (rating 0 and flag 'none') the row
// is deleted instead. It returns ErrInvalidFlag for a flag outside the allowed
// set, or ErrUserNotFound / ErrPhotoNotFound when either side does not exist.
func (s *Store) SetFlag(ctx context.Context, userUID, photoUID, flag string) error {
	if !RatingFlag(flag).valid() {
		return ErrInvalidFlag
	}
	return s.upsertRating(ctx, setFlagUpsertSQL, userUID, photoUID, flag, "flag write")
}

// upsertRating runs upsertSQL (which sets exactly one of rating/flag to value)
// and the default-pruning delete in a single transaction, so a row that falls
// back to all-defaults never lingers. op labels the operation for error wrapping.
func (s *Store) upsertRating(
	ctx context.Context, upsertSQL, userUID, photoUID string, value any, op string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("organize: begin %s: %w", op, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, upsertSQL, userUID, photoUID, value); err != nil {
		return translateUserPhotoFK(err, op)
	}
	if _, err := tx.Exec(ctx, pruneDefaultRatingSQL, userUID, photoUID); err != nil {
		return fmt.Errorf("organize: pruning default rating for %s: %w", photoUID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("organize: commit %s: %w", op, err)
	}
	return nil
}

// clearRatingSQL removes a user's rating row for a photo outright, dropping both
// the star rating and the pick/reject flag in one idempotent statement.
const clearRatingSQL = `DELETE FROM user_ratings WHERE user_uid = $1 AND photo_uid = $2`

// ClearRating removes the rating and flag the user identified by userUID has set
// on photoUID, resetting the photo back to the default rating 0 / flag "none".
// It is idempotent: clearing a photo the user never rated — or one that no longer
// exists — is a no-op that still succeeds, mirroring RemoveFavorite so the DELETE
// rating endpoint can answer 204 without a prior-existence check.
func (s *Store) ClearRating(ctx context.Context, userUID, photoUID string) error {
	if _, err := s.pool.Exec(ctx, clearRatingSQL, userUID, photoUID); err != nil {
		return fmt.Errorf("organize: clearing rating %s for user %s: %w", photoUID, userUID, err)
	}
	return nil
}

// GetRating returns the star rating and flag the user identified by userUID has
// set on photoUID. A photo the user has never rated has no row and yields the
// zero value (rating 0, flag "none") with a nil error.
func (s *Store) GetRating(ctx context.Context, userUID, photoUID string) (PhotoRating, error) {
	out := PhotoRating{Rating: 0, Flag: string(FlagNone)}
	err := s.pool.QueryRow(ctx,
		"SELECT rating, flag FROM user_ratings WHERE user_uid = $1 AND photo_uid = $2",
		userUID, photoUID).Scan(&out.Rating, &out.Flag)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return PhotoRating{}, fmt.Errorf("organize: reading rating %s for user %s: %w", photoUID, userUID, err)
	}
	return out, nil
}

// ratingsAmongSQL returns, for one user, the rating and flag of each given photo
// the user has rated. It annotates a whole page of photos with the current user's
// rating in one round-trip instead of one GetRating per photo.
const ratingsAmongSQL = `
SELECT photo_uid, rating, flag FROM user_ratings
WHERE user_uid = $1 AND photo_uid = ANY($2)`

// RatingsAmong returns the rating and flag the user identified by userUID has set
// on each of photoUIDs, keyed by photo UID. Only photos with a rating row are
// present; a photo absent from the map has never been rated and defaults to
// rating 0 / flag "none", which the caller applies. An empty photoUIDs slice
// yields an empty map without querying. It lets a caller annotate a whole page of
// photos with the current user's rating in one query.
func (s *Store) RatingsAmong(
	ctx context.Context, userUID string, photoUIDs []string,
) (map[string]PhotoRating, error) {
	out := make(map[string]PhotoRating, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, ratingsAmongSQL, userUID, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("organize: reading ratings for user %s: %w", userUID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		var pr PhotoRating
		if err := rows.Scan(&uid, &pr.Rating, &pr.Flag); err != nil {
			return nil, fmt.Errorf("organize: scanning rating row: %w", err)
		}
		out[uid] = pr
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating ratings for %s: %w", userUID, err)
	}
	return out, nil
}
