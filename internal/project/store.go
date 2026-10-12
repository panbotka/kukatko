package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// uniqueViolation is the PostgreSQL SQLSTATE of a unique-constraint violation.
const uniqueViolation = "23505"

// columns is the projects column list every read scans, in scanProject's order.
const columns = `uid, slug, name, description, active, storage_prefix,
	max_file_bytes, max_total_bytes, created_at, updated_at`

// Store is the database access layer for projects. It owns no connection; it
// borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// scanProject reads one projects row, in the order of columns.
func scanProject(row pgx.Row) (*Project, error) {
	var p Project
	if err := row.Scan(&p.UID, &p.Slug, &p.Name, &p.Description, &p.Active, &p.StoragePrefix,
		&p.MaxFileBytes, &p.MaxTotalBytes, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scanning project: %w", err)
	}
	return &p, nil
}

// lockSQL reads one project row and locks it for the rest of the transaction.
const lockSQL = `SELECT ` + columns + ` FROM projects WHERE uid = $1 FOR UPDATE`

// lockOne reads and locks the project uid inside tx, mapping "no row" to
// ErrNotFound, so a read-modify-write cannot interleave with another one.
func lockOne(ctx context.Context, tx pgx.Tx, uid string) (*Project, error) {
	p, err := scanProject(tx.QueryRow(ctx, lockSQL, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// Get returns the project with the given uid, or ErrNotFound.
func (s *Store) Get(ctx context.Context, uid string) (*Project, error) {
	return s.getBy(ctx, "uid", uid)
}

// GetBySlug returns the project with the given slug, or ErrNotFound.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	return s.getBy(ctx, "slug", slug)
}

// getBy returns the project whose column equals value, or ErrNotFound. column is
// one of the package's own literals ("uid", "slug"), never input.
func (s *Store) getBy(ctx context.Context, column, value string) (*Project, error) {
	query := `SELECT ` + columns + ` FROM projects WHERE ` + column + ` = $1`
	p, err := scanProject(s.pool.QueryRow(ctx, query, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project: reading %s %q: %w", column, value, err)
	}
	return p, nil
}

// List returns every project ordered by name (then uid, so equal names keep a
// stable order). An instance always has at least the bootstrap project.
func (s *Store) List(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM projects ORDER BY name, uid`)
	if err != nil {
		return nil, fmt.Errorf("project: listing: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("project: scanning: %w", err)
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: iterating: %w", err)
	}
	return out, nil
}

// insertSQL creates one project row and returns it.
const insertSQL = `
INSERT INTO projects (uid, slug, name, description, storage_prefix, max_file_bytes, max_total_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING ` + columns

// Create validates p, inserts a new active project with a fresh "pj…" uid and the
// storage prefix "projects/<uid>/" — a created project never has the empty root
// prefix — and records ActionProjectCreate in the same transaction. It returns
// ErrInvalidSlug, ErrInvalidName or ErrInvalidLimit for bad input and
// ErrSlugTaken when another project already has the slug.
func (s *Store) Create(ctx context.Context, meta audit.Meta, p CreateParams) (*Project, error) {
	if err := ValidateSlug(p.Slug); err != nil {
		return nil, err
	}
	name, err := normalizeName(p.Name)
	if err != nil {
		return nil, err
	}
	if err := validateLimit(p.MaxFileBytes); err != nil {
		return nil, err
	}
	if err := validateLimit(p.MaxTotalBytes); err != nil {
		return nil, err
	}
	uid, err := newUID()
	if err != nil {
		return nil, err
	}
	entry := meta.Entry(audit.ActionProjectCreate, TargetType, uid, map[string]any{
		"slug": p.Slug, "name": name,
		"max_file_bytes": p.MaxFileBytes, "max_total_bytes": p.MaxTotalBytes,
	})

	var created *Project
	err = s.inTx(ctx, entry, func(tx pgx.Tx) error {
		var insertErr error
		created, insertErr = scanProject(tx.QueryRow(ctx, insertSQL, uid, p.Slug, name,
			strings.TrimSpace(p.Description), StoragePrefixFor(uid), p.MaxFileBytes, p.MaxTotalBytes))
		return insertErr
	})
	if isSlugTaken(err) {
		return nil, fmt.Errorf("%w: %q", ErrSlugTaken, p.Slug)
	}
	if err != nil {
		return nil, fmt.Errorf("project: creating %q: %w", p.Slug, err)
	}
	return created, nil
}

// updateSQL writes every editable column of one project and returns the row.
const updateSQL = `
UPDATE projects
SET name = $2, description = $3, active = $4, max_file_bytes = $5, max_total_bytes = $6,
    updated_at = now()
WHERE uid = $1
RETURNING ` + columns

// Update applies patch to the project uid and records ActionProjectUpdate, with
// the field-level old → new diff, in the same transaction. A patch that changes
// nothing writes neither the row nor an audit entry and returns the project as
// it is. It returns ErrNotFound for an unknown uid and ErrInvalidName or
// ErrInvalidLimit for bad input.
func (s *Store) Update(ctx context.Context, meta audit.Meta, uid string, patch UpdatePatch) (*Project, error) {
	entry := meta.Entry(audit.ActionProjectUpdate, TargetType, uid, map[string]any{})

	var result *Project
	err := s.inTx(ctx, entry, func(tx pgx.Tx) error {
		current, err := lockOne(ctx, tx, uid)
		if err != nil {
			return err
		}
		next, err := applyPatch(*current, patch)
		if err != nil {
			return err
		}
		changes := diff(*current, next)
		if changes.Len() == 0 {
			result = current
			return errNoChange
		}
		changes.StampInto(entry.Details)
		result, err = scanProject(tx.QueryRow(ctx, updateSQL, uid, next.Name, next.Description, next.Active,
			next.MaxFileBytes, next.MaxTotalBytes))
		return err
	})
	if errors.Is(err, errNoChange) {
		return result, nil
	}
	if err != nil {
		return nil, wrapMutationErr("updating", uid, err)
	}
	return result, nil
}

// setPrefixSQL replaces one project's storage prefix and returns the row.
const setPrefixSQL = `
UPDATE projects SET storage_prefix = $2, updated_at = now()
WHERE uid = $1
RETURNING ` + columns

// SetStoragePrefix replaces the storage prefix of the project uid and records
// ActionProjectStoragePrefix (with the old and new prefix) in the same
// transaction. It is the only way the prefix changes; it is meant for the
// storage move, which rewrites the keys before switching the prefix. It returns
// ErrInvalidStoragePrefix for a malformed prefix and ErrNotFound for an unknown
// uid.
func (s *Store) SetStoragePrefix(ctx context.Context, meta audit.Meta, uid, prefix string) (*Project, error) {
	if err := ValidateStoragePrefix(prefix); err != nil {
		return nil, err
	}
	entry := meta.Entry(audit.ActionProjectStoragePrefix, TargetType, uid, map[string]any{})

	var result *Project
	err := s.inTx(ctx, entry, func(tx pgx.Tx) error {
		current, err := lockOne(ctx, tx, uid)
		if err != nil {
			return err
		}
		entry.Details["old"] = current.StoragePrefix
		entry.Details["new"] = prefix
		result, err = scanProject(tx.QueryRow(ctx, setPrefixSQL, uid, prefix))
		return err
	})
	if err != nil {
		return nil, wrapMutationErr("setting the storage prefix of", uid, err)
	}
	return result, nil
}

// libraryUsageSQL sums the whole library. Every photo row counts, archived ones
// included: their originals stay stored until a purge deletes the row.
const libraryUsageSQL = `SELECT count(*), COALESCE(sum(file_size), 0)::bigint FROM photos`

// Usage returns how many photos the project uid holds and the summed size of
// their originals. It returns ErrNotFound for an unknown uid.
func (s *Store) Usage(ctx context.Context, uid string) (Usage, error) {
	if _, err := s.Get(ctx, uid); err != nil {
		return Usage{}, err
	}
	//nolint:godox // a deliberate marker the later multi-tenancy steps look for; it goes when project_id lands
	if uid != BootstrapUID {
		// TODO multi-tenancy: scope by photos.project_id. Until that column
		// exists, the whole library belongs to the bootstrap project and every
		// other project is empty.
		return Usage{}, nil
	}
	var u Usage
	if err := s.pool.QueryRow(ctx, libraryUsageSQL).Scan(&u.Photos, &u.OriginalsBytes); err != nil {
		return Usage{}, fmt.Errorf("project: measuring usage of %s: %w", uid, err)
	}
	return u, nil
}

// errNoChange aborts an update transaction whose patch changed nothing, so no
// row is written and no audit entry recorded; Update turns it back into success.
var errNoChange = errors.New("project: nothing to change")

// inTx runs mutate in a transaction and writes entry to the audit log in the
// same transaction after it, committing only when both succeed. entry is taken
// by value but its Details map is shared, so mutate may stamp details into it;
// the target uid must be set before the call.
func (s *Store) inTx(ctx context.Context, entry audit.Entry, mutate func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// applyPatch returns current with patch applied, validating every field the
// patch sets. It returns ErrInvalidName or ErrInvalidLimit for bad input.
func applyPatch(current Project, patch UpdatePatch) (Project, error) {
	next := current
	if patch.Name != nil {
		name, err := normalizeName(*patch.Name)
		if err != nil {
			return Project{}, err
		}
		next.Name = name
	}
	if patch.Description != nil {
		next.Description = strings.TrimSpace(*patch.Description)
	}
	if patch.Active != nil {
		next.Active = *patch.Active
	}
	var err error
	next.MaxFileBytes, err = patchLimit(current.MaxFileBytes, patch.MaxFileBytes, patch.ClearMaxFileBytes)
	if err != nil {
		return Project{}, err
	}
	next.MaxTotalBytes, err = patchLimit(current.MaxTotalBytes, patch.MaxTotalBytes, patch.ClearMaxTotalBytes)
	if err != nil {
		return Project{}, err
	}
	return next, nil
}

// diff records every editable field that differs between before and after.
func diff(before, after Project) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("name", before.Name, after.Name)
	changes.Add("description", before.Description, after.Description)
	changes.Add("active", before.Active, after.Active)
	changes.Add("max_file_bytes", before.MaxFileBytes, after.MaxFileBytes)
	changes.Add("max_total_bytes", before.MaxTotalBytes, after.MaxTotalBytes)
	return changes
}

// wrapMutationErr passes the package's sentinel errors through unchanged and
// wraps anything else with the operation and the project uid.
func wrapMutationErr(op, uid string, err error) error {
	for _, sentinel := range []error{ErrNotFound, ErrInvalidName, ErrInvalidLimit} {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	return fmt.Errorf("project: %s %s: %w", op, uid, err)
}

// isSlugTaken reports whether err is the PostgreSQL unique-constraint violation
// of projects.slug (constraint projects_slug_key).
func isSlugTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
		strings.Contains(pgErr.ConstraintName, "slug")
}
