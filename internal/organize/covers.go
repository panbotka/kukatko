package organize

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Cover is the photo that stands for an album or a label: the uid to link to and
// the file hash the thumbnail cache is keyed by. Both travel together because a
// caller that wants to *draw* the cover needs the hash to address a cached
// thumbnail, and fetching the whole photo row for two strings is waste.
type Cover struct {
	// PhotoUID is the cover photo's uid.
	PhotoUID string
	// FileHash is that photo's SHA256, the thumbnail cache key.
	FileHash string
}

// coverVisible restricts a derived cover to a photo the entity's own grid would
// show: not archived, not hidden from the library, and not a stack member the
// grid folds into its primary. A cover is a promise about what is behind it, so
// it must never be a photo the reader cannot reach by opening the entity.
const coverVisible = `p.archived_at IS NULL AND NOT p.hidden_from_library
      AND (p.stack_uid IS NULL OR p.stack_primary)`

// coverOrder ranks the photos a derived cover is picked from: newest capture
// time first, with the upload time standing in for a photo whose capture time is
// unknown, and the uid breaking ties. Both keys are total, so the same entity
// yields the same cover on every request instead of reshuffling between reloads.
const coverOrder = "COALESCE(p.taken_at, p.created_at) DESC, p.uid"

// albumCoversSQL resolves the cover of a whole batch of albums in one statement.
//
// The DISTINCT ON picks each album's newest visible member; restricting the scan
// to the requested albums up front is what keeps it cheap — never a correlated
// "ORDER BY … LIMIT 1" per album, which on the production library walks the
// global photos.taken_at order once per row (see listAlbumsSQL's note and
// docs/PERF.md).
//
// A cover chosen by hand wins over the derived one, and is honoured whatever
// state its photo is in, because it is the user's own explicit answer to what
// the album looks like — the same rule ListAlbums applies.
const albumCoversSQL = `
WITH derived AS (
    SELECT DISTINCT ON (ap.album_uid) ap.album_uid, p.uid AS photo_uid, p.file_hash
    FROM album_photos ap
    JOIN photos p ON p.uid = ap.photo_uid
    WHERE ap.album_uid = ANY($1) AND ` + coverVisible + `
    ORDER BY ap.album_uid, ` + coverOrder + `
)
SELECT a.uid,
       COALESCE(picked.uid, derived.photo_uid),
       COALESCE(picked.file_hash, derived.file_hash)
FROM albums a
LEFT JOIN photos picked ON picked.uid = a.cover_photo_uid
LEFT JOIN derived ON derived.album_uid = a.uid
WHERE a.uid = ANY($1)`

// AlbumCovers returns, per album uid, the photo that stands for it: the
// hand-picked cover when one is set, otherwise the album's newest visible photo.
// An album holding no visible photo and carrying no chosen cover is simply
// absent from the map — as is a uid that names nothing — so a caller reads "no
// cover" the same way either way. An empty input yields an empty map without
// touching the database.
//
// It is a batch lookup on purpose: it answers a whole page of albums in ONE
// query, so a listing that draws covers never turns into a query per row.
func (s *Store) AlbumCovers(ctx context.Context, uids []string) (map[string]Cover, error) {
	return s.covers(ctx, albumCoversSQL, uids, "albums")
}

// labelCoversSQL resolves the cover of a whole batch of labels in one statement:
// each label's newest visible photo, picked by the same DISTINCT ON over the
// batch that albumCoversSQL uses. A label has no cover to choose by hand, so
// there is nothing to override the derivation with.
const labelCoversSQL = `
SELECT DISTINCT ON (pl.label_uid) pl.label_uid, p.uid, p.file_hash
FROM photo_labels pl
JOIN photos p ON p.uid = pl.photo_uid
WHERE pl.label_uid = ANY($1) AND ` + coverVisible + `
ORDER BY pl.label_uid, ` + coverOrder

// LabelCovers returns, per label uid, the newest visible photo carrying that
// label. A label on no visible photo is absent from the map rather than present
// with a zero cover, and an empty input yields an empty map without touching the
// database.
//
// Like AlbumCovers it answers the whole batch in ONE query, so the labels index
// and the global-search endpoint can draw previews without an N+1.
func (s *Store) LabelCovers(ctx context.Context, uids []string) (map[string]Cover, error) {
	return s.covers(ctx, labelCoversSQL, uids, "labels")
}

// covers runs a cover query — one whose projection is (entity uid, photo uid,
// file hash) — over the batch of uids and collects the rows that resolved. The
// photo columns arrive nullable because an entity with nothing to show still
// produces a row in the album variant; such a row is dropped rather than mapped
// to an empty cover. The kind names the entity in error messages.
func (s *Store) covers(
	ctx context.Context, query string, uids []string, kind string,
) (map[string]Cover, error) {
	out := make(map[string]Cover, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, query, uids)
	if err != nil {
		return nil, fmt.Errorf("organize: listing %s covers: %w", kind, err)
	}
	defer rows.Close()

	for rows.Next() {
		uid, cover, err := scanCover(rows)
		if err != nil {
			return nil, err
		}
		if cover != nil {
			out[uid] = *cover
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("organize: iterating %s covers: %w", kind, err)
	}
	return out, nil
}

// scanCover reads one (entity uid, photo uid, file hash) row, returning a nil
// cover for an entity that has none.
func scanCover(row pgx.Row) (string, *Cover, error) {
	var uid string
	var photoUID, fileHash *string
	if err := row.Scan(&uid, &photoUID, &fileHash); err != nil {
		return "", nil, fmt.Errorf("organize: scanning cover: %w", err)
	}
	if photoUID == nil {
		return uid, nil, nil
	}
	return uid, &Cover{PhotoUID: *photoUID, FileHash: deref(fileHash)}, nil
}

// deref returns the string ptr points at, or "" when it is nil. A cover's file
// hash is NOT NULL in its own table and only becomes nullable by riding on a
// LEFT JOIN, and the caller has already decided the row resolved by looking at
// the photo uid.
func deref(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
