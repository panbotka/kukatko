//go:build integration

package database_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
)

// rollbackChain is the README's order for going back to 0.31.0 (schema 0099):
// newest first.
var rollbackChain = []string{
	"0120", "0119", "0118", "0117", "0116", "0115", "0114", "0113", "0112", "0111", "0110", "0109",
	"0108", "0107", "0106", "0105", "0104", "0103", "0102", "0101", "0100",
}

// TestRollbackChain_queuedJobsOfAGoneProject runs the whole hand-run down chain
// over a database whose queue still holds per-project jobs of a project that no
// longer exists (deleted before the rollback, so 0102_down's guard passes; the
// payload's project_uid is no foreign key). 0108_down and 0106_down rebuild
// instance-wide keys for notification_purge and family_export; without their own
// cancellation the second queued job of each would fail the rebuild and stop
// the chain half-unwound. The chain must reach schema 0099 with only the
// bootstrap project's queued job of each kind left, and keep the face_cluster
// jobs whose old key was already per project.
//
// A job of the gone project that the stopped container left running is
// cancelled too (task_digest's of any project, since 0107_down cancels every
// queued digest): the previous release's stale-lock recovery would otherwise
// requeue it beside the kept queued job and fail on the rebuilt index on every
// tick. The running face_cluster job stays, its key being per project already.
func TestRollbackChain_queuedJobsOfAGoneProject(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	const bootstrap, gone = "pj0000000000000000veselice", "pj00000000000000000000gone"
	for _, typ := range []string{"family_export", "notification_purge", "face_cluster"} {
		for _, project := range []string{bootstrap, gone} {
			if _, err := db.Pool().Exec(ctx, `INSERT INTO jobs (type, payload) VALUES ($1, $2)`,
				typ, fmt.Sprintf(`{"project_uid": %q}`, project)); err != nil {
				t.Fatalf("queueing %s for %s: %v", typ, project, err)
			}
		}
	}
	for _, typ := range []string{"family_export", "notification_purge", "task_digest", "face_cluster"} {
		if _, err := db.Pool().Exec(ctx, `
			INSERT INTO jobs (type, payload, state, locked_by, locked_at)
			VALUES ($1, $2, 'running', 'stopped-container', now() - interval '1 hour')`,
			typ, fmt.Sprintf(`{"project_uid": %q}`, gone)); err != nil {
			t.Fatalf("leaving %s of %s running: %v", typ, gone, err)
		}
	}

	for _, version := range rollbackChain {
		runDownScript(t, db, version)
	}

	var latest int64
	if err := db.Pool().QueryRow(ctx, "SELECT max(version) FROM schema_migrations").Scan(&latest); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if latest != 99 {
		t.Errorf("schema version after the chain = %d, want 99", latest)
	}
	for typ, want := range map[string][]string{
		"family_export":      {bootstrap},
		"notification_purge": {bootstrap},
		"face_cluster":       {bootstrap, gone},
	} {
		got := queuedProjects(t, db, typ)
		if slices.Sort(want); !slices.Equal(got, want) {
			t.Errorf("queued %s jobs after the chain name %v, want %v", typ, got, want)
		}
	}
	for typ, want := range map[string][]string{
		"family_export":      nil,
		"notification_purge": nil,
		"task_digest":        nil,
		"face_cluster":       {gone},
	} {
		if got := projectsInState(t, db, typ, "running"); !slices.Equal(got, want) {
			t.Errorf("running %s jobs after the chain name %v, want %v", typ, got, want)
		}
	}
}

// runDownScript applies docs/migrations-rollback/<version>_down.sql in one
// transaction, the way the README's psql -1 loop does.
func runDownScript(t *testing.T, db *database.DB, version string) {
	t.Helper()
	name := "../../docs/migrations-rollback/" + version + "_down.sql"
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	tx, err := db.Pool().Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning %s: %v", name, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), string(body)); err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("committing %s: %v", name, err)
	}
}

// queuedProjects lists the project_uid of every queued job of typ, in byte
// order.
func queuedProjects(t *testing.T, db *database.DB, typ string) []string {
	t.Helper()
	return projectsInState(t, db, typ, "queued")
}

// projectsInState lists the project_uid of every job of typ in state, in byte
// order.
func projectsInState(t *testing.T, db *database.DB, typ, state string) []string {
	t.Helper()
	rows, err := db.Pool().Query(t.Context(), `
		SELECT payload ->> 'project_uid' FROM jobs
		WHERE type = $1 AND state = $2`, typ, state)
	if err != nil {
		t.Fatalf("listing %s %s jobs: %v", state, typ, err)
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			t.Fatalf("scanning a %s %s job: %v", state, typ, err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating %s %s jobs: %v", state, typ, err)
	}
	slices.Sort(projects)
	return projects
}
