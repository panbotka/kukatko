package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
)

// newMaintenanceCmd builds the "maintenance" subcommand group: an integrity scan,
// an opt-in repair runner, the nameless-subject repair and the guarded library
// wipe. All of them are ops/cron entry points that need no running server (repairs
// enqueue jobs the running server's worker will drain; the orphan import runs
// synchronously). The scan and the repairs are also available to admins over the
// HTTP API; the nameless-subject repair and the wipe deliberately are not — like
// `restore db`, they are CLI-only operations that delete catalogue rows.
func newMaintenanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maintenance",
		Short: "Library integrity scan, repair and reset",
		Long: "Scan the library for drift between the catalogue and the files on disk, " +
			"repair what can be regenerated (thumbnails, perceptual hashes, embeddings, " +
			"faces) or imported (orphan originals), detach nameless catch-all subjects, " +
			"or reset the library entirely. " +
			"Scan and repair never delete originals; reset deletes the whole library on " +
			"purpose and is guarded accordingly.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newMaintenanceScanCmd(), newMaintenanceRepairCmd(),
		newMaintenanceNamelessCmd(), newMaintenanceResetCmd())
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
		Short: "Run the selected repairs (thumbnails, embeddings, faces, phashes, places, orphans)",
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
	cmd.Flags().Bool("places", false,
		"reverse-geocode the photos that carry coordinates but have no place yet; "+
			"'maintenance scan' is its dry run")
	// No backticks in the usage string: Cobra reads the first backquoted word as
	// the flag's value placeholder, which would print this bool as if it took one.
	cmd.Flags().Bool("dimensions", false,
		"rewrite transposed pixel dimensions (and the faces normalised against them) "+
			"from each file's own EXIF; 'maintenance scan' is its dry run")
	cmd.Flags().Bool("face-markers", false,
		"clear surplus face-to-marker links so one marker is claimed by at most one "+
			"face; 'maintenance scan' is its dry run")
	cmd.Flags().Bool("sideways-faces", false,
		"re-detect quarter-turned photos whose face detection ran on a sideways image "+
			"(clears the detection record and enqueues face_detect); "+
			"'maintenance scan' is its dry run")
	cmd.Flags().Bool("missing-renditions", false,
		"drop the streaming rendition rows whose segments are gone, so those clips stop "+
			"claiming they stream and the encode backfill produces them again; "+
			"'maintenance scan' is its dry run")
	cmd.Flags().Bool("delete-orphan-segments", false,
		"DELETE the streaming objects no recorded rendition claims; the only repair that "+
			"removes anything from the store, and it skips every video with an unfinished "+
			"encode job because that job is still writing them")
	cmd.Flags().Bool("impossible-dates", false,
		"withdraw the capture date of photos dated to a year no photograph can have "+
			"been taken in; the date is cleared, never replaced, and the photos become "+
			"findable as dated:no; 'maintenance scan' is its dry run")
	return cmd
}

// buildMaintenanceForCLI assembles a maintenance service for the CLI, wiring the
// shared queue store, the embedding, face and reverse-geocode backfills, and the
// originals/disk collaborators. Metrics are disabled (nil registry) for one-off
// CLI runs, and so is the credit budget: the CLI only *schedules* geocodes, and
// the worker that performs them enforces the shared budget of the running server.
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
	placesSvc, err := buildPlacesServiceOrNil(cfg, db, enqueuer, nil, nil)
	if err != nil {
		return nil, err
	}
	return buildMaintenanceService(cfg, db, enqueuer, embedSvc, faceSvc, placesSvc, nil)
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

// storeSummary describes the store half of a scan in one clause: how many
// originals the store holds and which store that was, or — when the listing
// failed — that the number is unknown and why. A failure must not print as a
// zero: "the store holds nothing" and "nobody could read the store" are opposite
// conclusions.
func storeSummary(inv maintenance.StoreInventory) string {
	where := "on disk"
	if inv.Kind == maintenance.StoreObject {
		where = "in the object store"
	}
	if !inv.Listed() {
		return fmt.Sprintf("originals %s: unknown (%s)", where, inv.Error)
	}
	return fmt.Sprintf("%d originals %s", inv.Originals, where)
}

// printScanReport prints a scan report as a readable summary.
func printScanReport(cmd *cobra.Command, report maintenance.Report) {
	cmd.Printf("integrity scan: %d photos, %d files in DB, %s\n",
		report.Photos, report.FilesInDB, storeSummary(report.Store))
	cmd.Printf("  missing originals:  %d\n", report.MissingOriginals.Count)
	cmd.Printf("  orphan files:       %d\n", report.OrphanFiles.Count)
	cmd.Printf("  missing thumbnails: %d\n", report.MissingThumbnails.Count)
	cmd.Printf("  missing embeddings: %d\n", report.MissingEmbeddings.Count)
	cmd.Printf("  missing faces:      %d\n", report.MissingFaces.Count)
	cmd.Printf("  missing phashes:    %d\n", report.MissingPhashes.Count)
	// The dry run of `repair --places`: the geotagged photos still owed a geocode.
	// Always 0 with no mapy.com key — nothing on this instance could fill them in.
	cmd.Printf("  missing places:     %d\n", report.MissingPlaces.Count)
	// The dry run of `repair --dimensions`: the sample makes the affected photos
	// inspectable before anything is rewritten.
	cmd.Printf("  transposed dims:    %d\n", report.TransposedDimensions.Count)
	if len(report.TransposedDimensions.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.TransposedDimensions.Samples)
	}
	// The faces half of the same repair, counted per face row and sampled by photo:
	// only the rows whose coordinate space the photo's markers establish, which is
	// exactly what the repair would rewrite.
	cmd.Printf("  transposed faces:   %d\n", report.TransposedFaceBoxes.Count)
	if len(report.TransposedFaceBoxes.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.TransposedFaceBoxes.Samples)
	}
	// The dry run of `repair --sideways-faces`: quarter-turned photos whose faces were
	// detected on a sideways image, so both their boxes and what the detector found
	// are suspect. Sampled by photo uid.
	cmd.Printf("  sideways faces:     %d\n", report.SidewaysFaceDetections.Count)
	if len(report.SidewaysFaceDetections.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.SidewaysFaceDetections.Samples)
	}
	// The dry run of `repair --impossible-dates`: photos dated to a year no
	// photograph can have been taken in. The sample is what the repair would clear.
	cmd.Printf("  impossible dates:   %d\n", report.ImpossibleDates.Count)
	if len(report.ImpossibleDates.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.ImpossibleDates.Samples)
	}
	// Likewise the dry run of `repair --face-markers`, sampled by marker uid.
	cmd.Printf("  dup face markers:   %d\n", report.DuplicateFaceMarkers.Count)
	if len(report.DuplicateFaceMarkers.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.DuplicateFaceMarkers.Samples)
	}
	// The dry run of `repair --missing-renditions`: rendition rows the store cannot
	// back, sampled as <photo_uid>/<rendition>. Always 0 with streaming off.
	cmd.Printf("  missing renditions: %d\n", report.MissingRenditions.Count)
	if len(report.MissingRenditions.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.MissingRenditions.Samples)
	}
	// Reported, never swept on its own: `repair --delete-orphan-segments` has to
	// ask. Sampled by the rendition prefix the objects sit under, since one
	// abandoned encode is thousands of segments under a single prefix.
	cmd.Printf("  orphan segments:    %d (%s)\n",
		report.OrphanSegments.Count, humanBytes(report.OrphanSegments.Bytes))
	if len(report.OrphanSegments.Samples) > 0 {
		cmd.Printf("    e.g. %v\n", report.OrphanSegments.Samples)
	}
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
		cmd.Println("no repair selected; pass --thumbnails, --embeddings, --faces, --phashes, " +
			"--import-orphans, --places, --dimensions, --face-markers, --sideways-faces, " +
			"--impossible-dates, --missing-renditions or --delete-orphan-segments")
		return nil
	}
	svc, cleanup, err := openMaintenanceService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	// A CLI run has no acting user: the audit entries the repairs write record a
	// system action, which is exactly what a cron or an SSH session is.
	result, err := svc.Repair(cmd.Context(), opts, audit.Meta{})
	if err != nil {
		return fmt.Errorf("running repairs: %w", err)
	}
	cmd.Printf("repairs scheduled: thumbnails=%d phashes=%d embeddings=%d faces=%d places=%d\n",
		result.ThumbnailsEnqueued, result.PhashesEnqueued, result.EmbeddingsEnqueued,
		result.FacesEnqueued, result.PlacesEnqueued)
	cmd.Printf("videos skipped by face detection=%d (a clip's people are recorded by hand)\n",
		result.FacesSkippedVideos)
	cmd.Printf("orphans imported=%d skipped=%d failed=%d\n",
		result.OrphansImported, result.OrphansSkipped, result.OrphansFailed)
	cmd.Printf("dimensions fixed=%d face boxes fixed=%d left alone=%d\n",
		result.DimensionsFixed, result.FaceBoxesFixed, result.FaceBoxesSkipped)
	cmd.Printf("surplus face links cleared=%d\n", result.FaceLinksCleared)
	cmd.Printf("sideways faces re-detected=%d (queued; they run once the sidecar's box is awake)\n",
		result.SidewaysFacesEnqueued)
	cmd.Printf("impossible dates cleared=%d (those photos now have no date; find them with dated:no)\n",
		result.ImpossibleDatesCleared)
	cmd.Printf("renditions dropped=%d (those clips no longer claim to stream; POST /process/hls re-encodes them)\n",
		result.RenditionsDropped)
	cmd.Printf("orphan segments deleted=%d kept=%d (kept = an unfinished encode is still writing them)\n",
		result.OrphanSegmentsDeleted, result.OrphanSegmentsKept)
	return nil
}

// repairOptionsFromFlags reads the repair selection flags into a RepairOptions.
func repairOptionsFromFlags(cmd *cobra.Command) (maintenance.RepairOptions, error) {
	flags := cmd.Flags()
	var opts maintenance.RepairOptions
	for name, target := range map[string]*bool{
		"thumbnails":             &opts.Thumbnails,
		"embeddings":             &opts.Embeddings,
		"faces":                  &opts.Faces,
		"phashes":                &opts.Phashes,
		"import-orphans":         &opts.ImportOrphans,
		"places":                 &opts.Places,
		"dimensions":             &opts.Dimensions,
		"face-markers":           &opts.FaceMarkers,
		"sideways-faces":         &opts.SidewaysFaces,
		"impossible-dates":       &opts.ImpossibleDates,
		"missing-renditions":     &opts.MissingRenditions,
		"delete-orphan-segments": &opts.DeleteOrphanSegments,
	} {
		val, err := flags.GetBool(name)
		if err != nil {
			return maintenance.RepairOptions{}, fmt.Errorf("reading --%s flag: %w", name, err)
		}
		*target = val
	}
	return opts, nil
}
