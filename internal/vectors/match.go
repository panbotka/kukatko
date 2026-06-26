package vectors

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// updateFaceMarkerSQL writes the denormalised people-assignment cache columns on
// a single face identified by (photo_uid, face_index). Empty string arguments are
// stored as NULL for the nullable identifier columns so a cleared link round-trips
// as a nil pointer.
const updateFaceMarkerSQL = `
UPDATE faces
SET marker_uid   = NULLIF($3, ''),
    subject_uid  = NULLIF($4, ''),
    subject_name = $5
WHERE photo_uid = $1 AND face_index = $2`

// UpdateFaceMarker caches the marker/subject assignment on the face identified by
// (photoUID, faceIndex). An empty markerUID or subjectUID is stored as NULL; an
// empty subjectName is stored as the empty string (matching the column default).
// It is how face↔marker IoU matching persists the matched marker on the face row,
// and how an assignment links a specific face to its marker. A (photoUID,
// faceIndex) pair with no matching row updates nothing and returns nil.
func (s *Store) UpdateFaceMarker(
	ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string,
) error {
	if _, err := s.pool.Exec(ctx, updateFaceMarkerSQL,
		photoUID, faceIndex, markerUID, subjectUID, subjectName); err != nil {
		return fmt.Errorf("updating face marker for %s face %d: %w", photoUID, faceIndex, err)
	}
	return nil
}

// findSimilarFaceCandidatesSQL ranks face embeddings by cosine distance to the
// query vector, keeping only those within $2 and returning the $3 nearest, with
// the cached assignment columns and the bounding box needed to build suggestions.
const findSimilarFaceCandidatesSQL = `
SELECT photo_uid, face_index, embedding <=> $1 AS distance, bbox,
       subject_uid, subject_name, marker_uid
FROM faces
WHERE (embedding <=> $1) <= $2
ORDER BY embedding <=> $1
LIMIT $3`

// FindSimilarFaceCandidates returns the faces whose embedding is closest to vec
// by cosine distance, nearest first, each carrying its cached subject assignment
// and bounding box. limit and maxDistance behave as in FindSimilarFaces. The query
// runs in a read-only transaction with hnsw.ef_search tuned for recall. It returns
// ErrDimMismatch if vec is not FaceDim long. It backs the face-suggestion logic,
// which aggregates the assigned neighbours by subject.
func (s *Store) FindSimilarFaceCandidates(
	ctx context.Context, vec []float32, limit int, maxDistance float64,
) ([]FaceCandidate, error) {
	if len(vec) != FaceDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), FaceDim)
	}
	var candidates []FaceCandidate
	err := s.withReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarFaceCandidatesSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit))
		if err != nil {
			return fmt.Errorf("querying similar face candidates: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			candidate, scanErr := scanFaceCandidate(rows)
			if scanErr != nil {
				return scanErr
			}
			candidates = append(candidates, candidate)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

// scanFaceCandidate reads one row of findSimilarFaceCandidatesSQL, decoding the
// bounding-box array into the fixed-size BBox field.
func scanFaceCandidate(rows pgx.Rows) (FaceCandidate, error) {
	var (
		candidate FaceCandidate
		bbox      []float64
	)
	if err := rows.Scan(
		&candidate.PhotoUID, &candidate.FaceIndex, &candidate.Distance, &bbox,
		&candidate.SubjectUID, &candidate.SubjectName, &candidate.MarkerUID,
	); err != nil {
		return FaceCandidate{}, fmt.Errorf("scanning similar face candidate: %w", err)
	}
	copy(candidate.BBox[:], bbox)
	return candidate, nil
}
