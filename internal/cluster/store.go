package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/panbotka/kukatko/internal/vectors"
)

// Store is the database access layer for face clusters. It owns no connection; it
// borrows the shared pgx pool supplied at construction. It reads the clusterable
// faces from the shared faces table (the cluster_uid column added in migration
// 0010) and persists clusters in face_clusters.
//
// Every method is confined to one project: it takes the project uid right after
// the context and never defaults it (an empty uid is ErrNoProject). face_clusters
// carries project_id and so do the faces (a denormalised copy of their photo's),
// so a cluster of another project is answered exactly like a missing one
// (ErrClusterNotFound), a face of another project is never listed, grouped,
// claimed or detached, and a cluster only ever holds faces of its own project.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// listUnclusteredFacesSQL selects every clusterable face of one project —
// unassigned (no subject) and not yet in a cluster — with the data the algorithm
// needs, ordered by id so re-clustering is deterministic.
const listUnclusteredFacesSQL = `
SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, project_id
FROM faces
WHERE project_id = $1 AND subject_uid IS NULL AND cluster_uid IS NULL
ORDER BY id`

// ListUnclusteredFaces returns the project's faces eligible for clustering: those
// with no subject assignment and no cluster, newest grouping decisions preserved
// by excluding already-clustered faces. The result is ordered by id for
// determinism.
func (s *Store) ListUnclusteredFaces(ctx context.Context, projectUID string) ([]Face, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return s.queryFaces(ctx, listUnclusteredFacesSQL, projectUID)
}

// countClusterableFacesSQL counts the faces eligible for clustering, stopping at
// the caller's cap. The question it answers is "are there at least N", and a
// library with a hundred thousand unassigned faces must not pay for counting
// every one of them to answer it.
const countClusterableFacesSQL = `
SELECT count(*) FROM (
	SELECT 1 FROM faces WHERE project_id = $1 AND subject_uid IS NULL AND cluster_uid IS NULL LIMIT $2
) AS clusterable`

// CountClusterableFaces returns how many of the project's faces are eligible for
// clustering — unassigned and not yet in a cluster — counting at most limit of
// them, so the answer costs the same on an empty library and on a huge one. A
// non-positive limit counts nothing.
func (s *Store) CountClusterableFaces(ctx context.Context, projectUID string, limit int) (int, error) {
	if err := requireProject(projectUID); err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, nil
	}
	var count int
	if err := s.pool.QueryRow(ctx, countClusterableFacesSQL, projectUID, limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("cluster: counting clusterable faces: %w", err)
	}
	return count, nil
}

// listClusterFacesSQL selects every face of one project belonging to one
// cluster, ordered by id.
const listClusterFacesSQL = `
SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, project_id
FROM faces
WHERE cluster_uid = $1 AND project_id = $2
ORDER BY id`

// ListClusterFaces returns every face of the project that belongs to the cluster
// identified by clusterUID, ordered by id. A cluster with no faces — or one of
// another project — yields an empty slice.
func (s *Store) ListClusterFaces(ctx context.Context, projectUID, clusterUID string) ([]Face, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return s.queryFaces(ctx, listClusterFacesSQL, clusterUID, projectUID)
}

// queryFaces runs a face-selecting query in the canonical column order and scans
// the rows into Face values, decoding the halfvec embedding and bbox array.
func (s *Store) queryFaces(ctx context.Context, query string, args ...any) ([]Face, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cluster: querying faces: %w", err)
	}
	defer rows.Close()

	var faces []Face
	for rows.Next() {
		face, scanErr := scanFace(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		faces = append(faces, face)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cluster: iterating faces: %w", err)
	}
	return faces, nil
}

// scanFace reads one face row (id, photo_uid, face_index, embedding, bbox,
// det_score, model, project_id), decoding the halfvec embedding and the
// bounding-box array.
func scanFace(rows pgx.Rows) (Face, error) {
	var (
		face Face
		hv   pgvector.HalfVector
		bbox []float64
	)
	if err := rows.Scan(
		&face.ID, &face.PhotoUID, &face.FaceIndex, &hv, &bbox, &face.DetScore, &face.Model, &face.ProjectID,
	); err != nil {
		return Face{}, fmt.Errorf("cluster: scanning face: %w", err)
	}
	face.Vector = vectors.FromHalfVec(hv)
	copy(face.BBox[:], bbox)
	return face, nil
}

// insertClusterSQL inserts a cluster into one project and returns the stored row.
const insertClusterSQL = `
INSERT INTO face_clusters (uid, project_id, centroid, size, model)
VALUES ($1, $2, $3, $4, $5)
RETURNING ` + clusterColumns

// CreateCluster inserts a cluster of the project with the given centroid, size
// and model and returns it refreshed with a generated uid and timestamps.
func (s *Store) CreateCluster(
	ctx context.Context, projectUID string, centroid []float32, size int, model string,
) (Cluster, error) {
	if err := requireProject(projectUID); err != nil {
		return Cluster{}, err
	}
	uid, err := newClusterUID()
	if err != nil {
		return Cluster{}, err
	}
	row := s.pool.QueryRow(ctx, insertClusterSQL, uid, projectUID, vectors.ToHalfVec(centroid), size, model)
	return scanCluster(row)
}

// AddFacesToCluster sets cluster_uid on every face of the project whose id is in
// faceIDs, claiming them for the cluster; an id of another project's face is left
// alone. An empty slice is a no-op.
func (s *Store) AddFacesToCluster(ctx context.Context, projectUID, clusterUID string, faceIDs []int64) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	if len(faceIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		"UPDATE faces SET cluster_uid = $1 WHERE id = ANY($2) AND project_id = $3",
		clusterUID, faceIDs, projectUID)
	if err != nil {
		return fmt.Errorf("cluster: adding faces to cluster %s: %w", clusterUID, err)
	}
	return nil
}

// clusterColumns is the canonical column order every cluster-reading query uses,
// matched by scanCluster.
const clusterColumns = `uid, centroid, size, model, created_at, updated_at, summary`

// listReadyClustersSQL reads one page of one project's clusters that have a
// cached listing summary, newest first then by uid for a stable page boundary. It
// is served by idx_face_clusters_project_ready (migration 0102), which covers
// exactly this predicate and order.
const listReadyClustersSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE project_id = $1 AND summary IS NOT NULL
ORDER BY created_at DESC, uid
LIMIT $2 OFFSET $3`

// ListReadyClusters returns one page of the project's clusters whose listing
// summary has been built, newest first. Clusters still awaiting a summary are
// deliberately not returned: a group without one cannot be drawn, and the caller
// reports how many are pending instead. A page past the end yields an empty
// slice.
func (s *Store) ListReadyClusters(ctx context.Context, projectUID string, limit, offset int) ([]Cluster, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return s.queryClusters(ctx, listReadyClustersSQL, projectUID, limit, offset)
}

// listPendingClustersSQL reads one project's clusters with no cached summary yet,
// oldest first so the backlog drains in the order it was created. It is served
// by idx_face_clusters_project_pending (migration 0105).
const listPendingClustersSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE project_id = $1 AND summary IS NULL
ORDER BY created_at, uid
LIMIT $2`

// ListPendingClusters returns up to limit of the project's clusters that have no
// cached listing summary, oldest first. It is the work list of the background
// summary pass.
func (s *Store) ListPendingClusters(ctx context.Context, projectUID string, limit int) ([]Cluster, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return s.queryClusters(ctx, listPendingClustersSQL, projectUID, limit)
}

// queryClusters runs a cluster-selecting query in the canonical column order and
// scans the rows into Cluster values.
func (s *Store) queryClusters(ctx context.Context, query string, args ...any) ([]Cluster, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cluster: listing clusters: %w", err)
	}
	defer rows.Close()

	out := make([]Cluster, 0)
	for rows.Next() {
		c, scanErr := scanCluster(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cluster: iterating clusters: %w", err)
	}
	return out, nil
}

// countClustersSQL counts one project's clusters that can be listed and the ones
// still waiting for their summary, in one pass.
const countClustersSQL = `
SELECT count(*) FILTER (WHERE summary IS NOT NULL), count(*) FILTER (WHERE summary IS NULL)
FROM face_clusters
WHERE project_id = $1`

// CountClusters returns how many of the project's clusters have a cached listing
// summary (ready) and how many are still waiting for one (pending). The two
// together are every cluster of the project.
func (s *Store) CountClusters(ctx context.Context, projectUID string) (ready, pending int, err error) {
	if err := requireProject(projectUID); err != nil {
		return 0, 0, err
	}
	if err := s.pool.QueryRow(ctx, countClustersSQL, projectUID).Scan(&ready, &pending); err != nil {
		return 0, 0, fmt.Errorf("cluster: counting clusters: %w", err)
	}
	return ready, pending, nil
}

// SaveSummary stores the cached listing summary of one of the project's clusters,
// stamping when it was built. It returns ErrClusterNotFound when the project holds
// no such cluster (any more) — which a background pass treats as a skip, not a
// failure: a cluster named by somebody while its summary was being built is a
// cluster that no longer needs one.
func (s *Store) SaveSummary(ctx context.Context, projectUID, uid string, summary Summary) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("cluster: encoding summary of cluster %s: %w", uid, err)
	}
	tag, err := s.pool.Exec(ctx,
		"UPDATE face_clusters SET summary = $2, summary_at = now() WHERE uid = $1 AND project_id = $3",
		uid, encoded, projectUID)
	if err != nil {
		return fmt.Errorf("cluster: storing the summary of cluster %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClusterNotFound
	}
	return nil
}

// getClusterSQL reads one cluster of one project by uid.
const getClusterSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE uid = $1 AND project_id = $2`

// GetCluster returns the project's cluster identified by uid, or
// ErrClusterNotFound — also for a cluster of another project.
func (s *Store) GetCluster(ctx context.Context, projectUID, uid string) (Cluster, error) {
	if err := requireProject(projectUID); err != nil {
		return Cluster{}, err
	}
	c, err := scanCluster(s.pool.QueryRow(ctx, getClusterSQL, uid, projectUID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, ErrClusterNotFound
	}
	return c, err
}

// scanCluster reads one cluster row, decoding the halfvec centroid into
// []float32 and the cached summary JSON (absent for a cluster that has none).
func scanCluster(row pgx.Row) (Cluster, error) {
	var (
		c       Cluster
		hv      pgvector.HalfVector
		summary []byte
	)
	if err := row.Scan(&c.UID, &hv, &c.Size, &c.Model, &c.CreatedAt, &c.UpdatedAt, &summary); err != nil {
		return Cluster{}, fmt.Errorf("cluster: scanning cluster: %w", err)
	}
	c.Centroid = vectors.FromHalfVec(hv)
	if len(summary) > 0 {
		var decoded Summary
		if err := json.Unmarshal(summary, &decoded); err != nil {
			return Cluster{}, fmt.Errorf("cluster: decoding the summary of cluster %s: %w", c.UID, err)
		}
		c.Summary = &decoded
	}
	return c, nil
}

// DeleteCluster removes the project's cluster identified by uid. Its faces are
// detached (faces.cluster_uid is set NULL by the foreign key). It returns
// ErrClusterNotFound when the project holds no such cluster.
func (s *Store) DeleteCluster(ctx context.Context, projectUID, uid string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, "DELETE FROM face_clusters WHERE uid = $1 AND project_id = $2", uid, projectUID)
	if err != nil {
		return fmt.Errorf("cluster: deleting cluster %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClusterNotFound
	}
	return nil
}

// RemoveFaceFromCluster detaches one face (identified by photo and index) from
// the cluster, so a stray face does not pollute a name before assignment. It
// reports whether a member face was actually removed (false when the face was not
// in the cluster, or is a face of another project).
func (s *Store) RemoveFaceFromCluster(ctx context.Context, projectUID, clusterUID string, ref Ref) (bool, error) {
	if err := requireProject(projectUID); err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx,
		"UPDATE faces SET cluster_uid = NULL"+
			" WHERE cluster_uid = $1 AND photo_uid = $2 AND face_index = $3 AND project_id = $4",
		clusterUID, ref.PhotoUID, ref.FaceIndex, projectUID)
	if err != nil {
		return false, fmt.Errorf("cluster: removing face from cluster %s: %w", clusterUID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// RefreshCluster rewrites a cluster's centroid and size and drops its cached
// listing summary (bumping updated_at), used after a face is removed so neither
// the centroid nor the summary outlives the membership it was computed from. The
// caller writes the recomputed summary back with SaveSummary; until it does, the
// cluster is pending and the background pass would rebuild it anyway. It returns
// ErrClusterNotFound when the project holds no such cluster.
func (s *Store) RefreshCluster(ctx context.Context, projectUID, uid string, centroid []float32, size int) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		"UPDATE face_clusters SET centroid = $2, size = $3, summary = NULL, summary_at = NULL,"+
			" updated_at = now() WHERE uid = $1 AND project_id = $4",
		uid, vectors.ToHalfVec(centroid), size, projectUID)
	if err != nil {
		return fmt.Errorf("cluster: refreshing cluster %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClusterNotFound
	}
	return nil
}
