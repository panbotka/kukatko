package vectors

import (
	"context"
	"fmt"
)

// listSidewaysDetectionsSQL selects the quarter-turned photos whose recorded face
// detection cannot be shown to have run on an upright image — the fingerprint of a
// detection the sidecar performed on a sideways picture.
//
// Two kinds of row match. One has no frame recorded at all (NULL, i.e. written
// before migration 0061): on a quarter-turned photo every such detection ran
// sideways, because that is what the job did until this was fixed. The other has a
// frame recorded that is the photo's stored pair, not the displayed one, which says
// so outright. A detection whose frame is the display frame — what the fixed job
// records — never matches, so a re-detected photo leaves this set and stays out of
// it: the repair converges and is safe to re-run.
//
// Photos that are not quarter-turned are excluded on purpose: their raw and display
// frames coincide, so nothing about their boxes depends on the distinction (a
// mirror or a 180° flip does turn the picture, but re-detecting the whole library
// over an unrecorded frame is not what this repair is for). A square frame is
// likewise excluded, where a swap changes nothing.
const listSidewaysDetectionsSQL = `
SELECT fd.photo_uid
FROM face_detections fd
JOIN photos p ON p.uid = fd.photo_uid
WHERE p.file_orientation BETWEEN 5 AND 8
  AND p.file_width <> p.file_height
  AND (
        fd.detect_width IS NULL
     OR fd.detect_height IS NULL
     OR (fd.detect_width = p.file_width AND fd.detect_height = p.file_height)
  )
ORDER BY p.created_at DESC, fd.photo_uid DESC`

// clearFaceDetectionSQL deletes one photo's detection record, which is what makes
// the photo unprocessed again and therefore eligible for face detection. The face
// rows are deliberately left in place: the next detection replaces them wholesale
// (RecordFaceDetection), and until the sidecar — which is usually asleep — answers,
// keeping the boxes the library already has beats deleting data on the promise of
// better data later.
const clearFaceDetectionSQL = `DELETE FROM face_detections WHERE photo_uid = $1`

// ListSidewaysDetections returns the uids of quarter-turned photos whose recorded
// face detection ran on a sideways image (or cannot be shown not to have),
// newest first. It is read-only — the dry run of
// `maintenance repair --sideways-faces`, whose every uid it re-detects.
//
// Every face on such a photo is suspect twice over: the boxes were normalised
// against a frame the detector never saw, and faces the detector missed on a
// sideways picture are absent entirely. The second is why the repair re-detects
// rather than recomputing coordinates — measured against the running sidecar, one
// production original yielded 2 misshapen boxes sideways and 6 face-shaped ones
// upright, another 0 against 2 — and why a photo with no faces at all is in the
// set just as much as one with faces.
func (s *Store) ListSidewaysDetections(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, listSidewaysDetectionsSQL)
	if err != nil {
		return nil, fmt.Errorf("vectors: listing sideways face detections: %w", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if scanErr := rows.Scan(&uid); scanErr != nil {
			return nil, fmt.Errorf("vectors: scanning sideways face detection: %w", scanErr)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vectors: iterating sideways face detections: %w", err)
	}
	return uids, nil
}

// ClearFaceDetection removes the photo's face-detection record, reporting whether
// there was one to remove. The photo is then indistinguishable from one that has
// never been processed, so the face_detect job runs on it again instead of taking
// its idempotent skip; its existing face rows are kept until that run replaces
// them. Clearing a photo that has no record is a no-op, which keeps the repair
// re-runnable.
func (s *Store) ClearFaceDetection(ctx context.Context, photoUID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, clearFaceDetectionSQL, photoUID)
	if err != nil {
		return false, fmt.Errorf("vectors: clearing face detection for %s: %w", photoUID, err)
	}
	return tag.RowsAffected() > 0, nil
}
