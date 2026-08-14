package people

import (
	"context"
	"fmt"
)

// Cover is the photo that stands for a subject: the uid to link to and the file
// hash the thumbnail cache is keyed by. It is deliberately a *photo*, not the
// SubjectFace the people index crops its tiles from — a compact row (the command
// palette) draws a whole picture at medallion size, where a face crop cut from a
// small tile is mush.
type Cover struct {
	// PhotoUID is the cover photo's uid.
	PhotoUID string
	// FileHash is that photo's SHA256, the thumbnail cache key.
	FileHash string
}

// subjectCoversSQL resolves the cover of a whole batch of subjects in one
// statement: the newest photo carrying a valid marker of that subject, with the
// upload time standing in for an unknown capture time and the uid breaking ties,
// so the same subject yields the same cover on every request.
//
// The DISTINCT ON runs over the batch's markers only — never a correlated "ORDER
// BY … LIMIT 1" per subject, which walks the global photos.taken_at order once
// per row (see organize's listAlbumsSQL note and docs/PERF.md). The photo filters
// mirror the subject gallery's, so a cover never points at a photo the reader
// cannot reach by opening the person.
//
// A cover chosen by hand wins over the derived one, and is honoured whatever
// state its photo is in: it is the user's own explicit answer to what the
// subject looks like.
const subjectCoversSQL = `
WITH derived AS (
    SELECT DISTINCT ON (m.subject_uid) m.subject_uid, p.uid AS photo_uid, p.file_hash
    FROM markers m
    JOIN photos p ON p.uid = m.photo_uid
    WHERE m.subject_uid = ANY($1) AND m.invalid = FALSE
      AND p.archived_at IS NULL AND NOT p.hidden_from_library
      AND (p.stack_uid IS NULL OR p.stack_primary)
    ORDER BY m.subject_uid, COALESCE(p.taken_at, p.created_at) DESC, p.uid
)
SELECT s.uid,
       COALESCE(picked.uid, derived.photo_uid),
       COALESCE(picked.file_hash, derived.file_hash)
FROM subjects s
LEFT JOIN photos picked ON picked.uid = s.cover_photo_uid
LEFT JOIN derived ON derived.subject_uid = s.uid
WHERE s.uid = ANY($1)`

// SubjectCovers returns, per subject uid, the photo that stands for it: the
// hand-picked cover when one is set, otherwise the newest visible photo the
// subject appears on. A subject shown on no visible photo is simply absent from
// the map — as is a uid that names nothing — so a caller reads "no cover" the
// same way either way. An empty input yields an empty map without touching the
// database.
//
// It answers the whole batch in ONE query on purpose, so a caller drawing a page
// of people never turns the previews into a query per row.
func (s *Store) SubjectCovers(ctx context.Context, uids []string) (map[string]Cover, error) {
	out := make(map[string]Cover, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, subjectCoversSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("people: listing subject covers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		// Both photo columns ride on a LEFT JOIN, so a subject with nothing to
		// show still produces a row; a NULL uid is what says it has no cover.
		var photoUID, fileHash *string
		if err := rows.Scan(&uid, &photoUID, &fileHash); err != nil {
			return nil, fmt.Errorf("people: scanning subject cover: %w", err)
		}
		if photoUID == nil {
			continue
		}
		out[uid] = Cover{PhotoUID: *photoUID, FileHash: deref(fileHash)}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating subject covers: %w", err)
	}
	return out, nil
}
