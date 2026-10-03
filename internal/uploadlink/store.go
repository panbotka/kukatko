package uploadlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// TargetType is the audit target type of a link mutation.
const TargetType = "upload_links"

// pgForeignKeyViolation is the SQLSTATE of a foreign-key violation: a named
// album or label that does not exist.
const pgForeignKeyViolation = "23503"

// codeAttempts bounds how often Create redraws a code that collides with an
// existing one. With ~46 bits per code a single collision is already a
// curiosity; the bound only keeps a broken random source from looping forever.
const codeAttempts = 5

// Store is the database access layer for upload links. It borrows the shared
// pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// row is the column scanner shared by the single-row and listing queries.
type row interface {
	Scan(dest ...any) error
}

// selectLinkSQL reads links with their creator's name and both target lists
// (as JSON arrays ordered by name), so one query yields a complete Link.
const selectLinkSQL = `
SELECT l.uid, l.title, l.note, l.created_by,
       COALESCE(NULLIF(u.display_name, ''), u.username, ''),
       l.created_at, l.expires_at, l.revoked_at, l.upload_count, l.last_used_at,
       COALESCE((SELECT json_agg(json_build_object('uid', a.uid, 'name', a.title) ORDER BY a.title, a.uid)
                 FROM upload_link_albums la JOIN albums a ON a.uid = la.album_uid
                 WHERE la.link_uid = l.uid), '[]'::json),
       COALESCE((SELECT json_agg(json_build_object('uid', b.uid, 'name', b.name) ORDER BY b.name, b.uid)
                 FROM upload_link_labels lb JOIN labels b ON b.uid = lb.label_uid
                 WHERE lb.link_uid = l.uid), '[]'::json)
FROM upload_links l
LEFT JOIN users u ON u.uid = l.created_by`

// scanLink reads one selectLinkSQL row into a Link, mapping pgx.ErrNoRows to
// ErrNotFound.
func scanLink(r row) (Link, error) {
	var (
		link           Link
		albums, labels []byte
	)
	err := r.Scan(&link.UID, &link.Title, &link.Note, &link.CreatedBy, &link.CreatorName,
		&link.CreatedAt, &link.ExpiresAt, &link.RevokedAt, &link.UploadCount, &link.LastUsedAt,
		&albums, &labels)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("uploadlink: scanning link: %w", err)
	}
	if err := json.Unmarshal(albums, &link.Albums); err != nil {
		return Link{}, fmt.Errorf("uploadlink: decoding albums: %w", err)
	}
	if err := json.Unmarshal(labels, &link.Labels); err != nil {
		return Link{}, fmt.Errorf("uploadlink: decoding labels: %w", err)
	}
	return link, nil
}

// Get returns the link with uid, or ErrNotFound.
func (s *Store) Get(ctx context.Context, uid string) (Link, error) {
	return scanLink(s.pool.QueryRow(ctx, selectLinkSQL+" WHERE l.uid = $1", uid))
}

// ByCode returns the link whose short code is code, whatever its state, or
// ErrNotFound. A code of the wrong shape is ErrNotFound without a query.
func (s *Store) ByCode(ctx context.Context, code string) (Link, error) {
	if !ValidCode(code) {
		return Link{}, ErrNotFound
	}
	return scanLink(s.pool.QueryRow(ctx, selectLinkSQL+" WHERE l.code_hash = $1", HashSecret(code)))
}

// List returns links newest first: those created by creatorUID, or every link
// when creatorUID is empty (the administrator's view). No links yield an empty,
// non-nil slice.
func (s *Store) List(ctx context.Context, creatorUID string) ([]Link, error) {
	query := selectLinkSQL + " WHERE ($1 = '' OR l.created_by = $1) ORDER BY l.created_at DESC, l.uid"
	rows, err := s.pool.Query(ctx, query, creatorUID)
	if err != nil {
		return nil, fmt.Errorf("uploadlink: listing links: %w", err)
	}
	defer rows.Close()
	links := []Link{}
	for rows.Next() {
		link, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploadlink: iterating links: %w", err)
	}
	return links, nil
}

// Validate checks in for what Create refuses: an over-long title or note, no
// target at all, or too many of either kind. It is exported so the HTTP layer
// can answer 400 before it opens a transaction.
func (in NewLink) Validate() error {
	switch {
	case utf8.RuneCountInString(in.Title) > MaxTitleLen:
		return ErrTitleTooLong
	case utf8.RuneCountInString(in.Note) > MaxNoteLen:
		return ErrNoteTooLong
	case len(uniqueNonEmpty(in.AlbumUIDs))+len(uniqueNonEmpty(in.LabelUIDs)) == 0:
		return ErrNoTargets
	case len(uniqueNonEmpty(in.AlbumUIDs)) > MaxTargets, len(uniqueNonEmpty(in.LabelUIDs)) > MaxTargets:
		return ErrTooManyTargets
	}
	return nil
}

// insertLinkSQL inserts a link unless its code hash collides, in which case it
// returns no row and Create draws another code.
const insertLinkSQL = `
INSERT INTO upload_links (uid, code_hash, title, note, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (code_hash) DO NOTHING
RETURNING uid`

// Create stores the link in describes and returns it together with its
// plaintext code — the only time the code exists outside the creator's hands.
// The link, its targets and entry (whose TargetUID is stamped with the new
// link's UID) commit together. It returns the Validate errors and
// ErrTargetNotFound for an album or label that does not exist.
func (s *Store) Create(ctx context.Context, in NewLink, entry audit.Entry) (Link, string, error) {
	if err := in.Validate(); err != nil {
		return Link{}, "", err
	}
	uid, err := newLinkUID()
	if err != nil {
		return Link{}, "", err
	}
	var code string
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if code, err = insertLink(ctx, tx, uid, in); err != nil {
			return err
		}
		if err := insertTargets(ctx, tx, uid, in); err != nil {
			return err
		}
		entry.TargetType = TargetType
		entry.TargetUID = uid
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Link{}, "", err
	}
	link, err := s.Get(ctx, uid)
	if err != nil {
		return Link{}, "", err
	}
	return link, code, nil
}

// insertLink inserts the link row on tx under a fresh code, redrawing the code
// on the (astronomically unlikely) collision, and returns the plaintext code.
func insertLink(ctx context.Context, tx pgx.Tx, uid string, in NewLink) (string, error) {
	for range codeAttempts {
		code, err := NewCode()
		if err != nil {
			return "", err
		}
		var got string
		err = tx.QueryRow(ctx, insertLinkSQL, uid, HashSecret(code), in.Title, in.Note,
			in.CreatedBy, in.ExpiresAt).Scan(&got)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("uploadlink: inserting link: %w", err)
		}
		return code, nil
	}
	return "", errors.New("uploadlink: could not draw a unique code")
}

// insertTargets stores the link's album and label targets on tx, translating a
// missing album or label into ErrTargetNotFound.
func insertTargets(ctx context.Context, tx pgx.Tx, uid string, in NewLink) error {
	for _, albumUID := range uniqueNonEmpty(in.AlbumUIDs) {
		if _, err := tx.Exec(ctx,
			"INSERT INTO upload_link_albums (link_uid, album_uid) VALUES ($1, $2)", uid, albumUID); err != nil {
			return translateTargetFK(err)
		}
	}
	for _, labelUID := range uniqueNonEmpty(in.LabelUIDs) {
		if _, err := tx.Exec(ctx,
			"INSERT INTO upload_link_labels (link_uid, label_uid) VALUES ($1, $2)", uid, labelUID); err != nil {
			return translateTargetFK(err)
		}
	}
	return nil
}

// translateTargetFK maps a foreign-key violation onto ErrTargetNotFound and
// wraps anything else.
func translateTargetFK(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		return ErrTargetNotFound
	}
	return fmt.Errorf("uploadlink: inserting target: %w", err)
}

// Extend moves the expiry of the link with uid to expiresAt and returns the
// updated link. The change and entry (stamped with the old and new expiry)
// commit together. It returns ErrNotFound for an unknown link and ErrRevoked for
// a revoked one — revocation is final.
func (s *Store) Extend(ctx context.Context, uid string, expiresAt time.Time, entry audit.Entry) (Link, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var (
			old     time.Time
			revoked *time.Time
		)
		err := tx.QueryRow(ctx, "SELECT expires_at, revoked_at FROM upload_links WHERE uid = $1 FOR UPDATE",
			uid).Scan(&old, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("uploadlink: locking link: %w", err)
		}
		if revoked != nil {
			return ErrRevoked
		}
		if _, err := tx.Exec(ctx, "UPDATE upload_links SET expires_at = $2 WHERE uid = $1", uid, expiresAt); err != nil {
			return fmt.Errorf("uploadlink: extending link: %w", err)
		}
		entry.TargetType, entry.TargetUID = TargetType, uid
		entry.Details = withDetails(entry.Details, map[string]any{
			"old_expires_at": old.UTC(), "new_expires_at": expiresAt.UTC(),
		})
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Link{}, err
	}
	return s.Get(ctx, uid)
}

// Revoke revokes the link with uid for good and returns it. Revoking an already
// revoked link changes nothing and writes no audit entry; otherwise the change
// and entry commit together. It returns ErrNotFound for an unknown link.
func (s *Store) Revoke(ctx context.Context, uid string, entry audit.Entry) (Link, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var revoked *time.Time
		err := tx.QueryRow(ctx, "SELECT revoked_at FROM upload_links WHERE uid = $1 FOR UPDATE", uid).Scan(&revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("uploadlink: locking link: %w", err)
		}
		if revoked != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE upload_links SET revoked_at = now() WHERE uid = $1", uid); err != nil {
			return fmt.Errorf("uploadlink: revoking link: %w", err)
		}
		entry.TargetType, entry.TargetUID = TargetType, uid
		return audit.Write(ctx, tx, entry)
	})
	if err != nil {
		return Link{}, err
	}
	return s.Get(ctx, uid)
}

// inTx runs fn in a transaction and commits it, rolling back when fn fails.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("uploadlink: beginning transaction: %w", err)
	}
	// After a commit the rollback is a no-op; on every early return it undoes
	// whatever fn wrote.
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("uploadlink: committing transaction: %w", err)
	}
	return nil
}

// uniqueNonEmpty returns values without empty strings and repeats, in their
// first-seen order.
func uniqueNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// withDetails returns base extended with extra, allocating base when nil.
func withDetails(base, extra map[string]any) map[string]any {
	if base == nil {
		base = make(map[string]any, len(extra))
	}
	maps.Copy(base, extra)
	return base
}
