package photoapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

const (
	// defaultSimilarLimit is the number of similar photos returned when the
	// request does not set ?limit.
	defaultSimilarLimit = 24
	// maxSimilarLimit caps ?limit so a single request cannot ask for an
	// unbounded neighbourhood.
	maxSimilarLimit = 100
)

// SimilarSearcher is the embedding-search dependency the similar endpoint needs.
// It is an interface so photoapi depends on the vectors layer's behaviour, not
// its construction; vectors.Store satisfies it.
type SimilarSearcher interface {
	// GetEmbedding returns a photo's image embedding, or
	// vectors.ErrEmbeddingNotFound when it has not been embedded yet.
	GetEmbedding(ctx context.Context, photoUID string) (vectors.Embedding, error)
	// FindSimilar returns embeddings nearest to vec by cosine distance, nearest
	// first; maxDistance <= 0 disables the distance filter.
	FindSimilar(ctx context.Context, vec []float32, limit int, maxDistance float64) ([]vectors.Match, error)
}

// similarPhoto is one entry in the similar-photos response: the full photo
// record (so the client has the thumb/file info it needs) plus the cosine
// distance to the source photo (smaller is closer).
type similarPhoto struct {
	photos.Photo
	Distance float64 `json:"distance"`
}

// similarResponse is the JSON body of the similar endpoint.
type similarResponse struct {
	Similar []similarPhoto `json:"similar"`
}

// handleSimilar returns the photos most visually similar to the one named in the
// path, ordered by ascending cosine distance and excluding the source itself. It
// is empty-friendly: a photo that exists but has not been embedded yet (or a
// server with no search backend wired) returns an empty list with 200, while a
// genuinely missing photo returns 404.
func (a *API) handleSimilar(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if _, err := a.store.GetByUID(r.Context(), uid); err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}
	if a.similar == nil {
		writeJSON(w, http.StatusOK, similarResponse{Similar: []similarPhoto{}})
		return
	}

	limit := parseSimilarLimit(r.URL.Query().Get("limit"))
	emb, err := a.similar.GetEmbedding(r.Context(), uid)
	if errors.Is(err, vectors.ErrEmbeddingNotFound) {
		writeJSON(w, http.StatusOK, similarResponse{Similar: []similarPhoto{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading embedding failed")
		return
	}

	// Fetch one extra so removing the source still leaves a full page.
	matches, err := a.similar.FindSimilar(r.Context(), emb.Vector, limit+1, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "similarity search failed")
		return
	}
	result, err := a.resolveSimilar(r.Context(), matches, uid, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading similar photos failed")
		return
	}
	writeJSON(w, http.StatusOK, similarResponse{Similar: result})
}

// resolveSimilar drops the source uid, truncates to limit, batch-loads the photo
// records for the surviving matches and returns them in match (distance) order,
// each annotated with its cosine distance.
func (a *API) resolveSimilar(
	ctx context.Context, matches []vectors.Match, sourceUID string, limit int,
) ([]similarPhoto, error) {
	kept := make([]vectors.Match, 0, len(matches))
	uids := make([]string, 0, len(matches))
	for _, m := range matches {
		if m.PhotoUID == sourceUID {
			continue
		}
		kept = append(kept, m)
		uids = append(uids, m.PhotoUID)
		if len(kept) >= limit {
			break
		}
	}

	loaded, err := a.store.ListByUIDs(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("photoapi: loading similar photos: %w", err)
	}
	byUID := make(map[string]photos.Photo, len(loaded))
	for _, p := range loaded {
		byUID[p.UID] = p
	}

	out := make([]similarPhoto, 0, len(kept))
	for _, m := range kept {
		photo, ok := byUID[m.PhotoUID]
		if !ok {
			continue // raced delete between search and load; skip it
		}
		out = append(out, similarPhoto{Photo: photo, Distance: m.Distance})
	}
	return out, nil
}

// parseSimilarLimit parses the ?limit query value, applying defaultSimilarLimit
// for an empty or invalid value and clamping into [1, maxSimilarLimit].
func parseSimilarLimit(raw string) int {
	if raw == "" {
		return defaultSimilarLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSimilarLimit
	}
	if n > maxSimilarLimit {
		return maxSimilarLimit
	}
	return n
}
