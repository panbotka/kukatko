package organize

import (
	"context"
	"fmt"
)

// addFavoriteSQL records a user's favorite, doing nothing if the photo is already
// favorited by that user so the call is idempotent.
const addFavoriteSQL = `
INSERT INTO user_favorites (user_uid, photo_uid)
VALUES ($1, $2)
ON CONFLICT (user_uid, photo_uid) DO NOTHING`

// AddFavorite marks photoUID as a favorite of the user identified by userUID. It
// is idempotent: favoriting an already-favorited photo returns a nil error. It
// returns ErrUserNotFound or ErrPhotoNotFound when either side does not exist.
func (s *Store) AddFavorite(ctx context.Context, userUID, photoUID string) error {
	_, err := s.pool.Exec(ctx, addFavoriteSQL, userUID, photoUID)
	if err != nil {
		return translateUserPhotoFK(err, "favorite write")
	}
	return nil
}

// RemoveFavorite unfavorites photoUID for the user identified by userUID. It is
// idempotent: removing a photo that is not favorited returns a nil error.
func (s *Store) RemoveFavorite(ctx context.Context, userUID, photoUID string) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM user_favorites WHERE user_uid = $1 AND photo_uid = $2", userUID, photoUID)
	if err != nil {
		return fmt.Errorf("organize: removing favorite %s for user %s: %w", photoUID, userUID, err)
	}
	return nil
}

// IsFavorite reports whether the user identified by userUID has favorited
// photoUID.
func (s *Store) IsFavorite(ctx context.Context, userUID, photoUID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM user_favorites WHERE user_uid = $1 AND photo_uid = $2)",
		userUID, photoUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("organize: checking favorite %s for user %s: %w", photoUID, userUID, err)
	}
	return exists, nil
}

// listFavoritesSQL returns a user's favorited photos, most recently favorited
// first then by uid for a stable tie-break.
const listFavoritesSQL = `
SELECT photo_uid FROM user_favorites
WHERE user_uid = $1
ORDER BY added_at DESC, photo_uid`

// ListFavorites returns the UIDs of every photo the user identified by userUID has
// favorited, most recent first. A user with no favorites yields an empty slice and
// a nil error. The caller resolves the UIDs to full photo records.
func (s *Store) ListFavorites(ctx context.Context, userUID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, listFavoritesSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing favorites for user %s: %w", userUID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("organize: scanning favorite photo uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating favorites for %s: %w", userUID, err)
	}
	return out, nil
}

// favoritedAmongSQL returns, for one user, the subset of the given photo UIDs that
// the user has favorited. It is used to annotate a page of photos with the current
// user's favorite flag in a single round-trip instead of one IsFavorite per photo.
const favoritedAmongSQL = `
SELECT photo_uid FROM user_favorites
WHERE user_uid = $1 AND photo_uid = ANY($2)`

// FavoritedAmong reports which of photoUIDs the user identified by userUID has
// favorited, as a set keyed by photo UID (only favorited UIDs are present, each
// mapped to true). An empty photoUIDs slice yields an empty map without querying.
// It lets a caller annotate a whole page of photos with the current user's
// is-favorite flag in one query.
func (s *Store) FavoritedAmong(ctx context.Context, userUID string, photoUIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, favoritedAmongSQL, userUID, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("organize: checking favorites for user %s: %w", userUID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("organize: scanning favorited photo uid: %w", err)
		}
		out[uid] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating favorited photos for %s: %w", userUID, err)
	}
	return out, nil
}
