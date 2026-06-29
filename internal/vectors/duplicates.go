package vectors

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DuplicatePair is an unordered pair of photos whose image embeddings lie within
// a cosine-distance threshold of each other. It backs the duplicates review,
// which builds groups from these near-neighbour links.
type DuplicatePair struct {
	// A and B are the two photos' uids; A is the row driving the neighbour scan.
	A string
	// B is the neighbour photo's uid.
	B string
	// Distance is the cosine distance between the two embeddings (smaller is
	// closer).
	Distance float64
}

// findDuplicatePairsSQL finds, for every embedding, its nearest neighbours by
// cosine distance using the HNSW index inside a correlated lateral join, keeping
// only neighbours within $2. The outer embedding is treated as a per-row query
// vector, so the inner ORDER BY ... LIMIT is served by the HNSW index rather than
// a full O(n^2) cross product. $1 caps the neighbours considered per photo.
const findDuplicatePairsSQL = `
SELECT a.photo_uid, n.photo_uid, n.distance
FROM embeddings a
CROSS JOIN LATERAL (
    SELECT e.photo_uid, a.embedding <=> e.embedding AS distance
    FROM embeddings e
    WHERE e.photo_uid <> a.photo_uid
    ORDER BY a.embedding <=> e.embedding
    LIMIT $1
) n
WHERE n.distance <= $2`

// FindDuplicatePairs returns pairs of photos whose image embeddings are within
// maxDist cosine distance, using each photo's neighbours (capped at the given
// count) found through the HNSW index. neighbours is clamped into [1, maxLimit];
// a non-positive maxDist returns no pairs (the caller must supply a real
// threshold, since every photo is trivially within distance 0 of itself and any
// identical copy). The scan runs in a read-only transaction with hnsw.ef_search
// tuned for recall. Pairs may appear in both directions and are NOT
// de-duplicated here; callers that build undirected groups treat (A,B) and
// (B,A) alike.
func (s *Store) FindDuplicatePairs(ctx context.Context, neighbours int, maxDist float64) ([]DuplicatePair, error) {
	if maxDist <= 0 {
		return nil, nil
	}
	var pairs []DuplicatePair
	err := s.withReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findDuplicatePairsSQL, normalizeLimit(neighbours), maxDist)
		if err != nil {
			return fmt.Errorf("querying duplicate pairs: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var p DuplicatePair
			if err := rows.Scan(&p.A, &p.B, &p.Distance); err != nil {
				return fmt.Errorf("scanning duplicate pair: %w", err)
			}
			pairs = append(pairs, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return pairs, nil
}
