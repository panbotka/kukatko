package main

import (
	"github.com/spf13/cobra"
)

// newImportCmd builds the "import" subcommand group and its only child, the dir
// import, which ingests a directory of originals from disk. It is the ops/cron
// entry point that does not need the server running.
//
// The migration from PhotoPrism and photo-sorter used to live here too; it
// finished in August 2026 and its code is gone (see docs/MIGRATION_PLAN.md).
func newImportCmd() *cobra.Command {
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import media from a directory of files on disk",
		Long:  "Import media into Kukátko from a directory of files on the server's disk.",
		Args:  cobra.NoArgs,
	}
	importCmd.AddCommand(newImportDirCmd())
	return importCmd
}
