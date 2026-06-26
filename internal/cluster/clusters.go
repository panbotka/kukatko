package cluster

import "context"

// ListClusters returns a view of every cluster: its size, a representative face,
// up to maxExamples example faces and the suggested existing subject (the nearest
// already-named neighbour of the cluster's centroid, or nil when none is close
// enough). A store with no clusters yields an empty slice.
func (s *Service) ListClusters(ctx context.Context) ([]View, error) {
	clusters, err := s.store.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(clusters))
	for i := range clusters {
		view, viewErr := s.clusterView(ctx, clusters[i])
		if viewErr != nil {
			return nil, viewErr
		}
		views = append(views, view)
	}
	return views, nil
}

// clusterView builds the listing view for one cluster: it loads the cluster's
// faces, picks the face closest to the centroid as the representative, takes a
// handful of examples and computes the nearest-named-subject suggestion.
func (s *Service) clusterView(ctx context.Context, c Cluster) (View, error) {
	faces, err := s.store.ListClusterFaces(ctx, c.UID)
	if err != nil {
		return View{}, err
	}
	view := View{
		UID:       c.UID,
		Size:      len(faces),
		Examples:  []ExampleFace{},
		CreatedAt: c.CreatedAt,
	}
	if len(faces) > 0 {
		rep := nearestToCentroid(c.Centroid, faces)
		view.Representative = exampleOf(faces[rep])
		view.Examples = exampleFaces(faces, rep)
	}
	view.Suggestion = s.suggestForCluster(ctx, c.Centroid)
	return view, nil
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
// the candidate search fails — a missing suggestion must never fail the listing).
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
