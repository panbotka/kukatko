package expand

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/vectors"
)

// votedCandidate accumulates the votes for one photo across the source photos'
// kNN results: its minimum distance to any source photo and how many source photos
// returned it.
type votedCandidate struct {
	photoUID   string
	distance   float64
	matchCount int
}

// search runs one kNN per source vector concurrently (bounded by the service's
// concurrency) and merges the hits by photo into voted candidates: match_count is
// how many source photos returned the photo, distance is the minimum across them.
// Each source photo's kNN is deduplicated by Postgres, so a photo counts at most
// once per source and match_count is a true distinct-source vote.
func (s *Service) search(ctx context.Context, sourceVecs [][]float32, threshold float64) ([]votedCandidate, error) {
	var mu sync.Mutex
	merged := make(map[string]*votedCandidate)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency)
	for i := range sourceVecs {
		vec := sourceVecs[i]
		group.Go(func() error {
			matches, err := s.vectors.FindSimilar(groupCtx, vec, s.searchLimit, threshold)
			if err != nil {
				return fmt.Errorf("expand: kNN over a source photo: %w", err)
			}
			mu.Lock()
			for j := range matches {
				mergeCandidate(merged, matches[j])
			}
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("expand: source search: %w", err)
	}
	return mapToSlice(merged), nil
}

// mergeCandidate folds one kNN hit into the vote map: a new photo starts at one
// vote, a repeat bumps the count and keeps the smaller (nearer) distance.
func mergeCandidate(merged map[string]*votedCandidate, m vectors.Match) {
	if existing, ok := merged[m.PhotoUID]; ok {
		existing.matchCount++
		if m.Distance < existing.distance {
			existing.distance = m.Distance
		}
		return
	}
	merged[m.PhotoUID] = &votedCandidate{photoUID: m.PhotoUID, distance: m.Distance, matchCount: 1}
}

// mapToSlice flattens the vote map into a slice for the downstream filters.
func mapToSlice(merged map[string]*votedCandidate) []votedCandidate {
	out := make([]votedCandidate, 0, len(merged))
	for _, candidate := range merged {
		out = append(out, *candidate)
	}
	return out
}

// excludeMembers drops candidates already in the collection. This is the "already a
// member" filter and the whole point of the endpoint — a result full of photos
// already on the label is worthless.
func excludeMembers(cands []votedCandidate, members []string) []votedCandidate {
	inCollection := make(map[string]struct{}, len(members))
	for _, uid := range members {
		inCollection[uid] = struct{}{}
	}
	out := cands[:0]
	for _, candidate := range cands {
		if _, member := inCollection[candidate.photoUID]; member {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// filterVoted keeps only candidates at least minMatch source photos agree on. Below
// the floor is the firehose the vote rule exists to close off.
func filterVoted(cands []votedCandidate, minMatch int) []votedCandidate {
	out := cands[:0]
	for _, candidate := range cands {
		if candidate.matchCount >= minMatch {
			out = append(out, candidate)
		}
	}
	return out
}

// filterRejected applies the label rejection filters: it drops candidates the user
// rejected outright for the label, then applies the negative-exemplar rule. With no
// rejections (always the case for albums, which have no rejection model) it is a
// no-op returning its input untouched.
func (s *Service) filterRejected(
	ctx context.Context, cands []votedCandidate, acceptedVecs [][]float32, rejected []string,
) ([]votedCandidate, error) {
	if len(rejected) == 0 || len(cands) == 0 {
		return cands, nil
	}
	rejectedSet := make(map[string]struct{}, len(rejected))
	for _, uid := range rejected {
		rejectedSet[uid] = struct{}{}
	}
	kept := cands[:0]
	for _, candidate := range cands {
		if _, dropped := rejectedSet[candidate.photoUID]; !dropped {
			kept = append(kept, candidate)
		}
	}
	return s.filterNegatives(ctx, kept, acceptedVecs, rejected)
}

// filterNegatives applies the negative-exemplar rule: a candidate closer to a photo
// rejected for the label than to any photo carrying it is dropped, so a rejection
// teaches rather than just hides one row. acceptedVecs are the (sampled) source
// embeddings; the rejected and candidate embeddings are loaded here. When no
// rejected photo is embedded the rule cannot fire and the input is returned as-is.
func (s *Service) filterNegatives(
	ctx context.Context, cands []votedCandidate, acceptedVecs [][]float32, rejected []string,
) ([]votedCandidate, error) {
	if len(cands) == 0 {
		return cands, nil
	}
	rejectedVecs, err := s.loadVectors(ctx, rejected)
	if err != nil {
		return nil, err
	}
	if len(rejectedVecs) == 0 {
		return cands, nil
	}
	candidateVecs, err := s.loadVectorMap(ctx, candidateUIDs(cands))
	if err != nil {
		return nil, err
	}
	out := cands[:0]
	for _, candidate := range cands {
		vec, ok := candidateVecs[candidate.photoUID]
		if ok && vectors.IsNegativeExemplar(vec, acceptedVecs, rejectedVecs) {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}

// candidateUIDs extracts the photo UIDs of a candidate slice.
func candidateUIDs(cands []votedCandidate) []string {
	uids := make([]string, len(cands))
	for i, candidate := range cands {
		uids[i] = candidate.photoUID
	}
	return uids
}
