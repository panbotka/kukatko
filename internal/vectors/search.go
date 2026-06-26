package vectors

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// findSimilarSQL ranks image embeddings by cosine distance to the query vector,
// keeping only those within $2 and returning the $3 nearest. The HNSW index on
// (embedding halfvec_cosine_ops) serves the ORDER BY ... LIMIT.
const findSimilarSQL = `
SELECT photo_uid, embedding <=> $1 AS distance
FROM embeddings
WHERE (embedding <=> $1) <= $2
ORDER BY embedding <=> $1
LIMIT $3`

// findSimilarFacesSQL ranks face embeddings by cosine distance to the query
// vector, keeping only those within $2 and returning the $3 nearest.
const findSimilarFacesSQL = `
SELECT id, photo_uid, face_index, embedding <=> $1 AS distance
FROM faces
WHERE (embedding <=> $1) <= $2
ORDER BY embedding <=> $1
LIMIT $3`

// FindSimilar returns the photos whose image embedding is closest to vec by
// cosine distance, nearest first. limit is clamped into [1, maxLimit] (with a
// default for non-positive values); maxDistance, when positive, drops matches
// farther than that distance (a non-positive value disables the filter). The
// query runs in a read-only transaction with hnsw.ef_search tuned for recall.
// It returns ErrDimMismatch if vec is not ImageDim long.
func (s *Store) FindSimilar(
	ctx context.Context, vec []float32, limit int, maxDistance float64,
) ([]Match, error) {
	if len(vec) != ImageDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), ImageDim)
	}
	var matches []Match
	err := s.withReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit))
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

// FindSimilarFaces returns the faces whose embedding is closest to vec by cosine
// distance, nearest first. limit and maxDistance behave as in FindSimilar. The
// query runs in a read-only transaction with hnsw.ef_search tuned for recall. It
// returns ErrDimMismatch if vec is not FaceDim long.
func (s *Store) FindSimilarFaces(
	ctx context.Context, vec []float32, limit int, maxDistance float64,
) ([]FaceMatch, error) {
	if len(vec) != FaceDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), FaceDim)
	}
	var matches []FaceMatch
	err := s.withReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarFacesSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit))
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
