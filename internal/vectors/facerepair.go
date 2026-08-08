package vectors

import (
	"context"
	"fmt"
)

// listTransposedFacesSQL selects every face row whose cached frame is its photo's
// stored pixel pair transposed — the fingerprint of the orientation defect — with
// the photo geometry the decision needs.
//
// It joins the photo instead of taking the frame on trust, which is what makes the
// repair re-runnable: the photos half of `repair --dimensions` corrects
// file_width/file_height, and a face row is a candidate exactly while its cached
// pair is the corrected one swapped. A row this repair decides to skip therefore
// still matches the next time it runs — a later marker on the photo is new
// evidence — and a row it has rewritten no longer does. Photos the photos half
// never fixed are absent by construction: without the file's own dimensions there
// is no frame to reason against. A square frame is excluded, where the swap and
// every candidate transform are identities.
const listTransposedFacesSQL = `
SELECT f.photo_uid, f.face_index, f.bbox, p.file_orientation, p.file_width, p.file_height
FROM faces f
JOIN photos p ON p.uid = f.photo_uid
WHERE p.file_orientation BETWEEN 5 AND 8
  AND f.orientation BETWEEN 5 AND 8
  AND p.file_width <> p.file_height
  AND f.photo_width = p.file_height
  AND f.photo_height = p.file_width
ORDER BY f.photo_uid, f.face_index`

// listFaceMarkersSQL reads the face markers of the given photos: the regions a
// person has seen sit on a face in the DISPLAYED image, which is the reference
// frame the repair reconciles a detection against. Invalid markers (a region a
// user rejected) and degenerate boxes are left out — a rejected region is not
// evidence of where a face is.
const listFaceMarkersSQL = `
SELECT photo_uid, x, y, w, h
FROM markers
WHERE photo_uid = ANY($1)
  AND type = 'face'
  AND invalid = FALSE
  AND w > 0 AND h > 0`

// repairFaceBoxSQL writes one planned row: the corrected cached frame and the
// transformed box. The WHERE clause repeats the fingerprint with the pair swapped,
// so the write is a no-op once applied (and if the row changed underneath), which
// is what makes the repair idempotent and re-runnable.
const repairFaceBoxSQL = `
UPDATE faces
SET photo_width  = $3,
    photo_height = $4,
    bbox         = $5::double precision[]
WHERE photo_uid = $1
  AND face_index = $2
  AND orientation BETWEEN 5 AND 8
  AND photo_width = $4
  AND photo_height = $3`

// PlanFaceBoxRepair decides, for every face row recorded against the transposed
// frame of a quarter-turned photo, what the faces half of the orientation repair
// would do with it. It is read-only — the dry run — so `maintenance scan` and
// `maintenance repair --dimensions` report and apply the same decisions.
//
// The result holds one plan per candidate row, in (photo, face index) order,
// including the rows the evidence cannot place (TransformSkip). An empty slice
// means the catalogue holds no such row.
func (s *Store) PlanFaceBoxRepair(ctx context.Context) ([]FaceBoxPlan, error) {
	faces, err := s.listTransposedFaces(ctx)
	if err != nil {
		return nil, err
	}
	if len(faces) == 0 {
		return nil, nil
	}
	markers, err := s.faceMarkersByPhoto(ctx, photoUIDsOf(faces))
	if err != nil {
		return nil, err
	}
	plans := make([]FaceBoxPlan, 0, len(faces))
	for start := 0; start < len(faces); {
		end := start + 1
		for end < len(faces) && faces[end].PhotoUID == faces[start].PhotoUID {
			end++
		}
		plans = append(plans, planFaceBoxes(faces[start:end], markers[faces[start].PhotoUID])...)
		start = end
	}
	return plans, nil
}

// ApplyFaceBoxRepair writes the plans that carry a transform and returns how many
// face rows changed. Plans with TransformSkip are written not at all — not even
// their cached frame — so a row the evidence could not place keeps the fingerprint
// a later run finds it by.
//
// Each row is written under the fingerprint it was planned from, so a re-run, an
// interrupted run and a row somebody else corrected in between all converge
// without moving a box twice.
func (s *Store) ApplyFaceBoxRepair(ctx context.Context, plans []FaceBoxPlan) (int64, error) {
	var applied int64
	for _, plan := range plans {
		if plan.Transform == TransformSkip {
			continue
		}
		bbox := plan.BBox
		tag, err := s.pool.Exec(ctx, repairFaceBoxSQL,
			plan.Face.PhotoUID, plan.Face.FaceIndex,
			plan.Face.RawWidth, plan.Face.RawHeight, bbox[:])
		if err != nil {
			return applied, fmt.Errorf("vectors: repairing face box %s#%d: %w",
				plan.Face.PhotoUID, plan.Face.FaceIndex, err)
		}
		applied += tag.RowsAffected()
	}
	return applied, nil
}

// listTransposedFaces reads the candidate rows of listTransposedFacesSQL.
func (s *Store) listTransposedFaces(ctx context.Context) ([]TransposedFace, error) {
	rows, err := s.pool.Query(ctx, listTransposedFacesSQL)
	if err != nil {
		return nil, fmt.Errorf("vectors: listing transposed faces: %w", err)
	}
	defer rows.Close()

	var faces []TransposedFace
	for rows.Next() {
		var (
			face TransposedFace
			bbox []float64
		)
		if scanErr := rows.Scan(&face.PhotoUID, &face.FaceIndex, &bbox,
			&face.Orientation, &face.RawWidth, &face.RawHeight); scanErr != nil {
			return nil, fmt.Errorf("vectors: scanning transposed face: %w", scanErr)
		}
		copy(face.BBox[:], bbox)
		faces = append(faces, face)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vectors: iterating transposed faces: %w", err)
	}
	return faces, nil
}

// faceMarkersByPhoto reads the face markers of the given photos, grouped by photo.
// A photo with no usable marker is simply absent from the map, which the caller
// reads as "no evidence".
func (s *Store) faceMarkersByPhoto(ctx context.Context, photoUIDs []string) (map[string][][4]float64, error) {
	rows, err := s.pool.Query(ctx, listFaceMarkersSQL, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("vectors: listing face markers: %w", err)
	}
	defer rows.Close()

	markers := make(map[string][][4]float64, len(photoUIDs))
	for rows.Next() {
		var (
			photoUID string
			box      [4]float64
		)
		if scanErr := rows.Scan(&photoUID, &box[0], &box[1], &box[2], &box[3]); scanErr != nil {
			return nil, fmt.Errorf("vectors: scanning face marker: %w", scanErr)
		}
		markers[photoUID] = append(markers[photoUID], box)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vectors: iterating face markers: %w", err)
	}
	return markers, nil
}

// photoUIDsOf returns the distinct photo uids of an ordered run of candidate rows.
func photoUIDsOf(faces []TransposedFace) []string {
	uids := make([]string, 0, len(faces))
	for i, face := range faces {
		if i == 0 || face.PhotoUID != faces[i-1].PhotoUID {
			uids = append(uids, face.PhotoUID)
		}
	}
	return uids
}
