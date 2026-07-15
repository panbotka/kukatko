package expand

import "context"

// find is the shared album/label pipeline over a resolved source set: sample the
// members down to the cap, load their embeddings, vote each embedded member's kNN
// into a merged candidate set, exclude the members themselves, apply the vote rule
// and (for labels) the rejection and negative-exemplar filters, then hydrate, rank
// and truncate. An empty or unembedded collection returns an empty, non-error
// result carrying a Reason so the UI can explain it.
func (s *Service) find(ctx context.Context, src collection, req Request) (Result, error) {
	sampled, capped := sampleSource(src.members, s.sourceCap)
	sourceVecs, err := s.loadVectors(ctx, sampled)
	if err != nil {
		return Result{}, err
	}
	threshold := orDefaultFloat(req.Threshold, s.maxDistance)
	limit := s.resolveLimit(req.Limit)
	result := baseResult(src, len(sampled), capped, s.sourceCap, len(sourceVecs), threshold, limit)
	if len(sourceVecs) == 0 {
		result.Reason = emptyReason(src)
		return result, nil
	}
	result.MinMatchCount = computeMinMatchCount(len(sourceVecs), threshold, s.maxDistance)

	voted, err := s.search(ctx, sourceVecs, threshold)
	if err != nil {
		return Result{}, err
	}
	voted = excludeMembers(voted, src.members)
	voted = filterVoted(voted, result.MinMatchCount)
	voted, err = s.filterRejected(ctx, voted, sourceVecs, src.rejected)
	if err != nil {
		return Result{}, err
	}

	candidates, err := s.build(ctx, voted)
	if err != nil {
		return Result{}, err
	}
	sortCandidates(candidates)
	candidates = truncate(candidates, limit)
	result.Candidates = candidates
	result.ResultCount = len(candidates)
	return result, nil
}

// resolveLimit applies the configured default for a non-positive request limit and
// clamps any value to the configured maximum.
func (s *Service) resolveLimit(requested int) int {
	limit := requested
	if limit <= 0 {
		limit = s.limit
	}
	if limit > s.maxLimit {
		limit = s.maxLimit
	}
	return limit
}

// baseResult builds the summary shell of a Result — every count and echoed
// parameter filled in — with an empty (non-nil) candidate slice. The pipeline
// overwrites MinMatchCount, Candidates and ResultCount when it proceeds past the
// early empty return.
func baseResult(
	src collection, sampled int, capped bool, sourceCap, withEmbedding int, threshold float64, limit int,
) Result {
	return Result{
		Kind:                      src.kind,
		CollectionUID:             src.uid,
		SourcePhotoCount:          len(src.members),
		SourcePhotosSampled:       sampled,
		SourcePhotosWithEmbedding: withEmbedding,
		SourceCapped:              capped,
		SourceCap:                 sourceCap,
		Threshold:                 threshold,
		Limit:                     limit,
		Candidates:                []Candidate{},
	}
}

// emptyReason names why a collection produced no query vectors: no members at all,
// or members none of which is embedded yet.
func emptyReason(src collection) string {
	if len(src.members) == 0 {
		return ReasonEmpty
	}
	return ReasonNoEmbeddings
}
