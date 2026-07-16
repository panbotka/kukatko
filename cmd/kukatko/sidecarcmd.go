package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/sidecarjob"
)

// newSidecarCmd builds the "sidecar" subcommand group: the metadata sidecar
// export's terminal entry point.
//
// It exists as a CLI command and not only as an admin endpoint because of when it
// is wanted: the moment before a risky operation — a migration, an upgrade, a
// restore rehearsal — is exactly when someone wants to force every photo's
// curation onto disk, and it is exactly the moment they are in a terminal and may
// not have a browser, a session, or a server they trust.
func newSidecarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sidecar",
		Short: "Metadata sidecar export (curation that survives the database)",
		Long: "Write each photo's metadata and curation to a YAML sidecar next to the originals " +
			"in storage, so the catalogue can be rebuilt from the storage alone. " +
			"See docs/RESTORE.md for the format.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newSidecarBackfillCmd())
	return cmd
}

// newSidecarBackfillCmd builds the "sidecar backfill" command.
func newSidecarBackfillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Enqueue a sidecar write for every photo whose sidecar is missing or stale",
		Long: "Enqueue a sidecar job for every photo whose sidecar has never been written or " +
			"predates the photo's last edit. The jobs are drained by the running server's " +
			"worker, so this returns as soon as they are scheduled. It is idempotent: a run " +
			"over a library whose sidecars are current schedules nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSidecarBackfill(cmd)
		},
	}
	cmd.Flags().Bool("all", false,
		"schedule every non-archived photo, not only the missing and stale ones (a forced full re-run)")
	return cmd
}

// runSidecarBackfill loads the config, opens the database and enqueues the
// sidecar backfill, reporting how many jobs were scheduled.
//
// It only enqueues; the running server's worker writes the files. That is
// deliberate — it is the same queue, the same handler and the same dedup as every
// other write, so a backfill run alongside live edits cannot race them or write a
// photo twice. The cost is that a backfill on a box with no server running
// schedules work that waits, which the printed count makes visible.
func runSidecarBackfill(cmd *cobra.Command) error {
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return fmt.Errorf("reading --all flag: %w", err)
	}
	svc, cleanup, err := openSidecarService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	enqueued, err := svc.BackfillSidecars(cmd.Context(), all)
	if err != nil {
		return fmt.Errorf("backfilling sidecars: %w", err)
	}
	if enqueued == 0 {
		cmd.Println("sidecars: nothing to do, every sidecar is current")
		return nil
	}
	cmd.Printf("sidecars: %d job(s) scheduled; the running server's worker writes the files\n", enqueued)
	return nil
}

// openSidecarService loads the config, opens the database (applying migrations)
// and builds the sidecar export service. The caller owns closing the database via
// the returned cleanup.
//
// It fails when the export is switched off rather than silently doing nothing:
// someone typing this command has asked for sidecars, and answering "0 scheduled"
// to a config that writes none would be a lie of omission on the one day it
// matters.
func openSidecarService(cmd *cobra.Command) (*sidecarjob.Service, func(), error) {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return nil, nil, err
	}
	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to database: %w", err)
	}
	if _, err := db.Migrate(cmd.Context()); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("applying migrations: %w", err)
	}
	svc, err := buildSidecarServiceOrNil(cfg, db, jobs.NewEnqueuer(jobs.NewStore(db.Pool())))
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	if svc == nil {
		db.Close()
		return nil, nil, errors.New(
			"sidecar export is disabled (sidecar.enabled = false); enable it to write sidecars")
	}
	return svc, db.Close, nil
}
