//go:build integration

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/reset"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestRunPurge_dryRunThenExecute drives the purge the way the command does: a
// dry run prints the plan on stderr and deletes nothing; --yes with the slug
// confirmed deletes the project and its prefix and writes the report.
func TestRunPurge_dryRunThenExecute(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	root := t.TempDir()
	store, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	object := filepath.Join(root, filepath.FromSlash(f.B.StoragePrefix), "2026", "10", "b.jpg")
	if err := os.MkdirAll(filepath.Dir(object), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(object, []byte("photo of B"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	purger := reset.NewPurger(reset.PurgeConfig{Pool: db.Pool(), Store: store})

	var stderr, stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	cmd.SetOut(&stdout)
	cmd.SetContext(t.Context())
	if err := runPurge(cmd, purger, f.B.Slug, purgeFlags{}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{"photos", "1 object(s)", "dry run: nothing was deleted"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("dry run stderr lacks %q:\n%s", want, stderr.String())
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("dry run wrote to stdout: %q (the plan belongs on stderr)", stdout.String())
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("the dry run deleted B's object: %v", err)
	}

	report := filepath.Join(t.TempDir(), "purge.json")
	stderr.Reset()
	flags := purgeFlags{yes: true, confirm: f.B.Slug, report: report}
	if err := runPurge(cmd, purger, f.B.Slug, flags); err != nil {
		t.Fatalf("purge: %v\n%s", err, stderr.String())
	}
	if _, err := os.Stat(object); !os.IsNotExist(err) {
		t.Errorf("B's object after the purge: %v, want it gone", err)
	}
	if _, err := os.Stat(report); err != nil {
		t.Errorf("report: %v", err)
	}
	var n int
	if err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM projects WHERE uid = $1`,
		f.B.ProjectUID).Scan(&n); err != nil || n != 0 {
		t.Errorf("project B rows after the purge = %d (%v), want 0", n, err)
	}
	if err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM photos WHERE project_id = $1`,
		f.A.ProjectUID).Scan(&n); err != nil || n != 1 {
		t.Errorf("project A photos after the purge = %d (%v), want 1", n, err)
	}
}
