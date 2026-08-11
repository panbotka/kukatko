package main

import (
	"github.com/spf13/cobra"
)

// newImportCmd builds the "import" subcommand group and its only child, the dir
// import, which ingests a directory of originals from disk. It is the ops/cron
// entry point that does not need the server running, and the only import there
// is: the one-off importers that filled this library initially were removed once
// they had done their job.
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
