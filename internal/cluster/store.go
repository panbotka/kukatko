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
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// listUnclusteredFacesSQL selects every clusterable face — unassigned (no
// subject) and not yet in a cluster — with the data the algorithm needs, ordered
// by id so re-clustering is deterministic.
const listUnclusteredFacesSQL = `
SELECT id, photo_uid, face_index, embedding, bbox, det_score, model
FROM faces
WHERE subject_uid IS NULL AND cluster_uid IS NULL
ORDER BY id`

// ListUnclusteredFaces returns the faces eligible for clustering: those with no
// subject assignment and no cluster, newest grouping decisions preserved by
// excluding already-clustered faces. The result is ordered by id for determinism.
func (s *Store) ListUnclusteredFaces(ctx context.Context) ([]Face, error) {
	return s.queryFaces(ctx, listUnclusteredFacesSQL)
}

// countClusterableFacesSQL counts the faces eligible for clustering, stopping at
// the caller's cap. The question it answers is "are there at least N", and a
// library with a hundred thousand unassigned faces must not pay for counting
// every one of them to answer it.
const countClusterableFacesSQL = `
SELECT count(*) FROM (
	SELECT 1 FROM faces WHERE subject_uid IS NULL AND cluster_uid IS NULL LIMIT $1
) AS clusterable`

// CountClusterableFaces returns how many faces are eligible for clustering —
// unassigned and not yet in a cluster — counting at most limit of them, so the
// answer costs the same on an empty library and on a huge one. A non-positive
// limit counts nothing.
func (s *Store) CountClusterableFaces(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	var count int
	if err := s.pool.QueryRow(ctx, countClusterableFacesSQL, limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("cluster: counting clusterable faces: %w", err)
	}
	return count, nil
}

// listClusterFacesSQL selects every face belonging to one cluster, ordered by id.
const listClusterFacesSQL = `
SELECT id, photo_uid, face_index, embedding, bbox, det_score, model
FROM faces
WHERE cluster_uid = $1
ORDER BY id`

// ListClusterFaces returns every face that belongs to the cluster identified by
// clusterUID, ordered by id. A cluster with no faces yields an empty slice.
func (s *Store) ListClusterFaces(ctx context.Context, clusterUID string) ([]Face, error) {
	return s.queryFaces(ctx, listClusterFacesSQL, clusterUID)
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
// det_score, model), decoding the halfvec embedding and the bounding-box array.
func scanFace(rows pgx.Rows) (Face, error) {
	var (
		face Face
		hv   pgvector.HalfVector
		bbox []float64
	)
	if err := rows.Scan(&face.ID, &face.PhotoUID, &face.FaceIndex, &hv, &bbox, &face.DetScore, &face.Model); err != nil {
		return Face{}, fmt.Errorf("cluster: scanning face: %w", err)
	}
	face.Vector = vectors.FromHalfVec(hv)
	copy(face.BBox[:], bbox)
	return face, nil
}

// insertClusterSQL inserts a cluster and returns the stored row.
const insertClusterSQL = `
INSERT INTO face_clusters (uid, centroid, size, model)
VALUES ($1, $2, $3, $4)
RETURNING ` + clusterColumns

// CreateCluster inserts a cluster with the given centroid, size and model and
// returns it refreshed with a generated uid and timestamps.
func (s *Store) CreateCluster(ctx context.Context, centroid []float32, size int, model string) (Cluster, error) {
	uid, err := newClusterUID()
	if err != nil {
		return Cluster{}, err
	}
	row := s.pool.QueryRow(ctx, insertClusterSQL, uid, vectors.ToHalfVec(centroid), size, model)
	return scanCluster(row)
}

// AddFacesToCluster sets cluster_uid on every face whose id is in faceIDs,
// claiming them for the cluster. An empty slice is a no-op.
func (s *Store) AddFacesToCluster(ctx context.Context, clusterUID string, faceIDs []int64) error {
	if len(faceIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		"UPDATE faces SET cluster_uid = $1 WHERE id = ANY($2)", clusterUID, faceIDs)
	if err != nil {
		return fmt.Errorf("cluster: adding faces to cluster %s: %w", clusterUID, err)
	}
	return nil
}

// clusterColumns is the canonical column order every cluster-reading query uses,
// matched by scanCluster.
const clusterColumns = `uid, centroid, size, model, created_at, updated_at, summary`

// listReadyClustersSQL reads one page of the clusters that have a cached listing
// summary, newest first then by uid for a stable page boundary. It is served by
// idx_face_clusters_ready, which covers exactly this predicate and order.
const listReadyClustersSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE summary IS NOT NULL
ORDER BY created_at DESC, uid
LIMIT $1 OFFSET $2`

// ListReadyClusters returns one page of the clusters whose listing summary has
// been built, newest first. Clusters still awaiting a summary are deliberately
// not returned: a group without one cannot be drawn, and the caller reports how
// many are pending instead. A page past the end yields an empty slice.
func (s *Store) ListReadyClusters(ctx context.Context, limit, offset int) ([]Cluster, error) {
	return s.queryClusters(ctx, listReadyClustersSQL, limit, offset)
}

// listPendingClustersSQL reads the clusters with no cached summary yet, oldest
// first so the backlog drains in the order it was created. It is served by
// idx_face_clusters_summary_pending.
const listPendingClustersSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE summary IS NULL
ORDER BY created_at, uid
LIMIT $1`

// ListPendingClusters returns up to limit clusters that have no cached listing
// summary, oldest first. It is the work list of the background summary pass.
func (s *Store) ListPendingClusters(ctx context.Context, limit int) ([]Cluster, error) {
	return s.queryClusters(ctx, listPendingClustersSQL, limit)
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

// countClustersSQL counts the clusters that can be listed and the ones still
// waiting for their summary, in one pass over the table.
const countClustersSQL = `
SELECT count(*) FILTER (WHERE summary IS NOT NULL), count(*) FILTER (WHERE summary IS NULL)
FROM face_clusters`

// CountClusters returns how many clusters have a cached listing summary (ready)
// and how many are still waiting for one (pending). The two together are every
// cluster in the library.
func (s *Store) CountClusters(ctx context.Context) (ready, pending int, err error) {
	if err := s.pool.QueryRow(ctx, countClustersSQL).Scan(&ready, &pending); err != nil {
		return 0, 0, fmt.Errorf("cluster: counting clusters: %w", err)
	}
	return ready, pending, nil
}

// SaveSummary stores the cached listing summary of one cluster, stamping when it
// was built. It returns ErrClusterNotFound when the cluster is gone — which a
// background pass treats as a skip, not a failure: a cluster named by somebody
// while its summary was being built is a cluster that no longer needs one.
func (s *Store) SaveSummary(ctx context.Context, uid string, summary Summary) error {
	encoded, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("cluster: encoding summary of cluster %s: %w", uid, err)
	}
	tag, err := s.pool.Exec(ctx,
		"UPDATE face_clusters SET summary = $2, summary_at = now() WHERE uid = $1", uid, encoded)
	if err != nil {
		return fmt.Errorf("cluster: storing the summary of cluster %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClusterNotFound
	}
	return nil
}

// getClusterSQL reads one cluster by uid.
const getClusterSQL = `
SELECT ` + clusterColumns + `
FROM face_clusters
WHERE uid = $1`

// GetCluster returns the cluster identified by uid, or ErrClusterNotFound.
func (s *Store) GetCluster(ctx context.Context, uid string) (Cluster, error) {
	c, err := scanCluster(s.pool.QueryRow(ctx, getClusterSQL, uid))
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

// DeleteCluster removes the cluster identified by uid. Its faces are detached
// (faces.cluster_uid is set NULL by the foreign key). It returns
// ErrClusterNotFound when no such cluster exists.
func (s *Store) DeleteCluster(ctx context.Context, uid string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM face_clusters WHERE uid = $1", uid)
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
// in the cluster).
func (s *Store) RemoveFaceFromCluster(ctx context.Context, clusterUID string, ref Ref) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		"UPDATE faces SET cluster_uid = NULL WHERE cluster_uid = $1 AND photo_uid = $2 AND face_index = $3",
		clusterUID, ref.PhotoUID, ref.FaceIndex)
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
// ErrClusterNotFound when no such cluster exists.
func (s *Store) RefreshCluster(ctx context.Context, uid string, centroid []float32, size int) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE face_clusters SET centroid = $2, size = $3, summary = NULL, summary_at = NULL,"+
			" updated_at = now() WHERE uid = $1",
		uid, vectors.ToHalfVec(centroid), size)
	if err != nil {
		return fmt.Errorf("cluster: refreshing cluster %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClusterNotFound
	}
	return nil
}
