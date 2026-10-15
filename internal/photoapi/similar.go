package photoapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/panbotka/kukatko/internal/auth"
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
	// GetEmbedding returns the image embedding of a photo of projectUID, or
	// vectors.ErrEmbeddingNotFound when it has not been embedded yet (or belongs
	// to another project).
	GetEmbedding(ctx context.Context, projectUID, photoUID string) (vectors.Embedding, error)
	// FindSimilar returns embeddings nearest to vec by cosine distance, nearest
	// first; maxDistance <= 0 disables the distance filter.
	FindSimilar(
		ctx context.Context, projectUID string, vec []float32, limit int, maxDistance float64,
	) ([]vectors.Match, error)
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
	source, ok := a.photoInProject(w, r)
	if !ok {
		return
	}
	uid := source.UID
	if a.similar == nil {
		writeJSON(w, http.StatusOK, similarResponse{Similar: []similarPhoto{}})
		return
	}

	limit := parseSimilarLimit(r.URL.Query().Get("limit"))
	emb, err := a.similar.GetEmbedding(r.Context(), source.ProjectID, uid)
	if errors.Is(err, vectors.ErrEmbeddingNotFound) {
		writeJSON(w, http.StatusOK, similarResponse{Similar: []similarPhoto{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading embedding failed")
		return
	}

	// Over-fetch so removing the source, archived photos and hidden stack members
	// still leaves a full page in the common case.
	matches, err := a.similar.FindSimilar(r.Context(), auth.ProjectUIDFromContext(r.Context()), emb.Vector, 2*limit+1, 0)
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

// resolveSimilar drops the source uid, batch-loads the photo records for the
// remaining matches and returns at most limit of them in match (distance) order,
// each annotated with its cosine distance. An archived photo — on its way to the
// trash, gone from every listing — and a non-primary stack member are left out.
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
	}

	// The vector index spans every project; resolving the candidates inside the
	// request's project is what keeps another tenant's photos out of the strip.
	loaded, err := a.store.ListByUIDsInProject(ctx, auth.ProjectUIDFromContext(ctx), uids)
	if err != nil {
		return nil, fmt.Errorf("photoapi: loading similar photos: %w", err)
	}
	byUID := make(map[string]photos.Photo, len(loaded))
	for _, p := range loaded {
		byUID[p.UID] = p
	}

	out := make([]similarPhoto, 0, min(len(kept), limit))
	for _, m := range kept {
		photo, ok := byUID[m.PhotoUID]
		if !ok {
			continue // raced delete between search and load; skip it
		}
		if !inSimilarStrip(photo) {
			continue
		}
		a.media.DecorateOne(ctx, &photo)
		out = append(out, similarPhoto{Photo: photo, Distance: m.Distance})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// inSimilarStrip reports whether photo may surface in the similar strip. A
// non-primary stack member is hidden everywhere else, so it stays out — only its
// stack's primary surfaces — and an archived photo is out of every listing, so
// it is out of this one too.
func inSimilarStrip(photo photos.Photo) bool {
	return (photo.StackUID == nil || photo.StackPrimary) && photo.ArchivedAt == nil
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
