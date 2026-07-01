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
package globalsearchapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
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
}

// PeopleSearcher is the subset of people.Store the endpoint needs: name search
// over subjects (people/pets/other).
type PeopleSearcher interface {
	// SearchSubjects returns up to limit subjects whose name matches q.
	SearchSubjects(ctx context.Context, q string, limit int) ([]people.Subject, error)
}

// PhotoSearcher is the subset of photos.Store the endpoint needs: the existing
// Czech-aware full-text search, driven through ListParams.FullText.
type PhotoSearcher interface {
	// Search returns the photos whose search vector matches params.FullText,
	// honouring params' limit for the per-group cap.
	Search(ctx context.Context, params photos.ListParams) ([]photos.Photo, error)
}

// API exposes the grouped global-search endpoint over HTTP. The auth guard is
// supplied by the caller so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	organizer   Organizer
	people      PeopleSearcher
	photos      PhotoSearcher
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
type albumHit struct {
	UID        string  `json:"uid"`
	Title      string  `json:"title"`
	Cover      *string `json:"cover,omitempty"`
	PhotoCount int     `json:"photo_count"`
}

// labelHit is a single label match.
type labelHit struct {
	UID        string `json:"uid"`
	Name       string `json:"name"`
	PhotoCount int    `json:"photo_count"`
}

// subjectHit is a single person/subject match.
type subjectHit struct {
	UID   string  `json:"uid"`
	Name  string  `json:"name"`
	Cover *string `json:"cover,omitempty"`
}

// response is the grouped global-search JSON envelope. Every group is a non-nil
// slice so absent groups serialise as [] rather than null.
type response struct {
	Query  string         `json:"query"`
	Albums []albumHit     `json:"albums"`
	Labels []labelHit     `json:"labels"`
	People []subjectHit   `json:"people"`
	Photos []photos.Photo `json:"photos"`
}

// handleGlobal runs the query across all entity groups and writes the grouped
// top-N result. The q parameter is required; an empty or whitespace-only value is
// answered with 400. Any store failure is answered with 500.
func (a *API) handleGlobal(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
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

	writeJSON(w, http.StatusOK, response{
		Query:  query,
		Albums: toAlbumHits(albums),
		Labels: toLabelHits(labels),
		People: toSubjectHits(subjects),
		Photos: matchedPhotos,
	})
}

// toAlbumHits projects album search rows onto the wire shape, always returning a
// non-nil slice.
func toAlbumHits(rows []organize.AlbumCount) []albumHit {
	out := make([]albumHit, 0, len(rows))
	for _, a := range rows {
		out = append(out, albumHit{
			UID: a.UID, Title: a.Title, Cover: a.CoverPhotoUID, PhotoCount: a.PhotoCount,
		})
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
// returning a non-nil slice.
func toSubjectHits(rows []people.Subject) []subjectHit {
	out := make([]subjectHit, 0, len(rows))
	for _, s := range rows {
		out = append(out, subjectHit{UID: s.UID, Name: s.Name, Cover: s.CoverPhotoUID})
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
