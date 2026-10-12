package organize

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/audit"
)

// The project scope of albums, labels, favourites and ratings.
//
// albums and labels are root tables and carry project_id; album_photos,
// photo_labels, user_favorites and user_ratings carry none and belong to the
// project of the album, label or photo they hang off. Every Store method takes
// the project uid as its first argument after the context and never defaults
// it: an empty uid is ErrNoProject, a programming error rather than "the whole
// instance". An album, label or photo of another project is answered exactly
// like a missing one (ErrAlbumNotFound, ErrLabelNotFound, ErrPhotoNotFound, an
// empty list), so the HTTP layer gives the same 404.
//
// A write proves what it touches inside the project on its own transaction (or
// in the statement itself) before it changes anything: the album or label by a
// project_id predicate on the row, the photos of a membership batch all at once
// (requirePhotosInProject — one foreign uid refuses the whole batch, nothing is
// added and no audit row is written), a hand-picked cover like a member. So a
// membership, an attachment or a cover never joins two projects. A favourite or
// rating is the user's own, but it is about a photo, and the photo must belong to
// the project the request runs in.
//
// The background callers — the sidecar writer and the album_featured
// notification job — have no request: the sidecar job passes the project of the
// photo it loaded (Photo.ProjectID), and FeaturedAlbumTx is the one explicitly
// unscoped job loader, keyed by the album uid a job payload carries.

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every scoped Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// withProject defaults entry's ProjectUID to projectUID, so every audited
// organise write records the project it happened in.
func withProject(entry audit.Entry, projectUID string) audit.Entry {
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}
	return entry
}

// photosInProjectSQL reports whether every uid of the batch $1 names a photo of
// the project $2: one statement, whatever the batch size, so a membership write
// is all or nothing.
const photosInProjectSQL = `
SELECT NOT EXISTS (
    SELECT 1 FROM unnest($1::text[]) AS b(uid)
    WHERE NOT EXISTS (SELECT 1 FROM photos p WHERE p.uid = b.uid AND p.project_id = $2)
)`

// requirePhotosInProject returns ErrPhotoNotFound unless every uid of photoUIDs
// names a photo of the project, run on q — the pool, or the transaction of the
// write it guards. An empty batch is trivially inside.
func requirePhotosInProject(ctx context.Context, q rowQuerier, projectUID string, photoUIDs ...string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	if len(photoUIDs) == 0 {
		return nil
	}
	var ok bool
	if err := q.QueryRow(ctx, photosInProjectSQL, photoUIDs, projectUID).Scan(&ok); err != nil {
		return fmt.Errorf("organize: checking the project of %d photos: %w", len(photoUIDs), err)
	}
	if !ok {
		return ErrPhotoNotFound
	}
	return nil
}

// requireRowInProject returns notFound unless the row uid of table (a trusted
// constant, albums or labels) belongs to the project, run on q. A row deleted
// after the check still fails the guarded write on its foreign key.
func requireRowInProject(ctx context.Context, q rowQuerier, table, projectUID, uid string, notFound error) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	var ok bool
	if err := q.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM "+table+" WHERE uid = $1 AND project_id = $2)", uid, projectUID,
	).Scan(&ok); err != nil {
		return fmt.Errorf("organize: looking up %s %s: %w", table, uid, err)
	}
	if !ok {
		return notFound
	}
	return nil
}

// requireAlbumInProject returns ErrAlbumNotFound unless albumUID names an album
// of the project, run on q.
func requireAlbumInProject(ctx context.Context, q rowQuerier, projectUID, albumUID string) error {
	return requireRowInProject(ctx, q, "albums", projectUID, albumUID, ErrAlbumNotFound)
}

// requireLabelInProject returns ErrLabelNotFound unless labelUID names a label
// of the project, run on q.
func requireLabelInProject(ctx context.Context, q rowQuerier, projectUID, labelUID string) error {
	return requireRowInProject(ctx, q, "labels", projectUID, labelUID, ErrLabelNotFound)
}

// requireCoverInProject returns ErrPhotoNotFound when cover names a photo
// outside the project; a nil cover (none chosen) passes.
func requireCoverInProject(ctx context.Context, q rowQuerier, projectUID string, cover *string) error {
	if cover == nil {
		return requireProject(projectUID)
	}
	return requirePhotosInProject(ctx, q, projectUID, *cover)
}

// photoInProject is the predicate confining a row keyed by a photo uid column
// (user_favorites.photo_uid, user_ratings.photo_uid) to the photos of the
// project bound to placeholder.
func photoInProject(column, placeholder string) string {
	return "EXISTS (SELECT 1 FROM photos pp WHERE pp.uid = " + column + " AND pp.project_id = " + placeholder + ")"
}
