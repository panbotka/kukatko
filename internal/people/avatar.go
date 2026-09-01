package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoAvatar indicates the subject exists but has nothing to show: no cover
// photo was chosen for it and none of its markers falls on a visible photo the
// crop maths can use. It is an ordinary answer, not a failure — the people index
// draws its placeholder for exactly these subjects.
var ErrNoAvatar = errors.New("people: subject has no avatar source")

// AvatarSource says which picture stands for a subject and how to cut it: the
// photo to read, and — when the picture is a face rather than a hand-picked
// cover — the normalised box of that face within the photo's display frame.
//
// It is deliberately the same choice the people index makes client-side, taken
// server-side instead, so the avatar route can hand back a small square rendition
// rather than a whole preview the browser crops. See SubjectAvatar for the rule.
type AvatarSource struct {
	// PhotoUID is the photo the avatar is cut from.
	PhotoUID string
	// Face is the subject's face box in 0..1 display space, nil when the subject
	// has a hand-picked cover photo (which is shown whole, centre-cropped square).
	Face *Box
}

// Box is a normalised [x, y, w, h] rectangle in a photo's display space, each
// value in 0..1. It is the same geometry SubjectFace carries, named for the one
// thing the avatar renderer needs from it.
type Box struct {
	X float64
	Y float64
	W float64
	H float64
}

// subjectAvatarSQL resolves one subject's avatar source in a single statement: the
// hand-picked cover photo when the subject has one, otherwise the face the people
// index would crop. The best_face selection (and its filters) mirrors
// listSubjectsSQL exactly — biggest box, then detector score, then uid — because
// the two must agree: the grid decides whether to request an avatar at all from
// the list payload, and the route must then cut the face that payload named.
//
// The cover join is LEFT so a subject whose cover_photo_uid points at a photo that
// has since been archived still falls back to its best face instead of answering
// with a photo the reader cannot open.
const subjectAvatarSQL = `
WITH best_face AS (
    SELECT m.photo_uid, m.x, m.y, m.w, m.h
    FROM markers m
    JOIN photos p ON p.uid = m.photo_uid
    WHERE m.subject_uid = $1
      AND m.type = 'face'
      AND m.invalid = FALSE
      AND m.w > 0 AND m.h > 0
      AND p.archived_at IS NULL
      AND (p.stack_uid IS NULL OR p.stack_primary)
      AND p.file_width > 0 AND p.file_height > 0
    ORDER BY m.w * m.h DESC, m.score DESC, m.uid
    LIMIT 1
)
SELECT picked.uid, bf.photo_uid, bf.x, bf.y, bf.w, bf.h
FROM subjects s
LEFT JOIN photos picked ON picked.uid = s.cover_photo_uid
    AND picked.archived_at IS NULL
LEFT JOIN best_face bf ON TRUE
WHERE s.uid = $1`

// SubjectAvatar returns what the subject's avatar is cut from: the hand-picked
// cover photo (shown whole) when one is set and still visible, otherwise the
// subject's best face and its box. It returns ErrSubjectNotFound for a uid that
// names nothing and ErrNoAvatar for a subject with neither.
//
// The order is the people index's own: an explicitly chosen cover is a decision
// and a detected face is a guess, so the guess never overrides it.
func (s *Store) SubjectAvatar(ctx context.Context, uid string) (AvatarSource, error) {
	var coverUID, facePhotoUID *string
	var x, y, w, h *float64
	err := s.pool.QueryRow(ctx, subjectAvatarSQL, uid).
		Scan(&coverUID, &facePhotoUID, &x, &y, &w, &h)
	if errors.Is(err, pgx.ErrNoRows) {
		return AvatarSource{}, ErrSubjectNotFound
	}
	if err != nil {
		return AvatarSource{}, fmt.Errorf("people: reading avatar source for %s: %w", uid, err)
	}
	if coverUID != nil {
		return AvatarSource{PhotoUID: *coverUID}, nil
	}
	if facePhotoUID == nil {
		return AvatarSource{}, ErrNoAvatar
	}
	box := Box{X: deref(x), Y: deref(y), W: deref(w), H: deref(h)}
	return AvatarSource{PhotoUID: *facePhotoUID, Face: &box}, nil
}
