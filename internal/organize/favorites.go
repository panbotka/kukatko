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

// AddFavorite marks photoUID, a photo of the project, as a favorite of the user
// identified by userUID. It is idempotent: favoriting an already-favorited photo
// returns a nil error. It returns ErrUserNotFound or ErrPhotoNotFound when either
// side does not exist (a photo of another project included).
func (s *Store) AddFavorite(ctx context.Context, projectUID, userUID, photoUID string) error {
	if err := requirePhotosInProject(ctx, s.pool, projectUID, photoUID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, addFavoriteSQL, userUID, photoUID)
	if err != nil {
		return translateUserPhotoFK(err, "favorite write")
	}
	return nil
}

// RemoveFavorite unfavorites photoUID, a photo of the project, for the user
// identified by userUID. It is idempotent: removing a photo that is not
// favorited — or not one of the project's — returns a nil error and touches
// nothing.
func (s *Store) RemoveFavorite(ctx context.Context, projectUID, userUID, photoUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		"DELETE FROM user_favorites WHERE user_uid = $1 AND photo_uid = $2 AND "+
			photoInProject("user_favorites.photo_uid", "$3"), userUID, photoUID, projectUID)
	if err != nil {
		return fmt.Errorf("organize: removing favorite %s for user %s: %w", photoUID, userUID, err)
	}
	return nil
}

// IsFavorite reports whether the user identified by userUID has favorited
// photoUID, a photo of the project (a photo of another project reads false).
func (s *Store) IsFavorite(ctx context.Context, projectUID, userUID, photoUID string) (bool, error) {
	if err := requireProject(projectUID); err != nil {
		return false, err
	}
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM user_favorites f WHERE f.user_uid = $1 AND f.photo_uid = $2 AND "+
			photoInProject("f.photo_uid", "$3")+")",
		userUID, photoUID, projectUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("organize: checking favorite %s for user %s: %w", photoUID, userUID, err)
	}
	return exists, nil
}

// listFavoritesSQL returns a user's favorited photos of the project ($2), most
// recently favorited first then by uid for a stable tie-break.
const listFavoritesSQL = `
SELECT f.photo_uid FROM user_favorites f
JOIN photos p ON p.uid = f.photo_uid AND p.project_id = $2
WHERE f.user_uid = $1
ORDER BY f.added_at DESC, f.photo_uid`

// ListFavorites returns the UIDs of every photo of the project the user
// identified by userUID has favorited, most recent first; a favourite in another
// project is not listed. A user with no favorites yields an empty slice and a nil
// error. The caller resolves the UIDs to full photo records.
func (s *Store) ListFavorites(ctx context.Context, projectUID, userUID string) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listFavoritesSQL, userUID, projectUID)
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
SELECT f.photo_uid FROM user_favorites f
JOIN photos p ON p.uid = f.photo_uid AND p.project_id = $3
WHERE f.user_uid = $1 AND f.photo_uid = ANY($2)`

// FavoritedAmong reports which of photoUIDs (photos of the project; any other
// uid reads as not favorited) the user identified by userUID has favorited, as
// a set keyed by photo UID (only favorited UIDs are present, each mapped to
// true). An empty photoUIDs slice yields an empty map without querying.
// It lets a caller annotate a whole page of photos with the current user's
// is-favorite flag in one query.
func (s *Store) FavoritedAmong(
	ctx context.Context, projectUID, userUID string, photoUIDs []string,
) (map[string]bool, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, favoritedAmongSQL, userUID, photoUIDs, projectUID)
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
