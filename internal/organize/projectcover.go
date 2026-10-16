package organize

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// projectCoverVisible narrows coverVisible for the project chooser: besides
// what a grid would fold away, a photo marked private never stands for the
// whole library on the screen an account sees before it enters one.
const projectCoverVisible = coverVisible + ` AND NOT p.private`

// projectCoverAlbumSQL reads the project's first featured album — the one the
// homepage strip lists first (newest feature first) — that the reader may see:
// a private one only when $2 allows it. It yields the album's uid and its
// hand-picked cover ("" when none was picked).
const projectCoverAlbumSQL = `
SELECT a.uid, COALESCE(a.cover_photo_uid, '')
FROM albums a
WHERE a.project_id = $1 AND a.featured_until > now() AND a.type = 'album' AND ($2::boolean OR NOT a.private)
ORDER BY a.featured_at DESC NULLS LAST, a.uid
LIMIT 1`

// projectCoverPickedSQL reads the hand-picked cover $2 of a featured album of
// the project $1, provided it is still a photo the chooser may show.
const projectCoverPickedSQL = `
SELECT p.uid, p.file_hash, p.file_path
FROM photos p
WHERE p.project_id = $1 AND p.uid = $2 AND ` + projectCoverVisible

// projectCoverDerivedSQL reads the derived cover of the album $2 of the project
// $1: its newest showable photo, in the order every derived cover follows.
const projectCoverDerivedSQL = `
SELECT p.uid, p.file_hash, p.file_path
FROM photos p
JOIN album_photos ap ON ap.photo_uid = p.uid
WHERE p.project_id = $1 AND ap.album_uid = $2 AND ` + projectCoverVisible + `
ORDER BY ` + coverOrder + `
LIMIT 1`

// projectCoverNewestSQL reads the most recently added showable photo of the
// project $1 (idx_photos_project_live_created_at walks it newest first).
const projectCoverNewestSQL = `
SELECT p.uid, p.file_hash, p.file_path
FROM photos p
WHERE p.project_id = $1 AND ` + projectCoverVisible + `
ORDER BY p.created_at DESC NULLS LAST, p.uid DESC
LIMIT 1`

// ProjectCover returns the picture that stands for the project projectUID on
// the project chooser, and false when the project has none to offer: the cover
// of its first featured album (the hand-picked one, else the album's newest
// photo), otherwise the photo most recently added to the library. Private
// albums count only when includePrivate is set — the caller passes whether the
// reader may see them (curator and up, as on the homepage strip). Whatever is
// returned is a live, library-visible, non-private photo of that project and of
// no other: every statement is confined to projectUID. The caller must only ask
// about a project the reader may enter.
func (s *Store) ProjectCover(ctx context.Context, projectUID string, includePrivate bool) (Cover, bool, error) {
	if err := requireProject(projectUID); err != nil {
		return Cover{}, false, err
	}
	var albumUID, pickedUID string
	err := s.pool.QueryRow(ctx, projectCoverAlbumSQL, projectUID, includePrivate).Scan(&albumUID, &pickedUID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Cover{}, false, fmt.Errorf("organize: reading the featured album of %s: %w", projectUID, err)
	default:
		if pickedUID != "" {
			if c, ok, err := s.projectCoverRow(ctx, projectCoverPickedSQL, projectUID, pickedUID); ok || err != nil {
				return c, ok, err
			}
		}
		if c, ok, err := s.projectCoverRow(ctx, projectCoverDerivedSQL, projectUID, albumUID); ok || err != nil {
			return c, ok, err
		}
	}
	return s.projectCoverRow(ctx, projectCoverNewestSQL, projectUID)
}

// projectCoverRow runs one of the cover statements and scans its single photo,
// reporting false when it found none.
func (s *Store) projectCoverRow(ctx context.Context, query string, args ...any) (Cover, bool, error) {
	var c Cover
	err := s.pool.QueryRow(ctx, query, args...).Scan(&c.PhotoUID, &c.FileHash, &c.FilePath)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cover{}, false, nil
	}
	if err != nil {
		return Cover{}, false, fmt.Errorf("organize: reading a project cover: %w", err)
	}
	return c, true, nil
}
