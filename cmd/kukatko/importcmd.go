package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/ppimport"
)

// errImportNotConfigured indicates the PhotoPrism import was invoked without a
// configured base URL.
var errImportNotConfigured = errors.New(
	"photoprism import not configured: set import.photoprism.base_url (and token)")

// newImportCmd builds the "import" subcommand group and its photoprism child,
// which runs a full PhotoPrism import synchronously and prints the resulting
// counts. It is the ops/cron entry point that does not need the server running;
// the same import also runs as a background pp_import job triggered from the API.
func newImportCmd() *cobra.Command {
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import media from external sources",
		Long:  "Import media into Kukátko from external catalogues (currently PhotoPrism).",
		Args:  cobra.NoArgs,
	}
	ppCmd := &cobra.Command{
		Use:   "photoprism",
		Short: "Run a read-only, incremental PhotoPrism import",
		Long: "Pull new and changed photos (plus albums, labels and people) from the " +
			"configured PhotoPrism instance, resuming from the last successful watermark.\n\n" +
			"With --album, only that album's photos are imported (whole, however old they " +
			"are) and only that album is mapped. Such a run does not advance the watermark, " +
			"so a later full import still sees every photo.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportPhotoPrism(cmd)
		},
	}
	ppCmd.Flags().String("album", "",
		"import only this PhotoPrism album uid (partial run; leaves the watermark untouched)")
	importCmd.AddCommand(ppCmd)
	return importCmd
}

// runImportPhotoPrism loads the configuration, opens the database (applying
// migrations), builds the import service and runs one import pass — full, or
// scoped to the --album uid — printing the run id and counts. It returns
// errImportNotConfigured when the PhotoPrism base URL is unset.
func runImportPhotoPrism(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	if cfg.Import.PhotoPrism.BaseURL == "" {
		return errImportNotConfigured
	}
	album, err := cmd.Flags().GetString("album")
	if err != nil {
		return fmt.Errorf("reading --album: %w", err)
	}

	ctx := cmd.Context()
	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	// The CLI import command does not serve metrics, so pass a nil registry.
	svc, err := buildImportService(cfg, db, jobs.NewEnqueuer(jobs.NewStore(db.Pool())), nil)
	if err != nil {
		return err
	}
	var result ppimport.Result
	if album != "" {
		result, err = svc.ImportAlbum(ctx, album)
	} else {
		result, err = svc.Import(ctx)
	}
	if err != nil {
		return fmt.Errorf("running photoprism import: %w", err)
	}
	cmd.Printf("photoprism import run %d: imported=%d updated=%d skipped=%d failed=%d\n",
		result.RunID, result.Counts.Imported, result.Counts.Updated,
		result.Counts.Skipped, result.Counts.Failed)
	if album != "" {
		cmd.Printf("album %s only: the incremental watermark was left untouched\n", album)
	}
	return nil
}
