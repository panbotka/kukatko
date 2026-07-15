package expand

import (
	"context"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/photos"
)

// build hydrates the surviving voted candidates into full result rows: it batch-
// loads the photo records, skips any that raced a delete or is a non-primary stack
// member (hidden everywhere else, so kept out of here too), stamps each photo's
// media URLs, and carries the vote count and distance/similarity through.
func (s *Service) build(ctx context.Context, cands []votedCandidate) ([]Candidate, error) {
	if len(cands) == 0 {
		return []Candidate{}, nil
	}
	loaded, err := s.photos.ListByUIDs(ctx, candidateUIDs(cands))
	if err != nil {
		return nil, fmt.Errorf("expand: loading candidate photos: %w", err)
	}
	byUID := make(map[string]photos.Photo, len(loaded))
	for _, photo := range loaded {
		byUID[photo.UID] = photo
	}
	out := make([]Candidate, 0, len(cands))
	for _, candidate := range cands {
		photo, ok := byUID[candidate.photoUID]
		if !ok {
			continue // raced delete between search and load; skip it
		}
		if photo.StackUID != nil && !photo.StackPrimary {
			continue // a non-primary stack member surfaces only through its primary
		}
		s.media.DecorateOne(&photo)
		out = append(out, Candidate{
			Photo:      photo,
			Distance:   candidate.distance,
			Similarity: 1 - candidate.distance,
			MatchCount: candidate.matchCount,
		})
	}
	return out, nil
}

// sortCandidates ranks the results by match_count descending then distance
// ascending: a photo five source photos agree on beats one a single source photo
// matches very strongly. The photo UID breaks remaining ties for a stable order.
func sortCandidates(cands []Candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		switch {
		case cands[i].MatchCount != cands[j].MatchCount:
			return cands[i].MatchCount > cands[j].MatchCount
		case cands[i].Distance != cands[j].Distance:
			return cands[i].Distance < cands[j].Distance
		default:
			return cands[i].Photo.UID < cands[j].Photo.UID
		}
	})
}

// truncate caps the ranked results at limit; a non-positive limit keeps them all.
func truncate(cands []Candidate, limit int) []Candidate {
	if limit > 0 && len(cands) > limit {
		return cands[:limit]
	}
	return cands
}
