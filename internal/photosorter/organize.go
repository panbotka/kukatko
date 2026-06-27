package photosorter

import (
	"context"
	"fmt"
)

// listAlbumsSQL pages albums ordered by uid for stable iteration.
const listAlbumsSQL = `SELECT uid, slug, title, description, type, private
FROM albums
ORDER BY uid
LIMIT $1 OFFSET $2`

// ListAlbums returns one page of albums ordered by uid. A short page signals the
// last page.
func (r *Reader) ListAlbums(ctx context.Context, params ListParams) ([]Album, error) {
	rows, err := r.pool.Query(ctx, listAlbumsSQL, pageLimit(params.Limit), params.Offset)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing albums: %w", err)
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		if scanErr := rows.Scan(
			&a.UID, &a.Slug, &a.Title, &a.Description, &a.Type, &a.Private,
		); scanErr != nil {
			return nil, fmt.Errorf("photosorter: scanning album: %w", scanErr)
		}
		albums = append(albums, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating albums: %w", err)
	}
	return albums, nil
}

// listLabelsSQL pages labels ordered by uid for stable iteration.
const listLabelsSQL = `SELECT uid, slug, name, priority
FROM labels
ORDER BY uid
LIMIT $1 OFFSET $2`

// ListLabels returns one page of labels ordered by uid. A short page signals the
// last page.
func (r *Reader) ListLabels(ctx context.Context, params ListParams) ([]Label, error) {
	rows, err := r.pool.Query(ctx, listLabelsSQL, pageLimit(params.Limit), params.Offset)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing labels: %w", err)
	}
	defer rows.Close()

	var labels []Label
	for rows.Next() {
		var l Label
		if scanErr := rows.Scan(&l.UID, &l.Slug, &l.Name, &l.Priority); scanErr != nil {
			return nil, fmt.Errorf("photosorter: scanning label: %w", scanErr)
		}
		labels = append(labels, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating labels: %w", err)
	}
	return labels, nil
}

// AlbumMemberships returns the album memberships of photoUID, ordered by sort
// order, so the migration can attach the photo to its mapped albums.
func (r *Reader) AlbumMemberships(ctx context.Context, photoUID string) ([]AlbumPhoto, error) {
	const q = `SELECT album_uid, photo_uid, sort_order
		FROM album_photos WHERE photo_uid = $1 ORDER BY sort_order, album_uid`
	rows, err := r.pool.Query(ctx, q, photoUID)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing album memberships for %s: %w", photoUID, err)
	}
	defer rows.Close()

	var members []AlbumPhoto
	for rows.Next() {
		var m AlbumPhoto
		if scanErr := rows.Scan(&m.AlbumUID, &m.PhotoUID, &m.SortOrder); scanErr != nil {
			return nil, fmt.Errorf("photosorter: scanning album membership: %w", scanErr)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating album memberships for %s: %w", photoUID, err)
	}
	return members, nil
}

// LabelMemberships returns the label attachments of photoUID with their
// provenance, so the migration can attach the photo to its mapped labels.
func (r *Reader) LabelMemberships(ctx context.Context, photoUID string) ([]PhotoLabel, error) {
	const q = `SELECT photo_uid, label_uid, source, uncertainty
		FROM photo_labels WHERE photo_uid = $1 ORDER BY label_uid`
	rows, err := r.pool.Query(ctx, q, photoUID)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing label memberships for %s: %w", photoUID, err)
	}
	defer rows.Close()

	var members []PhotoLabel
	for rows.Next() {
		var m PhotoLabel
		if scanErr := rows.Scan(&m.PhotoUID, &m.LabelUID, &m.Source, &m.Uncertainty); scanErr != nil {
			return nil, fmt.Errorf("photosorter: scanning label membership: %w", scanErr)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating label memberships for %s: %w", photoUID, err)
	}
	return members, nil
}
