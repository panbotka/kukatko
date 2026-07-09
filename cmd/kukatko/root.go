package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
)

// newRootCmd builds the root Cobra command for the kukatko CLI and attaches all
// subcommands. Usage and errors are silenced so that RunE failures surface as a
// single, clean message via main rather than a duplicated usage dump.
//
// argv0 is the name the binary was invoked under (os.Args[0]). Through a symlink
// named kukatkoctl the ctl subtree becomes the root, so `kukatkoctl photos list`
// works without the ctl level — one binary, two names.
func newRootCmd(argv0 string) *cobra.Command {
	if impliesCtl(argv0) {
		ctlRoot := newCtlCmd()
		ctlRoot.Use = ctlProgramName
		return ctlRoot
	}
	root := &cobra.Command{
		Use:   "kukatko",
		Short: "Kukátko — self-hosted photo & video library",
		Long: "Kukátko is a single-binary photo and video management application, " +
			"a robust replacement for PhotoPrism.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("config", "",
		"path to the YAML config file (default: $KUKATKO_CONFIG or config.yaml)")
	root.AddCommand(newServeCmd(), newMigrateCmd(), newImportCmd(), newBackupCmd(),
		newRestoreCmd(), newMaintenanceCmd(), newStorageCmd(), newCtlCmd(), newVersionCmd())
	return root
}

// loadConfigFromFlags reads the persistent --config flag from cmd and loads the
// typed configuration, wrapping any flag-lookup or load failure with context.
func loadConfigFromFlags(cmd *cobra.Command) (*config.Config, error) {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, fmt.Errorf("reading --config flag: %w", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}
