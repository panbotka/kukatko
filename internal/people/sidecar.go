package people

import (
	"context"
	"fmt"
)

// MarkerSubject is a marker together with the name of the subject it is assigned
// to. It exists because ListMarkersByPhoto returns only a SubjectUID, and a UID
// is the half of an assignment that does not survive losing the database: an
// exporter has to record the name to have recorded anything at all.
type MarkerSubject struct {
	Marker
	// SubjectName is the assigned subject's display name, empty for an unassigned
	// marker (a detected face nobody has named yet).
	SubjectName string `json:"subject_name,omitempty"`
	// SubjectType is the assigned subject's kind (person, animal, other), empty for
	// an unassigned marker.
	SubjectType SubjectType `json:"subject_type,omitempty"`
}

// listMarkersWithSubjectsSQL selects a photo's markers with the assigned
// subject's name and type, oldest first. The join is LEFT because an unassigned
// marker — a face the detector found and nobody has named — is a normal and
// common row, and losing those would silently drop every pending face from the
// export.
const listMarkersWithSubjectsSQL = `
SELECT m.uid, m.photo_uid, m.subject_uid, m.type, m.x, m.y, m.w, m.h,
       m.score, m.invalid, m.reviewed, m.created_at, m.updated_at,
       COALESCE(s.name, ''), COALESCE(s.type, '')
FROM markers m
LEFT JOIN subjects s ON s.uid = m.subject_uid
WHERE m.photo_uid = $1
ORDER BY m.created_at, m.uid`

// ListMarkersWithSubjects returns every marker on the photo identified by
// photoUID together with the assigned subject's name and type, oldest first. An
// unknown photo yields an empty slice (not an error).
//
// It is ListMarkersByPhoto resolved in one query rather than a lookup per
// distinct subject, and it is what the metadata sidecar export records: a marker
// without its box cannot be rebuilt, and a box without its subject's name is a
// rectangle nobody can identify.
func (s *Store) ListMarkersWithSubjects(ctx context.Context, photoUID string) ([]MarkerSubject, error) {
	rows, err := s.pool.Query(ctx, listMarkersWithSubjectsSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("people: listing markers with subjects for photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]MarkerSubject, 0)
	for rows.Next() {
		var ms MarkerSubject
		if err := rows.Scan(&ms.UID, &ms.PhotoUID, &ms.SubjectUID, &ms.Type,
			&ms.X, &ms.Y, &ms.W, &ms.H, &ms.Score, &ms.Invalid, &ms.Reviewed,
			&ms.CreatedAt, &ms.UpdatedAt, &ms.SubjectName, &ms.SubjectType); err != nil {
			return nil, fmt.Errorf("people: scanning marker for photo %s: %w", photoUID, err)
		}
		out = append(out, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating markers for photo %s: %w", photoUID, err)
	}
	return out, nil
}
