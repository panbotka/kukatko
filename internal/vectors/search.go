package vectors

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// findSimilarSQL ranks the image embeddings of one project ($4) by cosine
// distance to the query vector and returns the $3 nearest within $2. The HNSW
// index on (embedding halfvec_cosine_ops) serves the inner ORDER BY ... LIMIT
// with the project filter inside the scan; the outer query re-sorts (the
// iterative scan runs in relaxed order) and applies the distance cutoff, which
// would blind the iterative scan were it inside (see project.go).
const findSimilarSQL = `
WITH nearest AS MATERIALIZED (
    SELECT photo_uid, embedding <=> $1 AS distance
    FROM embeddings
    WHERE project_id = $4
    ORDER BY embedding <=> $1
    LIMIT $3
)
SELECT photo_uid, distance
FROM nearest
WHERE distance <= $2
ORDER BY distance, photo_uid`

// findSimilarFacesSQL ranks the face embeddings of one project ($4) by cosine
// distance to the query vector and returns the $3 nearest within $2, the same
// way findSimilarSQL does.
const findSimilarFacesSQL = `
WITH nearest AS MATERIALIZED (
    SELECT id, photo_uid, face_index, embedding <=> $1 AS distance
    FROM faces
    WHERE project_id = $4
    ORDER BY embedding <=> $1
    LIMIT $3
)
SELECT id, photo_uid, face_index, distance
FROM nearest
WHERE distance <= $2
ORDER BY distance, photo_uid, face_index`

// FindSimilar returns the photos of projectUID whose image embedding is closest
// to vec by cosine distance, nearest first. limit is clamped into [1, maxLimit]
// (with a default for non-positive values); maxDistance, when positive, drops
// matches farther than that distance (a non-positive value disables the filter).
// The query runs in a read-only transaction with hnsw.ef_search tuned for recall
// and, where pgvector supports it, the iterative scan that keeps a small project's
// result full (see project.go). It returns ErrNoProject without a project and
// ErrDimMismatch if vec is not ImageDim long.
func (s *Store) FindSimilar(
	ctx context.Context, projectUID string, vec []float32, limit int, maxDistance float64,
) ([]Match, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	if len(vec) != ImageDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), ImageDim)
	}
	var matches []Match
	err := s.withProjectReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit), projectUID)
		if err != nil {
			return fmt.Errorf("querying similar embeddings: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var m Match
			if err := rows.Scan(&m.PhotoUID, &m.Distance); err != nil {
				return fmt.Errorf("scanning similar embedding: %w", err)
			}
			matches = append(matches, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// FindSimilarFaces returns the faces of projectUID whose embedding is closest to
// vec by cosine distance, nearest first. limit and maxDistance behave as in
// FindSimilar, and so does the transaction. It returns ErrNoProject without a
// project and ErrDimMismatch if vec is not FaceDim long.
func (s *Store) FindSimilarFaces(
	ctx context.Context, projectUID string, vec []float32, limit int, maxDistance float64,
) ([]FaceMatch, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	if len(vec) != FaceDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), FaceDim)
	}
	var matches []FaceMatch
	err := s.withProjectReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarFacesSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit), projectUID)
		if err != nil {
			return fmt.Errorf("querying similar faces: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var m FaceMatch
			if err := rows.Scan(&m.ID, &m.PhotoUID, &m.FaceIndex, &m.Distance); err != nil {
				return fmt.Errorf("scanning similar face: %w", err)
			}
			matches = append(matches, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}
