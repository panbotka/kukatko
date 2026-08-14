// Package globalsearchapi exposes a single grouped global-search endpoint that
// spans several entity kinds at once: albums, labels, people (subjects) and
// photos. It powers the navbar quick-results dropdown and the search page's
// cross-entity section. Albums, labels and subjects are matched on their
// name/description via the stores' case- and accent-insensitive search methods;
// photos reuse the existing Czech-aware full-text search over the fts tsvector.
//
// Each group is capped at a small top-N so the response stays light enough for a
// type-ahead. The collaborating stores and the auth guard are injected as small
// interfaces, so the handler stays decoupled from their construction and is
// unit-testable with fakes.
//
// It also answers the most obvious gesture of all: pasting an id. A query
// carrying a UID is not fuzzy-searched — its two-letter prefix says which table
// it belongs to (see query.ClassifyUID), so it is resolved there and returned as
// a "direct" hit, and the fan-out is skipped rather than extended. A marker id
// resolves to the photo it sits on, a stack id to the stack's primary and a
// PhotoPrism id to the catalogue row that holds that source photo; a well-formed
// id matching nothing says so instead of returning an empty result set.
package globalsearchapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
	"github.com/panbotka/kukatko/internal/storage"
)

// defaultGroupLimit is the per-group top-N cap applied when Config.Limit is
// non-positive. It keeps the grouped response small enough for a navbar
// type-ahead while showing enough matches to be useful.
const defaultGroupLimit = 8

// Organizer is the subset of organize.Store the endpoint needs: name/description
// search over albums and labels. It is an interface so the handler depends on
// behaviour rather than the concrete store, keeping it unit-testable with fakes.
type Organizer interface {
	// SearchAlbums returns up to limit albums whose title or description matches q.
	SearchAlbums(ctx context.Context, q string, limit int) ([]organize.AlbumCount, error)
	// SearchLabels returns up to limit labels whose name matches q.
	SearchLabels(ctx context.Context, q string, limit int) ([]organize.LabelCount, error)
	// GetAlbumByUID resolves one album by its uid, or organize.ErrAlbumNotFound.
	GetAlbumByUID(ctx context.Context, uid string) (organize.Album, error)
	// GetLabelByUID resolves one label by its uid, or organize.ErrLabelNotFound.
	GetLabelByUID(ctx context.Context, uid string) (organize.Label, error)
	// AlbumCovers returns the cover photo of every named album, in one query for
	// the whole batch. Albums with no cover are absent from the map.
	AlbumCovers(ctx context.Context, uids []string) (map[string]organize.Cover, error)
	// LabelCovers returns the cover photo of every named label, in one query for
	// the whole batch. Labels with no cover are absent from the map.
	LabelCovers(ctx context.Context, uids []string) (map[string]organize.Cover, error)
}

// PeopleSearcher is the subset of people.Store the endpoint needs: name search
// over subjects (people/pets/other), plus the uid lookups a pasted id resolves
// through.
type PeopleSearcher interface {
	// SearchSubjects returns up to limit subjects whose name matches q.
	SearchSubjects(ctx context.Context, q string, limit int) ([]people.Subject, error)
	// GetSubjectByUID resolves one subject by its uid, or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
	// GetMarkerByUID resolves one marker by its uid, or people.ErrMarkerNotFound.
	// A marker id stands for the photo it sits on.
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
	// SubjectCovers returns the cover photo of every named subject, in one query
	// for the whole batch. Subjects with no cover are absent from the map.
	SubjectCovers(ctx context.Context, uids []string) (map[string]people.Cover, error)
}

// PhotoSearcher is the subset of photos.Store the endpoint needs: the existing
// Czech-aware full-text search, driven through ListParams.FullText, plus the
// exact lookups that resolve a pasted photo/stack/PhotoPrism id. The lookups are
// unscoped on purpose — an explicit id must find an archived, hidden or stacked
// photo too.
type PhotoSearcher interface {
	// Search returns the photos whose search vector matches params.FullText,
	// honouring params' limit for the per-group cap.
	Search(ctx context.Context, params photos.ListParams) ([]photos.Photo, error)
	// GetByUID resolves one photo by its uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// GetByPhotoprismUID resolves the photo imported under a PhotoPrism source
	// uid, or photos.ErrPhotoNotFound.
	GetByPhotoprismUID(ctx context.Context, ppUID string) (photos.Photo, error)
	// GetByPhotoprismAlias resolves a PhotoPrism source uid whose bytes were
	// already catalogued under another uid, or photos.ErrPhotoNotFound.
	GetByPhotoprismAlias(ctx context.Context, ppUID string) (photos.Photo, error)
	// ListStackMembers returns a stack's photos, the primary first.
	ListStackMembers(ctx context.Context, stackUID string) ([]photos.Photo, error)
}

// API exposes the grouped global-search endpoint over HTTP. The auth guard is
// supplied by the caller so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	organizer Organizer
	people    PeopleSearcher
	photos    PhotoSearcher
	// media stamps the thumb/download URLs onto every photo hit.
	media       *mediaurl.Builder
	limit       int
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Organizer backs the album and label groups.
	Organizer Organizer
	// People backs the people (subject) group.
	People PeopleSearcher
	// Photos backs the photo group via the existing full-text search.
	Photos PhotoSearcher
	// Storage decides where a client fetches the returned photos' media. A nil
	// storage points them at this application's own media routes.
	Storage storage.Storage
	// Limit caps each group's results. A non-positive value uses defaultGroupLimit.
	Limit int
	// RequireAuth guards the endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg, substituting defaultGroupLimit for a
// non-positive Limit.
func NewAPI(cfg Config) *API {
	limit := cfg.Limit
	if limit <= 0 {
		limit = defaultGroupLimit
	}
	return &API{
		organizer:   cfg.Organizer,
		people:      cfg.People,
		photos:      cfg.Photos,
		media:       mediaurl.NewBuilder(cfg.Storage),
		limit:       limit,
		requireAuth: cfg.RequireAuth,
	}
}

// RegisterRoutes mounts the global-search endpoint onto r, which the caller has
// scoped under the API base path (for example /api/v1). The route requires auth:
//
//	GET /search/global?q=  grouped top-N matches across albums, labels, people, photos
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/search/global", a.handleGlobal)
}

// albumHit is a single album match: enough to link to and render a row.
//
// Cover and ThumbURL are filled together or not at all (see covers.go): the uid
// of the photo standing for the album, and where a client fetches that photo's
// medallion. An album with nothing to show carries neither, which is the
// client's cue to draw its own glyph.
type albumHit struct {
	UID        string  `json:"uid"`
	Title      string  `json:"title"`
	Cover      *string `json:"cover,omitempty"`
	ThumbURL   string  `json:"thumb_url,omitempty"`
	PhotoCount int     `json:"photo_count"`
}

// labelHit is a single label match, with the same cover pair as an album hit.
type labelHit struct {
	UID        string  `json:"uid"`
	Name       string  `json:"name"`
	Cover      *string `json:"cover,omitempty"`
	ThumbURL   string  `json:"thumb_url,omitempty"`
	PhotoCount int     `json:"photo_count"`
}

// subjectHit is a single person/subject match, with the same cover pair as an
// album hit.
type subjectHit struct {
	UID      string  `json:"uid"`
	Name     string  `json:"name"`
	Cover    *string `json:"cover,omitempty"`
	ThumbURL string  `json:"thumb_url,omitempty"`
}

// response is the grouped global-search JSON envelope. Every group is a non-nil
// slice so absent groups serialise as [] rather than null.
type response struct {
	Query string `json:"query"`
	// Direct is the resolved UID lookup, present only when the query names an
	// entity by its id. When it is present the fuzzy groups are all empty: the
	// id lookup REPLACES the four-way fan-out rather than adding a fifth query
	// to it, so a search-as-you-type box does not get more expensive.
	Direct *directHit     `json:"direct,omitempty"`
	Albums []albumHit     `json:"albums"`
	Labels []labelHit     `json:"labels"`
	People []subjectHit   `json:"people"`
	Photos []photos.Photo `json:"photos"`
}

// emptyGroups returns the envelope's groups as empty (never nil) slices, for the
// uid branch that skips the fan-out entirely.
func emptyGroups(query string, direct *directHit) response {
	return response{
		Query:  query,
		Direct: direct,
		Albums: []albumHit{},
		Labels: []labelHit{},
		People: []subjectHit{},
		Photos: []photos.Photo{},
	}
}

// handleGlobal runs the query across all entity groups and writes the grouped
// top-N result. The q parameter is required; an empty or whitespace-only value is
// answered with 400. Any store failure is answered with 500.
//
// A query that carries a UID takes a different, cheaper path: the id is resolved
// against the one table its prefix names and returned as a direct hit, and the
// four-way fuzzy fan-out is skipped — a uid matches no album title, label name,
// subject name or photo full text anyway, so running it would only cost four
// queries to return nothing.
func (a *API) handleGlobal(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	ctx := r.Context()

	if ref, ok := query.FindUID(q); ok {
		hit, err := a.resolveDirect(ctx, ref)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "resolving uid failed")
			return
		}
		writeJSON(w, http.StatusOK, emptyGroups(q, hit))
		return
	}

	a.handleFuzzy(w, r, q)
}

// handleFuzzy runs the ordinary cross-entity search: the four per-group top-N
// searches over names and full text.
func (a *API) handleFuzzy(w http.ResponseWriter, r *http.Request, query string) {
	ctx := r.Context()

	albums, err := a.organizer.SearchAlbums(ctx, query, a.limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching albums failed")
		return
	}
	labels, err := a.organizer.SearchLabels(ctx, query, a.limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching labels failed")
		return
	}
	subjects, err := a.people.SearchSubjects(ctx, query, a.limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching people failed")
		return
	}
	matchedPhotos, err := a.photos.Search(ctx, photos.ListParams{FullText: query, Limit: a.limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching photos failed")
		return
	}
	a.media.Decorate(matchedPhotos)

	out := response{
		Query:  query,
		Albums: toAlbumHits(albums),
		Labels: toLabelHits(labels),
		People: toSubjectHits(subjects),
		Photos: matchedPhotos,
	}
	if err := a.stampCovers(ctx, &out); err != nil {
		writeError(w, http.StatusInternalServerError, "reading covers failed")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// stampCovers gives each entity group the photo that stands for its hits, and
// the address to draw it from. It is three batched lookups — one per group, each
// answering the group's whole top-N in a single query — so an album, a label and
// a person cost the same three queries whether they matched one row or eight.
func (a *API) stampCovers(ctx context.Context, out *response) error {
	if err := a.stampAlbumCovers(ctx, out.Albums); err != nil {
		return err
	}
	if err := a.stampLabelCovers(ctx, out.Labels); err != nil {
		return err
	}
	return a.stampSubjectCovers(ctx, out.People)
}

// toAlbumHits projects album search rows onto the wire shape, always returning a
// non-nil slice. The cover is deliberately not taken from the row's hand-picked
// Album.CoverPhotoUID: covers are stamped afterwards for the whole group at once
// (see stampAlbumCovers), which honours that same choice and falls back to the
// album's newest photo when nobody made one.
func toAlbumHits(rows []organize.AlbumCount) []albumHit {
	out := make([]albumHit, 0, len(rows))
	for _, a := range rows {
		out = append(out, albumHit{UID: a.UID, Title: a.Title, PhotoCount: a.PhotoCount})
	}
	return out
}

// toLabelHits projects label search rows onto the wire shape, always returning a
// non-nil slice.
func toLabelHits(rows []organize.LabelCount) []labelHit {
	out := make([]labelHit, 0, len(rows))
	for _, l := range rows {
		out = append(out, labelHit{UID: l.UID, Name: l.Name, PhotoCount: l.PhotoCount})
	}
	return out
}

// toSubjectHits projects subject search rows onto the wire shape, always
// returning a non-nil slice. The cover is stamped afterwards for the whole group
// (see stampSubjectCovers), for the same reason as an album's.
func toSubjectHits(rows []people.Subject) []subjectHit {
	out := make([]subjectHit, 0, len(rows))
	for _, s := range rows {
		out = append(out, subjectHit{UID: s.UID, Name: s.Name})
	}
	return out
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("globalsearchapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
