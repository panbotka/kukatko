package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/inventory"
	"github.com/panbotka/kukatko/internal/storage"
)

// errStoreCannotList reports a storage backend that cannot list its objects
// with their sizes, so the storage check cannot run against it.
var errStoreCannotList = errors.New("storage backend cannot list object sizes")

// newMaintenanceInventoryCmd builds "maintenance inventory", which writes a
// read-only, deterministic snapshot of the catalogue into a directory: the
// "before" and "after" of an upgrade that migrates data. It deliberately does
// not apply migrations — the new binary must be able to inventory a database
// still on the old schema, before the deploy.
func newMaintenanceInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Write a read-only before/after snapshot of the catalogue (never migrates)",
		Long: "Write counts.tsv, columns.tsv, digests.tsv, photos.tsv and users.tsv for the " +
			"configured database into --out. With --storage, also list the storage backend once " +
			"and write storage-missing.tsv: every original that is absent or has the wrong size " +
			"(empty = good). Compare two snapshots with 'maintenance inventory-diff'.",
		Args: cobra.NoArgs,
		RunE: runMaintenanceInventory,
	}
	cmd.Flags().String("out", "", "directory to write the snapshot into (required)")
	cmd.Flags().Bool("storage", false, "also check every original exists in storage with its recorded size")
	_ = cmd.MarkFlagRequired("out") // the flag is defined just above
	return cmd
}

// runMaintenanceInventory opens the database without migrating it, optionally
// builds the storage backend, and writes the snapshot.
func runMaintenanceInventory(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	out, _ := cmd.Flags().GetString("out")           // defined on this command
	withStorage, _ := cmd.Flags().GetBool("storage") // defined on this command
	var lister storage.SizedKeyLister
	if withStorage {
		store, err := newStorage(cfg)
		if err != nil {
			return err
		}
		var ok bool
		if lister, ok = store.(storage.SizedKeyLister); !ok {
			return fmt.Errorf("%w: %T", errStoreCannotList, store)
		}
	}
	// Deliberately database.New, not openCurrentDB: the inventory is the
	// upgrade's "before" snapshot the new binary takes of a database still
	// on the old schema, so it must neither migrate nor refuse a stale one.
	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if err := inventory.Collect(cmd.Context(), db.Pool(), out, lister); err != nil {
		return fmt.Errorf("taking the inventory: %w", err)
	}
	cmd.Printf("inventory written to %s\n", out)
	return nil
}

// newMaintenanceInventoryDiffCmd builds "maintenance inventory-diff", which
// compares two snapshots and fails on any difference no --expect allows.
func newMaintenanceInventoryDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory-diff <dirA> <dirB>",
		Short: "Compare two inventory snapshots; exit 1 on an unexpected difference",
		Long: "Print every difference between snapshot A (before) and B (after). Each --expect " +
			"is a glob over a difference key (e.g. table-added:projects, column-added:*.project_id, " +
			"count:schema_migrations) or the rule role-collapse (an account's role going from " +
			"viewer/curator/editor/admin to user and nothing else). Any difference no expectation " +
			"allows makes the command exit 1. Needs no database.",
		Args: cobra.ExactArgs(2),
		RunE: runMaintenanceInventoryDiff,
	}
	cmd.Flags().StringArray("expect", nil, "an allowed difference (glob over keys, or role-collapse); repeatable")
	return cmd
}

// runMaintenanceInventoryDiff loads both snapshots, judges the diff against the
// expectations, prints it on stdout and returns ErrUnexpected (exit 1) when
// anything is unexplained.
func runMaintenanceInventoryDiff(cmd *cobra.Command, args []string) error {
	values, _ := cmd.Flags().GetStringArray("expect") // defined on this command
	exp, err := inventory.ParseExpectations(values)
	if err != nil {
		return err //nolint:wrapcheck // already names the bad expectation
	}
	a, err := inventory.Load(args[0])
	if err != nil {
		return err //nolint:wrapcheck // already names the file
	}
	b, err := inventory.Load(args[1])
	if err != nil {
		return err //nolint:wrapcheck // already names the file
	}
	res := inventory.Judge(inventory.Diff(a, b), exp)
	if err := res.Write(cmd.OutOrStdout()); err != nil {
		return err //nolint:wrapcheck // already says what failed
	}
	return res.Err() //nolint:wrapcheck // the sentinel carries the count
}
