package people

// One subject's headline numbers, for the places that want to say something
// about a person in a sentence rather than list their photos: how much of the
// library they are on and how far back they go.
//
// It is a separate, single-row query rather than a filter over ListSubjects
// because the callers are different: the people index needs every subject and
// pays for a full aggregation once, while this answers "tell me about *this*
// person" on a path where the person is already known — the review game's reveal
// after a confirmed assignment, one small query per answered question. Both
// counts apply the same visibility rule ListSubjects does, so the number shown
// after an answer is the number the person's own page shows.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SubjectStats summarises one subject: how many visible photos they appear on
// and the span of years those photos cover.
type SubjectStats struct {
	// UID and Name identify the subject, so a caller holding only a uid can
	// render the whole line without a second read.
	UID  string `json:"uid"`
	Name string `json:"name"`
	// PhotoCount is how many distinct visible photos the subject appears on — the
	// same figure SubjectCount.PhotoCount carries and the same one the subject's
	// gallery pages through.
	PhotoCount int `json:"photo_count"`
	// OldestYear and NewestYear are the capture years of the subject's earliest
	// and latest dated photo, both zero when none of their photos carries a date
	// (a wholly undated person is common in a scanned archive).
	OldestYear int `json:"oldest_year"`
	NewestYear int `json:"newest_year"`
}

// subjectStatsSQL aggregates one subject's visible photos. The joins mirror
// listSubjectsSQL's — non-invalid markers, non-archived photos, primary stack
// members only — so the count agrees with the people index and the subject's
// gallery rather than being a third opinion. COUNT(DISTINCT p.uid) because one
// photo can carry several markers of the same person, and MIN/MAX ignore the
// NULL taken_at of an undated photo, which is why an all-undated subject comes
// back as a real count with zero years rather than as no row.
//
// It is a primary-key lookup on subjects plus an index scan on
// idx_markers_subject_uid, which is what makes it cheap enough to run on the
// answer path.
const subjectStatsSQL = `
SELECT s.uid,
       s.name,
       COUNT(DISTINCT p.uid) AS photo_count,
       COALESCE(EXTRACT(YEAR FROM MIN(p.taken_at))::int, 0) AS oldest_year,
       COALESCE(EXTRACT(YEAR FROM MAX(p.taken_at))::int, 0) AS newest_year
FROM subjects s
LEFT JOIN markers m ON m.subject_uid = s.uid AND m.invalid = FALSE
LEFT JOIN photos p ON p.uid = m.photo_uid AND p.archived_at IS NULL
    AND (p.stack_uid IS NULL OR p.stack_primary)
WHERE s.uid = $1
GROUP BY s.uid, s.name`

// SubjectStats returns the subject's photo count and year span. It returns
// ErrSubjectNotFound when no subject carries that uid, and a wrapped error on
// any query failure.
func (s *Store) SubjectStats(ctx context.Context, subjectUID string) (SubjectStats, error) {
	var stats SubjectStats
	err := s.pool.QueryRow(ctx, subjectStatsSQL, subjectUID).Scan(
		&stats.UID, &stats.Name, &stats.PhotoCount, &stats.OldestYear, &stats.NewestYear,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubjectStats{}, ErrSubjectNotFound
	}
	if err != nil {
		return SubjectStats{}, fmt.Errorf("people: reading stats of subject %s: %w", subjectUID, err)
	}
	return stats, nil
}
