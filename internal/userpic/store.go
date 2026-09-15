package userpic

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the database access layer for user_pictures. It owns no connection;
// it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// getQuery reads one account's stored picture. The bytes are selected too, which
// is why nothing but the picture endpoint calls this: an uploaded picture is
// tens of kilobytes and has no business riding along with a login.
const getQuery = `SELECT kind, image, photo_uid, updated_at FROM user_pictures WHERE user_uid = $1`

// Get returns the account's stored picture, or ErrNoPicture when it has none.
// "None" here means no stored row — it says nothing about the rest of the chain,
// which the Service resolves.
func (s *Store) Get(ctx context.Context, userUID string) (Picture, error) {
	var (
		pic      Picture
		photoUID *string
	)
	err := s.pool.QueryRow(ctx, getQuery, userUID).
		Scan(&pic.Kind, &pic.Image, &photoUID, &pic.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Picture{}, ErrNoPicture
	}
	if err != nil {
		return Picture{}, fmt.Errorf("userpic: reading picture of %s: %w", userUID, err)
	}
	if photoUID != nil {
		pic.PhotoUID = *photoUID
	}
	return pic, nil
}

// setQuery replaces an account's picture, whatever it was. The upsert is what
// makes "set" one statement rather than a read and a branch: a user has one
// picture, and choosing a new one is always a replacement — the columns of the
// answer that is not being given are written NULL, so switching from an upload
// to a pick leaves no orphaned bytes behind.
const setQuery = `INSERT INTO user_pictures (user_uid, kind, image, photo_uid, updated_at)
	VALUES ($1, $2, $3, $4, now())
	ON CONFLICT (user_uid) DO UPDATE
	SET kind = EXCLUDED.kind, image = EXCLUDED.image, photo_uid = EXCLUDED.photo_uid,
		updated_at = EXCLUDED.updated_at`

// SetUpload stores image as the account's uploaded picture, replacing whatever
// it had. The bytes must already have been through Normalize; nothing here
// re-checks them.
func (s *Store) SetUpload(ctx context.Context, userUID string, image []byte) error {
	if _, err := s.pool.Exec(ctx, setQuery, userUID, KindUpload, image, nil); err != nil {
		return fmt.Errorf("userpic: storing uploaded picture of %s: %w", userUID, err)
	}
	return nil
}

// SetPhoto points the account's picture at the library photo named by photoUID,
// replacing whatever it had. Whether that photo may be a profile picture is the
// Service's decision, not this one's.
func (s *Store) SetPhoto(ctx context.Context, userUID, photoUID string) error {
	if _, err := s.pool.Exec(ctx, setQuery, userUID, KindPhoto, nil, photoUID); err != nil {
		return fmt.Errorf("userpic: storing picked picture of %s: %w", userUID, err)
	}
	return nil
}

// Clear removes the account's stored picture. It is idempotent: clearing a
// picture nobody set is not an error, because the state the caller asked for is
// the state that already holds.
//
// It does not put the account back to the coloured initial by itself — an
// account linked to a person falls back to that person's face, which is the
// chain doing what it is for.
func (s *Store) Clear(ctx context.Context, userUID string) error {
	const q = `DELETE FROM user_pictures WHERE user_uid = $1`
	if _, err := s.pool.Exec(ctx, q, userUID); err != nil {
		return fmt.Errorf("userpic: clearing picture of %s: %w", userUID, err)
	}
	return nil
}
