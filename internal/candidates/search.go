package candidates

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/vectors"
)

// votedCandidate is one untagged face accumulated across exemplar searches: its
// nearest distance to any voting exemplar, how many distinct exemplars returned it,
// and the render hints carried straight from the search rows.
type votedCandidate struct {
	key        vectors.FaceKey
	distance   float64
	matchCount int
	bbox       [4]float64
	markerUID  *string
}

// search runs one unassigned-face kNN per exemplar, bounded to the configured
// concurrency, and merges the neighbours by face into a voted set. The rejected
// faces are excluded in SQL (before each LIMIT) via exclude, so a rejected face
// never even competes for a slot. Merging is guarded by a mutex because the
// searches run concurrently.
func (s *Service) search(
	ctx context.Context, exemplars []vectors.Face, threshold float64, exclude []vectors.FaceKey,
) ([]votedCandidate, error) {
	var (
		mu     sync.Mutex
		merged = make(map[vectors.FaceKey]*votedCandidate)
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency)
	for i := range exemplars {
		exemplar := exemplars[i]
		group.Go(func() error {
			found, err := s.faces.FindSimilarUnassignedFaceCandidates(
				groupCtx, exemplar.Vector, s.searchLimit, threshold, exclude)
			if err != nil {
				return fmt.Errorf("searching from exemplar %s#%d: %w", exemplar.PhotoUID, exemplar.FaceIndex, err)
			}
			mu.Lock()
			for j := range found {
				mergeCandidate(merged, found[j])
			}
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("candidates: exemplar search: %w", err)
	}
	return mapToSlice(merged), nil
}

// mergeCandidate folds one search row into the voted set: a first sighting seeds the
// entry, a repeat sighting from another exemplar bumps the vote count and keeps the
// smaller (nearer) distance. Each exemplar's kNN yields a face at most once, so the
// count is a true count of distinct exemplars.
func mergeCandidate(merged map[vectors.FaceKey]*votedCandidate, row vectors.FaceCandidate) {
	key := vectors.FaceKey{PhotoUID: row.PhotoUID, FaceIndex: row.FaceIndex}
	if existing, ok := merged[key]; ok {
		existing.matchCount++
		if row.Distance < existing.distance {
			existing.distance = row.Distance
		}
		return
	}
	merged[key] = &votedCandidate{
		key:        key,
		distance:   row.Distance,
		matchCount: 1,
		bbox:       row.BBox,
		markerUID:  row.MarkerUID,
	}
}

// mapToSlice flattens the voted map into a slice; order is irrelevant here because
// the pipeline sorts by distance at the end.
func mapToSlice(merged map[vectors.FaceKey]*votedCandidate) []votedCandidate {
	out := make([]votedCandidate, 0, len(merged))
	for _, candidate := range merged {
		out = append(out, *candidate)
	}
	return out
}

// filterVoted applies the two cheap, embedding-free filters before any hydration:
// the vote rule (drop candidates seen by fewer than minMatch exemplars) and the
// relative size floor (drop faces narrower than minRel of the frame). Keeping these
// first bounds how much the later photo/embedding loads have to touch.
func filterVoted(cands []votedCandidate, minMatch int, minRel float64) []votedCandidate {
	out := cands[:0]
	for _, candidate := range cands {
		if candidate.matchCount < minMatch {
			continue
		}
		if minRel > 0 && candidate.bbox[2] < minRel {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// rejectionKeys converts feedback FaceRefs into the vectors.FaceKey exclusion set
// the unassigned-face search filters on.
func rejectionKeys(refs []feedback.FaceRef) []vectors.FaceKey {
	keys := make([]vectors.FaceKey, len(refs))
	for i, ref := range refs {
		keys[i] = vectors.FaceKey{PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex}
	}
	return keys
}
