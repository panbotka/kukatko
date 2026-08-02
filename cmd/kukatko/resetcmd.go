package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/reset"
	"github.com/panbotka/kukatko/internal/thumb"
)

// Flag names of the reset subcommand, named once so the definitions and the
// reads cannot drift.
const (
	flagExecute         = "execute"
	flagConfirmDatabase = "confirm-database"
	flagConfirmBucket   = "confirm-bucket"
	flagForce           = "force"
	flagOrphanSweep     = "orphan-sweep"
)

// Errors the reset subcommand ends on before it touches anything.
var (
	// errResetNotInteractive indicates a wipe was requested from something that
	// is not a terminal — a script, a cron job, an agent — without the explicit
	// --force that says the caller meant it. It is checked before the config is
	// even loaded, so a stray invocation ends having done nothing at all.
	errResetNotInteractive = errors.New(
		"refusing to reset from a non-interactive session without --force " +
			"(pass --force and --confirm-database <name> if you really mean it)")
	// errResetNotConfirmed indicates the operator typed nothing at the prompt.
	errResetNotConfirmed = errors.New("no database name typed; nothing was deleted")
	// errResetBucketNotConfirmed indicates the operator typed nothing at the bucket
	// prompt. The database name alone does not confirm a bucket: the two are
	// configured separately and can point at different deployments.
	errResetBucketNotConfirmed = errors.New("no bucket name typed; nothing was deleted")
)

// resetLong is the subcommand's help text. It is long on purpose: this is the
// one command in the binary that destroys the library, and the help is where an
// operator finds out what it does before running it rather than after.
const resetLong = `Empty this Kukátko instance's library: every catalogue table (photos, files,
albums, labels, people, faces, embeddings, places, edits, import history, the
job queue and the per-user curation) and every object the configured store owns
(the YYYY/MM originals, the thumb/ thumbnails and the sidecars/ metadata).

It is phase 1 of docs/MIGRATION_PLAN.md — the wipe that precedes a full
re-import — and it is the only command here that deletes the library on
purpose. There is no S3 backup of this deployment (docs/READINESS_AUDIT.md §4),
so the only way back is to re-import from PhotoPrism.

It never touches the accounts (users, sessions, API tokens), the announcement,
the audit trail or the migration history — a wipe must not lock you out of the
instance you just wiped, nor erase the record of the wipe. It has no client of
PhotoPrism or photo-sorter: the source libraries are read-only and are the
rollback.

Guards, all of them on by default:

  * Nothing is deleted without --execute. The default run prints a row count per
    table and an object count per prefix, and stops.
  * You must type the target database's name; y/N is not a confirmation.
  * On a bucket-backed store you must type the configured bucket's name too. The
    database and the bucket come from separate config keys and can name separate
    deployments — a development database pointed at the production bucket is the
    accident this refuses — so confirming one says nothing about the other.
  * The connected database must be the one the loaded config names, checked
    against the server, and printed before you are asked.
  * A non-interactive run (a script, a cron job, an agent) is refused unless
    --force is passed as well.
  * Deletion in the store is confined to the prefixes this application writes.
    An object the catalogue never referenced is deleted only with
    --orphan-sweep, and an object outside those prefixes is never deleted at
    all — it is counted and reported.
  * The truncation and its audit entry commit in one transaction.

Stop the server first. The wipe empties the job queue and the catalogue the
running instance is serving; a worker mid-job would keep writing rows into a
library that no longer exists.

Use --orphan-sweep for the cutover wipe. Without it the catalogue is the list of
what may be deleted, which leaves behind whatever an earlier interrupted import
put in the bucket — and on a bucket the sweep is also the faster path, since it
deletes the objects that are there instead of probing for the ones that might
be.`

// newMaintenanceResetCmd builds the "maintenance reset" subcommand: the guarded
// wipe of the whole library.
func newMaintenanceResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Empty the library — every catalogue table and every stored object (DESTRUCTIVE)",
		Long:  resetLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMaintenanceReset(cmd)
		},
	}
	flags := cmd.Flags()
	flags.Bool(flagExecute, false, "actually delete; without it the run is a dry run that changes nothing")
	flags.String(flagConfirmDatabase, "",
		"the target database's name, instead of typing it at the prompt (required for a non-interactive run)")
	flags.String(flagConfirmBucket, "",
		"the configured bucket's name, instead of typing it at the prompt (required for a non-interactive run "+
			"on a bucket-backed store)")
	flags.Bool(flagForce, false, "allow a non-interactive run (a script, a cron job, an agent)")
	flags.Bool(flagOrphanSweep, false,
		"also delete objects under Kukátko's prefixes that the catalogue does not reference")
	return cmd
}

// resetFlags is the parsed flag set of one reset invocation.
type resetFlags struct {
	execute      bool
	confirm      string
	confirmStore string
	force        bool
	orphanSweep  bool
}

// readResetFlags parses the subcommand's flags, wrapping any lookup failure with
// the flag it came from.
func readResetFlags(cmd *cobra.Command) (resetFlags, error) {
	var (
		parsed resetFlags
		err    error
	)
	if parsed.execute, err = cmd.Flags().GetBool(flagExecute); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagExecute, err)
	}
	if parsed.confirm, err = cmd.Flags().GetString(flagConfirmDatabase); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagConfirmDatabase, err)
	}
	if parsed.confirmStore, err = cmd.Flags().GetString(flagConfirmBucket); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagConfirmBucket, err)
	}
	if parsed.force, err = cmd.Flags().GetBool(flagForce); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagForce, err)
	}
	if parsed.orphanSweep, err = cmd.Flags().GetBool(flagOrphanSweep); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagOrphanSweep, err)
	}
	return parsed, nil
}

// runMaintenanceReset runs the guarded wipe: it checks the flags that can be
// checked before anything is opened, loads the config, verifies the target,
// prints what would be deleted, and — only with --execute and a typed database
// name — deletes it and prints a before/after summary.
func runMaintenanceReset(cmd *cobra.Command) error {
	flags, err := readResetFlags(cmd)
	if err != nil {
		return err
	}
	// The non-interactive guard comes first, before the config is loaded and
	// before any connection is opened: a stray invocation from a script must end
	// having touched nothing.
	if flags.execute && !flags.force && !interactiveInput(cmd.InOrStdin()) {
		return errResetNotInteractive
	}
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	target, err := reset.TargetFromConfig(cfg.Database.URL, configuredBucket(cfg))
	if err != nil {
		return fmt.Errorf("resolving the reset target: %w", err)
	}

	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	svc, err := buildResetService(cfg, db, target)
	if err != nil {
		return err
	}
	return runReset(cmd, svc, flags)
}

// configuredBucket returns the bucket the configured store writes to, or the
// empty string for a backend that has none. It is the name the operator has to
// type before a wipe, so it is read from the same key newStorage builds the store
// from — a bucket nobody is about to empty must never end up being confirmed.
func configuredBucket(cfg *config.Config) string {
	if cfg.Storage.Backend != config.StorageBackendR2 {
		return ""
	}
	return cfg.Storage.R2.Bucket
}

// buildResetService assembles the wipe over the configured originals store, the
// local thumbnail cache and the pool of the database the config names.
func buildResetService(cfg *config.Config, db *database.DB, target reset.Target) (*reset.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	return reset.New(reset.Config{
		Pool:     db.Pool(),
		Target:   target,
		Storage:  store,
		Thumbs:   thumb.New(store, cfg.Storage.CachePath),
		CacheDir: cfg.Storage.CachePath,
	}), nil
}

// runReset drives the preflight, the confirmation and the wipe itself.
func runReset(cmd *cobra.Command, svc *reset.Service, flags resetFlags) error {
	opts := reset.Options{
		Execute:     flags.execute,
		OrphanSweep: flags.orphanSweep,
		Operator:    operatorIdentity(),
	}
	pre, err := svc.Preflight(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("preparing the reset: %w", err)
	}
	printResetPreflight(cmd, pre)
	if !flags.execute {
		cmd.Println("dry run: nothing was deleted — re-run with --execute to wipe the library")
		return nil
	}

	if opts.Confirm, opts.ConfirmBucket, err = confirmTarget(cmd, flags, pre.Target); err != nil {
		return err
	}

	result, err := svc.Execute(cmd.Context(), opts, pre.Counts)
	printResetResult(cmd, result)
	if err != nil {
		return fmt.Errorf("resetting the library: %w", err)
	}
	return nil
}

// confirmTarget obtains both typed confirmations — the database's name and, on a
// bucket-backed store, the bucket's — and returns them as typed. The comparison
// belongs to the service, which is where a caller that skipped these prompts
// still meets it.
//
// Both prompts read through ONE scanner: bufio reads ahead, so a second scanner
// over the same stream would start after the bytes the first one already buffered
// and find nothing where the second answer is.
func confirmTarget(cmd *cobra.Command, flags resetFlags, target reset.Target) (database, bucket string, err error) {
	input := bufio.NewScanner(cmd.InOrStdin())
	if database, err = confirmDatabaseName(cmd, flags, input, target.Database); err != nil {
		return "", "", err
	}
	if bucket, err = confirmBucketName(cmd, flags, input, target.Bucket); err != nil {
		return "", "", err
	}
	return database, bucket, nil
}

// confirmDatabaseName obtains the typed confirmation of the target database:
// from --confirm-database when given, otherwise by prompting on the command's own
// streams.
func confirmDatabaseName(
	cmd *cobra.Command, flags resetFlags, input *bufio.Scanner, database string,
) (string, error) {
	if flags.confirm != "" {
		return flags.confirm, nil
	}
	return promptForName(cmd, input,
		fmt.Sprintf("Type the database name (%s) to confirm the wipe: ", database), errResetNotConfirmed)
}

// confirmBucketName obtains the typed confirmation of the object store's bucket:
// from --confirm-bucket when given, otherwise by prompting. A store with no
// bucket asks nothing and returns whatever the flag held — including a name typed
// against a store that has none, which the service then refuses rather than
// quietly ignores.
func confirmBucketName(
	cmd *cobra.Command, flags resetFlags, input *bufio.Scanner, bucket string,
) (string, error) {
	if bucket == "" || flags.confirmStore != "" {
		return flags.confirmStore, nil
	}
	return promptForName(cmd, input,
		fmt.Sprintf("Type the bucket name (%s) to confirm the wipe: ", bucket), errResetBucketNotConfirmed)
}

// promptForName writes question to the command's output and reads one trimmed
// line from input, returning empty as the given error so an operator who just
// pressed Enter ends the run instead of confirming it.
func promptForName(cmd *cobra.Command, scanner *bufio.Scanner, question string, empty error) (string, error) {
	cmd.Print(question)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading the confirmation: %w", err)
		}
		return "", empty
	}
	typed := strings.TrimSpace(scanner.Text())
	if typed == "" {
		return "", empty
	}
	return typed, nil
}

// interactiveInput reports whether in is a terminal a human can type into.
// Anything else — a pipe, a file, a test buffer, a cron job's /dev/null — is a
// non-interactive session, which the wipe refuses without --force.
//
// The test is a terminal ioctl rather than the usual "is it a character device",
// because /dev/null is a character device too — and /dev/null is precisely what
// cron, a systemd unit and a spawned agent hand a process as stdin. Reading it as
// a terminal would switch the guard off in the one case it exists for.
func interactiveInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	//nolint:gosec // G115: a file descriptor is a small non-negative integer; it cannot overflow an int.
	_, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	return err == nil
}

// unknownIdentity stands in for a user or host the process cannot name. It is
// recorded as-is rather than left blank, so the audit entry says "this ran
// somewhere that could not identify itself" instead of saying nothing.
const unknownIdentity = "unknown"

// operatorIdentity names who is running the wipe for the audit trail. A CLI run
// has no session to attribute, so the OS user and the host are the strongest
// identity available.
func operatorIdentity() string {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		user = unknownIdentity
	}
	host, err := os.Hostname()
	if err != nil {
		host = unknownIdentity
	}
	return user + "@" + host
}

// printResetPreflight prints the target, then what would be deleted: one line
// per non-empty catalogue table, the objects per owned prefix, and what stays.
func printResetPreflight(cmd *cobra.Command, pre reset.Preflight) {
	cmd.Printf("target: %s (server reports database %q at %s:%d)\n",
		pre.Target, pre.Connection.Database, serverAddrOrSocket(pre.Connection.ServerAddr), pre.Connection.ServerPort)
	cmd.Printf("catalogue: %d row(s) in %d table(s) would be deleted\n",
		pre.Counts.Rows(), len(pre.Counts.Catalogue))
	for _, table := range pre.Counts.Catalogue {
		if table.Rows > 0 {
			cmd.Printf("  %-22s %d\n", table.Table, table.Rows)
		}
	}
	printResetStoragePlan(cmd, pre.Target, pre.Storage)
	cmd.Println("preserved (never touched):")
	for _, table := range pre.Counts.Preserved {
		cmd.Printf("  %-22s %d\n", table.Table, table.Rows)
	}
}

// printResetStoragePlan prints which store is about to be emptied, the object
// counts per owned prefix, and — after a sweep — what the store actually holds
// and how many foreign keys it will leave alone.
func printResetStoragePlan(cmd *cobra.Command, target reset.Target, plan reset.StoragePlan) {
	if target.Bucket != "" {
		cmd.Printf("store: bucket %s\n", target.Bucket)
	} else {
		cmd.Println("store: local filesystem (no bucket)")
	}
	cmd.Printf("store (catalogue-referenced keys): %d original(s), %d thumbnail(s), %d sidecar(s)\n",
		plan.Referenced.Originals, plan.Referenced.Thumbnails, plan.Referenced.Sidecars)
	if !plan.Sweep {
		cmd.Println("  orphan sweep off: only the keys above would be deleted")
		return
	}
	cmd.Printf("  orphan sweep on: the store holds %d original(s), %d thumbnail(s), %d sidecar(s)"+
		" under Kukátko's prefixes — all of them would be deleted\n",
		plan.Stored.Originals, plan.Stored.Thumbnails, plan.Stored.Sidecars)
	cmd.Printf("  %d key(s) outside those prefixes would be left untouched\n", plan.Foreign)
}

// printResetResult prints the before/after summary, so the outcome is verifiable
// rather than assumed, and names any table that survived the truncation.
func printResetResult(cmd *cobra.Command, result reset.Result) {
	stored := result.Storage
	if !stored.Touched() && result.After.Catalogue == nil {
		// A guard stopped the run before it did anything; an all-zero summary would
		// only bury the error that follows it.
		return
	}
	cmd.Printf("store: %d object(s) deleted, %d already absent, %d skipped, %d failed, %d foreign left alone\n",
		stored.Deleted, stored.Missing, stored.Skipped, stored.Failed, stored.Foreign)
	for _, failure := range stored.Failures {
		cmd.Printf("  - %s\n", failure)
	}
	if stored.ThumbCacheSwept {
		cmd.Println("local thumbnail cache: removed")
	} else if stored.ThumbCacheCleared > 0 {
		cmd.Printf("local thumbnail cache: cleared for %d file hash(es)\n", stored.ThumbCacheCleared)
	}
	if result.After.Catalogue == nil {
		return
	}
	cmd.Printf("catalogue: %d row(s) before, %d row(s) after\n", result.Before.Rows(), result.After.Rows())
	for _, table := range result.After.NonEmpty() {
		cmd.Printf("  WARNING: %s still holds %d row(s)\n", table.Table, table.Rows)
	}
	cmd.Printf("preserved: %s\n", preservedSummary(result.After))
}

// preservedSummary renders the preserved tables' counts on one line, the proof
// that the accounts and the audit trail are still there.
func preservedSummary(counts reset.Counts) string {
	parts := make([]string, 0, len(counts.Preserved))
	for _, table := range counts.Preserved {
		parts = append(parts, fmt.Sprintf("%s=%d", table.Table, table.Rows))
	}
	return strings.Join(parts, " ")
}

// serverAddrOrSocket renders an empty server address — what Postgres reports over
// a Unix socket — as something an operator can read.
func serverAddrOrSocket(addr string) string {
	if addr == "" {
		return "local socket"
	}
	return addr
}
