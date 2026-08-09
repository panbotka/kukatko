package system

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// countLibrarySQL gathers every library-wide count in one round trip as a row of
// scalar subqueries. Each one is a plain COUNT over an indexed predicate — this
// is deliberately not the maintenance scan, which walks the whole originals tree:
// the statistics page must stay cheap enough to open on a whim.
//
// The archived, video and live-photo predicates hit the partial indexes created
// for exactly these minorities (idx_photos_archived_at, idx_photos_media_type);
// plain images are not counted at all but derived from the total in Library.derive,
// because the media_type index deliberately excludes the majority value. The two
// coverage counts are semi-joins onto the embeddings primary key and the faces
// (photo_uid, face_index) unique index, so neither reads a heap row it does not
// have to. The two counts behind the coverage meters — photos carrying their own
// coordinates and faces that already name a subject — have no index of their own
// and are plain scans; they are aggregates over the whole table anyway, which is
// what the shared 30 s memoisation is for.
//
// The album breakdown is a single CTE rather than one filtered subquery per type:
// the auto-generated month/moment/state albums make this table the one that grows
// with the library rather than with the user's curation, so it is scanned once.
const countLibrarySQL = `
WITH album_types AS (
    SELECT count(*)                                AS total,
           count(*) FILTER (WHERE type = 'album')  AS manual,
           count(*) FILTER (WHERE type = 'folder') AS folder,
           count(*) FILTER (WHERE type = 'moment') AS moment,
           count(*) FILTER (WHERE type = 'state')  AS state,
           count(*) FILTER (WHERE type = 'month')  AS month
    FROM albums
)
SELECT
    (SELECT count(*) FROM photos),
    (SELECT count(*) FROM photos WHERE media_type = 'video'),
    (SELECT count(*) FROM photos WHERE media_type = 'live'),
    (SELECT count(*) FROM photos WHERE archived_at IS NOT NULL),
    (SELECT count(*) FROM photos p WHERE EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid)),
    (SELECT count(*) FROM photos p WHERE EXISTS (SELECT 1 FROM faces f WHERE f.photo_uid = p.uid)),
    (SELECT count(*) FROM photos WHERE lat IS NOT NULL AND lng IS NOT NULL),
    (SELECT count(*) FROM photo_places WHERE lat IS NOT NULL AND lng IS NOT NULL),
    (SELECT count(*) FROM photos p
        WHERE p.archived_at IS NULL AND p.lat IS NOT NULL AND p.lng IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM photo_places pp WHERE pp.photo_uid = p.uid)),
    (SELECT count(*) FROM embeddings),
    (SELECT count(*) FROM faces),
    (SELECT count(*) FROM faces WHERE subject_uid IS NOT NULL),
    (SELECT count(*) FROM subjects),
    (SELECT count(*) FROM subjects WHERE type = 'person'),
    (SELECT count(*) FROM subjects WHERE type = 'pet'),
    (SELECT count(*) FROM subjects WHERE type = 'other'),
    (SELECT count(*) FROM markers),
    (SELECT count(*) FROM markers WHERE subject_uid IS NOT NULL),
    a.total, a.manual, a.folder, a.moment, a.state, a.month,
    (SELECT count(*) FROM labels)
FROM album_types a`

// Store reads the library-wide counts straight from the catalogue. It owns no
// connection; it borrows the shared pgx pool supplied at construction, like the
// stores of the domain packages it counts across (photos, vectors, people,
// organize) — the aggregation spans all of them, so it belongs to none.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CountLibrary returns the raw instance-wide counts in a single query. The
// derived values (the live/archived split, the plain-image share, the coverage
// gaps, the unassigned markers) are left zero — Service fills them in — so this
// method reports only what the database actually counted. It returns an error
// when the query fails; callers must not treat a failure as a library of zeroes.
func (s *Store) CountLibrary(ctx context.Context) (Library, error) {
	var counts Library
	err := s.pool.QueryRow(ctx, countLibrarySQL).Scan(
		&counts.Photos,
		&counts.Videos,
		&counts.LivePhotos,
		&counts.PhotosArchived,
		&counts.PhotosWithEmbedding,
		&counts.PhotosWithFaces,
		&counts.PhotosWithGPS,
		&counts.PhotosGeocoded,
		&counts.PhotosPendingGeocode,
		&counts.Embeddings,
		&counts.Faces,
		&counts.FacesAssigned,
		&counts.Subjects,
		&counts.SubjectsPerson,
		&counts.SubjectsPet,
		&counts.SubjectsOther,
		&counts.Markers,
		&counts.MarkersAssigned,
		&counts.Albums,
		&counts.AlbumsManual,
		&counts.AlbumsFolder,
		&counts.AlbumsMoment,
		&counts.AlbumsState,
		&counts.AlbumsMonth,
		&counts.Labels,
	)
	if err != nil {
		return Library{}, fmt.Errorf("system: counting library: %w", err)
	}
	return counts, nil
}
