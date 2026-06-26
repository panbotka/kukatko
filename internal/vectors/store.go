package vectors

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// efSearchStmt tunes HNSW recall for the duration of a read transaction. A
// higher ef_search visits more candidates per query, trading a little latency
// for better recall; SET LOCAL scopes it to the current transaction only.
const efSearchStmt = "SET LOCAL hnsw.ef_search = 100"

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
// for the photo, refreshing created_at to the time of the latest write.
const upsertEmbeddingSQL = `
INSERT INTO embeddings (photo_uid, embedding, model, pretrained, dim)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (photo_uid) DO UPDATE SET
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
		emb.PhotoUID, ToHalfVec(emb.Vector), emb.Model, emb.Pretrained, emb.Dim,
	).Scan(&emb.CreatedAt)
	if err != nil {
		return Embedding{}, fmt.Errorf("saving embedding for %s: %w", emb.PhotoUID, err)
	}
	return emb, nil
}

// getEmbeddingSQL reads the full image-embedding row for one photo.
const getEmbeddingSQL = `
SELECT photo_uid, embedding, model, pretrained, dim, created_at
FROM embeddings
WHERE photo_uid = $1`

// GetEmbedding returns the image embedding stored for photoUID, or
// ErrEmbeddingNotFound when none exists.
func (s *Store) GetEmbedding(ctx context.Context, photoUID string) (Embedding, error) {
	var (
		emb Embedding
		hv  pgvector.HalfVector
	)
	err := s.pool.QueryRow(ctx, getEmbeddingSQL, photoUID).Scan(
		&emb.PhotoUID, &hv, &emb.Model, &emb.Pretrained, &emb.Dim, &emb.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Embedding{}, ErrEmbeddingNotFound
	}
	if err != nil {
		return Embedding{}, fmt.Errorf("getting embedding for %s: %w", photoUID, err)
	}
	emb.Vector = FromHalfVec(hv)
	return emb, nil
}

// withReadTx runs fn inside a read-only transaction with hnsw.ef_search tuned for
// recall. The transaction is always rolled back (queries make no changes), so the
// SET LOCAL never leaks beyond the call. Errors from fn are returned unwrapped so
// callers can attribute them to the query itself.
func (s *Store) withReadTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, efSearchStmt); err != nil {
		return fmt.Errorf("setting hnsw.ef_search: %w", err)
	}
	return fn(tx)
}
