package main

import "github.com/spf13/cobra"

// newRootCmd builds the root Cobra command for the kukatko CLI and attaches all
// subcommands. Usage and errors are silenced so that RunE failures surface as a
// single, clean message via main rather than a duplicated usage dump.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "kukatko",
		Short: "Kukátko — self-hosted photo & video library",
		Long: "Kukátko is a single-binary photo and video management application, " +
			"a robust replacement for PhotoPrism.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServeCmd(), newVersionCmd())
	return root
}
