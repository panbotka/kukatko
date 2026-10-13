package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/reset"
)

// TestRootCmd_hasProjectPurge verifies the purge is reachable as
// `kukatko project purge` and that deleting is off by default.
func TestRootCmd_hasProjectPurge(t *testing.T) {
	t.Parallel()

	cmd, _, err := newRootCmd("kukatko").Find([]string{"project", "purge"})
	if err != nil || cmd.Name() != "purge" {
		t.Fatalf("Find(project purge) = %v, %v; want the purge subcommand", cmd, err)
	}
	for _, name := range []string{flagPurgeYes, flagPurgeConfirm, flagPurgeAllowBootstrap, flagPurgeReport} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s is not registered", name)
		}
	}
	if yes, _ := cmd.Flags().GetBool(flagPurgeYes); yes {
		t.Error("--yes defaults to true; a purge must be a dry run by default")
	}
}

// TestProjectPurgeCmd_yesNeedsConfirmBeforeConnecting verifies --yes without the
// slug typed again fails before the config is even loaded — the DSN here points
// at nothing, and the error must be the guard's, not a connection error.
func TestProjectPurgeCmd_yesNeedsConfirmBeforeConnecting(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", unreachableDSN)

	_, err := executeResetCmd(t, "", "project", "purge", "some-project", "--yes")
	if !errors.Is(err, errPurgeConfirmMissing) {
		t.Errorf("purge --yes without --confirm = %v, want errPurgeConfirmMissing", err)
	}
}

// TestProjectPurgeCmd_needsASlug verifies the slug is a required argument.
func TestProjectPurgeCmd_needsASlug(t *testing.T) {
	t.Parallel()

	if _, err := executeResetCmd(t, "", "project", "purge"); err == nil {
		t.Error("purge with no slug succeeded, want an argument error")
	}
}

// TestWritePurgeReport verifies the report is the result as JSON, at the path
// given or at the default name derived from the slug and the purge time.
func TestWritePurgeReport(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	result := reset.PurgeResult{
		Plan: reset.PurgePlan{
			Project: reset.PurgeProject{Slug: "b", UID: "pj00000000000000000000000b"},
			Tables:  []reset.TableCount{{Table: "photos", Rows: 2}},
			Objects: 3,
		},
		PurgedAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Storage:  reset.StorageResult{Deleted: 3},
	}

	path, err := writePurgeReport("", result)
	if err != nil {
		t.Fatalf("writePurgeReport: %v", err)
	}
	if path != "project-purge-b-20261013T120000Z.json" {
		t.Errorf("default report path = %q", path)
	}
	explicit := filepath.Join(dir, "custom.json")
	if got, err := writePurgeReport(explicit, result); err != nil || got != explicit {
		t.Fatalf("writePurgeReport(%s) = %q, %v", explicit, got, err)
	}
	for _, p := range []string{path, explicit} {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		var back reset.PurgeResult
		if err := json.Unmarshal(body, &back); err != nil {
			t.Fatalf("decoding %s: %v", p, err)
		}
		if back.Plan.Project.Slug != "b" || back.Storage.Deleted != 3 || back.Plan.Rows() != 2 {
			t.Errorf("report %s = %+v, want the result back", p, back)
		}
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != purgeReportPerm {
			t.Errorf("report %s mode = %v (%v), want %v", p, info.Mode().Perm(), err, os.FileMode(purgeReportPerm))
		}
	}
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("report path %q is not a .json file", path)
	}
}
