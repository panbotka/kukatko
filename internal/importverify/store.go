package importverify

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the concrete Catalog backed by the Kukátko Postgres catalogue over a
// shared pgx pool. It only ever reads.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// compile-time assertion that Store satisfies Catalog.
var _ Catalog = (*Store)(nil)

// importedRefsSQL selects the external identifiers of every photo carrying one,
// so the reconciler can classify a source photo as imported (by uid) or
// deduplicated (by file hash).
const importedRefsSQL = `
SELECT photoprism_uid, photoprism_file_hash
FROM photos
WHERE photoprism_uid IS NOT NULL OR photoprism_file_hash IS NOT NULL`

// ImportedRefs returns the sets of photoprism_uid and photoprism_file_hash across
// imported photos. Both columns are nullable, so NULL and empty values are
// skipped rather than added to the sets.
func (s *Store) ImportedRefs(ctx context.Context) (map[string]struct{}, map[string]struct{}, error) {
	rows, err := s.pool.Query(ctx, importedRefsSQL)
	if err != nil {
		return nil, nil, fmt.Errorf("importverify: querying imported refs: %w", err)
	}
	defer rows.Close()

	uids := make(map[string]struct{})
	hashes := make(map[string]struct{})
	for rows.Next() {
		var uid, hash *string
		if err := rows.Scan(&uid, &hash); err != nil {
			return nil, nil, fmt.Errorf("importverify: scanning imported ref: %w", err)
		}
		if uid != nil && *uid != "" {
			uids[*uid] = struct{}{}
		}
		if hash != nil && *hash != "" {
			hashes[*hash] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("importverify: iterating imported refs: %w", err)
	}
	return uids, hashes, nil
}

// originalFileCountsSQL counts the role='original' photo_files of each
// PhotoPrism-imported photo, keyed by its photoprism_uid.
const originalFileCountsSQL = `
SELECT p.photoprism_uid, count(pf.id)
FROM photos p
JOIN photo_files pf ON pf.photo_uid = p.uid AND pf.role = 'original'
WHERE p.photoprism_uid IS NOT NULL
GROUP BY p.photoprism_uid`

// OriginalFileCounts maps each imported photo's photoprism_uid to its number of
// role='original' photo_files. A photo with no original files simply has no entry
// (treated as zero by the reconciler).
func (s *Store) OriginalFileCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, originalFileCountsSQL)
	if err != nil {
		return nil, fmt.Errorf("importverify: querying original file counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var (
			uid   string
			count int
		)
		if err := rows.Scan(&uid, &count); err != nil {
			return nil, fmt.Errorf("importverify: scanning original file count: %w", err)
		}
		counts[uid] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importverify: iterating original file counts: %w", err)
	}
	return counts, nil
}

// countsSQL reads every catalogue aggregate in one round-trip. The embeddings and
// faces counts join photos and restrict to photoprism_uid IS NOT NULL so they
// compare against photo-sorter's PhotoPrism-keyed population.
const countsSQL = `
SELECT
	(SELECT count(*) FROM photos),
	(SELECT count(*) FROM photos WHERE photoprism_uid IS NOT NULL),
	(SELECT count(*) FROM embeddings e
		JOIN photos p ON p.uid = e.photo_uid WHERE p.photoprism_uid IS NOT NULL),
	(SELECT count(DISTINCT f.photo_uid) FROM faces f
		JOIN photos p ON p.uid = f.photo_uid WHERE p.photoprism_uid IS NOT NULL),
	(SELECT count(*) FROM faces f
		JOIN photos p ON p.uid = f.photo_uid WHERE p.photoprism_uid IS NOT NULL),
	(SELECT count(*) FROM albums),
	(SELECT count(*) FROM labels),
	(SELECT count(*) FROM subjects)`

// Counts returns the catalogue aggregates used for reconciliation.
func (s *Store) Counts(ctx context.Context) (CatalogCounts, error) {
	var c CatalogCounts
	err := s.pool.QueryRow(ctx, countsSQL).Scan(
		&c.Photos, &c.PhotoprismImported, &c.Embeddings,
		&c.FacePhotos, &c.Faces, &c.Albums, &c.Labels, &c.Subjects,
	)
	if err != nil {
		return CatalogCounts{}, fmt.Errorf("importverify: querying catalog counts: %w", err)
	}
	return c, nil
}

// missingEmbeddingsCountSQL counts imported photos with no embeddings row.
const missingEmbeddingsCountSQL = `
SELECT count(*) FROM photos p
WHERE p.photoprism_uid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid)`

// missingEmbeddingsListSQL lists up to $1 photoprism_uids of imported photos with
// no embeddings row, ordered for a stable sample.
const missingEmbeddingsListSQL = `
SELECT p.photoprism_uid FROM photos p
WHERE p.photoprism_uid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.photo_uid = p.uid)
ORDER BY p.photoprism_uid
LIMIT $1`

// PhotosMissingEmbeddings returns up to limit photoprism_uids of imported photos
// lacking an embeddings row, plus the full total.
func (s *Store) PhotosMissingEmbeddings(ctx context.Context, limit int) ([]string, int, error) {
	return s.missingPhotos(ctx, missingEmbeddingsCountSQL, missingEmbeddingsListSQL, limit)
}

// missingFacesCountSQL counts imported photos with no face-detection record.
const missingFacesCountSQL = `
SELECT count(*) FROM photos p
WHERE p.photoprism_uid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM face_detections fd WHERE fd.photo_uid = p.uid)`

// missingFacesListSQL lists up to $1 photoprism_uids of imported photos with no
// face-detection record, ordered for a stable sample.
const missingFacesListSQL = `
SELECT p.photoprism_uid FROM photos p
WHERE p.photoprism_uid IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM face_detections fd WHERE fd.photo_uid = p.uid)
ORDER BY p.photoprism_uid
LIMIT $1`

// PhotosMissingFaces returns up to limit photoprism_uids of imported photos
// lacking a face-detection record, plus the full total.
func (s *Store) PhotosMissingFaces(ctx context.Context, limit int) ([]string, int, error) {
	return s.missingPhotos(ctx, missingFacesCountSQL, missingFacesListSQL, limit)
}

// missingPhotos runs a paired count/list query for a "missing" section: countSQL
// yields the full total and listSQL (parameterised by the limit) yields a capped,
// ordered sample of photoprism_uids. A non-positive limit skips the list query.
func (s *Store) missingPhotos(
	ctx context.Context, countSQL, listSQL string, limit int,
) ([]string, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, countSQL).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("importverify: counting missing photos: %w", err)
	}
	sample := make([]string, 0)
	if limit <= 0 {
		return sample, total, nil
	}
	rows, err := s.pool.Query(ctx, listSQL, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("importverify: listing missing photos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, 0, fmt.Errorf("importverify: scanning missing photo: %w", err)
		}
		sample = append(sample, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("importverify: iterating missing photos: %w", err)
	}
	return sample, total, nil
}

// albumTitlesSQL selects every catalogue album title.
const albumTitlesSQL = `SELECT title FROM albums`

// AlbumTitles returns the set of catalogue album titles.
func (s *Store) AlbumTitles(ctx context.Context) (map[string]struct{}, error) {
	return s.stringSet(ctx, albumTitlesSQL)
}

// labelNamesSQL selects every catalogue label name.
const labelNamesSQL = `SELECT name FROM labels`

// LabelNames returns the set of catalogue label names.
func (s *Store) LabelNames(ctx context.Context) (map[string]struct{}, error) {
	return s.stringSet(ctx, labelNamesSQL)
}

// subjectNamesSQL selects every catalogue subject name.
const subjectNamesSQL = `SELECT name FROM subjects`

// SubjectNames returns the set of catalogue subject names.
func (s *Store) SubjectNames(ctx context.Context) (map[string]struct{}, error) {
	return s.stringSet(ctx, subjectNamesSQL)
}

// stringSet runs a single-column query and collects the non-null values into a
// deduplicated set. It backs the album/label/subject name lookups.
func (s *Store) stringSet(ctx context.Context, query string) (map[string]struct{}, error) {
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("importverify: querying string set: %w", err)
	}
	defer rows.Close()

	set := make(map[string]struct{})
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("importverify: scanning string set value: %w", err)
		}
		set[value] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importverify: iterating string set: %w", err)
	}
	return set, nil
}
