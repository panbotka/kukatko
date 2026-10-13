package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// The per-project wipe: `kukatko project purge <slug>`.
//
// Where Service empties every library of the instance and keeps the projects,
// a Purger removes one project outright — its catalogue rows, its memberships
// and invitations, the project row itself, and everything under its storage
// prefix — and leaves every other project exactly as it was. It shares the
// reset's table classification (catalogueTables), so a table a migration adds is
// classified once and both wipes know about it, and the reset's per-object
// deletion, so a store failure is counted the same way.
//
// The guards are the reset's in miniature: nothing happens without Execute and
// the slug typed again; the bootstrap project is refused unless explicitly
// allowed; and a project whose storage prefix is empty is refused outright,
// because "everything under the prefix" would then be the whole store.

// Sentinel errors a purge ends on, matchable with errors.Is.
var (
	// ErrPurgeBootstrap is returned when the bootstrap project is named without
	// AllowBootstrap: it holds the library everything before multi-tenancy
	// belonged to, and removing it is never a side effect of a typo.
	ErrPurgeBootstrap = errors.New("reset: refusing to purge the bootstrap project without --allow-bootstrap")
	// ErrPurgeRootPrefix is returned for a project whose storage prefix is
	// empty: it lives in the root layout, so deleting "its prefix" would delete
	// the whole store, every other project's included.
	ErrPurgeRootPrefix = errors.New(
		"reset: refusing to purge a project with an empty storage prefix (it lives in the store root)")
	// ErrPurgeNotExecuted is returned by Execute when the options do not ask for
	// execution: a dry run must never reach the deleting half.
	ErrPurgeNotExecuted = errors.New("reset: purge not executed; it is a dry run without --yes")
	// ErrPurgeNotConfirmed is returned when the slug typed to confirm does not
	// match the project being purged.
	ErrPurgeNotConfirmed = errors.New("reset: the confirmation does not match the project's slug; nothing was deleted")
)

// jobsTable is the job queue, the one catalogue table whose rows name their
// project inside the payload rather than in a column.
const jobsTable = "jobs"

// purgeJobsWhere selects the queued, failed and finished jobs of one project:
// the library-wide ones by the project in their payload, the per-photo,
// per-album and per-subject ones by what they are about. A running job is left
// alone — the worker holds it — and fails on its own once its row is gone.
const purgeJobsWhere = `state <> 'running' AND (
	payload ->> 'project_uid' = $1
	OR payload ->> 'photo_uid' IN (SELECT uid FROM photos WHERE project_id = $1)
	OR payload ->> 'album_uid' IN (SELECT uid FROM albums WHERE project_id = $1)
	OR payload ->> 'subject_uid' IN (SELECT uid FROM subjects WHERE project_id = $1))`

// PurgeStore is the subset of a storage backend a purge needs: deleting one
// object, and listing every object with its size so the plan can say how much
// lies under the project's prefix. Both backends satisfy it.
type PurgeStore interface {
	ObjectStore
	storage.SizedKeyLister
}

// PurgeConfig assembles a Purger. Pool and Store are required; CacheDir (the
// local derived-artefact cache, storage.cache_path) is optional — when set, the
// project's tree inside it is removed too.
type PurgeConfig struct {
	Pool        *pgxpool.Pool
	Store       PurgeStore
	CacheDir    string
	Concurrency int
}

// Purger removes one project and everything it owns.
type Purger struct {
	pool        *pgxpool.Pool
	store       PurgeStore
	cacheDir    string
	concurrency int
	now         func() time.Time
}

// NewPurger returns a Purger over cfg. It panics when Pool or Store is nil: a
// purge without either cannot do what it says.
func NewPurger(cfg PurgeConfig) *Purger {
	if cfg.Pool == nil || cfg.Store == nil {
		panic("reset: NewPurger needs a Pool and a Store")
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	return &Purger{
		pool: cfg.Pool, store: cfg.Store, cacheDir: cfg.CacheDir, concurrency: concurrency, now: time.Now,
	}
}

// PurgeOptions are the switches and guards of one purge.
type PurgeOptions struct {
	// Slug names the project to purge.
	Slug string
	// Execute asks for the deletion; without it only Plan may run.
	Execute bool
	// Confirm is the slug typed again; it must equal the project's slug.
	Confirm string
	// AllowBootstrap lifts the refusal of the bootstrap project. The empty-prefix
	// refusal still applies to it.
	AllowBootstrap bool
	// ActorUID is the account the audit entry names, or "" for the CLI.
	ActorUID string
	// Operator is who ran the command (user@host), recorded in the audit entry.
	Operator string
}

// PurgeProject is the project a purge is about, as it was read before anything
// was deleted.
type PurgeProject struct {
	UID           string `json:"uid"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	StoragePrefix string `json:"storage_prefix"`
}

// PurgePlan is what a purge would delete: the rows per table, and the objects
// under the project's storage prefix with their total size.
type PurgePlan struct {
	// Project is the project to purge.
	Project PurgeProject `json:"project"`
	// Tables are the row counts of the tables the purge deletes from, in the
	// order it deletes them: the catalogue tables that carry project_id, the job
	// queue, then project_members and project_invites. Rows of the tables below
	// them (files, markers, album members, comments, …) go with their parent row
	// through ON DELETE CASCADE and are not counted separately.
	Tables []TableCount `json:"tables"`
	// Objects is the number of objects under the storage prefix.
	Objects int `json:"objects"`
	// Bytes is their total size.
	Bytes int64 `json:"bytes"`
}

// Rows returns the total of every table count in the plan.
func (p PurgePlan) Rows() int64 {
	var total int64
	for _, table := range p.Tables {
		total += table.Rows
	}
	return total
}

// PurgeResult is what a purge did.
type PurgeResult struct {
	// Plan is what was counted before the deletion, which is what was deleted.
	Plan PurgePlan `json:"plan"`
	// PurgedAt is when the database half committed.
	PurgedAt time.Time `json:"purged_at"`
	// Storage reports the deletion of the objects under the prefix.
	Storage StorageResult `json:"storage"`
	// CacheRemoved reports that the project's tree in the local cache was removed.
	CacheRemoved bool `json:"cache_removed"`
}

// Plan resolves the project named by opts.Slug, applies the refusals and counts
// what a purge would delete. It is read-only.
func (p *Purger) Plan(ctx context.Context, opts PurgeOptions) (PurgePlan, error) {
	target, err := p.target(ctx, opts)
	if err != nil {
		return PurgePlan{}, err
	}
	tables, err := p.purgeTables(ctx)
	if err != nil {
		return PurgePlan{}, err
	}
	counts, err := p.countRows(ctx, target.UID, tables)
	if err != nil {
		return PurgePlan{}, err
	}
	objects, bytes, err := p.measurePrefix(ctx, target.StoragePrefix)
	if err != nil {
		return PurgePlan{}, err
	}
	return PurgePlan{Project: target, Tables: counts, Objects: objects, Bytes: bytes}, nil
}

// Execute deletes the project of plan: every row of it in one transaction with
// the audit entry, then everything under its storage prefix and its tree in the
// local cache. It refuses unless opts asks for execution and confirms the slug.
//
// The database goes first. Once it has committed nothing points at the objects
// any more, and an object the store fails to delete is an orphan project's
// object that the maintenance scan reports — whereas deleting the objects first
// and then failing the transaction would leave a catalogue whose every original
// is gone.
func (p *Purger) Execute(ctx context.Context, opts PurgeOptions, plan PurgePlan) (PurgeResult, error) {
	result := PurgeResult{Plan: plan}
	if !opts.Execute {
		return result, ErrPurgeNotExecuted
	}
	if opts.Confirm != plan.Project.Slug || plan.Project.Slug == "" {
		return result, ErrPurgeNotConfirmed
	}
	target, err := p.target(ctx, opts)
	if err != nil {
		return result, err
	}
	if target.UID != plan.Project.UID {
		return result, fmt.Errorf("%w: the slug names a different project than the plan", ErrPurgeNotConfirmed)
	}
	if err := p.deleteRows(ctx, opts, plan); err != nil {
		return result, err
	}
	result.PurgedAt = p.now().UTC()
	stored, err := p.deletePrefix(ctx, target.StoragePrefix)
	result.Storage = stored
	if err != nil {
		return result, err
	}
	result.CacheRemoved, err = p.removeCache(target.StoragePrefix)
	return result, err
}

// target resolves the project opts names and applies the two refusals.
func (p *Purger) target(ctx context.Context, opts PurgeOptions) (PurgeProject, error) {
	found, err := project.NewStore(p.pool).GetBySlug(ctx, opts.Slug)
	if err != nil {
		return PurgeProject{}, fmt.Errorf("reset: resolving project %q: %w", opts.Slug, err)
	}
	if found.UID == project.BootstrapUID && !opts.AllowBootstrap {
		return PurgeProject{}, ErrPurgeBootstrap
	}
	if found.StoragePrefix == "" {
		return PurgeProject{}, ErrPurgeRootPrefix
	}
	if err := storekeys.ValidatePrefix(found.StoragePrefix); err != nil {
		return PurgeProject{}, fmt.Errorf("reset: project %q: %w", found.Slug, err)
	}
	return PurgeProject{
		UID: found.UID, Slug: found.Slug, Name: found.Name, StoragePrefix: found.StoragePrefix,
	}, nil
}

// purgeTables returns the catalogue tables that carry a project_id column, in
// the order a purge deletes from them: every table before the tables it
// references, so no foreign key ever has to null or cascade into a row the
// purge deletes next anyway.
//
// The list is the reset's own (catalogueTables), filtered against the live
// schema: a migration that gives a catalogue table a project_id makes the purge
// delete from it with no change here, and a table nobody classified fails the
// schema check before anything is deleted.
func (p *Purger) purgeTables(ctx context.Context) ([]string, error) {
	present, err := listPublicTables(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	if err := classifySchema(present); err != nil {
		return nil, err
	}
	scoped, err := queryStrings(ctx, p.pool, `SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'project_id' ORDER BY table_name`)
	if err != nil {
		return nil, fmt.Errorf("reset: listing project-scoped tables: %w", err)
	}
	tables := make([]string, 0, len(scoped))
	for _, name := range scoped {
		if slices.Contains(catalogueTables, name) {
			tables = append(tables, name)
		}
	}
	edges, err := p.foreignKeys(ctx)
	if err != nil {
		return nil, err
	}
	return referencingFirst(tables, edges), nil
}

// foreignKey is one foreign-key edge between two tables of the public schema.
type foreignKey struct {
	from string
	to   string
}

// foreignKeys returns every foreign-key edge of the public schema.
func (p *Purger) foreignKeys(ctx context.Context) ([]foreignKey, error) {
	rows, err := p.pool.Query(ctx, `SELECT src.relname, dst.relname
		FROM pg_constraint c
		JOIN pg_class src ON src.oid = c.conrelid
		JOIN pg_class dst ON dst.oid = c.confrelid
		JOIN pg_namespace n ON n.oid = src.relnamespace
		WHERE c.contype = 'f' AND n.nspname = 'public'`)
	if err != nil {
		return nil, fmt.Errorf("reset: listing foreign keys: %w", err)
	}
	defer rows.Close()
	var edges []foreignKey
	for rows.Next() {
		var edge foreignKey
		if err := rows.Scan(&edge.from, &edge.to); err != nil {
			return nil, fmt.Errorf("reset: scanning a foreign key: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reset: iterating foreign keys: %w", err)
	}
	return edges, nil
}

// referencingFirst orders tables so every table comes before each table it
// references (a topological order of the foreign keys among them), breaking ties
// alphabetically for a stable plan. Self-references and edges leaving the set are
// ignored. Should the keys ever form a cycle, the tables on it follow in
// alphabetical order — every key between catalogue tables cascades or nulls, so
// the deletion still succeeds, it merely does one update too many.
func referencingFirst(tables []string, edges []foreignKey) []string {
	inSet := make(map[string]bool, len(tables))
	for _, name := range tables {
		inSet[name] = true
	}
	// blockers[t] counts the tables that reference t and are not yet placed.
	blockers := make(map[string]int, len(tables))
	referencedBy := make(map[string][]string, len(tables))
	for _, edge := range edges {
		if edge.from == edge.to || !inSet[edge.from] || !inSet[edge.to] {
			continue
		}
		blockers[edge.to]++
		referencedBy[edge.from] = append(referencedBy[edge.from], edge.to)
	}
	remaining := slices.Clone(tables)
	slices.Sort(remaining)
	ordered := make([]string, 0, len(tables))
	for len(remaining) > 0 {
		// No unblocked table left means a cycle: take the alphabetically first
		// (IndexFunc's -1 becomes 0) and carry on.
		next := max(slices.IndexFunc(remaining, func(name string) bool { return blockers[name] == 0 }), 0)
		name := remaining[next]
		remaining = slices.Delete(remaining, next, next+1)
		ordered = append(ordered, name)
		for _, referenced := range referencedBy[name] {
			blockers[referenced]--
		}
	}
	return ordered
}

// countRows counts the rows of projectUID in every table the purge deletes from,
// in deletion order.
func (p *Purger) countRows(ctx context.Context, projectUID string, tables []string) ([]TableCount, error) {
	counts := make([]TableCount, 0, len(tables)+3)
	for _, statement := range purgeStatements(tables) {
		var rows int64
		query := "SELECT count(*) FROM " + pgx.Identifier{statement.table}.Sanitize() + " WHERE " + statement.where
		if err := p.pool.QueryRow(ctx, query, projectUID).Scan(&rows); err != nil {
			return nil, fmt.Errorf("reset: counting %s rows of the project: %w", statement.table, err)
		}
		counts = append(counts, TableCount{Table: statement.table, Rows: rows})
	}
	return counts, nil
}

// purgeStatement is one table a purge deletes from and the condition selecting
// the project's rows, with the project uid as $1.
type purgeStatement struct {
	table string
	where string
}

// purgeStatements returns the deletions of a purge in order: the job queue
// first (its conditions read the photos, albums and subjects about to go), then
// the project-scoped catalogue tables, then the memberships and the invitations.
// The project row itself follows them, outside this list.
func purgeStatements(tables []string) []purgeStatement {
	statements := []purgeStatement{{table: jobsTable, where: purgeJobsWhere}}
	for _, name := range tables {
		statements = append(statements, purgeStatement{table: name, where: "project_id = $1"})
	}
	return append(statements,
		purgeStatement{table: "project_members", where: "project_uid = $1"},
		purgeStatement{table: "project_invites", where: "project_uid = $1"},
	)
}

// deleteRows deletes the project's rows and the project itself in one
// transaction with the audit entry, so the purge and its record commit together
// or not at all.
func (p *Purger) deleteRows(ctx context.Context, opts PurgeOptions, plan PurgePlan) error {
	tables, err := p.purgeTables(ctx)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reset: beginning the purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, statement := range purgeStatements(tables) {
		query := "DELETE FROM " + pgx.Identifier{statement.table}.Sanitize() + " WHERE " + statement.where
		if _, err := tx.Exec(ctx, query, plan.Project.UID); err != nil {
			return fmt.Errorf("reset: deleting the project's %s rows: %w", statement.table, err)
		}
	}
	tag, err := tx.Exec(ctx, `DELETE FROM projects WHERE uid = $1`, plan.Project.UID)
	if err != nil {
		return fmt.Errorf("reset: deleting the project row: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("reset: project %s vanished during the purge: %w", plan.Project.Slug, project.ErrNotFound)
	}
	if err := audit.Write(ctx, tx, purgeAuditEntry(opts, plan)); err != nil {
		return fmt.Errorf("reset: recording the purge: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reset: committing the purge: %w", err)
	}
	return nil
}

// purgeAuditEntry builds the audit record of a purge: who ran it, which project
// it removed and what it counted. The entry carries the purged project's uid, so
// the trail of a project ends with the record of its removal.
func purgeAuditEntry(opts PurgeOptions, plan PurgePlan) audit.Entry {
	rows := make(map[string]any, len(plan.Tables))
	for _, table := range plan.Tables {
		if table.Rows > 0 {
			rows[table.Table] = table.Rows
		}
	}
	return audit.Entry{
		ActorUID:   opts.ActorUID,
		Action:     audit.ActionProjectPurge,
		TargetType: project.TargetType,
		TargetUID:  plan.Project.UID,
		ProjectUID: plan.Project.UID,
		Details: map[string]any{
			"operator":       opts.Operator,
			"slug":           plan.Project.Slug,
			"name":           plan.Project.Name,
			"storage_prefix": plan.Project.StoragePrefix,
			"rows_deleted":   plan.Rows(),
			"rows_by_table":  rows,
			"objects":        plan.Objects,
			"bytes":          plan.Bytes,
		},
	}
}

// measurePrefix counts the objects under prefix and their total size, in one
// streamed listing of the store.
func (p *Purger) measurePrefix(ctx context.Context, prefix string) (int, int64, error) {
	var (
		objects int
		bytes   int64
	)
	err := p.store.KeySizes(ctx, func(key string, size int64) error {
		if strings.HasPrefix(key, prefix) {
			objects++
			bytes += size
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("reset: listing the store under %s: %w", prefix, err)
	}
	return objects, bytes, nil
}

// deletePrefix deletes every object under prefix. Everything there is the
// project's: the prefix is projects/<uid>/ with the uid of a project row, a tree
// no other project and nothing else writes into.
func (p *Purger) deletePrefix(ctx context.Context, prefix string) (StorageResult, error) {
	var keys []string
	err := p.store.KeySizes(ctx, func(key string, _ int64) error {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return StorageResult{}, fmt.Errorf("reset: listing the store under %s: %w", prefix, err)
	}
	return deleteKeys(ctx, p.store, keys, p.concurrency)
}

// removeCache removes the project's tree from the local derived-artefact cache
// (its thumbnails, storyboards and avatars), reporting whether there was a cache
// to clear. The prefix was validated as projects/<uid>/, so the path stays
// inside the cache directory.
func (p *Purger) removeCache(prefix string) (bool, error) {
	if p.cacheDir == "" {
		return false, nil
	}
	dir := filepath.Join(p.cacheDir, filepath.FromSlash(strings.TrimSuffix(prefix, "/")))
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("reset: removing the project's cache %s: %w", dir, err)
	}
	return true, nil
}
