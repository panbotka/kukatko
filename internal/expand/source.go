package expand

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/vectors"
)

// sampleSource caps how many members are used as query vectors. When the collection
// is within the cap it is used whole. Otherwise it is sampled deterministically —
// an even stride across the already-ordered member list — so a huge album is spread
// across rather than reduced to its newest slice, and the same album always yields
// the same sample (important for reproducible results and tests). It returns the
// sampled UIDs and whether a cap was applied.
func sampleSource(members []string, sourceCap int) ([]string, bool) {
	if sourceCap <= 0 || len(members) <= sourceCap {
		return members, false
	}
	step := float64(len(members)) / float64(sourceCap)
	sampled := make([]string, 0, sourceCap)
	for i := range sourceCap {
		sampled = append(sampled, members[int(float64(i)*step)])
	}
	return sampled, true
}

// loadVectors loads the image embeddings for uids, dropping the ones not embedded
// yet, and returns just the vectors (order is irrelevant to voting). len of the
// result is how many of the inputs were embedded.
func (s *Service) loadVectors(ctx context.Context, uids []string) ([][]float32, error) {
	byUID, err := s.loadVectorMap(ctx, uids)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(byUID))
	for _, vec := range byUID {
		out = append(out, vec)
	}
	return out, nil
}

// loadVectorMap loads the image embeddings for uids concurrently (bounded by the
// service's concurrency), keyed by photo UID. A photo with no embedding is simply
// absent from the map — vectors.ErrEmbeddingNotFound is expected, not an error,
// because the sidecar is often offline and a collection can be half-embedded.
func (s *Service) loadVectorMap(ctx context.Context, uids []string) (map[string][]float32, error) {
	var mu sync.Mutex
	out := make(map[string][]float32, len(uids))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency)
	for i := range uids {
		uid := uids[i]
		group.Go(func() error {
			emb, err := s.vectors.GetEmbedding(groupCtx, uid)
			if errors.Is(err, vectors.ErrEmbeddingNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("expand: loading embedding for %s: %w", uid, err)
			}
			mu.Lock()
			out[uid] = emb.Vector
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("expand: loading source embeddings: %w", err)
	}
	return out, nil
}

// computeMinMatchCount is the vote rule: how many source photos must agree on a
// candidate for it to survive. It scales with the source-set size (sqrt, so it
// grows gently) and the threshold relative to the baseline (a looser search demands
// more agreement), then clamps to 1..min(5, sourceCount). A one-photo collection
// always yields 1, degenerating cleanly to plain per-photo similarity.
func computeMinMatchCount(sourceCount int, threshold, baseThreshold float64) int {
	if sourceCount <= 0 {
		return 0
	}
	ratio := 1.0
	if baseThreshold > 0 {
		ratio = threshold / baseThreshold
	}
	raw := int(math.Round(math.Sqrt(float64(sourceCount)) * ratio / minMatchDivisor))
	return clampMatchCount(raw, sourceCount)
}

// clampMatchCount confines raw to 1..min(5, sourceCount): never zero (that would
// return every neighbour), never above five (beyond which agreement is diminishing
// returns), and never more than the source set can supply.
func clampMatchCount(raw, sourceCount int) int {
	ceiling := min(5, sourceCount)
	if raw < 1 {
		return 1
	}
	if raw > ceiling {
		return ceiling
	}
	return raw
}
