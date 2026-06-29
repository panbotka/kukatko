package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
)

// newMaintenanceCmd builds the "maintenance" subcommand group: an integrity scan
// and an opt-in repair runner. Both are ops/cron entry points that need no
// running server (repairs enqueue jobs the running server's worker will drain;
// the orphan import runs synchronously). The same scan and repairs are also
// available to admins over the HTTP API.
func newMaintenanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Library integrity scan and repair",
		Long: "Scan the library for drift between the catalogue and the files on disk, " +
			"and repair what can be regenerated (thumbnails, perceptual hashes, embeddings, " +
			"faces) or imported (orphan originals). Never deletes originals.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newMaintenanceScanCmd(), newMaintenanceRepairCmd())
	return cmd
}

// newMaintenanceScanCmd builds the "maintenance scan" subcommand, which runs one
// read-only integrity scan and prints the report.
func newMaintenanceScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Run an integrity scan and print the report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMaintenanceScan(cmd)
		},
	}
}

// newMaintenanceRepairCmd builds the "maintenance repair" subcommand, which runs
// the repairs selected by its flags. With no flag it does nothing and reports the
// available options.
func newMaintenanceRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Run the selected repairs (thumbnails, embeddings, faces, phashes, orphans)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMaintenanceRepair(cmd)
		},
	}
	cmd.Flags().Bool("thumbnails", false, "regenerate missing thumbnails")
	cmd.Flags().Bool("embeddings", false, "backfill missing image embeddings")
	cmd.Flags().Bool("faces", false, "backfill missing face detections")
	cmd.Flags().Bool("phashes", false, "recompute missing perceptual hashes")
	cmd.Flags().Bool("import-orphans", false, "import orphan originals on disk into the catalogue")
	return cmd
}

// buildMaintenanceForCLI assembles a maintenance service for the CLI, wiring the
// shared queue store, the embedding and face backfills, and the originals/disk
// collaborators. Metrics are disabled (nil registry) for one-off CLI runs.
func buildMaintenanceForCLI(cfg *config.Config, db *database.DB) (*maintenance.Service, error) {
	enqueuer := jobs.NewEnqueuer(jobs.NewStore(db.Pool()))
	embedSvc, vectorStore, embedClient, err := buildEmbedService(cfg, db, enqueuer, nil)
	if err != nil {
		return nil, err
	}
	faceSvc, err := buildFaceService(cfg, db, enqueuer, vectorStore, embedClient)
	if err != nil {
		return nil, err
	}
	return buildMaintenanceService(cfg, db, enqueuer, embedSvc, faceSvc, nil)
}

// openMaintenanceService loads the config, opens the database (applying
// migrations), and builds the maintenance service. The caller owns closing the
// database via the returned cleanup.
func openMaintenanceService(cmd *cobra.Command) (*maintenance.Service, func(), error) {
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
	svc, err := buildMaintenanceForCLI(cfg, db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return svc, db.Close, nil
}

// runMaintenanceScan runs one integrity scan and prints the report.
func runMaintenanceScan(cmd *cobra.Command) error {
	svc, cleanup, err := openMaintenanceService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	report, err := svc.Scan(cmd.Context())
	if err != nil {
		return fmt.Errorf("running integrity scan: %w", err)
	}
	printScanReport(cmd, report)
	return nil
}

// printScanReport prints a scan report as a readable summary.
func printScanReport(cmd *cobra.Command, report maintenance.Report) {
	cmd.Printf("integrity scan: %d photos, %d files in DB, %d originals on disk\n",
		report.Photos, report.FilesInDB, report.OriginalsOnDisk)
	cmd.Printf("  missing originals:  %d\n", report.MissingOriginals.Count)
	cmd.Printf("  orphan files:       %d\n", report.OrphanFiles.Count)
	cmd.Printf("  missing thumbnails: %d\n", report.MissingThumbnails.Count)
	cmd.Printf("  missing embeddings: %d\n", report.MissingEmbeddings.Count)
	cmd.Printf("  missing faces:      %d\n", report.MissingFaces.Count)
	cmd.Printf("  missing phashes:    %d\n", report.MissingPhashes.Count)
	if report.Clean() {
		cmd.Println("library is consistent")
	}
}

// runMaintenanceRepair reads the repair flags and runs the selected repairs,
// printing what each scheduled or did.
func runMaintenanceRepair(cmd *cobra.Command) error {
	opts, err := repairOptionsFromFlags(cmd)
	if err != nil {
		return err
	}
	if !opts.Any() {
		cmd.Println("no repair selected; pass --thumbnails, --embeddings, --faces, --phashes or --import-orphans")
		return nil
	}
	svc, cleanup, err := openMaintenanceService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	result, err := svc.Repair(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("running repairs: %w", err)
	}
	cmd.Printf("repairs scheduled: thumbnails=%d phashes=%d embeddings=%d faces=%d\n",
		result.ThumbnailsEnqueued, result.PhashesEnqueued, result.EmbeddingsEnqueued, result.FacesEnqueued)
	cmd.Printf("orphans imported=%d skipped=%d failed=%d\n",
		result.OrphansImported, result.OrphansSkipped, result.OrphansFailed)
	return nil
}

// repairOptionsFromFlags reads the repair selection flags into a RepairOptions.
func repairOptionsFromFlags(cmd *cobra.Command) (maintenance.RepairOptions, error) {
	flags := cmd.Flags()
	var opts maintenance.RepairOptions
	for name, target := range map[string]*bool{
		"thumbnails":     &opts.Thumbnails,
		"embeddings":     &opts.Embeddings,
		"faces":          &opts.Faces,
		"phashes":        &opts.Phashes,
		"import-orphans": &opts.ImportOrphans,
	} {
		val, err := flags.GetBool(name)
		if err != nil {
			return maintenance.RepairOptions{}, fmt.Errorf("reading --%s flag: %w", name, err)
		}
		*target = val
	}
	return opts, nil
}
