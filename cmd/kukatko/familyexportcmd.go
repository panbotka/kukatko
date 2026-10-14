package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/familyexportjob"
)

// newSidecarFamiliesCmd builds the "sidecar families" command: write every
// project's genealogy export now.
//
// It writes the file itself instead of enqueuing a job, unlike its per-photo
// sibling. There is one file and the work is one small read of a small table, so
// there is nothing to spread over a queue — and the moment this is typed is
// exactly the moment a running server cannot be assumed: before a migration, an
// upgrade or a restore rehearsal, from a terminal.
func newSidecarFamiliesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "families",
		Short: "Write every project's family tree export (" + familyexport.Key + ") now",
		Long: "Write each project's whole genealogy — every person who appears in a family and " +
			"every family tying them together — to " + familyexport.Key + " under the project's " +
			"storage prefix (the root of the storage for a project with an empty prefix), so the " +
			"family tree can be rebuilt without the database. The server rewrites a project's file " +
			"whenever a relation in it changes; this forces all of them now and does not need a " +
			"running server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSidecarFamilies(cmd)
		},
	}
}

// runSidecarFamilies opens the database and storage and writes every project's
// export, reporting the key each one landed at.
func runSidecarFamilies(cmd *cobra.Command) error {
	svc, cleanup, err := openFamilyExportService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	keys, err := svc.ExportAll(cmd.Context())
	for _, key := range keys {
		cmd.Printf("family tree: written to %s\n", key)
	}
	if err != nil {
		return fmt.Errorf("writing the family tree export: %w", err)
	}
	return nil
}

// openFamilyExportService loads the config, opens the database (refusing a
// stale schema) and builds the genealogy export service. The caller owns closing
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
	db, err := openCurrentDB(cmd.Context(), cfg.Database)
	if err != nil {
		return nil, nil, err
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
