// Package duplicates finds groups of likely-duplicate photos and exposes them
// for review. It links photos two ways — perceptual-hash (pHash) Hamming
// distance within a configured bit threshold, and image-embedding cosine
// distance within a configured threshold — then merges the links into connected
// components with a union-find. Both linking steps avoid an O(n^2) all-pairs
// scan: pHash uses banded LSH buckets, embeddings use the HNSW vector index.
// Each group suggests a "keeper" (highest resolution, then largest file, then
// oldest) and lists its members with enough detail to compare them. The package
// never mutates anything; cleanup happens through the bulk/archive APIs after the
// user confirms a choice.
package duplicates

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

const (
	// defaultNeighbours caps how many embedding neighbours are considered per
	// photo when scanning for near-duplicate pairs.
	defaultNeighbours = 8
	// defaultLimit is the page size used when a caller requests a non-positive
	// limit.
	defaultLimit = 20
	// maxLimit caps how many groups a single page may return.
	maxLimit = 100
)

// PhashSource lists the perceptual hashes of the photos eligible for duplicate
// detection. It is satisfied by *photos.Store.
type PhashSource interface {
	// ListActivePhashes returns the pHash/dHash of every non-archived photo.
	ListActivePhashes(ctx context.Context) ([]photos.Phash, error)
}

// PhotoSource fetches photo metadata for the grouped members. It is satisfied by
// *photos.Store.
type PhotoSource interface {
	// ListByUIDs returns the photos for the given uids, ignoring unknown ones.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// EmbeddingSource finds near-duplicate pairs by embedding cosine distance. It is
// satisfied by *vectors.Store. A nil EmbeddingSource disables embedding-based
// grouping (pHash still runs).
type EmbeddingSource interface {
	// FindDuplicatePairs returns photo pairs within maxDist cosine distance,
	// using up to neighbours nearest neighbours per photo.
	FindDuplicatePairs(ctx context.Context, neighbours int, maxDist float64) ([]vectors.DuplicatePair, error)
}

// Config bundles the dependencies and thresholds of a Service. Photos and Phashes
// are required; Embeddings is optional.
type Config struct {
	// Photos fetches member metadata.
	Photos PhotoSource
	// Phashes lists candidate perceptual hashes.
	Phashes PhashSource
	// Embeddings finds near-duplicate pairs by embedding distance; nil disables
	// embedding grouping.
	Embeddings EmbeddingSource
	// PhashMaxDiff is the maximum pHash Hamming distance (in bits) for two photos
	// to be linked. A negative value disables pHash grouping.
	PhashMaxDiff int
	// EmbeddingMaxDist is the maximum embedding cosine distance for two photos to
	// be linked. A non-positive value disables embedding grouping.
	EmbeddingMaxDist float64
	// Neighbours caps the embedding neighbours scanned per photo; non-positive
	// uses defaultNeighbours.
	Neighbours int
}

// Service finds duplicate groups from the configured sources and thresholds.
type Service struct {
	cfg Config
}

// New returns a Service from cfg. It panics if a required source is nil, mirroring
// the fail-fast wiring of the other internal services.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Phashes == nil {
		panic("duplicates.New: Photos and Phashes are required")
	}
	if cfg.Neighbours <= 0 {
		cfg.Neighbours = defaultNeighbours
	}
	return &Service{cfg: cfg}
}

// Match reason constants describe which signal linked a group's members.
const (
	// ReasonPhash means the group was linked only by perceptual-hash similarity.
	ReasonPhash = "phash"
	// ReasonEmbedding means the group was linked only by embedding similarity.
	ReasonEmbedding = "embedding"
	// ReasonBoth means the group was linked by both signals.
	ReasonBoth = "both"
)

// Member is one photo within a duplicate group, carrying the fields needed to
// compare it against the others and decide which to keep.
type Member struct {
	UID               string     `json:"uid"`
	Title             string     `json:"title"`
	FileName          string     `json:"file_name"`
	FileWidth         int        `json:"file_width"`
	FileHeight        int        `json:"file_height"`
	FileSize          int64      `json:"file_size"`
	MediaType         string     `json:"media_type"`
	TakenAt           *time.Time `json:"taken_at,omitempty"`
	IsKeeper          bool       `json:"is_keeper"`
	PhashDistance     *int       `json:"phash_distance,omitempty"`
	EmbeddingDistance *float64   `json:"embedding_distance,omitempty"`

	// sortTime and phash are internal aids for keeper selection and distance
	// computation; they are not serialised.
	sortTime time.Time
	phash    uint64
}

// Group is a set of photos detected as likely duplicates of each other, with a
// suggested keeper. ID is stable across calls (the smallest member uid).
type Group struct {
	ID        string   `json:"id"`
	Reason    string   `json:"reason"`
	KeeperUID string   `json:"keeper_uid"`
	Members   []Member `json:"members"`

	// keeperSortTime is the keeper's capture/creation time, used to order groups;
	// it is not serialised.
	keeperSortTime time.Time
}

// Result is one page of duplicate groups plus the pagination cursor.
type Result struct {
	Groups     []Group `json:"groups"`
	Total      int     `json:"total"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
	NextOffset *int    `json:"next_offset"`
}

// FindGroups scans the catalogue, builds duplicate groups, and returns the
// requested page (groups ordered largest-first, then newest keeper, then id).
// limit is clamped into [1, maxLimit] (defaulting when non-positive); a negative
// offset is treated as zero. It returns a wrapped error if any source fails.
func (s *Service) FindGroups(ctx context.Context, limit, offset int) (Result, error) {
	limit, offset = clampPaging(limit, offset)

	g, err := s.buildGraph(ctx)
	if err != nil {
		return Result{}, err
	}

	comps := g.components()
	uids := memberUIDs(g.nodes, comps)
	byUID, err := s.fetchPhotos(ctx, uids)
	if err != nil {
		return Result{}, err
	}

	groups := g.buildGroups(comps, byUID)
	sortGroups(groups)
	return paginate(groups, limit, offset), nil
}

// buildGraph loads the pHash entries and embedding pairs and links them into a
// union-find, returning the populated graph.
func (s *Service) buildGraph(ctx context.Context) (*graph, error) {
	hashes, err := s.cfg.Phashes.ListActivePhashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("duplicates: listing phashes: %w", err)
	}
	g := newGraph()
	g.addPhashes(hashes)

	pairs, err := s.embeddingPairs(ctx)
	if err != nil {
		return nil, err
	}
	g.addEmbedPairs(pairs)

	g.runPhash(s.cfg.PhashMaxDiff)
	return g, nil
}

// embeddingPairs returns the near-duplicate embedding pairs, or nil when
// embedding grouping is disabled (no source or non-positive threshold).
func (s *Service) embeddingPairs(ctx context.Context) ([]vectors.DuplicatePair, error) {
	if s.cfg.Embeddings == nil || s.cfg.EmbeddingMaxDist <= 0 {
		return nil, nil
	}
	pairs, err := s.cfg.Embeddings.FindDuplicatePairs(ctx, s.cfg.Neighbours, s.cfg.EmbeddingMaxDist)
	if err != nil {
		return nil, fmt.Errorf("duplicates: finding embedding pairs: %w", err)
	}
	return pairs, nil
}

// fetchPhotos loads the given photos keyed by uid. An empty input yields an empty
// map without a query.
func (s *Service) fetchPhotos(ctx context.Context, uids []string) (map[string]photos.Photo, error) {
	byUID := make(map[string]photos.Photo, len(uids))
	if len(uids) == 0 {
		return byUID, nil
	}
	list, err := s.cfg.Photos.ListByUIDs(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("duplicates: loading photos: %w", err)
	}
	for _, p := range list {
		byUID[p.UID] = p
	}
	return byUID, nil
}

// clampPaging normalises a requested limit and offset into the allowed ranges.
func clampPaging(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// paginate slices groups into the page at offset and reports the next offset.
func paginate(groups []Group, limit, offset int) Result {
	total := len(groups)
	page := []Group{}
	if offset < total {
		page = groups[offset:min(offset+limit, total)]
	}
	var next *int
	if offset+len(page) < total {
		n := offset + len(page)
		next = &n
	}
	return Result{Groups: page, Total: total, Limit: limit, Offset: offset, NextOffset: next}
}
