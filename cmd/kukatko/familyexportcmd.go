package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/familyexportjob"
)

// newSidecarFamiliesCmd builds the "sidecar families" command: write the
// library's genealogy export now.
//
// It writes the file itself instead of enqueuing a job, unlike its per-photo
// sibling. There is one file and the work is one small read of a small table, so
// there is nothing to spread over a queue — and the moment this is typed is
// exactly the moment a running server cannot be assumed: before a migration, an
// upgrade or a restore rehearsal, from a terminal.
func newSidecarFamiliesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "families",
		Short: "Write the family tree export (" + familyexport.Key + ") now",
		Long: "Write the library's whole genealogy — every person who appears in a family and " +
			"every family tying them together — to " + familyexport.Key + " at the root of the " +
			"storage, so the family tree can be rebuilt without the database. The server rewrites " +
			"it whenever a relation changes; this forces it now and does not need a running server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSidecarFamilies(cmd)
		},
	}
}

// runSidecarFamilies opens the database and storage and writes the export,
// reporting the key it landed at.
func runSidecarFamilies(cmd *cobra.Command) error {
	svc, cleanup, err := openFamilyExportService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := svc.Export(cmd.Context()); err != nil {
		return fmt.Errorf("writing the family tree export: %w", err)
	}
	cmd.Printf("family tree: written to %s\n", familyexport.Key)
	return nil
}

// openFamilyExportService loads the config, opens the database (applying
// migrations) and builds the genealogy export service. The caller owns closing
// the database via the returned cleanup.
//
// It fails when the export is switched off rather than silently doing nothing,
// for the same reason openSidecarService does: someone typing this command has
// asked for the file, and writing none while reporting success would be a lie of
// omission on the one day it matters.
func openFamilyExportService(cmd *cobra.Command) (*familyexportjob.Service, func(), error) {
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
	svc, err := buildFamilyExportServiceOrNil(cfg, db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	if svc == nil {
		db.Close()
		return nil, nil, errors.New(
			"sidecar export is disabled (sidecar.enabled = false); enable it to write the family tree")
	}
	return svc, db.Close, nil
}
