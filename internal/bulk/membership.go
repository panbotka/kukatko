package bulk

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MembershipSummary describes, over one selection of photos, how many of them
// are already filed into an album or under a label, and where. It is what the
// upload page needs once a batch has settled with nothing chosen: the sentence
// "these photos are not in any album or label yet" is only true when Filed is
// zero, and a batch of duplicates often already sits in an album somebody filed
// it into the first time.
//
// Total counts the photos that exist, each once: a UID repeated in the request
// is one photo, and a UID of a photo that is gone is none — the same rule as
// LocationSummary.
type MembershipSummary struct {
	// Total is how many of the requested photos exist.
	Total int `json:"total"`
	// Filed is how many of them belong to at least one album or carry at least
	// one label, whatever the label's source.
	Filed int `json:"filed"`
	// Albums lists every album holding at least one of the photos, by title.
	Albums []AlbumRef `json:"albums"`
	// Labels lists every label on at least one of the photos, by priority then
	// name, the order the photo detail shows its chips in.
	Labels []LabelRef `json:"labels"`
}

// AlbumRef names an album in a MembershipSummary.
type AlbumRef struct {
	// UID is the album's identifier.
	UID string `json:"uid"`
	// Title is the album's title (it may be empty).
	Title string `json:"title"`
	// PhotoCount is how many of the requested photos the album holds.
	PhotoCount int `json:"photo_count"`
}

// LabelRef names a label in a MembershipSummary.
type LabelRef struct {
	// UID is the label's identifier.
	UID string `json:"uid"`
	// Name is the label's name.
	Name string `json:"name"`
	// PhotoCount is how many of the requested photos carry the label.
	PhotoCount int `json:"photo_count"`
}

// membershipCountSQL counts the existing target photos and, among them, the
// ones in an album or under a label.
const membershipCountSQL = `
SELECT count(*),
       count(*) FILTER (WHERE
           EXISTS (SELECT 1 FROM album_photos ap WHERE ap.photo_uid = p.uid)
        OR EXISTS (SELECT 1 FROM photo_labels pl WHERE pl.photo_uid = p.uid))
FROM photos p
WHERE p.uid = ANY($1)`

// membershipAlbumsSQL lists the albums holding any of the target photos.
const membershipAlbumsSQL = `
SELECT a.uid, a.title, count(*)
FROM albums a
JOIN album_photos ap ON ap.album_uid = a.uid
WHERE ap.photo_uid = ANY($1)
GROUP BY a.uid, a.title
ORDER BY a.title, a.uid`

// membershipLabelsSQL lists the labels on any of the target photos.
const membershipLabelsSQL = `
SELECT l.uid, l.name, count(*)
FROM labels l
JOIN photo_labels pl ON pl.label_uid = l.uid
WHERE pl.photo_uid = ANY($1)
GROUP BY l.uid, l.name, l.priority
ORDER BY l.priority DESC, l.name, l.uid`

// MembershipSummary reports how many of the given photos are already in an
// album or under a label, and which albums and labels those are. It reads and
// never writes, in three indexed queries whatever the batch size.
//
// It rejects the same batches Apply does — an empty list is ErrNoPhotos and an
// oversized one ErrBatchTooLarge.
func (s *Service) MembershipSummary(ctx context.Context, photoUIDs []string) (MembershipSummary, error) {
	if len(photoUIDs) == 0 {
		return MembershipSummary{}, ErrNoPhotos
	}
	if len(photoUIDs) > s.maxBatch {
		return MembershipSummary{}, fmt.Errorf(
			"%w: %d exceeds limit %d", ErrBatchTooLarge, len(photoUIDs), s.maxBatch)
	}
	summary := MembershipSummary{Albums: []AlbumRef{}, Labels: []LabelRef{}}
	row := s.pool.QueryRow(ctx, membershipCountSQL, photoUIDs)
	if err := row.Scan(&summary.Total, &summary.Filed); err != nil {
		return MembershipSummary{}, fmt.Errorf("bulk: counting filed photos: %w", err)
	}
	if summary.Filed == 0 {
		return summary, nil
	}
	albums, err := s.queryRefs(ctx, membershipAlbumsSQL, photoUIDs)
	if err != nil {
		return MembershipSummary{}, fmt.Errorf("bulk: listing albums of the selection: %w", err)
	}
	for _, ref := range albums {
		summary.Albums = append(summary.Albums, AlbumRef{UID: ref.uid, Title: ref.name, PhotoCount: ref.count})
	}
	labels, err := s.queryRefs(ctx, membershipLabelsSQL, photoUIDs)
	if err != nil {
		return MembershipSummary{}, fmt.Errorf("bulk: listing labels of the selection: %w", err)
	}
	for _, ref := range labels {
		summary.Labels = append(summary.Labels, LabelRef{UID: ref.uid, Name: ref.name, PhotoCount: ref.count})
	}
	return summary, nil
}

// namedCount is one row of the album or label listing: a uid, its display name
// and how many of the selection it covers.
type namedCount struct {
	uid   string
	name  string
	count int
}

// queryRefs runs one of the membership listings over photoUIDs.
func (s *Service) queryRefs(ctx context.Context, sql string, photoUIDs []string) ([]namedCount, error) {
	rows, err := s.pool.Query(ctx, sql, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (namedCount, error) {
		var ref namedCount
		if err := row.Scan(&ref.uid, &ref.name, &ref.count); err != nil {
			return namedCount{}, fmt.Errorf("scanning row: %w", err)
		}
		return ref, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning: %w", err)
	}
	return out, nil
}
