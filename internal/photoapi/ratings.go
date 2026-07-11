package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
)

// maxRatingBody caps the rating request body. A rating payload is a small object
// (an optional star value and a flag), so 1 MiB comfortably bounds it.
const maxRatingBody = 1 << 20

// RatingStore is the subset of the organize repository the photo API needs to
// expose per-user ratings: setting/clearing a photo's star rating and pick/reject
// flag for a user and reporting a whole page's ratings in one query. It is an
// interface so photoapi depends on the behaviour, not the organize store's
// construction; organize.Store satisfies it and a test fake can stand in. When
// nil the rating endpoints answer 503 and annotation falls back to rating 0 /
// flag "none".
type RatingStore interface {
	// SetRating sets photoUID's star rating (0–5) for userUID, leaving any flag
	// untouched. It returns organize.ErrInvalidRating for an out-of-range value or
	// organize.ErrPhotoNotFound when the photo does not exist.
	SetRating(ctx context.Context, userUID, photoUID string, rating int) error
	// SetFlag sets photoUID's pick/reject flag for userUID, leaving any rating
	// untouched. It returns organize.ErrInvalidFlag for an unknown flag or
	// organize.ErrPhotoNotFound when the photo does not exist.
	SetFlag(ctx context.Context, userUID, photoUID, flag string) error
	// ClearRating removes userUID's rating and flag for photoUID, idempotently.
	ClearRating(ctx context.Context, userUID, photoUID string) error
	// RatingsAmong returns, keyed by photo UID, the rating and flag userUID has set
	// on each of photoUIDs. Only rated photos are present; an absent photo defaults
	// to rating 0 / flag "none".
	RatingsAmong(ctx context.Context, userUID string, photoUIDs []string) (map[string]organize.PhotoRating, error)
}

// ratingRequest is the JSON body of PUT /photos/{uid}/rating: an optional star
// rating and an optional pick/reject flag. At least one must be supplied; an
// omitted field is left unchanged.
type ratingRequest struct {
	Rating *int    `json:"rating"`
	Flag   *string `json:"flag"`
}

// validate checks that the request sets at least one of rating/flag and that the
// supplied values are in range, so the handler can answer 400 before touching the
// store and never apply a partial change.
func (b ratingRequest) validate() error {
	if b.Rating == nil && b.Flag == nil {
		return errors.New("rating or flag is required")
	}
	if b.Rating != nil && (*b.Rating < ratingMin || *b.Rating > ratingMax) {
		return fmt.Errorf("rating %d out of range [%d, %d]", *b.Rating, ratingMin, ratingMax)
	}
	if b.Flag != nil && !isValidFlag(*b.Flag) {
		return fmt.Errorf("unknown flag %q (want none, pick, reject or eye)", *b.Flag)
	}
	return nil
}

// The inclusive bounds of a star rating accepted by the HTTP layer, mirroring the
// organize store's validation and the SQL CHECK on user_ratings.rating.
const (
	ratingMin = 0
	ratingMax = 5
)

// isValidFlag reports whether flag is one of the recognised personal-marking
// values (none / pick / reject / eye).
func isValidFlag(flag string) bool {
	switch organize.RatingFlag(flag) {
	case organize.FlagNone, organize.FlagPick, organize.FlagReject, organize.FlagEye:
		return true
	default:
		return false
	}
}

// annotateRatings sets each view's star rating and pick/reject flag from one
// RatingsAmong query, leaving the defaults (rating 0, flag "none") in place when
// no ratings store is wired or a photo has never been rated.
func (a *API) annotateRatings(ctx context.Context, userUID string, uids []string, views []photoView) error {
	if a.ratings == nil {
		return nil
	}
	rated, err := a.ratings.RatingsAmong(ctx, userUID, uids)
	if err != nil {
		return fmt.Errorf("photoapi: resolving ratings: %w", err)
	}
	for i := range views {
		if pr, ok := rated[views[i].UID]; ok {
			views[i].Rating = pr.Rating
			views[i].Flag = pr.Flag
		}
	}
	return nil
}

// handleSetRating sets the current user's star rating and/or personal-marking
// flag for the photo named in the path. The body carries an optional rating (0–5)
// and an optional flag ("none"/"pick"/"reject"/"eye"); at least one is required. It returns 204
// on success, 400 for a malformed body or out-of-range value, 404 for a missing
// photo, 401 when unauthenticated and 503 when no ratings backend is wired.
func (a *API) handleSetRating(w http.ResponseWriter, r *http.Request) {
	if a.ratings == nil {
		writeError(w, http.StatusServiceUnavailable, "ratings backend not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	body, err := decodeRating(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := body.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := chi.URLParam(r, "uid")
	if err := a.applyRating(r.Context(), user.UID, uid, body); err != nil {
		writeRatingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleClearRating clears the current user's rating and flag for the photo named
// in the path. It is idempotent (clearing an unrated photo still succeeds) and
// returns 204, 401 when unauthenticated, or 503 when no ratings backend is wired.
func (a *API) handleClearRating(w http.ResponseWriter, r *http.Request) {
	if a.ratings == nil {
		writeError(w, http.StatusServiceUnavailable, "ratings backend not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	uid := chi.URLParam(r, "uid")
	if err := a.ratings.ClearRating(r.Context(), user.UID, uid); err != nil {
		writeError(w, http.StatusInternalServerError, "clearing rating failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// applyRating writes the requested rating and/or flag for the acting user,
// preserving (via %w) a missing-photo error so the caller can map it to 404. The
// values are validated by the handler beforehand, so no partial-write reordering
// is needed: a missing photo fails the first write before anything is stored.
func (a *API) applyRating(ctx context.Context, userUID, photoUID string, body ratingRequest) error {
	if body.Rating != nil {
		if err := a.ratings.SetRating(ctx, userUID, photoUID, *body.Rating); err != nil {
			return fmt.Errorf("photoapi: setting rating: %w", err)
		}
	}
	if body.Flag != nil {
		if err := a.ratings.SetFlag(ctx, userUID, photoUID, *body.Flag); err != nil {
			return fmt.Errorf("photoapi: setting flag: %w", err)
		}
	}
	return nil
}

// decodeRating decodes the rating request body, rejecting unknown fields and a
// body larger than maxRatingBody.
func decodeRating(r *http.Request) (ratingRequest, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRatingBody))
	dec.DisallowUnknownFields()
	var body ratingRequest
	if err := dec.Decode(&body); err != nil {
		return ratingRequest{}, errors.New("invalid request body: " + err.Error())
	}
	return body, nil
}

// writeRatingError maps a ratings store error to an HTTP response: 404 for a
// missing photo, 400 for an invalid value (defensive — the handler validates
// first), otherwise 500.
func writeRatingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, organize.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, organize.ErrInvalidRating), errors.Is(err, organize.ErrInvalidFlag):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "updating rating failed")
	}
}
