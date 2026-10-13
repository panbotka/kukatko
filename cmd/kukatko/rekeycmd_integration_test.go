//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/storage"
)

// cliNoRewrites drops the rewrites Finish schedules; the test never finishes.
type cliNoRewrites struct{}

func (cliNoRewrites) EnqueueFamilyExport(context.Context, string) error { return nil }
func (cliNoRewrites) EnqueueSidecar(context.Context, string) error      { return nil }

// TestRunRekey_againstTheDatabase drives the subcommand over the real move: a
// dry run counts the photo and changes nothing, the start records a running
// run and queues its job, --status reports it, and a second start is refused.
func TestRunRekey_againstTheDatabase(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	if _, err := db.Pool().Exec(t.Context(), `INSERT INTO photos (uid, file_hash, file_path, file_name, file_size,
		file_mime) VALUES ('ph1', repeat('a', 64), '2023/01/a.jpg', 'a.jpg', 2048, 'image/jpeg')`); err != nil {
		t.Fatalf("seeding a photo: %v", err)
	}
	mover := rekey.New(rekey.Config{Pool: db.Pool(), Store: store, Jobs: cliNoRewrites{}})
	proj, err := project.NewStore(db.Pool()).Get(t.Context(), project.BootstrapUID)
	if err != nil {
		t.Fatalf("reading the bootstrap project: %v", err)
	}
	run := func(action rekeyAction) (string, error) {
		var stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&stderr)
		cmd.SetOut(&stderr)
		cmd.SetContext(t.Context())
		err := runRekey(cmd, mover, *proj, action)
		return stderr.String(), err
	}

	out, err := run(rekeyDryRun)
	if err != nil || !strings.Contains(out, "1 photo(s), 2.0 KiB") || !strings.Contains(out, "2023/01/a.jpg →") {
		t.Fatalf("dry run = %v:\n%s", err, out)
	}
	if _, err := mover.Status(t.Context(), project.BootstrapUID); !errors.Is(err, rekey.ErrNoRun) {
		t.Fatalf("Status after a dry run = %v, want ErrNoRun", err)
	}

	if out, err := run(rekeyStart); err != nil || !strings.Contains(out, ": running") {
		t.Fatalf("start = %v:\n%s", err, out)
	}
	var jobs int
	if err := db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM jobs WHERE type = 'storage_rekey'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Errorf("storage_rekey jobs after the start = %d (%v), want 1", jobs, err)
	}
	if out, err := run(rekeyStatus); err != nil || !strings.Contains(out, "0 of 1 moved") {
		t.Errorf("status = %v:\n%s", err, out)
	}
	if _, err := run(rekeyStart); !errors.Is(err, rekey.ErrRunActive) {
		t.Errorf("a second start = %v, want ErrRunActive", err)
	}
}
