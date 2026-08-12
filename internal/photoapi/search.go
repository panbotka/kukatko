package photoapi

import (
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"maps"
	"slices"
	"sort"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/photos"
)

// TextEmbedder embeds a text query into the vector space shared with image
// embeddings, so a natural-language query can be matched against photo vectors.
// It is an interface so photoapi depends on the behaviour, not the embedding
// client's construction; embedding.Client (and a test fake) satisfies it.
type TextEmbedder interface {
	// TextEmbedding returns the query's embedding vector plus the model and
	// pretrained tags. It returns an error wrapping embedding.ErrUnavailable when
	// the sidecar is offline, which callers treat as a signal to degrade to
	// full-text search.
	TextEmbedding(ctx context.Context, text string) (embedding []float32, model, pretrained string, err error)
}

// searchMode selects how the search endpoint ranks results.
type searchMode string

const (
	// modeFulltext ranks by Czech-aware full-text relevance only.
	modeFulltext searchMode = "fulltext"
	// modeSemantic ranks by image-vector similarity to the embedded query.
	modeSemantic searchMode = "semantic"
	// modeHybrid fuses the full-text and semantic rankings with RRF (the default).
	modeHybrid searchMode = "hybrid"
	// modeFilter is the effective mode reported for a pure filter query (no
	// free text): the plain list path ran and no ranking or embedding took
	// place. It is an output value only; parseSearchMode does not accept it.
	modeFilter searchMode = "filter"
)

const (
	// rrfK is the Reciprocal Rank Fusion constant. 60 is the value from the
	// original RRF paper (Cormack, Clarke & Büttcher, SIGIR 2009) and the de-facto
	// standard; it damps the contribution of low-ranked items without letting any
	// single list dominate.
	rrfK = 60
	// semanticCandidatePool is how many nearest neighbours the vector search
	// fetches before the list filters are applied. Filters can drop candidates, so
	// we over-fetch (up to the vectors layer's own cap) to keep enough survivors to
	// paginate. It also bounds the semantic result set's total.
	semanticCandidatePool = 500
	// fusionPool is how many top results from each ranking are fed into the hybrid
	// fusion. It bounds the fused set (and thus the reported total) while keeping
	// enough depth for the RRF ordering to be meaningful across pages.
	fusionPool = 200
)

// searchResult is the outcome of one search: the full ranked, filtered result
// set is not returned; instead photos already holds the requested page, total
// the size of the whole result set (for pagination), and degraded whether the
// search fell back to full-text because the sidecar was unavailable.
type searchResult struct {
	photos   []photos.Photo
	total    int
	degraded bool
}

// parseSearchMode maps the `mode` query value to a searchMode, defaulting to
// hybrid for an empty value and returning a descriptive error for an unknown one
// so the caller can answer 400.
func parseSearchMode(raw string) (searchMode, error) {
	switch searchMode(raw) {
	case "":
		return modeHybrid, nil
	case modeFulltext, modeSemantic, modeHybrid:
		return searchMode(raw), nil
	default:
		return "", fmt.Errorf("unknown mode %q (want fulltext, semantic or hybrid)", raw)
	}
}

// runSearch dispatches to the handler for mode. query is the trimmed search text
// and params carries the parsed filters and pagination (with FullText already
// set to query). It returns the requested page, the total and the degraded flag.
func (a *API) runSearch(
	ctx context.Context, mode searchMode, query string, params photos.ListParams,
) (searchResult, error) {
	switch mode {
	case modeFulltext:
		return a.fulltextSearch(ctx, params)
	case modeSemantic:
		return a.semanticSearch(ctx, query, params)
	case modeHybrid:
		return a.hybridSearch(ctx, query, params)
	default:
		return a.hybridSearch(ctx, query, params)
	}
}

// fulltextSearch runs the existing Czech-aware full-text search, returning the
// requested page and the total matching the filters.
func (a *API) fulltextSearch(ctx context.Context, params photos.ListParams) (searchResult, error) {
	list, err := a.store.Search(ctx, params)
	if err != nil {
		return searchResult{}, fmt.Errorf("photoapi: full-text search: %w", err)
	}
	total, err := a.store.Count(ctx, params)
	if err != nil {
		return searchResult{}, fmt.Errorf("photoapi: counting search results: %w", err)
	}
	return searchResult{photos: list, total: total}, nil
}

// semanticSearch embeds the query and ranks photos by vector similarity, honour-
// ing the list filters and pagination. When the sidecar is unavailable (or no
// embedder/vector backend is wired) it falls back to full-text search and flags
// the result degraded.
func (a *API) semanticSearch(ctx context.Context, query string, params photos.ListParams) (searchResult, error) {
	vec, ok := a.embedQuery(ctx, query)
	if !ok || a.similar == nil {
		return a.degradedFulltext(ctx, params)
	}
	ranked, byUID, err := a.semanticRanking(ctx, vec, params, semanticCandidatePool)
	if err != nil {
		return searchResult{}, err
	}
	page := paginateUIDs(shuffled(ranked, params), params.Offset, effectiveLimit(params))
	return searchResult{photos: resolvePhotos(page, byUID), total: len(ranked)}, nil
}

// hybridSearch fuses the full-text and semantic rankings with Reciprocal Rank
// Fusion, de-duplicates and paginates the result. When the sidecar is
// unavailable it falls back to full-text search and flags the result degraded.
func (a *API) hybridSearch(ctx context.Context, query string, params photos.ListParams) (searchResult, error) {
	vec, ok := a.embedQuery(ctx, query)
	if !ok || a.similar == nil {
		return a.degradedFulltext(ctx, params)
	}

	ftParams := params
	ftParams.Limit = fusionPool
	ftParams.Offset = 0
	ftList, err := a.store.Search(ctx, ftParams)
	if err != nil {
		return searchResult{}, fmt.Errorf("photoapi: full-text search: %w", err)
	}

	semUIDs, semByUID, err := a.semanticRanking(ctx, vec, params, fusionPool)
	if err != nil {
		return searchResult{}, err
	}

	fused, byUID := fuse(ftList, semUIDs, semByUID)
	page := paginateUIDs(shuffled(fused, params), params.Offset, effectiveLimit(params))
	return searchResult{photos: resolvePhotos(page, byUID), total: len(fused)}, nil
}

// degradedFulltext runs a full-text search and marks the result degraded, used
// when semantic or hybrid mode cannot reach the embeddings sidecar.
func (a *API) degradedFulltext(ctx context.Context, params photos.ListParams) (searchResult, error) {
	res, err := a.fulltextSearch(ctx, params)
	res.degraded = true
	return res, err
}

// embedQuery embeds the query text into the shared space. It returns ok=false (and
// no error) when no embedder is configured or any embedding error occurs — an
// unavailable sidecar is the expected case (the box is often offline), and other
// embedding failures also degrade to full-text rather than failing the whole
// search. Non-unavailable errors are logged so they are not silently hidden.
func (a *API) embedQuery(ctx context.Context, query string) (vec []float32, ok bool) {
	if a.embedder == nil {
		return nil, false
	}
	vec, _, _, err := a.embedder.TextEmbedding(ctx, query)
	if err != nil {
		if !embedding.IsUnavailable(err) {
			log.Printf("photoapi: text embedding failed, falling back to full-text: %v", err)
		}
		return nil, false
	}
	return vec, true
}

// semanticRanking runs the vector search for vec, applies the list filters to the
// candidates and returns the surviving photo uids in ascending-distance (most
// similar first) order plus a uid→photo lookup for them. pool caps how many
// nearest neighbours are fetched before filtering. A nil similar backend yields
// an empty ranking.
func (a *API) semanticRanking(
	ctx context.Context, vec []float32, params photos.ListParams, pool int,
) ([]string, map[string]photos.Photo, error) {
	matches, err := a.similar.FindSimilar(ctx, vec, pool, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("photoapi: semantic search: %w", err)
	}
	uids := make([]string, 0, len(matches))
	for _, m := range matches {
		uids = append(uids, m.PhotoUID)
	}
	filtered, err := a.store.FilterUIDs(ctx, uids, params)
	if err != nil {
		return nil, nil, fmt.Errorf("photoapi: filtering semantic candidates: %w", err)
	}
	byUID := make(map[string]photos.Photo, len(filtered))
	for _, p := range filtered {
		byUID[p.UID] = p
	}
	ordered := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, kept := byUID[m.PhotoUID]; kept {
			ordered = append(ordered, m.PhotoUID)
		}
	}
	return ordered, byUID, nil
}

// fuse combines the full-text photo list and the semantic uid ranking with
// Reciprocal Rank Fusion and returns the fused uid order plus a combined
// uid→photo lookup spanning both inputs (so every fused uid resolves to a photo).
func fuse(
	ftList []photos.Photo, semUIDs []string, semByUID map[string]photos.Photo,
) ([]string, map[string]photos.Photo) {
	ftUIDs := make([]string, len(ftList))
	byUID := make(map[string]photos.Photo, len(ftList)+len(semByUID))
	for i, p := range ftList {
		ftUIDs[i] = p.UID
		byUID[p.UID] = p
	}
	maps.Copy(byUID, semByUID)
	return fuseRRF(ftUIDs, semUIDs), byUID
}

// fuseRRF combines ranked uid lists with Reciprocal Rank Fusion: each list
// contributes 1/(rrfK + rank) to a uid's score, where rank is its 1-based
// position in that list. The uids are returned sorted by descending fused score,
// ties broken by descending uid for a stable, deterministic order matching the
// full-text tiebreaker. Uids appearing in several lists are merged (de-duplicated).
func fuseRRF(lists ...[]string) []string {
	score := make(map[string]float64)
	for _, list := range lists {
		for i, uid := range list {
			score[uid] += 1.0 / float64(rrfK+i+1)
		}
	}
	uids := make([]string, 0, len(score))
	for uid := range score {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool {
		if score[uids[i]] != score[uids[j]] {
			return score[uids[i]] > score[uids[j]]
		}
		return uids[i] > uids[j]
	})
	return uids
}

// shuffled returns uids in the pseudo-random order params asks for, and the
// input untouched for every other sort. The semantic and hybrid rankings are
// ordered in Go rather than by the database, so a shuffled slideshow replaying
// such a search has to be reordered here — otherwise shuffle would be a no-op on
// the one search mode that never reaches the SQL ORDER BY.
//
// The order depends on the uid and the seed alone, which is the same guarantee
// the SQL path gets from ordering on a digest of the two: every page of one
// shuffled show sees the same permutation, so paging neither repeats nor drops a
// photo. The digest is a different function from the database's, and deliberately
// so — the two never order the same result set.
func shuffled(uids []string, params photos.ListParams) []string {
	if params.Sort != photos.SortByRandom {
		return uids
	}
	out := slices.Clone(uids)
	slices.SortFunc(out, func(a, b string) int {
		if d := cmp.Compare(seedHash(a, params.Seed), seedHash(b, params.Seed)); d != 0 {
			return d
		}
		// Equal digests are a collision, not equal photos: fall back to the uid so
		// the order stays total (and identical on the next page).
		return cmp.Compare(a, b)
	})
	return out
}

// seedHash is the FNV-1a digest of a photo uid under a shuffle seed: the value
// the random order sorts on. FNV is not a cryptographic hash and does not need
// to be — nothing here resists an adversary, it only has to spread uids evenly
// and depend on nothing but its two inputs.
func seedHash(uid, seed string) uint64 {
	h := fnv.New64a()
	// hash.Hash.Write never returns an error, as its own contract states.
	_, _ = h.Write([]byte(uid))
	_, _ = h.Write([]byte(seed))
	return h.Sum64()
}

// paginateUIDs returns the window of uids for the page at offset with the given
// limit, clamped to the slice bounds. An offset past the end yields nil.
func paginateUIDs(uids []string, offset, limit int) []string {
	if offset >= len(uids) {
		return nil
	}
	end := min(offset+limit, len(uids))
	return uids[offset:end]
}

// resolvePhotos maps an ordered slice of uids to their photo records via byUID,
// preserving order and skipping any uid absent from the lookup (for example a
// photo deleted between the search and the resolve).
func resolvePhotos(uids []string, byUID map[string]photos.Photo) []photos.Photo {
	out := make([]photos.Photo, 0, len(uids))
	for _, uid := range uids {
		if p, ok := byUID[uid]; ok {
			out = append(out, p)
		}
	}
	return out
}

// effectiveLimit returns the page size to apply, substituting the default when
// params left the limit unset (<= 0). It mirrors pageResponse so the page slice
// and the reported limit agree.
func effectiveLimit(params photos.ListParams) int {
	if params.Limit <= 0 {
		return defaultPageLimit
	}
	return params.Limit
}
