package system

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// countLibrarySQL gathers every count of one project's library in one round
// trip as a row of scalar subqueries, with $1 the project. Each one is a plain
// COUNT over an indexed predicate — this is deliberately not the maintenance
// scan, which walks the whole originals tree: the statistics page must stay
// cheap enough to open on a whim.
//
// Every subquery is scoped to the project. The root tables (photos, albums,
// labels, subjects, embeddings, faces) carry project_id themselves; the child
// tables (photo_places, markers) have none and are scoped through a join onto
// their photo's project_id. Every photos predicate leads with project_id, which
// is what the per-project photo indexes lead with.
//
// The two counts that explain why the library grid lists fewer photos than the
// catalogue holds — the hidden ones and the stacked-away siblings — mirror the
// listing's own default filters (photos.hiddenClauses, photos.stackClauses)
// exactly, so the subtraction in Library.derive lands on the number the grid
// reports. They are counted as disjoint sets (the stacked count excludes the
// hidden ones), because two overlapping deductions would subtract a photo twice.
//
// Plain images are not counted at all but derived from the total in
// Library.derive. The two coverage counts are semi-joins onto the embeddings
// primary key and the faces (photo_uid, face_index) unique index, so neither
// reads a heap row it does not have to. The two counts behind the coverage
// meters — photos carrying their own coordinates and faces that already name a
// subject — have no index of their own and are plain scans of the project's
// rows; they are aggregates over the whole library anyway, which is what the
// 30 s per-project memoisation is for.
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
    WHERE project_id = $1
)
SELECT
    (SELECT count(*) FROM photos WHERE project_id = $1),
    (SELECT count(*) FROM photos WHERE project_id = $1 AND media_type = 'video'),
    (SELECT count(*) FROM photos WHERE project_id = $1 AND media_type = 'live'),
    (SELECT count(*) FROM photos WHERE project_id = $1 AND archived_at IS NOT NULL),
    (SELECT count(*) FROM photos WHERE project_id = $1 AND archived_at IS NULL AND hidden_from_library),
    (SELECT count(*) FROM photos
        WHERE project_id = $1 AND archived_at IS NULL AND NOT hidden_from_library
          AND stack_uid IS NOT NULL AND NOT stack_primary),
    (SELECT count(*) FROM photos p
        WHERE p.project_id = $1 AND EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid)),
    (SELECT count(*) FROM photos p
        WHERE p.project_id = $1 AND EXISTS (SELECT 1 FROM faces f WHERE f.photo_uid = p.uid)),
    (SELECT count(*) FROM photos WHERE project_id = $1 AND lat IS NOT NULL AND lng IS NOT NULL),
    (SELECT count(*) FROM photo_places pp JOIN photos p ON p.uid = pp.photo_uid
        WHERE p.project_id = $1 AND pp.lat IS NOT NULL AND pp.lng IS NOT NULL),
    (SELECT count(*) FROM photos p
        WHERE p.project_id = $1 AND p.archived_at IS NULL AND p.lat IS NOT NULL AND p.lng IS NOT NULL
          AND NOT EXISTS (SELECT 1 FROM photo_places pp WHERE pp.photo_uid = p.uid)),
    (SELECT count(*) FROM embeddings WHERE project_id = $1),
    (SELECT count(*) FROM faces WHERE project_id = $1),
    (SELECT count(*) FROM faces WHERE project_id = $1 AND subject_uid IS NOT NULL),
    (SELECT count(*) FROM subjects WHERE project_id = $1),
    (SELECT count(*) FROM subjects WHERE project_id = $1 AND type = 'person'),
    (SELECT count(*) FROM subjects WHERE project_id = $1 AND type = 'pet'),
    (SELECT count(*) FROM subjects WHERE project_id = $1 AND type = 'other'),
    (SELECT count(*) FROM markers m JOIN photos p ON p.uid = m.photo_uid WHERE p.project_id = $1),
    (SELECT count(*) FROM markers m JOIN photos p ON p.uid = m.photo_uid
        WHERE p.project_id = $1 AND m.subject_uid IS NOT NULL),
    a.total, a.manual, a.folder, a.moment, a.state, a.month,
    (SELECT count(*) FROM labels WHERE project_id = $1)
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

// CountLibrary returns the raw counts of the project projectUID in a single
// query. An empty projectUID is ErrNoProject, never "every project". The
// derived values (the live/archived split, the plain-image share, the coverage
// gaps, the unassigned markers) are left zero — Service fills them in — so this
// method reports only what the database actually counted. It returns an error
// when the query fails; callers must not treat a failure as a library of zeroes.
func (s *Store) CountLibrary(ctx context.Context, projectUID string) (Library, error) {
	if projectUID == "" {
		return Library{}, ErrNoProject
	}
	var counts Library
	err := s.pool.QueryRow(ctx, countLibrarySQL, projectUID).Scan(
		&counts.Photos,
		&counts.Videos,
		&counts.LivePhotos,
		&counts.PhotosArchived,
		&counts.PhotosHidden,
		&counts.PhotosStacked,
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
