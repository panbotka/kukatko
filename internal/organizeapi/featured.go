package organizeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/thumb"
)

// FeaturedStore is the subset of organize.Store the featured-album endpoints
// need: putting an album on the homepage, taking it off, and reading what is on
// it now. It is its own interface rather than more of AlbumStore so the
// homepage surface stays one thing a later kind of featured entity (labels)
// can join without touching the album catalogue.
type FeaturedStore interface {
	// FeatureAlbumAudited features (or re-dates the feature of) an album until
	// the given instant, auditing the change.
	FeatureAlbumAudited(
		ctx context.Context, uid string, until time.Time, byUID string, entry audit.Entry,
	) (organize.Album, error)
	// UnfeatureAlbumAudited takes an album off the homepage, auditing the change.
	UnfeatureAlbumAudited(ctx context.Context, uid string, entry audit.Entry) (organize.Album, error)
	// ListFeaturedAlbums returns the albums featured right now, newest first,
	// private ones only when includePrivate is set.
	ListFeaturedAlbums(ctx context.Context, includePrivate bool) ([]organize.FeaturedAlbum, error)
}

// MaxFeaturedDays is the furthest ahead a feature may end, in days: a feature
// always ends, and "a year" is the longest anyone means by "for a while". It is
// 366 rather than 365 so the dialog's last offered date (today + 365, until
// the end of that day) always fits whatever the time of day.
const MaxFeaturedDays = 366

// fallbackFeaturedDays is how long a feature runs when the configuration
// supplies no usable albums.featured.default_days.
const fallbackFeaturedDays = 61

// day is one calendar day as the feature length arithmetic counts it.
const day = 24 * time.Hour

// errFeaturedUntilPast and errFeaturedUntilFar are the two ways a requested end
// can be wrong; both answer 400.
var (
	errFeaturedUntilPast = errors.New("until must be in the future")
	errFeaturedUntilFar  = fmt.Errorf("until must be at most %d days ahead", MaxFeaturedDays)
)

// featureInput is the JSON body of PUT /albums/{uid}/featured. Until is
// optional: omitted (or an empty body) means "for the configured default".
type featureInput struct {
	Until *time.Time `json:"until"`
}

// featuredAlbumView is one featured album as the homepage receives it: the
// store's record plus the cover's thumbnail URL, minted through
// internal/mediaurl so a published bucket yields a signed edge URL.
type featuredAlbumView struct {
	organize.FeaturedAlbum
	// ThumbURL is where the cover's grid thumbnail is fetched, absent for an
	// album with nothing to show.
	ThumbURL string `json:"thumb_url,omitempty"`
}

// featuredResponse is the body of GET /albums/featured. The two bounds ride
// along so the feature dialog pre-fills and caps its date from the server's
// configuration rather than from a copy compiled into the frontend.
type featuredResponse struct {
	Albums      []featuredAlbumView `json:"albums"`
	DefaultDays int                 `json:"default_days"`
	MaxDays     int                 `json:"max_days"`
}

// featuredDays resolves the configured default feature length, falling back to
// fallbackFeaturedDays for a value that is not a usable length.
func featuredDays(configured int) int {
	if configured < 1 || configured > MaxFeaturedDays {
		return fallbackFeaturedDays
	}
	return configured
}

// handleFeaturedList returns the albums featured right now, newest feature
// first, each with its cover thumbnail. A private album is listed only for a
// caller who may curate albums: the homepage strip is the one album surface
// pushed at everybody who opens the app, so it does not broadcast what the
// owner marked private. It answers 500 if the store fails.
func (a *API) handleFeaturedList(w http.ResponseWriter, r *http.Request) {
	includePrivate := auth.ProjectRoleFromContext(r.Context()).CanCurate()
	albums, err := a.featured.ListFeaturedAlbums(r.Context(), includePrivate)
	if err != nil {
		fail(w, r, http.StatusInternalServerError, "listing featured albums failed", err)
		return
	}
	views := make([]featuredAlbumView, 0, len(albums))
	for _, album := range albums {
		view := featuredAlbumView{FeaturedAlbum: album}
		if album.CoverUID != nil && album.CoverFileHash != "" {
			view.ThumbURL = a.media.Thumb(r.Context(), *album.CoverUID, album.CoverFileHash, thumb.GridSize)
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, featuredResponse{
		Albums: views, DefaultDays: a.featuredDefaultDays, MaxDays: MaxFeaturedDays,
	})
}

// handleAlbumFeature features the album identified by the path UID until the
// requested instant (or for the default length when the body names none), and
// re-dates the feature of an album already featured — extending and shortening
// are the same request. It returns the refreshed album. A malformed body, an end
// in the past or more than MaxFeaturedDays ahead, and an album that is not a
// hand-made one all answer 400; a missing album answers 404.
func (a *API) handleAlbumFeature(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	existing, err := a.albums.GetAlbumByUID(r.Context(), uid)
	if err != nil {
		status, msg := albumStatus(err)
		fail(w, r, status, msg, err)
		return
	}
	in, err := decodeFeatureInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := a.featureUntil(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	details := map[string]any{"title": existing.Title, "until": until, "previous_until": existing.FeaturedUntil}
	entry := a.auditEntry(r, audit.ActionAlbumFeature, "albums", uid, details)
	album, err := a.featured.FeatureAlbumAudited(r.Context(), uid, until, user.UID, entry)
	if err != nil {
		status, msg := featureStatus(err)
		fail(w, r, status, msg, err)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// handleAlbumUnfeature takes the album identified by the path UID off the
// homepage and returns the refreshed album. Unfeaturing an album that is not
// featured succeeds; a missing album answers 404.
func (a *API) handleAlbumUnfeature(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	existing, err := a.albums.GetAlbumByUID(r.Context(), uid)
	if err != nil {
		status, msg := albumStatus(err)
		fail(w, r, status, msg, err)
		return
	}
	details := map[string]any{"title": existing.Title, "previous_until": existing.FeaturedUntil}
	entry := a.auditEntry(r, audit.ActionAlbumUnfeature, "albums", uid, details)
	album, err := a.featured.UnfeatureAlbumAudited(r.Context(), uid, entry)
	if err != nil {
		status, msg := featureStatus(err)
		fail(w, r, status, msg, err)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// featureUntil resolves the end a feature request asks for: the requested
// instant when there is one, else now plus the configured default. It refuses an
// end that is not in the future or lies more than MaxFeaturedDays ahead.
func (a *API) featureUntil(in featureInput) (time.Time, error) {
	now := a.now()
	if in.Until == nil {
		return now.Add(time.Duration(a.featuredDefaultDays) * day), nil
	}
	until := *in.Until
	if !until.After(now) {
		return time.Time{}, errFeaturedUntilPast
	}
	if until.After(now.Add(MaxFeaturedDays * day)) {
		return time.Time{}, errFeaturedUntilFar
	}
	return until, nil
}

// decodeFeatureInput reads the optional feature body. An empty body is the
// same as {} — "feature it for the default length" — so a client (or curl)
// need not send a document just to say nothing; anything else must be the
// strict JSON object every other mutation here takes.
func decodeFeatureInput(r *http.Request) (featureInput, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return featureInput{}, errors.New("invalid request body: " + err.Error())
	}
	var in featureInput
	if len(bytes.TrimSpace(raw)) == 0 {
		return in, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return featureInput{}, errors.New("invalid request body: " + err.Error())
	}
	return in, nil
}

// featureStatus maps a store error from a feature operation: an album that is
// not a hand-made one is 400, everything else as albumStatus maps it.
func featureStatus(err error) (int, string) {
	if errors.Is(err, organize.ErrNotFeaturable) {
		return http.StatusBadRequest, "only a hand-made album can be featured"
	}
	return albumStatus(err)
}
