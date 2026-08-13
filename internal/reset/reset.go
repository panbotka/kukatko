// Package reset implements the guarded wipe of a Kukátko library: every
// catalogue table emptied and every object the store owns deleted, so the
// library can be re-imported from scratch. It is what `kukatko maintenance
// reset` runs.
//
// # Why the guards are the feature
//
// This package deletes on purpose, and the deployment it was written for has no
// backup of its own (S3 backup and the restore rehearsal were waived by an
// explicit owner decision). It has no rollback left either: everything the
// library holds arrived through `kukatko import dir`, and re-walking those
// folders is the only way back. A misfire is simply unrecoverable, so the
// interesting part of this package is not the
// truncation, which is one statement, but everything that has to be true before
// it runs:
//
//   - Nothing is deleted unless the caller sets Options.Execute. A run without it
//     counts what it would delete and stops.
//   - The connected database must be the one the loaded config names, checked
//     against the server rather than trusted from the DSN (verifyTarget).
//   - The operator must have typed that database's name; y/N is not a
//     confirmation, because y/N is what a tired person answers by reflex.
//   - When the store is a bucket, the operator must have typed that bucket's name
//     too. The database and the bucket are configured independently and can be
//     pointed at different deployments — a dev database against the production
//     bucket is exactly the accident that motivated this guard — so confirming one
//     of them says nothing about the other.
//   - The live schema must match the tables this package classifies, so a
//     migration that adds a table cannot silently leave part of the library
//     behind (classifySchema).
//   - Deletion in the store is confined to the prefixes this application writes:
//     YYYY/MM originals, thumb/ and sidecars/. Anything else in the bucket is
//     counted and left alone, and a key the catalogue never referenced is deleted
//     only under Options.OrphanSweep (classifyKey, sweepKeys).
//   - The truncation and its audit entry share one transaction, so the record of
//     the wipe cannot be lost to a failure between them — and audit_log is one of
//     the tables the wipe must never touch.
//
// # Order of operations
//
// The store is emptied before the catalogue, because the catalogue is where the
// object keys come from: truncating first would strand every object as an orphan
// nothing can name. Any object that could not be deleted aborts the run before
// the truncation, leaving a catalogue that still describes the store — the whole
// operation is idempotent, so the fix is to solve the storage failure and run it
// again.
//
// # What it never touches
//
// The accounts, sessions, API tokens, the announcement, the audit trail and the
// import-run history (preservedTables) — the last of which is the only surviving
// record of where the library came from.
package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/thumb"
)

// Sentinel errors a reset ends on, matchable with errors.Is.
var (
	// ErrTargetMismatch indicates the connected database is not the one the
	// loaded config names. It is the guard against a typo, a stale environment
	// variable or a forgotten --config reaching a database nobody meant to wipe.
	ErrTargetMismatch = errors.New("reset: connected to a different database than the config names")
	// ErrConfirmationMismatch indicates the operator did not type the target
	// database's name exactly.
	ErrConfirmationMismatch = errors.New("reset: the typed name does not match the target database")
	// ErrBucketConfirmationMismatch indicates the operator did not type the
	// configured bucket's name exactly. It is also what a run gets when it names a
	// bucket against a store that has none: an operator who types a bucket name is
	// aiming at a bucket, and the run they meant is not the one about to happen.
	ErrBucketConfirmationMismatch = errors.New("reset: the typed name does not match the configured bucket")
	// ErrSchemaDrift indicates the database holds tables this package does not
	// classify, or lacks tables it does. Either way the wipe would be incomplete
	// or misdirected, so it does not run.
	ErrSchemaDrift = errors.New("reset: the schema does not match the tables this command classifies")
	// ErrNotExecuting indicates Execute was called without Options.Execute. A dry
	// run reports through Preflight and deletes nothing.
	ErrNotExecuting = errors.New("reset: refusing to delete without Options.Execute")
	// ErrStorageIncomplete indicates objects could not be deleted from the store.
	// The catalogue is left intact — it is the only remaining record of what those
	// objects are — and the run may be repeated once the store is reachable.
	ErrStorageIncomplete = errors.New("reset: some objects could not be deleted; the catalogue was left intact")
	// ErrSweepUnsupported indicates an orphan sweep was requested of a store that
	// cannot enumerate its keys.
	ErrSweepUnsupported = errors.New("reset: the configured store cannot list its keys, so it cannot be swept")
)

// defaultConcurrency bounds how many objects are deleted in parallel. A full
// library is a six-figure number of small requests against an object store, where
// latency rather than bandwidth is the cost; a handful in flight turns hours into
// minutes without pretending the store has no rate limits.
const defaultConcurrency = 8

// auditTargetType names the entity a reset entry targets: not a photo, an album
// or a user, but the library as a whole.
const auditTargetType = "library"

// ObjectStore is the subset of storage.Storage the wipe needs: removing one
// object by its key. Deleting a key that holds nothing must report an error
// wrapping os.ErrNotExist rather than failing the run.
type ObjectStore interface {
	// Delete removes the object at relPath.
	Delete(ctx context.Context, relPath string) error
}

// ThumbCache is the subset of *thumb.Thumbnailer the wipe needs: dropping the
// locally cached thumbnails of one file hash. It is separate from ObjectStore
// because the cache is local even when the objects are not — on a publishing
// backend every thumbnail exists twice, in the bucket and in the cache
// directory, and a wipe that forgot the second would leave the next import
// serving the previous library's thumbnails.
type ThumbCache interface {
	// Remove deletes every cached thumbnail size for the given file hash.
	Remove(hash string) error
}

// Target names the two things a reset is allowed to touch, as the loaded config
// names them: one database and — on a bucket-backed store — one bucket. The
// database is compared against what the server reports, so the DSN alone never
// decides what gets wiped.
type Target struct {
	// Host and Port are where the config points; they are printed for the
	// operator and not otherwise enforced, because a host is reachable under many
	// names and the database name is the identity that matters.
	Host string `json:"host"`
	Port uint16 `json:"port"`
	// Database is the database name the wipe is confined to.
	Database string `json:"database"`
	// Bucket is the object store's bucket, empty when the configured backend has
	// none (the local filesystem). It is not an address the wipe enforces — the
	// store was already built from the same config — but the name the operator has
	// to type, and the one printed before they are asked.
	Bucket string `json:"bucket"`
}

// TargetFromConfig resolves what a reset may touch from the loaded config: dsn is
// database.url and bucket is the configured object store's bucket, which is the
// empty string for a backend that has no bucket.
//
// It returns an error when the DSN is unusable or names no database, since
// "whatever the server defaults to" is not a target anyone chose. A missing
// bucket is not an error for the same reason it is not a guess: the filesystem
// backend genuinely has none, and its originals are already confined to a
// configured directory.
func TargetFromConfig(dsn, bucket string) (Target, error) {
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return Target{}, fmt.Errorf("reset: parsing database url: %w", err)
	}
	if parsed.Database == "" {
		return Target{}, fmt.Errorf("%w: the configured database url names no database", ErrTargetMismatch)
	}
	return Target{Host: parsed.Host, Port: parsed.Port, Database: parsed.Database, Bucket: bucket}, nil
}

// String renders the target as host:port/database, plus the bucket when there is
// one — the one line an operator has to read before confirming.
func (t Target) String() string {
	target := fmt.Sprintf("%s:%d/%s", t.Host, t.Port, t.Database)
	if t.Bucket == "" {
		return target
	}
	return target + " + bucket " + t.Bucket
}

// Connection is what the server says about itself, read back over the same pool
// the wipe would run on.
type Connection struct {
	// Database is the result of current_database().
	Database string `json:"database"`
	// ServerAddr is the server's own address, empty over a Unix socket.
	ServerAddr string `json:"server_addr"`
	// ServerPort is the port the server listens on.
	ServerPort int `json:"server_port"`
}

// Options are the switches and guards of one run.
type Options struct {
	// Execute turns the dry run into a real wipe. Without it nothing is deleted.
	Execute bool
	// Confirm is the database name the operator typed. It must equal
	// Target.Database exactly or Execute refuses.
	Confirm string
	// ConfirmBucket is the bucket name the operator typed. It must equal
	// Target.Bucket exactly or Execute refuses — including when Target.Bucket is
	// empty, where anything typed means the operator is aiming at a bucket this run
	// does not have.
	ConfirmBucket string
	// OrphanSweep also deletes the objects under Kukátko's prefixes that the
	// catalogue does not reference — leftovers from an interrupted import, or from
	// a library wiped before this command existed. Off by default: without it the
	// catalogue is the complete list of what may be deleted.
	OrphanSweep bool
	// Concurrency bounds parallel object deletions; zero or less uses
	// defaultConcurrency.
	Concurrency int
	// ActorUID is the acting user's UID when one is known. A CLI run has no
	// session, so it stays empty and the audit entry records a system action whose
	// operator is named in Operator.
	ActorUID string
	// Operator identifies who ran the wipe for the audit trail — for a CLI run the
	// OS user and host, which is the strongest identity available without a login.
	Operator string
}

// concurrency returns the effective parallel-deletion limit.
func (o Options) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	return defaultConcurrency
}

// StoragePlan is what a run would remove from the store, per owned prefix.
type StoragePlan struct {
	// Referenced counts the keys the catalogue names: one per original, one
	// sidecar beside it, and one thumbnail per registered size. They are
	// candidates, not measurements — a thumbnail size that was never generated is
	// counted here and simply found missing when deleted.
	Referenced PrefixCounts `json:"referenced"`
	// Stored counts the objects the store actually holds under each owned prefix.
	// It is measured by listing the store, which only a sweep does, and is zero
	// otherwise.
	Stored PrefixCounts `json:"stored"`
	// Foreign is the number of keys the store holds outside every owned prefix.
	// They are reported so the operator can see them, and never deleted.
	Foreign int `json:"foreign"`
	// Sweep reports whether Stored and Foreign were measured at all.
	Sweep bool `json:"sweep"`
}

// Preflight is everything the operator is shown before being asked to confirm:
// which database is about to be emptied, and how much is in it.
type Preflight struct {
	// Target is the database the config names.
	Target Target `json:"target"`
	// Connection is what the server reports about itself.
	Connection Connection `json:"connection"`
	// Counts holds the row counts of the catalogue and preserved tables.
	Counts Counts `json:"counts"`
	// Storage is the per-prefix object plan.
	Storage StoragePlan `json:"storage"`
}

// Result is the outcome of a completed wipe: the counts before and after, so the
// result is verifiable rather than assumed, and what happened in the store.
type Result struct {
	// Before holds the row counts as the preflight measured them.
	Before Counts `json:"before"`
	// After holds the row counts re-measured once the truncation committed.
	After Counts `json:"after"`
	// Storage reports what was deleted from the store.
	Storage StorageResult `json:"storage"`
}

// Config bundles the dependencies of New.
type Config struct {
	// Pool is the connection pool of the database to be wiped. Required.
	Pool *pgxpool.Pool
	// Target is the database the config names. Required, and its Database must be
	// non-empty: there is no "wipe whatever we happen to be connected to".
	Target Target
	// Storage holds the originals, sidecars and (on a publishing backend) the
	// thumbnails. Required.
	Storage ObjectStore
	// Thumbs drops locally cached thumbnails. Optional; nil leaves the local cache
	// alone, which is safe (a stale cache entry is addressed by a hash no photo
	// has any more) but wastes disk.
	Thumbs ThumbCache
	// CacheDir is the local derived-artifact root (storage.cache_path), whose
	// thumbnail subdirectory an orphan sweep removes wholesale. Optional.
	CacheDir string
}

// Service performs the guarded wipe.
type Service struct {
	pool     *pgxpool.Pool
	target   Target
	store    ObjectStore
	thumbs   ThumbCache
	cacheDir string
}

// New returns a Service from cfg. It panics when Pool or Storage is nil or the
// target names no database, since all three are wiring bugs that must not be
// discoverable by running a destructive command and watching what happens.
func New(cfg Config) *Service {
	if cfg.Pool == nil || cfg.Storage == nil {
		panic("reset: Pool and Storage are required")
	}
	if cfg.Target.Database == "" {
		panic("reset: Target.Database is required")
	}
	return &Service{
		pool:     cfg.Pool,
		target:   cfg.Target,
		store:    cfg.Storage,
		thumbs:   cfg.Thumbs,
		cacheDir: cfg.CacheDir,
	}
}

// Target returns the database this service is confined to.
func (s *Service) Target() Target {
	return s.target
}

// Preflight verifies the target and the schema, then measures what a wipe would
// remove: the row count of every table on both sides of the classification, and
// the object count of every owned prefix. It changes nothing, and it is what a
// dry run consists of.
//
// It returns ErrTargetMismatch when the connected database is not the configured
// one, and ErrSchemaDrift when the live schema holds tables this package does not
// classify (or lacks ones it does).
func (s *Service) Preflight(ctx context.Context, opts Options) (Preflight, error) {
	conn, err := s.verifyTarget(ctx)
	if err != nil {
		return Preflight{}, err
	}
	if err := s.checkSchema(ctx); err != nil {
		return Preflight{}, err
	}
	counts, err := s.counts(ctx)
	if err != nil {
		return Preflight{}, err
	}
	plan, err := s.plan(ctx, opts)
	if err != nil {
		return Preflight{}, err
	}
	return Preflight{Target: s.target, Connection: conn, Counts: counts, Storage: plan}, nil
}

// Execute performs the wipe: it re-verifies the target, checks the typed
// confirmations, empties the store, then truncates every catalogue table and
// writes the audit entry in one transaction, and finally re-counts every table so
// the caller can print a before/after summary.
//
// before is the preflight's counts, carried into the audit entry and the summary.
//
// It returns ErrNotExecuting without Options.Execute, ErrConfirmationMismatch or
// ErrBucketConfirmationMismatch when a typed name is wrong (the database's and,
// on a bucket-backed store, the bucket's), ErrTargetMismatch when the connection
// moved, and ErrStorageIncomplete when an object could not be deleted — in that
// last case the catalogue is deliberately left intact and the run can simply be
// repeated.
func (s *Service) Execute(ctx context.Context, opts Options, before Counts) (Result, error) {
	if !opts.Execute {
		return Result{}, ErrNotExecuting
	}
	if _, err := s.verifyTarget(ctx); err != nil {
		return Result{}, err
	}
	// Re-checked rather than trusted from the preflight: every invariant this
	// package documents is enforced on the path that actually deletes, so a caller
	// that skipped the preflight cannot skip a guard with it.
	if err := s.checkSchema(ctx); err != nil {
		return Result{}, err
	}
	if err := s.checkConfirmation(opts); err != nil {
		return Result{}, err
	}

	stored, err := s.wipeStorage(ctx, opts)
	result := Result{Before: before, Storage: stored}
	if err != nil {
		return result, err
	}
	if stored.Failed > 0 {
		return result, fmt.Errorf("%w: %d object(s) failed", ErrStorageIncomplete, stored.Failed)
	}
	if err := s.truncate(ctx, opts, before, stored); err != nil {
		return result, err
	}
	after, err := s.counts(ctx)
	if err != nil {
		return result, err
	}
	result.After = after
	return result, nil
}

// checkConfirmation compares what the operator typed against what the config
// names — the database, and the bucket when the store has one. Both are exact
// comparisons, so a runbook line copied from another deployment is refused rather
// than accepted with a shrug: that is the whole point of typing them.
//
// The bucket is checked even when there is none to wipe. A typed bucket name
// against a filesystem store means the operator believed they were emptying a
// bucket, and a wipe that proceeds on that belief is the misfire this guard is
// here to stop.
func (s *Service) checkConfirmation(opts Options) error {
	if opts.Confirm != s.target.Database {
		return fmt.Errorf("%w: typed %q, target %q", ErrConfirmationMismatch, opts.Confirm, s.target.Database)
	}
	if opts.ConfirmBucket != s.target.Bucket {
		return fmt.Errorf("%w: typed %q, configured %q",
			ErrBucketConfirmationMismatch, opts.ConfirmBucket, s.target.Bucket)
	}
	return nil
}

// verifyTarget asks the server which database this pool is actually connected to
// and refuses when it is not the configured one. The check is deliberately made
// against the server rather than by re-reading the DSN: a DSN can name a database
// it never reached (a socket default, a pooler rewriting the target), and the
// only authority on what would be truncated is the connection itself.
func (s *Service) verifyTarget(ctx context.Context) (Connection, error) {
	const query = `SELECT current_database(),
		coalesce(host(inet_server_addr()), ''), coalesce(inet_server_port(), 0)`
	var conn Connection
	if err := s.pool.QueryRow(ctx, query).Scan(&conn.Database, &conn.ServerAddr, &conn.ServerPort); err != nil {
		return Connection{}, fmt.Errorf("reset: identifying the connected database: %w", err)
	}
	if conn.Database != s.target.Database {
		return conn, fmt.Errorf("%w: connected to %q, the config names %q",
			ErrTargetMismatch, conn.Database, s.target.Database)
	}
	return conn, nil
}

// checkSchema reads the live table set and refuses when it is not the one this
// package classifies, so a migration that adds a table cannot leave part of the
// library behind on every reset from then on.
func (s *Service) checkSchema(ctx context.Context) error {
	tables, err := listPublicTables(ctx, s.pool)
	if err != nil {
		return err
	}
	return classifySchema(tables)
}

// counts measures the row count of every classified table, on both sides.
func (s *Service) counts(ctx context.Context) (Counts, error) {
	catalogue, err := countTables(ctx, s.pool, catalogueTables)
	if err != nil {
		return Counts{}, err
	}
	preserved, err := countTables(ctx, s.pool, preservedTables)
	if err != nil {
		return Counts{}, err
	}
	return Counts{Catalogue: catalogue, Preserved: preserved}, nil
}

// plan counts the objects a run would delete: always the keys the catalogue
// references, and — under an orphan sweep — what the store actually holds under
// each owned prefix, plus how many foreign keys it holds that will be left alone.
func (s *Service) plan(ctx context.Context, opts Options) (StoragePlan, error) {
	files, err := s.catalogueFiles(ctx)
	if err != nil {
		return StoragePlan{}, err
	}
	_, referenced := files.objectKeys()
	plan := StoragePlan{Referenced: referenced, Sweep: opts.OrphanSweep}
	if !opts.OrphanSweep {
		return plan, nil
	}
	lister, ok := s.store.(storage.KeyLister)
	if !ok {
		return StoragePlan{}, ErrSweepUnsupported
	}
	_, stored, foreign, err := sweepKeys(ctx, lister)
	if err != nil {
		return StoragePlan{}, err
	}
	plan.Stored = stored
	plan.Foreign = foreign
	return plan, nil
}

// catalogueFiles reads the storage key of every file the catalogue references and
// the content hash every thumbnail set is keyed by, from both the photo rows and
// the per-file rows — a stacked RAW sibling lives only in the second.
func (s *Service) catalogueFiles(ctx context.Context) (catalogueFiles, error) {
	const pathQuery = `SELECT file_path FROM photos WHERE file_path <> ''
		UNION SELECT file_path FROM photo_files WHERE file_path <> ''`
	const hashQuery = `SELECT file_hash FROM photos WHERE file_hash <> ''
		UNION SELECT file_hash FROM photo_files WHERE file_hash <> ''`

	paths, err := queryStrings(ctx, s.pool, pathQuery)
	if err != nil {
		return catalogueFiles{}, fmt.Errorf("reset: reading catalogued file paths: %w", err)
	}
	hashes, err := queryStrings(ctx, s.pool, hashQuery)
	if err != nil {
		return catalogueFiles{}, fmt.Errorf("reset: reading catalogued file hashes: %w", err)
	}
	return catalogueFiles{paths: paths, hashes: hashes}, nil
}

// queryStrings runs a single-column query and collects its values.
func queryStrings(ctx context.Context, pool *pgxpool.Pool, query string) ([]string, error) {
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scanning: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating: %w", err)
	}
	return values, nil
}

// wipeStorage empties the store: the keys the catalogue references, or — under an
// orphan sweep — every object under an owned prefix, referenced or not. The
// locally cached thumbnails go with them.
func (s *Service) wipeStorage(ctx context.Context, opts Options) (StorageResult, error) {
	files, err := s.catalogueFiles(ctx)
	if err != nil {
		return StorageResult{}, err
	}
	keys, _ := files.objectKeys()
	var foreign int
	if opts.OrphanSweep {
		// The sweep supersedes the catalogue's candidates: it is the set of objects
		// that actually exist under the owned prefixes, so it covers every
		// referenced key that is really there and skips the ones that never were.
		if keys, foreign, err = s.sweptKeys(ctx); err != nil {
			return StorageResult{}, err
		}
	}
	result, err := deleteKeys(ctx, s.store, keys, opts.concurrency())
	result.Foreign = foreign
	if err != nil {
		return result, err
	}
	if err := s.clearThumbCache(files.hashes, opts, &result); err != nil {
		return result, err
	}
	return result, nil
}

// sweptKeys lists the store and returns the owned keys plus the number of foreign
// ones left alone, or ErrSweepUnsupported when the store cannot be listed.
func (s *Service) sweptKeys(ctx context.Context) ([]string, int, error) {
	lister, ok := s.store.(storage.KeyLister)
	if !ok {
		return nil, 0, ErrSweepUnsupported
	}
	keys, _, foreign, err := sweepKeys(ctx, lister)
	if err != nil {
		return nil, 0, err
	}
	return keys, foreign, nil
}

// clearThumbCache drops the local derived-image cache: the whole thumbnail
// directory (and the video storyboard sprites beside it) under an orphan sweep,
// otherwise the cached sizes of every catalogued hash. A cache entry addressed by
// a hash no photo has any more is harmless but occupies disk that the re-import
// wants back.
func (s *Service) clearThumbCache(hashes []string, opts Options, result *StorageResult) error {
	if opts.OrphanSweep && s.cacheDir != "" {
		for _, subdir := range []string{thumb.CacheSubdir, storyboard.CacheSubdir} {
			dir := filepath.Join(s.cacheDir, subdir)
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("reset: removing the derived-media cache %s: %w", dir, err)
			}
		}
		result.ThumbCacheSwept = true
		return nil
	}
	if s.thumbs == nil {
		return nil
	}
	for _, hash := range hashes {
		if err := s.thumbs.Remove(hash); err != nil {
			if errors.Is(err, thumb.ErrInvalidHash) {
				continue
			}
			return fmt.Errorf("reset: removing cached thumbnails for %s: %w", hash, err)
		}
		result.ThumbCacheCleared++
	}
	return nil
}

// The wipe's one piece of surgery, in three statements around the truncation.
//
// users.subject_uid is the only pointer a preserved table holds into the library:
// the person an account says it is. Postgres refuses to TRUNCATE a table that an
// unlisted table references — structurally, whatever the rows say — so that one
// foreign key would make the whole wipe fail, and TRUNCATE … CASCADE, which would
// take the accounts with it, is the one escape this command must never use.
//
// So the constraint is dropped, the column is nulled, the catalogue is truncated
// and the constraint goes straight back, all inside the transaction that also
// writes the audit entry. Postgres makes DDL transactional, so a failure anywhere
// in that sequence rolls the constraint back into place along with everything
// else; there is no window in which the database is left without it.
//
// Nulling the column is also what the truth requires rather than a way round the
// error: the person that link named is about to stop existing. Everything that
// makes an account an account — credentials, role, note, its history — survives
// untouched.
//
// The constraint is named in migration 0060; renaming it there without renaming
// it here breaks the reset.
const (
	// dropSubjectFKSQL removes the foreign key for the duration of the wipe.
	dropSubjectFKSQL = `ALTER TABLE users DROP CONSTRAINT users_subject_uid_fkey`
	// clearUserSubjectsSQL unlinks every account from the person it named.
	clearUserSubjectsSQL = `UPDATE users SET subject_uid = NULL WHERE subject_uid IS NOT NULL`
	// restoreSubjectFKSQL puts the foreign key back, against the now-empty table.
	restoreSubjectFKSQL = `ALTER TABLE users ADD CONSTRAINT users_subject_uid_fkey
		FOREIGN KEY (subject_uid) REFERENCES subjects (uid) ON DELETE SET NULL`
)

// unlinkAccounts clears the accounts' links to people and lifts the foreign key
// that would otherwise refuse the truncation, on tx so it commits or rolls back
// with the wipe itself.
func unlinkAccounts(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, dropSubjectFKSQL); err != nil {
		return fmt.Errorf("reset: lifting the accounts' link constraint: %w", err)
	}
	if _, err := tx.Exec(ctx, clearUserSubjectsSQL); err != nil {
		return fmt.Errorf("reset: clearing the accounts' linked people: %w", err)
	}
	return nil
}

// truncate empties every catalogue table and writes the audit entry in the same
// transaction, so the wipe and its record commit together or not at all.
//
// The truncation runs without CASCADE on purpose. Every foreign key between the
// catalogue tables is inside the list, so the statement succeeds as written; if a
// future migration adds a table that references one of them without being
// classified here, Postgres refuses the statement instead of quietly extending
// the blast radius. The single reference from outside the list is cleared first —
// see clearUserSubjectsSQL.
func (s *Service) truncate(ctx context.Context, opts Options, before Counts, stored StorageResult) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reset: beginning the truncation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := unlinkAccounts(ctx, tx); err != nil {
		return err
	}
	quoted := make([]string, 0, len(catalogueTables))
	for _, name := range catalogueTables {
		quoted = append(quoted, pgx.Identifier{name}.Sanitize())
	}
	if _, err := tx.Exec(ctx, "TRUNCATE TABLE "+strings.Join(quoted, ", ")+" RESTART IDENTITY"); err != nil {
		return fmt.Errorf("reset: truncating the catalogue: %w", err)
	}
	if _, err := tx.Exec(ctx, restoreSubjectFKSQL); err != nil {
		return fmt.Errorf("reset: restoring the accounts' link constraint: %w", err)
	}
	if err := audit.Write(ctx, tx, s.auditEntry(opts, before, stored)); err != nil {
		return fmt.Errorf("reset: recording the wipe: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reset: committing the truncation: %w", err)
	}
	return nil
}

// auditEntry builds the audit record of the wipe: who ran it, against which
// database, how many rows each table lost and what happened in the store.
func (s *Service) auditEntry(opts Options, before Counts, stored StorageResult) audit.Entry {
	rows := make(map[string]any, len(before.Catalogue))
	for _, table := range before.Catalogue {
		if table.Rows > 0 {
			rows[table.Table] = table.Rows
		}
	}
	return audit.Entry{
		ActorUID:   opts.ActorUID,
		Action:     audit.ActionLibraryReset,
		TargetType: auditTargetType,
		Details: map[string]any{
			"operator":        opts.Operator,
			"host":            s.target.Host,
			"database":        s.target.Database,
			"bucket":          s.target.Bucket,
			"orphan_sweep":    opts.OrphanSweep,
			"rows_deleted":    before.Rows(),
			"rows_by_table":   rows,
			"objects_deleted": stored.Deleted,
			"objects_missing": stored.Missing,
			"objects_foreign": stored.Foreign,
		},
	}
}
