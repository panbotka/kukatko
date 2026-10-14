package vectors

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

const (
	// efSearch is the HNSW hnsw.ef_search applied to every read transaction. A
	// higher value visits more candidates per query, trading a little latency for
	// better recall. The design pins it at 100: a measured sweet spot for the
	// halfvec/cosine indexes on the Pi (see docs/PERF.md), comfortably below the
	// pgvector ceiling. The Pi's limited RAM and the modest library size make a
	// larger ef_search pure latency cost with no recall benefit.
	efSearch = 100
	// efSearchMax is the upper bound the design forbids reaching: an ef_search at
	// or above it spends far more time per query for negligible recall gain on
	// these indexes and is never used. The guard test enforces efSearch stays
	// below it.
	efSearchMax = 400
)

// efSearchStmt tunes HNSW recall for the duration of a read transaction. A
// higher ef_search visits more candidates per query, trading a little latency
// for better recall; SET LOCAL scopes it to the current transaction only.
var efSearchStmt = "SET LOCAL hnsw.ef_search = " + strconv.Itoa(efSearch)

// Store is the database access layer for embeddings and faces. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// upsertEmbeddingSQL inserts an image embedding or overwrites the existing one
// for the photo, refreshing created_at to the time of the latest write. The
// row carries a denormalised copy of its photo's project (migration 0102), so a
// project-filtered kNN never joins photos; a re-embed refreshes it too. The
// photo uid is bound twice ($1 and $6): one placeholder used for the VARCHAR
// photo_uid column and the TEXT photos.uid fails type deduction (42P08).
const upsertEmbeddingSQL = `
INSERT INTO embeddings (photo_uid, embedding, model, pretrained, dim, project_id)
VALUES ($1, $2, $3, $4, $5, (SELECT project_id FROM photos WHERE uid = $6))
ON CONFLICT (photo_uid) DO UPDATE SET
    project_id = EXCLUDED.project_id,
    embedding  = EXCLUDED.embedding,
    model      = EXCLUDED.model,
    pretrained = EXCLUDED.pretrained,
    dim        = EXCLUDED.dim,
    created_at = now()
RETURNING created_at`

// SaveEmbedding inserts the image embedding for emb.PhotoUID, replacing any
// existing one (a re-embed overwrites in place). It validates the vector length
// against ImageDim and returns ErrDimMismatch on a mismatch. On success the
// stored Embedding, with Dim and CreatedAt populated, is returned.
func (s *Store) SaveEmbedding(ctx context.Context, emb Embedding) (Embedding, error) {
	if len(emb.Vector) != ImageDim {
		return Embedding{}, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(emb.Vector), ImageDim)
	}
	emb.Dim = len(emb.Vector)
	err := s.pool.QueryRow(ctx, upsertEmbeddingSQL,
		emb.PhotoUID, ToHalfVec(emb.Vector), emb.Model, emb.Pretrained, emb.Dim, emb.PhotoUID,
	).Scan(&emb.CreatedAt)
	if err != nil {
		return Embedding{}, fmt.Errorf("saving embedding for %s: %w", emb.PhotoUID, err)
	}
	return emb, nil
}

// getEmbeddingSQL reads the full image-embedding row for one photo of one
// project ($2). A photo of another project has no row here, exactly like a photo
// that was never embedded.
const getEmbeddingSQL = `
SELECT photo_uid, embedding, model, pretrained, dim, created_at, project_id
FROM embeddings
WHERE photo_uid = $1 AND project_id = $2`

// getEmbeddingForJobSQL reads the full image-embedding row for one photo in
// whichever project holds it — the unscoped loader of the embedding job.
const getEmbeddingForJobSQL = `
SELECT photo_uid, embedding, model, pretrained, dim, created_at, project_id
FROM embeddings
WHERE photo_uid = $1`

// GetEmbedding returns the image embedding stored for photoUID inside the
// project projectUID, or ErrEmbeddingNotFound when none exists — also when the
// photo belongs to another project, so the two cannot be told apart. An empty
// projectUID is ErrNoProject. Correctness does not depend on the caller having
// proved the photo first: the statement itself names the project.
func (s *Store) GetEmbedding(ctx context.Context, projectUID, photoUID string) (Embedding, error) {
	if err := requireProject(projectUID); err != nil {
		return Embedding{}, err
	}
	return s.getEmbedding(ctx, getEmbeddingSQL, photoUID, projectUID)
}

// GetEmbeddingForJob returns the image embedding stored for photoUID in
// whichever project holds it, or ErrEmbeddingNotFound. It is the unscoped loader
// of the embedding job, which has no request and so no project (the row carries
// it, Embedding.ProjectID). Nothing serving an HTTP request may call it — the
// *ForJob guard in internal/photos fails the build if an internal/*api package
// does.
func (s *Store) GetEmbeddingForJob(ctx context.Context, photoUID string) (Embedding, error) {
	return s.getEmbedding(ctx, getEmbeddingForJobSQL, photoUID)
}

// getEmbedding runs one of the embedding loaders and scans its single row,
// mapping "no row" to ErrEmbeddingNotFound. The photo uid is bound as $1, args
// follow it.
func (s *Store) getEmbedding(ctx context.Context, query, photoUID string, args ...any) (Embedding, error) {
	var (
		emb Embedding
		hv  pgvector.HalfVector
	)
	err := s.pool.QueryRow(ctx, query, append([]any{photoUID}, args...)...).Scan(
		&emb.PhotoUID, &hv, &emb.Model, &emb.Pretrained, &emb.Dim, &emb.CreatedAt, &emb.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Embedding{}, ErrEmbeddingNotFound
	}
	if err != nil {
		return Embedding{}, fmt.Errorf("getting embedding for %s: %w", photoUID, err)
	}
	emb.Vector = FromHalfVec(hv)
	return emb, nil
}

// listMissingEmbeddingSQL selects the uids of non-archived photos that have no
// row in embeddings, newest first. The %s placeholder is replaced with a LIMIT
// clause only when a positive limit is requested.
const listMissingEmbeddingSQL = `
SELECT p.uid
FROM photos p
LEFT JOIN embeddings e ON e.photo_uid = p.uid
WHERE p.project_id = $1
  AND e.photo_uid IS NULL AND p.archived_at IS NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListPhotosMissingEmbedding returns the uids of non-archived photos that do not
// yet have an image embedding, newest first. A positive limit caps the result;
// a non-positive limit returns every missing photo. It backs the embedding
// backfill, which enqueues an image_embed job per returned uid. It reads one
// project only: an empty projectUID is ErrNoProject.
func (s *Store) ListPhotosMissingEmbedding(ctx context.Context, projectUID string, limit int) ([]string, error) {
	return s.queryPhotoUIDs(ctx, listMissingEmbeddingSQL, projectUID, limit)
}

// queryPhotoUIDs runs a uid-listing query templated with a LIMIT placeholder and
// returns the scanned uids. tmpl must bind the project as $1 and contain a
// single %s that receives a LIMIT clause when limit is positive (and an empty
// string otherwise). It backs the "photos missing X" backfill listings, whose
// only difference is the join; an empty projectUID is ErrNoProject.
func (s *Store) queryPhotoUIDs(ctx context.Context, tmpl, projectUID string, limit int) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(tmpl, "")
	args := []any{projectUID}
	if limit > 0 {
		query = fmt.Sprintf(tmpl, "\nLIMIT $2")
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing photo uids: %w", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scanning photo uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating photo uids: %w", err)
	}
	return uids, nil
}

// withTunedReadTx opens a read-only transaction, applies the HNSW tuning (always
// hnsw.ef_search; hnsw.iterative_scan too when iterative is true) and runs fn. The
// transaction is always rolled back (queries make no changes), so the SET LOCAL
// settings never leak beyond the call. Errors from fn are returned unwrapped so
// callers can attribute them to the query itself.
func (s *Store) withTunedReadTx(ctx context.Context, iterative bool, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, efSearchStmt); err != nil {
		return fmt.Errorf("setting hnsw.ef_search: %w", err)
	}
	if iterative {
		if _, err := tx.Exec(ctx, iterativeScanStmt); err != nil {
			return fmt.Errorf("setting hnsw.iterative_scan: %w", err)
		}
	}
	return fn(tx)
}
