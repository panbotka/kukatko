package main

import (
	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/version"
)

// newVersionCmd builds the "version" subcommand, which prints the build version
// and commit hash injected at link time.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and commit information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			info := version.Get()
			cmd.Printf("kukatko %s\ncommit: %s\n", info.Version, info.Commit)
		},
	}
}
