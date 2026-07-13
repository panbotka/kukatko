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
// which runs a PhotoPrism import synchronously — full, or scoped to an album, a
// label, a person and/or a year — and prints the resulting counts. It is the
// ops/cron entry point that does not need the server running; the same full
// import also runs as a background pp_import job triggered from the API.
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
			"The --album, --label, --person and --year flags scope the run to a slice of the " +
			"source library: only the photos they select are imported (whole, however old they " +
			"are), and only the structure of those photos is mapped. Several flags combine and " +
			"narrow the run together, e.g. --album <uid> --year 1985.\n\n" +
			"A scoped run is PARTIAL and does not advance the resume cursor: the incremental " +
			"watermark is left untouched, so a later full import still sees every photo — " +
			"including the ones the scoped run never listed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportPhotoPrism(cmd)
		},
	}
	ppCmd.Flags().String("album", "",
		"import only this PhotoPrism album uid (partial run; leaves the watermark untouched)")
	ppCmd.Flags().String("label", "",
		"import only photos carrying this PhotoPrism label slug, e.g. sdh "+
			"(partial run; leaves the watermark untouched)")
	ppCmd.Flags().String("person", "",
		`import only photos this person appears on, by full name, e.g. "Aleš Kozák" `+
			"(partial run; leaves the watermark untouched)")
	ppCmd.Flags().Int("year", 0,
		"import only photos taken in this year, e.g. 1985 (partial run; leaves the watermark untouched)")
	importCmd.AddCommand(ppCmd)
	return importCmd
}

// importScopeFromFlags reads the scoping flags of "import photoprism" into a
// ppimport.Scope. The zero Scope (no flag given) means a full incremental run.
func importScopeFromFlags(cmd *cobra.Command) (ppimport.Scope, error) {
	album, err := cmd.Flags().GetString("album")
	if err != nil {
		return ppimport.Scope{}, fmt.Errorf("reading --album: %w", err)
	}
	label, err := cmd.Flags().GetString("label")
	if err != nil {
		return ppimport.Scope{}, fmt.Errorf("reading --label: %w", err)
	}
	person, err := cmd.Flags().GetString("person")
	if err != nil {
		return ppimport.Scope{}, fmt.Errorf("reading --person: %w", err)
	}
	year, err := cmd.Flags().GetInt("year")
	if err != nil {
		return ppimport.Scope{}, fmt.Errorf("reading --year: %w", err)
	}
	return ppimport.Scope{AlbumUID: album, Label: label, Person: person, Year: year}, nil
}

// runImportPhotoPrism loads the configuration, opens the database (applying
// migrations), builds the import service and runs one import pass — full, or
// scoped by the --album/--label/--person/--year flags — printing the run id and
// counts. It returns errImportNotConfigured when the PhotoPrism base URL is unset.
func runImportPhotoPrism(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	if cfg.Import.PhotoPrism.BaseURL == "" {
		return errImportNotConfigured
	}
	scope, err := importScopeFromFlags(cmd)
	if err != nil {
		return err
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
	result, err := runImportPass(cmd, svc, scope)
	if err != nil {
		return err
	}
	cmd.Printf("photoprism import run %d: imported=%d updated=%d skipped=%d failed=%d\n",
		result.RunID, result.Counts.Imported, result.Counts.Updated,
		result.Counts.Skipped, result.Counts.Failed)
	if !scope.IsEmpty() {
		cmd.Printf("scoped run (%s): partial import, the incremental watermark was left untouched\n", scope)
	}
	return nil
}

// runImportPass runs the scoped import when scope names any filter, and the full
// incremental import otherwise. It returns the run's result even on failure, so
// the caller can still report what the aborted run managed to do.
func runImportPass(
	cmd *cobra.Command, svc *ppimport.Service, scope ppimport.Scope,
) (ppimport.Result, error) {
	var (
		result ppimport.Result
		err    error
	)
	if scope.IsEmpty() {
		result, err = svc.Import(cmd.Context())
	} else {
		result, err = svc.ImportScoped(cmd.Context(), scope)
	}
	if err != nil {
		return result, fmt.Errorf("running photoprism import: %w", err)
	}
	return result, nil
}
