package organize

import (
	"context"
	"fmt"
	"time"
)

// PhotoLabel is a label as it is attached to one photo: the label itself plus the
// provenance the join row carries. It exists because LabelsForPhoto answers "what
// is on this photo" for the UI and drops the join columns, while an exporter
// rebuilding the attachment needs them.
type PhotoLabel struct {
	Label
	// Source is who attached the label: manual, ai or import.
	Source LabelSource `json:"source"`
	// Uncertainty is the classifier's uncertainty as an integer percentage, where
	// 0 means certain. It is stored as uncertainty rather than confidence because
	// that is what the classifier reports; a manual attachment is always 0.
	Uncertainty int `json:"uncertainty"`
	// AttachedAt is when the label was put on this photo.
	AttachedAt time.Time `json:"attached_at"`
}

// UserFavorite records one user having favorited a photo. Favorites are per-user
// (there is no global "favorite" on a photo), so a photo carries as many of these
// as there are users who marked it.
type UserFavorite struct {
	// UserUID identifies the user in this database.
	UserUID string `json:"user_uid"`
	// Username is the user's login name, carried alongside the UID because it is
	// the half that survives a rebuild: UIDs are regenerated, names are recognised.
	Username string `json:"username"`
	// AddedAt is when the photo was favorited.
	AddedAt time.Time `json:"added_at"`
}

// UserRating records one user's star rating and flag on a photo. Like favorites
// these are per-user, so a photo carries one per user who rated it.
type UserRating struct {
	// UserUID identifies the user in this database.
	UserUID string `json:"user_uid"`
	// Username is the user's login name — see UserFavorite.Username.
	Username string `json:"username"`
	// Rating is the star rating, 0 (unrated) to 5.
	Rating int `json:"rating"`
	// Flag is the personal marker: none, pick or reject.
	Flag RatingFlag `json:"flag"`
	// UpdatedAt is when the rating last changed.
	UpdatedAt time.Time `json:"updated_at"`
}

// listPhotoLabelsSQL selects every label attached to a photo together with the
// join row's provenance, highest priority first then by name. The label columns
// are alias-qualified because photo_labels also has a created_at column.
const listPhotoLabelsSQL = "SELECT l.uid, l.slug, l.name, l.priority, l.created_at, l.updated_at, " +
	"pl.source, pl.uncertainty, pl.created_at " +
	"FROM labels l JOIN photo_labels pl ON pl.label_uid = l.uid WHERE pl.photo_uid = $1 " +
	"ORDER BY l.priority DESC, l.name"

// PhotoLabelsForPhoto returns the labels attached to the photo identified by
// photoUID together with each attachment's source, uncertainty and time, ordered
// by priority then name. An unknown photo yields an empty slice (not an error).
//
// It is LabelsForPhoto plus the join columns, for callers that must reproduce the
// attachment rather than merely display it — the metadata sidecar export, whose
// whole job is to record enough that AttachLabel can be replayed.
func (s *Store) PhotoLabelsForPhoto(ctx context.Context, photoUID string) ([]PhotoLabel, error) {
	rows, err := s.pool.Query(ctx, listPhotoLabelsSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing photo labels for %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]PhotoLabel, 0)
	for rows.Next() {
		var pl PhotoLabel
		if err := rows.Scan(&pl.UID, &pl.Slug, &pl.Name, &pl.Priority, &pl.CreatedAt, &pl.UpdatedAt,
			&pl.Source, &pl.Uncertainty, &pl.AttachedAt); err != nil {
			return nil, fmt.Errorf("organize: scanning photo label for %s: %w", photoUID, err)
		}
		out = append(out, pl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating photo labels for %s: %w", photoUID, err)
	}
	return out, nil
}

// listFavoritesForPhotoSQL selects every user who favorited a photo, oldest
// first. It is served by idx_user_favorites_photo_uid.
const listFavoritesForPhotoSQL = "SELECT f.user_uid, u.username, f.added_at " +
	"FROM user_favorites f JOIN users u ON u.uid = f.user_uid " +
	"WHERE f.photo_uid = $1 ORDER BY f.added_at, f.user_uid"

// FavoritesForPhoto returns every user who has favorited the photo identified by
// photoUID, oldest first. A photo nobody favorited yields an empty slice (not an
// error).
//
// It is the reverse of IsFavorite: that answers "did this user favorite it",
// which is what a request scoped to one user needs, whereas an export must record
// everybody's — a favorite dropped because the exporter only knew about one user
// is curation quietly lost.
func (s *Store) FavoritesForPhoto(ctx context.Context, photoUID string) ([]UserFavorite, error) {
	rows, err := s.pool.Query(ctx, listFavoritesForPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing favorites for photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]UserFavorite, 0)
	for rows.Next() {
		var fav UserFavorite
		if err := rows.Scan(&fav.UserUID, &fav.Username, &fav.AddedAt); err != nil {
			return nil, fmt.Errorf("organize: scanning favorite for photo %s: %w", photoUID, err)
		}
		out = append(out, fav)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating favorites for photo %s: %w", photoUID, err)
	}
	return out, nil
}

// listRatingsForPhotoSQL selects every user's rating of a photo, by user. It is
// served by idx_user_ratings_photo_uid.
const listRatingsForPhotoSQL = "SELECT r.user_uid, u.username, r.rating, r.flag, r.updated_at " +
	"FROM user_ratings r JOIN users u ON u.uid = r.user_uid " +
	"WHERE r.photo_uid = $1 ORDER BY u.username, r.user_uid"

// RatingsForPhoto returns every user's rating of the photo identified by
// photoUID, ordered by username. A photo nobody rated yields an empty slice (not
// an error).
//
// It is the reverse of GetRating — see FavoritesForPhoto for why an export needs
// all users rather than the requesting one.
func (s *Store) RatingsForPhoto(ctx context.Context, photoUID string) ([]UserRating, error) {
	rows, err := s.pool.Query(ctx, listRatingsForPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("organize: listing ratings for photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]UserRating, 0)
	for rows.Next() {
		var rating UserRating
		if err := rows.Scan(&rating.UserUID, &rating.Username, &rating.Rating,
			&rating.Flag, &rating.UpdatedAt); err != nil {
			return nil, fmt.Errorf("organize: scanning rating for photo %s: %w", photoUID, err)
		}
		out = append(out, rating)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating ratings for photo %s: %w", photoUID, err)
	}
	return out, nil
}
