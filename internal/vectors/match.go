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

// findSimilarUnassignedFaceCandidatesSQL ranks the face embeddings that are not yet
// assigned to any subject (subject_uid IS NULL) by cosine distance to the query
// vector, keeping only those within $2 and excluding any (photo_uid, face_index)
// pair present in the exclusion arrays ($4 photo uids, $5 face indexes), then
// returning the $3 nearest. The exclusion is a NOT EXISTS anti-join over the two
// parallel arrays unnested into rows, so it filters in SQL — before the LIMIT — and
// an empty exclusion set unnests to no rows (matching everything). Run under an
// iterative HNSW scan (see withFilteredReadTx), the LIMIT is filled from rows that
// pass the filters instead of shrinking to whatever survives the first ef_search
// candidates.
const findSimilarUnassignedFaceCandidatesSQL = `
SELECT photo_uid, face_index, embedding <=> $1 AS distance, bbox,
       subject_uid, subject_name, marker_uid
FROM faces
WHERE subject_uid IS NULL
  AND (embedding <=> $1) <= $2
  AND NOT EXISTS (
      SELECT 1
      FROM unnest($4::text[], $5::int[]) AS ex(photo_uid, face_index)
      WHERE ex.photo_uid = faces.photo_uid AND ex.face_index = faces.face_index)
ORDER BY embedding <=> $1
LIMIT $3`

// FindSimilarUnassignedFaceCandidates returns the faces whose embedding is closest
// to vec by cosine distance, nearest first, restricted to faces not yet assigned to
// any subject (subject_uid IS NULL) and with every face in exclude filtered out. It
// is the search every review feature needs: "find the nearest faces nobody has named
// yet", minus the ones already rejected for the subject being searched. limit and
// maxDistance behave as in FindSimilarFaceCandidates. Both filters are applied in
// SQL before the LIMIT and the query runs with pgvector's iterative index scan, so
// the caller gets the number of candidates it asked for even when the exclusion set
// eats into the nearest neighbours — filtering after the HNSW limit would silently
// shrink the result set, a real bug. It returns ErrDimMismatch if vec is not FaceDim
// long.
func (s *Store) FindSimilarUnassignedFaceCandidates(
	ctx context.Context, vec []float32, limit int, maxDistance float64, exclude []FaceKey,
) ([]FaceCandidate, error) {
	if len(vec) != FaceDim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vec), FaceDim)
	}
	excludePhotos, excludeIndexes := splitFaceKeys(exclude)
	var candidates []FaceCandidate
	err := s.withFilteredReadTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, findSimilarUnassignedFaceCandidatesSQL,
			ToHalfVec(vec), normalizeMaxDistance(maxDistance), normalizeLimit(limit),
			excludePhotos, excludeIndexes)
		if err != nil {
			return fmt.Errorf("querying similar unassigned face candidates: %w", err)
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

// splitFaceKeys unzips an exclusion set into the two parallel arrays the SQL
// anti-join binds: photo uids and face indexes at matching positions. It always
// returns non-nil slices so an empty exclusion set binds as empty arrays (which
// unnest to no rows) rather than SQL NULL. The indexes stay []int (bound as
// bigint[] and cast to int[] by the query's $5::int[]), avoiding a narrowing
// conversion.
func splitFaceKeys(keys []FaceKey) ([]string, []int) {
	photos := make([]string, len(keys))
	indexes := make([]int, len(keys))
	for i, key := range keys {
		photos[i] = key.PhotoUID
		indexes[i] = key.FaceIndex
	}
	return photos, indexes
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
