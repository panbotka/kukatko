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

// errFeedsImportNotConfigured indicates the photo-sorter feeds import was invoked
// without a configured feeds API base URL.
var errFeedsImportNotConfigured = errors.New(
	"photo-sorter feeds import not configured: set import.photosorter.base_url (and token)")

// newImportCmd builds the "import" subcommand group and its children: the
// photoprism import, which runs a PhotoPrism import synchronously — full, or
// scoped to an album, a label, a person and/or a year — and prints the resulting
// counts, and the dir import, which ingests a directory of originals from disk.
// It is the ops/cron entry point that does not need the server running; the same
// full PhotoPrism import also runs as a background pp_import job triggered from
// the API.
func newImportCmd() *cobra.Command {
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import media from external sources",
		Long: "Import media into Kukátko from an external catalogue (PhotoPrism) " +
			"or from a directory of files on disk.",
		Args: cobra.NoArgs,
	}
	ppCmd := &cobra.Command{
		Use:   "photoprism",
		Short: "Run a read-only, incremental PhotoPrism import",
		Long: "Pull new and changed photos (plus albums, labels and people) from the " +
			"configured PhotoPrism instance, resuming from the last successful watermark.\n\n" +
			"The --album, --label, --person and --year flags scope the run to a slice of the " +
			"source library: only the photos they select are imported (whole, however old they " +
			"are), and each of them arrives with its whole context — every album it belongs to " +
			"and every label it carries, not merely the one the scope named. Several flags " +
			"combine and narrow the run together, e.g. --album <uid> --year 1985.\n\n" +
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
	importCmd.AddCommand(ppCmd, newImportDirCmd(), newImportPSFeedsCmd())
	return importCmd
}

// newImportPSFeedsCmd builds the "import photosorter-feeds" command: it enriches
// the already-imported PhotoPrism photos with photo-sorter's pre-computed CLIP
// embeddings and InsightFace faces, copied 1:1 from its read-only migration
// feeds and attached by photoprism_uid. It runs synchronously and prints the
// resulting counts; the same pass also runs as a background ps_feeds_import job
// triggered from the API. It never downloads originals or touches PhotoPrism.
func newImportPSFeedsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "photosorter-feeds",
		Short: "Enrich imported photos with photo-sorter's 1:1 embeddings and faces",
		Long: "Page photo-sorter's read-only migration feeds (embeddings + faces) and attach each " +
			"item to the Kukátko photo whose photoprism_uid matches, copying the vectors verbatim " +
			"(no GPU recompute). Markers and subject assignments the faces feed carries come across " +
			"too. Idempotent and incremental: safe to run before, during or after the PhotoPrism " +
			"import, and safe to re-run. A feed entry whose photo is not imported yet is skipped.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportPSFeeds(cmd)
		},
	}
}

// runImportPSFeeds loads the configuration, opens the database (applying
// migrations), builds the feeds importer and runs one full enrichment pass,
// printing the run id and counts. It returns errFeedsImportNotConfigured when the
// feeds API base URL is unset.
func runImportPSFeeds(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	if !psFeedsConfigured(cfg) {
		return errFeedsImportNotConfigured
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

	result, err := runPSFeedsImport(ctx, cfg, db)
	if err != nil {
		return err
	}
	reportPSFeedsImport(cmd.Printf, result)
	return nil
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
