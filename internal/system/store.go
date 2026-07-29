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
// The archived and video predicates hit the partial indexes created for exactly
// these minorities (idx_photos_archived_at, idx_photos_media_type); the two
// coverage counts are semi-joins onto the embeddings primary key and the faces
// (photo_uid, face_index) unique index, so neither reads a heap row it does not
// have to.
const countLibrarySQL = `
SELECT
    (SELECT count(*) FROM photos),
    (SELECT count(*) FROM photos WHERE media_type = 'video'),
    (SELECT count(*) FROM photos WHERE archived_at IS NOT NULL),
    (SELECT count(*) FROM photos p WHERE EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid)),
    (SELECT count(*) FROM photos p WHERE EXISTS (SELECT 1 FROM faces f WHERE f.photo_uid = p.uid)),
    (SELECT count(*) FROM embeddings),
    (SELECT count(*) FROM faces),
    (SELECT count(*) FROM subjects),
    (SELECT count(*) FROM subjects WHERE type = 'person'),
    (SELECT count(*) FROM subjects WHERE type = 'pet'),
    (SELECT count(*) FROM subjects WHERE type = 'other'),
    (SELECT count(*) FROM markers),
    (SELECT count(*) FROM markers WHERE subject_uid IS NOT NULL),
    (SELECT count(*) FROM albums),
    (SELECT count(*) FROM labels)`

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
// derived values (the live/archived split, the coverage gaps, the unassigned
// markers) are left zero — Service fills them in — so this method reports only
// what the database actually counted. It returns an error when the query fails;
// callers must not treat a failure as a library of zeroes.
func (s *Store) CountLibrary(ctx context.Context) (Library, error) {
	var counts Library
	err := s.pool.QueryRow(ctx, countLibrarySQL).Scan(
		&counts.Photos,
		&counts.Videos,
		&counts.PhotosArchived,
		&counts.PhotosWithEmbedding,
		&counts.PhotosWithFaces,
		&counts.Embeddings,
		&counts.Faces,
		&counts.Subjects,
		&counts.SubjectsPerson,
		&counts.SubjectsPet,
		&counts.SubjectsOther,
		&counts.Markers,
		&counts.MarkersAssigned,
		&counts.Albums,
		&counts.Labels,
	)
	if err != nil {
		return Library{}, fmt.Errorf("system: counting library: %w", err)
	}
	return counts, nil
}
