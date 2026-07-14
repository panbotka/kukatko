package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/dirimport"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
)

// errImportFailures indicates the folder import finished but at least one file
// could not be ingested. The run itself is complete and everything else was
// imported; the non-zero exit only lets a script notice.
var errImportFailures = errors.New("some files failed to import")

// errNoUploader indicates --uploader named a user that does not exist.
var errNoUploader = errors.New("no such user")

// dirImportOptions is the CLI's view of a folder import, read from the flags.
type dirImportOptions struct {
	root        string
	album       string
	labels      []string
	recursive   bool
	dryRun      bool
	noSidecars  bool
	concurrency int
	uploader    string
}

// newImportDirCmd builds the "import dir" subcommand: a recursive walk of a
// directory on disk that ingests every media file through the same pipeline as an
// upload. It is idempotent (the SHA256 dedup skips what is already in the
// library) and resumable (a run that dies leaves its imported photos behind), so
// it is always safe to run it again.
func newImportDirCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dir <path>",
		Short: "Import a directory of photos and videos from disk",
		Long: "Recursively walk a directory and ingest every media file into the library: " +
			"the original is copied into storage, its metadata and thumbnails are generated, and " +
			"the embedding and face-detection jobs are queued.\n\n" +
			"The source directory is only ever read — originals are copied, never moved or " +
			"modified. The import is idempotent and resumable: identity is the file's SHA256 " +
			"content hash, so a re-run skips everything already in the library (reporting it as a " +
			"duplicate) and imports only what is new. A file that fails is logged and the run " +
			"continues; the command exits non-zero if anything failed.\n\n" +
			"Metadata sidecars are read: a Google Photos (Takeout) export carries the capture " +
			"date, the caption and the GPS in a .json file next to a media file whose own EXIF was " +
			"stripped in re-encoding, and Apple exports carry them in a .xmp. They are matched to " +
			"their media file, folded into it (EXIF wins where it is plausible; the sidecar wins " +
			"where EXIF is missing, guessed, or bogusly dated to the export), and every sidecar " +
			"that matched nothing — and every media file that got none — is reported. No albums " +
			"are ever created from an export: use --album. Pass --no-sidecars to ignore them.\n\n" +
			"Dotfiles, @eaDir/__MACOSX, Thumbs.db/.DS_Store, sidecars (.xmp/.json/.aae/.thm) and " +
			"unsupported formats are skipped as media and counted by reason. Symlinks are skipped, " +
			"never followed. Use --dry-run to see what a run would do without writing anything.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportDir(cmd, args[0])
		},
	}
	cmd.Flags().String("album", "",
		"put every imported photo in this album, by uid or title (a title that does not exist is created)")
	cmd.Flags().StringSlice("labels", nil,
		"attach these labels to every imported photo, by name (a name that does not exist is created)")
	cmd.Flags().BoolP("recursive", "r", true, "walk nested subdirectories")
	cmd.Flags().Bool("no-recursive", false, "import only the named directory, not its subdirectories")
	cmd.Flags().Bool("dry-run", false,
		"report what would be imported (new / duplicate / skipped) without writing anything")
	cmd.Flags().Bool("no-sidecars", false,
		"ignore the metadata sidecars beside the media (a Takeout export then arrives with no dates)")
	cmd.Flags().Int("concurrency", dirimport.DefaultConcurrency,
		fmt.Sprintf("how many files to ingest in parallel (capped at %d — thumbnailing is memory-hungry)",
			dirimport.MaxConcurrency))
	cmd.Flags().String("uploader", "",
		"username owning the imported photos (default: the bootstrap admin, else any admin)")
	cmd.MarkFlagsMutuallyExclusive("recursive", "no-recursive")
	return cmd
}

// dirImportOptionsFromFlags reads the "import dir" flags into a dirImportOptions.
// --no-recursive is the negative form of --recursive (the two are mutually
// exclusive), so a flat import is spelled either way.
func dirImportOptionsFromFlags(cmd *cobra.Command, root string) (dirImportOptions, error) {
	flags := cmd.Flags()
	opts := dirImportOptions{root: root}
	var err error
	if opts.album, err = flags.GetString("album"); err != nil {
		return opts, fmt.Errorf("reading --album: %w", err)
	}
	if opts.labels, err = flags.GetStringSlice("labels"); err != nil {
		return opts, fmt.Errorf("reading --labels: %w", err)
	}
	if opts.recursive, err = flags.GetBool("recursive"); err != nil {
		return opts, fmt.Errorf("reading --recursive: %w", err)
	}
	noRecursive, err := flags.GetBool("no-recursive")
	if err != nil {
		return opts, fmt.Errorf("reading --no-recursive: %w", err)
	}
	if noRecursive {
		opts.recursive = false
	}
	if opts.dryRun, err = flags.GetBool("dry-run"); err != nil {
		return opts, fmt.Errorf("reading --dry-run: %w", err)
	}
	if opts.noSidecars, err = flags.GetBool("no-sidecars"); err != nil {
		return opts, fmt.Errorf("reading --no-sidecars: %w", err)
	}
	if opts.concurrency, err = flags.GetInt("concurrency"); err != nil {
		return opts, fmt.Errorf("reading --concurrency: %w", err)
	}
	if opts.uploader, err = flags.GetString("uploader"); err != nil {
		return opts, fmt.Errorf("reading --uploader: %w", err)
	}
	return opts, nil
}

// runImportDir loads the configuration, opens the database (applying migrations),
// resolves the uploading user, and runs one folder import, printing a line per
// file and a final summary. It returns errImportFailures when any file failed, so
// a script driving the import can tell.
//
// Ctrl-C cancels the run cleanly: the files already imported stay in the library,
// the run is recorded as failed, and re-running finishes the rest.
func runImportDir(cmd *cobra.Command, root string) error {
	opts, err := dirImportOptionsFromFlags(cmd, root)
	if err != nil {
		return err
	}
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	uploader, err := resolveUploader(ctx, cfg, auth.NewStore(db.Pool()), opts.uploader)
	if err != nil {
		return err
	}
	svc, err := buildDirImportService(cfg, db, opts.concurrency)
	if err != nil {
		return err
	}
	return executeImportDir(ctx, cmd, svc, opts, uploader)
}

// executeImportDir runs the import and prints its progress and summary. It
// returns errImportFailures when the run completed with per-file failures, and
// the run's own error otherwise (an unreadable root, an interrupted run). ctx
// carries the signal-aware cancellation; cmd only carries the command's I/O.
func executeImportDir(
	ctx context.Context, cmd *cobra.Command, svc *dirimport.Service, opts dirImportOptions, uploader string,
) error {
	started := time.Now()
	result, err := svc.Import(ctx, dirimport.Options{
		Root:       opts.root,
		Recursive:  opts.recursive,
		DryRun:     opts.dryRun,
		NoSidecars: opts.noSidecars,
		Album:      opts.album,
		Labels:     opts.labels,
		UploadedBy: uploader,
		Progress:   func(res dirimport.FileResult, done, total int) { printFileResult(cmd, res, done, total) },
	})
	if err != nil {
		printImportSummary(cmd, result, time.Since(started))
		return fmt.Errorf("importing %s: %w", opts.root, err)
	}
	printImportSummary(cmd, result, time.Since(started))
	if result.Counts.Failed > 0 {
		return fmt.Errorf("%w: %d of %d", errImportFailures, result.Counts.Failed, result.Counts.Total())
	}
	return nil
}

// printFileResult prints one file's outcome as it is decided, so a long import is
// readable while it runs.
func printFileResult(cmd *cobra.Command, res dirimport.FileResult, done, total int) {
	prefix := fmt.Sprintf("[%*d/%d] %-9s %s", len(strconv.Itoa(total)), done, total, res.Outcome, res.Path)
	switch res.Outcome {
	case dirimport.OutcomeDuplicate:
		cmd.Printf("%s%s%s\n", prefix, duplicateSuffix(res.ExistingPath), sidecarSuffix(res))
	case dirimport.OutcomeSkipped:
		cmd.Printf("%s (%s)\n", prefix, res.Reason)
	case dirimport.OutcomeFailed:
		cmd.Printf("%s: %v\n", prefix, res.Err)
	case dirimport.OutcomeImported:
		cmd.Printf("%s%s%s\n", prefix, warningSuffix(res.Warnings), sidecarSuffix(res))
	}
}

// sidecarSuffix names the metadata sidecar that travelled with a file — or says
// why it did not, which is the difference between a photo that kept its date and
// one that quietly lost it.
func sidecarSuffix(res dirimport.FileResult) string {
	switch {
	case res.Sidecar == "":
		return ""
	case res.SidecarErr != nil:
		return fmt.Sprintf(" (sidecar %s unreadable: %v)", res.Sidecar, res.SidecarErr)
	default:
		return fmt.Sprintf(" (sidecar: %s)", res.Sidecar)
	}
}

// warningSuffix names what the pipeline complained about while still creating the
// photo (an undecodable file gets its original stored but no thumbnail). The
// import is unattended, so the codes belong in the report, not only in the log.
func warningSuffix(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return fmt.Sprintf(" (warnings: %s)", strings.Join(warnings, ", "))
}

// duplicateSuffix names the library file a duplicate collided with, so the user
// can see the same bytes already arrived under a different name.
func duplicateSuffix(existingPath string) string {
	if existingPath == "" {
		return ""
	}
	return fmt.Sprintf(" (already in the library as %s)", existingPath)
}

// printImportSummary prints the final tally: the counts, the breakdown of what was
// skipped and why, and — for a real run — the reminder that the embedding and face
// jobs are queued and drain whenever the embedding service is reachable.
func printImportSummary(cmd *cobra.Command, result dirimport.Result, elapsed time.Duration) {
	counts := result.Counts
	label := fmt.Sprintf("folder import run %d", result.RunID)
	if result.DryRun {
		label = "folder import (dry run — nothing was written)"
	}
	cmd.Printf("%s: imported=%d duplicates=%d skipped=%d failed=%d in %s\n",
		label, counts.Imported, counts.Duplicates, counts.Skipped, counts.Failed,
		elapsed.Round(time.Millisecond))
	if breakdown := skipBreakdown(counts.ByReason); breakdown != "" {
		cmd.Printf("skipped: %s\n", breakdown)
	}
	printSidecarSummary(cmd, result.Sidecars)
	if !result.DryRun && counts.Imported > 0 {
		cmd.Println("embedding and face-detection jobs are queued in Postgres; " +
			"they drain when the embedding service is reachable")
	}
}

// maxListed caps how many paths a summary list prints before it says how many
// more there are: a Takeout export with a thousand unmatched sidecars needs to
// say so, not to say it a thousand times.
const maxListed = 10

// printSidecarSummary reports what became of the metadata sidecars: how many were
// applied, and — file by file — every one that matched nothing, could not be read,
// or was missing where its neighbours had one. Those three lists are the whole
// point of reading sidecars at all: they are where a lost capture date shows up
// while it can still be fixed, rather than years later in an empty timeline.
func printSidecarSummary(cmd *cobra.Command, report dirimport.SidecarReport) {
	if report.Matched == 0 && len(report.Orphans) == 0 && len(report.Missing) == 0 {
		return
	}
	cmd.Printf("sidecars: matched=%d applied=%d unreadable=%d unmatched=%d media-without-sidecar=%d\n",
		report.Matched, report.Applied, len(report.Unreadable), len(report.Orphans), len(report.Missing))
	printPathList(cmd, "sidecar could not be read", report.Unreadable)
	printPathList(cmd, "sidecar matched no media file", report.Orphans)
	printPathList(cmd, "media file has no sidecar", report.Missing)
}

// printPathList prints up to maxListed paths under a label, then how many were
// left unsaid.
func printPathList(cmd *cobra.Command, label string, paths []string) {
	for i, path := range paths {
		if i == maxListed {
			cmd.Printf("  %s: … and %d more\n", label, len(paths)-maxListed)
			return
		}
		cmd.Printf("  %s: %s\n", label, path)
	}
}

// skipBreakdown renders the skip tally as a stable, alphabetically ordered
// "reason=n" list, or "" when nothing was skipped.
func skipBreakdown(byReason map[dirimport.SkipReason]int) string {
	parts := make([]string, 0, len(byReason))
	for reason, n := range byReason {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// resolveUploader returns the UID of the user the imported photos belong to.
// A named user must exist (errNoUploader otherwise); with no --uploader the
// configured bootstrap admin is used, and failing that any admin account. An
// instance with no admin at all imports with no owner (uploaded_by stays NULL),
// which keeps a fresh, unbootstrapped install usable.
func resolveUploader(
	ctx context.Context, cfg *config.Config, store *auth.Store, username string,
) (string, error) {
	if username != "" {
		user, err := store.GetUserByUsername(ctx, username)
		if err != nil {
			return "", fmt.Errorf("%w: %s: %w", errNoUploader, username, err)
		}
		return user.UID, nil
	}
	if bootstrap := cfg.Auth.BootstrapAdminUsername; bootstrap != "" {
		if user, err := store.GetUserByUsername(ctx, bootstrap); err == nil {
			return user.UID, nil
		}
	}
	users, err := store.ListUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("listing users: %w", err)
	}
	for i := range users {
		if users[i].Role == auth.RoleAdmin && !users[i].Disabled {
			return users[i].UID, nil
		}
	}
	return "", nil
}

// buildDirImportService assembles the folder-import pipeline over the shared
// pool: the ingest service (the very same one the upload endpoint uses, wired to
// the configured original store, thumbnailer and persistent job queue), the
// import-run store, the photo catalogue and the album/label catalogues.
func buildDirImportService(cfg *config.Config, db *database.DB, concurrency int) (*dirimport.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	pool := db.Pool()
	photoStore := photos.NewStore(pool)
	organizeStore := organize.NewStore(pool)
	ingestSvc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, nil)...),
		Enqueuer:    jobs.NewEnqueuer(jobs.NewStore(pool)),
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
	})
	return dirimport.New(dirimport.Config{
		Ingest:      ingestSvc,
		Runs:        importer.NewStore(pool),
		Photos:      photoStore,
		Filler:      photoStore,
		Curation:    organizeStore,
		Albums:      organizeStore,
		Labels:      organizeStore,
		Concurrency: concurrency,
	}), nil
}
