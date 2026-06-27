package cluster

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/vectors"
)

// Recluster groups the currently clusterable faces (unassigned and not yet in a
// cluster) into new clusters and returns how many clusters were created. It is
// incremental and re-runnable: assigned faces and already-clustered faces are
// excluded, so existing clusters and named faces are never disturbed — only fresh
// unassigned faces are grouped. Faces whose connected component is smaller than
// the minimum size are left unclustered for a later run.
//
// The similarity graph is built from each clusterable face's HNSW nearest
// neighbours within the cosine-distance threshold; connected components of those
// edges become clusters. The work is deterministic for a fixed set of faces.
func (s *Service) Recluster(ctx context.Context) (int, error) {
	faces, err := s.store.ListUnclusteredFaces(ctx)
	if err != nil {
		return 0, err
	}
	if len(faces) == 0 {
		return 0, nil
	}
	edges, err := s.buildEdges(ctx, faces)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, component := range connectedComponents(len(faces), edges) {
		if len(component) < s.minSize {
			continue
		}
		if err := s.persistCluster(ctx, faces, component); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// buildEdges returns the undirected similarity edges between clusterable faces:
// an edge (i, j) exists when face j is among face i's HNSW neighbours within the
// distance threshold. Only edges between faces in the supplied set are kept
// (neighbours that are assigned or in another cluster are ignored), so clustering
// stays confined to the currently clusterable faces.
func (s *Service) buildEdges(ctx context.Context, faces []Face) ([][2]int, error) {
	index := make(map[int64]int, len(faces))
	for i := range faces {
		index[faces[i].ID] = i
	}
	var edges [][2]int
	for i := range faces {
		matches, err := s.faces.FindSimilarFaces(ctx, faces[i].Vector, neighborSearchLimit, s.threshold)
		if err != nil {
			return nil, fmt.Errorf("cluster: searching neighbours of face %d: %w", faces[i].ID, err)
		}
		for _, m := range matches {
			if j, ok := index[m.ID]; ok && j != i {
				edges = append(edges, [2]int{i, j})
			}
		}
	}
	return edges, nil
}

// persistCluster creates one cluster from the faces at the component's indices:
// it computes the centroid, inserts the cluster row and claims the member faces.
func (s *Service) persistCluster(ctx context.Context, faces []Face, component []int) error {
	vecs := make([][]float32, len(component))
	ids := make([]int64, len(component))
	for k, idx := range component {
		vecs[k] = faces[idx].Vector
		ids[k] = faces[idx].ID
	}
	created, err := s.store.CreateCluster(ctx, vectors.Centroid(vecs), len(component), faces[component[0]].Model)
	if err != nil {
		return err
	}
	if err := s.store.AddFacesToCluster(ctx, created.UID, ids); err != nil {
		return err
	}
	return nil
}
