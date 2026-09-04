package cluster

import (
	"context"
	"errors"
	"fmt"
)

// ListPage returns one page of the clusters that are ready to be shown — those
// whose listing summary has already been built — together with how many are
// ready in total and how many are still being prepared in the background.
//
// The page costs two indexed queries and no vector search: every cluster's
// representative, examples and suggestion are read from the cached summary the
// `face_cluster` job wrote. That is the whole point of the cache. A library
// whose clusters have never been summarised answers with an empty page and a
// pending count, which is what lets the page say "17 groups ready, 400 being
// prepared" rather than spin.
func (s *Service) ListPage(ctx context.Context, req PageRequest) (Listing, error) {
	limit, offset := req.clamp()
	ready, pending, err := s.store.CountClusters(ctx)
	if err != nil {
		return Listing{}, err
	}
	clusters, err := s.store.ListReadyClusters(ctx, limit, offset)
	if err != nil {
		return Listing{}, err
	}
	views := make([]View, 0, len(clusters))
	for i := range clusters {
		views = append(views, viewOf(clusters[i]))
	}
	return Listing{
		Clusters:   views,
		Total:      ready,
		Pending:    pending,
		Limit:      limit,
		Offset:     offset,
		NextOffset: nextOffset(offset, limit, len(views), ready),
	}, nil
}

// clamp returns the page request's effective limit and offset: a non-positive
// limit becomes DefaultPageSize, one above MaxPageSize is capped there, and a
// negative offset is read as the first page.
func (r PageRequest) clamp() (limit, offset int) {
	limit = r.Limit
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}
	offset = max(r.Offset, 0)
	return limit, offset
}

// nextOffset returns where the next page starts, or nil when this page is the
// last one: either it came back short, or it reached the end of the ready
// clusters. A cluster named by somebody else meanwhile shortens the total, so
// the end is decided by both, not by the count alone.
func nextOffset(offset, limit, got, total int) *int {
	if got < limit || offset+got >= total {
		return nil
	}
	next := offset + got
	return &next
}

// viewOf projects a cluster row and its cached summary onto the listing view. A
// cluster with no summary yields a bare view (no representative, no examples),
// which the listing never returns — ListPage reads only summarised clusters.
func viewOf(c Cluster) View {
	view := View{UID: c.UID, Size: c.Size, Examples: []ExampleFace{}, CreatedAt: c.CreatedAt}
	if c.Summary == nil {
		return view
	}
	view.Representative = c.Summary.Representative
	view.Suggestion = c.Summary.Suggestion
	if c.Summary.Examples != nil {
		view.Examples = c.Summary.Examples
	}
	return view
}

// BuildSummaries prepares the listing summary of up to limit clusters that have
// none — the expensive half of the old per-request listing, moved to the
// background — and reports what the pass did: how many summaries it built, how
// many empty clusters it dropped, and how many clusters are still waiting once
// its budget ran out.
//
// It changes no face and assigns nobody: a summary is a cached read. The one
// write beyond the summary itself is dropping a cluster all of whose faces are
// gone, which is the same cleanup RemoveFace does when it empties one — a group
// with no faces can be neither drawn nor named.
//
// A cluster that disappears while its summary is being built (named by somebody
// in the meantime) is skipped rather than failing the pass.
func (s *Service) BuildSummaries(ctx context.Context, limit int) (SummaryRun, error) {
	if limit <= 0 {
		limit = DefaultSummaryBatch
	}
	pending, err := s.store.ListPendingClusters(ctx, limit)
	if err != nil {
		return SummaryRun{}, err
	}
	run := SummaryRun{}
	for i := range pending {
		built, dropped, err := s.buildSummary(ctx, pending[i])
		if err != nil {
			return run, err
		}
		if built {
			run.Built++
		}
		if dropped {
			run.Dropped++
		}
	}
	_, remaining, err := s.store.CountClusters(ctx)
	if err != nil {
		return run, err
	}
	run.Remaining = remaining
	return run, nil
}

// buildSummary computes and stores the listing summary of one cluster. It
// reports whether a summary was stored and whether the cluster was dropped for
// having no faces left. A cluster that vanished meanwhile counts as neither.
func (s *Service) buildSummary(ctx context.Context, c Cluster) (built, dropped bool, err error) {
	faces, err := s.store.ListClusterFaces(ctx, c.UID)
	if err != nil {
		return false, false, err
	}
	if len(faces) == 0 {
		if err := s.store.DeleteCluster(ctx, c.UID); err != nil && !errors.Is(err, ErrClusterNotFound) {
			return false, false, err
		}
		return false, true, nil
	}
	if err := s.store.SaveSummary(ctx, c.UID, s.summarize(ctx, c.Centroid, faces)); err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("cluster: preparing cluster %s: %w", c.UID, err)
	}
	return true, false, nil
}

// summarize builds the listing summary of one cluster from its member faces: the
// face closest to the centroid as the representative, a handful of examples, and
// the nearest already-named subject as the suggestion. It is the expensive part
// — one HNSW search per cluster — and is therefore only ever called off the
// request path.
func (s *Service) summarize(ctx context.Context, centroidVec []float32, faces []Face) Summary {
	rep := nearestToCentroid(centroidVec, faces)
	return Summary{
		Representative: exampleOf(faces[rep]),
		Examples:       exampleFaces(faces, rep),
		Suggestion:     s.suggestForCluster(ctx, centroidVec),
	}
}

// exampleFaces returns up to maxExamples example faces for a cluster, the
// representative first followed by the others in id order.
func exampleFaces(faces []Face, rep int) []ExampleFace {
	out := make([]ExampleFace, 0, maxExamples)
	out = append(out, exampleOf(faces[rep]))
	for i := range faces {
		if i == rep {
			continue
		}
		if len(out) >= maxExamples {
			break
		}
		out = append(out, exampleOf(faces[i]))
	}
	return out
}

// exampleOf projects a face onto the lightweight ExampleFace shown in a listing.
func exampleOf(f Face) ExampleFace {
	return ExampleFace{
		PhotoUID:  f.PhotoUID,
		FaceIndex: f.FaceIndex,
		BBox:      f.BBox,
		DetScore:  f.DetScore,
	}
}

// suggestForCluster returns the nearest already-named subject to the cluster's
// centroid as a suggestion, or nil when no named neighbour is within the
// suggestion distance cutoff (or the centroid is empty, or the box is offline so
// the candidate search fails — a missing suggestion must never fail the pass).
func (s *Service) suggestForCluster(ctx context.Context, centroidVec []float32) *Suggestion {
	if len(centroidVec) == 0 {
		return nil
	}
	candidates, err := s.faces.FindSimilarFaceCandidates(
		ctx, centroidVec, suggestionSearchLimit, s.suggestionMaxDistance)
	if err != nil {
		return nil
	}
	if suggestion, ok := bestSubjectSuggestion(candidates); ok {
		return &suggestion
	}
	return nil
}
