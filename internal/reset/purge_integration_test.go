//go:build integration

package reset_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/reset"
	"github.com/panbotka/kukatko/internal/storage"
)

// purgeEnv is two seeded projects — A the bootstrap project in the root layout,
// B a created one under projects/<uid>/ — over a real test database, a
// filesystem store holding objects of both, and a local cache holding a tree of
// both.
type purgeEnv struct {
	db       *database.DB
	f        *projecttest.Fixture
	root     string
	cacheDir string
	purger   *reset.Purger
}

// newPurgeEnv seeds the two projects, an object or two of each layout, a queued
// job of each project and a cached thumbnail of each.
func newPurgeEnv(t *testing.T) *purgeEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())

	root, cacheDir := t.TempDir(), t.TempDir()
	store, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	env := &purgeEnv{db: db, f: f, root: root, cacheDir: cacheDir}
	for key, content := range map[string]string{
		"2026/10/a.jpg":                                  "photo of A",
		"families.yaml":                                  "families of A",
		f.B.StoragePrefix + "2026/10/b.jpg":              "photo of B",
		f.B.StoragePrefix + "sidecars/2026/10/b.jpg.yml": "sidecar of B",
		f.B.StoragePrefix + "families.yaml":              "families of B",
	} {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(key)), content)
	}
	writeTestFile(t, filepath.Join(cacheDir, "thumb", "aa", "a_tile_224.jpg"), "thumb of A")
	writeTestFile(t, filepath.Join(cacheDir, filepath.FromSlash(f.B.StoragePrefix), "thumb", "bb", "b_tile_224.jpg"),
		"thumb of B")

	enqueuer := jobs.NewEnqueuer(jobs.NewStore(db.Pool()))
	for _, tenant := range []*projecttest.Tenant{&f.A, &f.B} {
		if err := enqueuer.EnqueueThumbnail(t.Context(), tenant.PhotoUID); err != nil {
			t.Fatalf("enqueueing a thumbnail job: %v", err)
		}
		if err := enqueuer.EnqueueFamilyExport(t.Context(), tenant.ProjectUID); err != nil {
			t.Fatalf("enqueueing a family export: %v", err)
		}
	}
	env.purger = reset.NewPurger(reset.PurgeConfig{Pool: db.Pool(), Store: store, CacheDir: cacheDir})
	return env
}

// writeTestFile writes content at absPath, creating the parent directories.
func writeTestFile(t *testing.T, absPath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}
}

// rowsOf counts the rows of table matching where, with arg as $1.
func (e *purgeEnv) rowsOf(t *testing.T, table, where string, arg any) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE "+where, arg).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// storedKeys lists every file under the store root as a slash-separated key.
func (e *purgeEnv) storedKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(e.root, func(abs string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(e.root, abs)
		keys = append(keys, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatalf("walking the store: %v", err)
	}
	slices.Sort(keys)
	return keys
}

// tableRows returns the count plan holds for table, or -1 when it holds none.
func tableRows(plan reset.PurgePlan, table string) int64 {
	for _, count := range plan.Tables {
		if count.Table == table {
			return count.Rows
		}
	}
	return -1
}

// TestPurge_dryRunCountsAndDeletesNothing verifies the plan counts the
// project's rows per table and its objects and bytes under its prefix, and that
// nothing is deleted by planning — nor by an Execute that was not asked for.
func TestPurge_dryRunCountsAndDeletesNothing(t *testing.T) {
	env := newPurgeEnv(t)
	opts := reset.PurgeOptions{Slug: env.f.B.Slug}

	plan, err := env.purger.Plan(t.Context(), opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Project.UID != env.f.B.ProjectUID || plan.Project.StoragePrefix != env.f.B.StoragePrefix {
		t.Errorf("plan project = %+v, want B", plan.Project)
	}
	for table, want := range map[string]int64{
		"photos": 1, "subjects": 1, "albums": 1, "labels": 1, "photo_tasks": 1, "saved_searches": 1,
		"upload_links": 1, "jobs": 2, "project_members": 1, "project_invites": 0,
	} {
		if got := tableRows(plan, table); got != want {
			t.Errorf("plan %s = %d, want %d", table, got, want)
		}
	}
	wantBytes := int64(len("photo of B") + len("sidecar of B") + len("families of B"))
	if plan.Objects != 3 || plan.Bytes != wantBytes {
		t.Errorf("plan objects = %d (%d B), want 3 (%d B)", plan.Objects, plan.Bytes, wantBytes)
	}
	// The job queue is deleted first — its conditions read the rows after it.
	if plan.Tables[0].Table != "jobs" || plan.Tables[len(plan.Tables)-1].Table != "project_invites" {
		t.Errorf("deletion order = %+v, want jobs first and project_invites last", plan.Tables)
	}

	if _, err := env.purger.Execute(t.Context(), opts, plan); !errors.Is(err, reset.ErrPurgeNotExecuted) {
		t.Errorf("Execute without Execute = %v, want ErrPurgeNotExecuted", err)
	}
	if got := env.rowsOf(t, "projects", "uid = $1", env.f.B.ProjectUID); got != 1 {
		t.Errorf("project B rows after a dry run = %d, want 1", got)
	}
	if got := len(env.storedKeys(t)); got != 5 {
		t.Errorf("store holds %d objects after a dry run, want 5", got)
	}
}

// TestPurge_executeRemovesBAndLeavesAIntact purges B and checks every trace of
// it is gone — rows, memberships, jobs, the project row, its objects and its
// cache tree — while A's rows, objects and cache are exactly as they were, and
// the purge left its record in the audit trail.
func TestPurge_executeRemovesBAndLeavesAIntact(t *testing.T) {
	env := newPurgeEnv(t)
	opts := reset.PurgeOptions{Slug: env.f.B.Slug, Execute: true, Confirm: env.f.B.Slug, Operator: "test@host"}
	plan, err := env.purger.Plan(t.Context(), opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := env.purger.Execute(t.Context(), opts, plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Storage.Deleted != 3 || result.Storage.Failed != 0 || !result.CacheRemoved {
		t.Errorf("result = %+v, want 3 objects deleted and the cache tree removed", result)
	}

	for _, check := range []struct{ table, where string }{
		{"projects", "uid = $1"}, {"photos", "project_id = $1"}, {"subjects", "project_id = $1"},
		{"albums", "project_id = $1"}, {"project_members", "project_uid = $1"},
		{"jobs", "payload ->> 'project_uid' = $1"},
	} {
		if got := env.rowsOf(t, check.table, check.where, env.f.B.ProjectUID); got != 0 {
			t.Errorf("B's %s rows after the purge = %d, want 0", check.table, got)
		}
		if got := env.rowsOf(t, check.table, check.where, env.f.A.ProjectUID); got == 0 {
			t.Errorf("A's %s rows after the purge = 0, want them untouched", check.table)
		}
	}
	if got := env.rowsOf(t, "jobs", "payload ->> 'photo_uid' = $1", env.f.B.PhotoUID); got != 0 {
		t.Errorf("B's photo jobs after the purge = %d, want 0", got)
	}
	if got := env.rowsOf(t, "jobs", "payload ->> 'photo_uid' = $1", env.f.A.PhotoUID); got != 1 {
		t.Errorf("A's photo jobs after the purge = %d, want 1", got)
	}
	if got := env.rowsOf(t, "users", "uid = $1", env.f.B.UserUID); got != 1 {
		t.Errorf("B's member account after the purge = %d rows, want 1 (accounts are kept)", got)
	}
	if got := env.rowsOf(t, "audit_log", "action = $1", audit.ActionProjectPurge); got != 1 {
		t.Errorf("project.purge audit entries = %d, want 1", got)
	}

	if want := []string{"2026/10/a.jpg", "families.yaml"}; !slices.Equal(env.storedKeys(t), want) {
		t.Errorf("store after the purge = %v, want only A's %v", env.storedKeys(t), want)
	}
	if _, err := os.Stat(filepath.Join(env.cacheDir, "thumb", "aa", "a_tile_224.jpg")); err != nil {
		t.Errorf("A's cached thumbnail after the purge: %v, want it kept", err)
	}
	if _, err := os.Stat(filepath.Join(env.cacheDir, strings.TrimSuffix(env.f.B.StoragePrefix, "/"))); !os.IsNotExist(err) {
		t.Errorf("B's cache tree after the purge: %v, want it gone", err)
	}
}

// TestPurge_refusals verifies the purge refuses the bootstrap project unless
// allowed, refuses any project with an empty storage prefix even when allowed,
// refuses a confirmation that is not the slug, and deletes nothing in any case.
func TestPurge_refusals(t *testing.T) {
	env := newPurgeEnv(t)

	if _, err := env.purger.Plan(t.Context(), reset.PurgeOptions{Slug: project.BootstrapSlug}); !errors.Is(
		err, reset.ErrPurgeBootstrap) {
		t.Errorf("Plan(bootstrap) = %v, want ErrPurgeBootstrap", err)
	}
	allowed := reset.PurgeOptions{Slug: project.BootstrapSlug, AllowBootstrap: true}
	if _, err := env.purger.Plan(t.Context(), allowed); !errors.Is(err, reset.ErrPurgeRootPrefix) {
		t.Errorf("Plan(bootstrap, allowed) = %v, want ErrPurgeRootPrefix (it lives in the store root)", err)
	}
	if _, err := env.purger.Plan(t.Context(), reset.PurgeOptions{Slug: "no-such-project"}); !errors.Is(
		err, project.ErrNotFound) {
		t.Errorf("Plan(unknown) = %v, want project.ErrNotFound", err)
	}

	plan, err := env.purger.Plan(t.Context(), reset.PurgeOptions{Slug: env.f.B.Slug})
	if err != nil {
		t.Fatalf("Plan(B): %v", err)
	}
	for _, typed := range []string{"", env.f.A.Slug, env.f.B.Slug + " ", strings.ToUpper(env.f.B.Slug)} {
		opts := reset.PurgeOptions{Slug: env.f.B.Slug, Execute: true, Confirm: typed}
		if _, err := env.purger.Execute(t.Context(), opts, plan); !errors.Is(err, reset.ErrPurgeNotConfirmed) {
			t.Errorf("Execute(confirm %q) = %v, want ErrPurgeNotConfirmed", typed, err)
		}
	}
	if got := env.rowsOf(t, "photos", "project_id = $1", env.f.B.ProjectUID); got != 1 {
		t.Errorf("B's photos after refused purges = %d, want 1", got)
	}
	if got := len(env.storedKeys(t)); got != 5 {
		t.Errorf("store holds %d objects after refused purges, want 5", got)
	}
}
