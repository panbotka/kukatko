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
	flags.Bool(flagForce, false, "allow a non-interactive run (a script, a cron job, an agent)")
	flags.Bool(flagOrphanSweep, false,
		"also delete objects under Kukátko's prefixes that the catalogue does not reference")
	return cmd
}

// resetFlags is the parsed flag set of one reset invocation.
type resetFlags struct {
	execute     bool
	confirm     string
	force       bool
	orphanSweep bool
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
	target, err := reset.TargetFromDSN(cfg.Database.URL)
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

	typed, err := confirmDatabaseName(cmd, flags, pre.Target.Database)
	if err != nil {
		return err
	}
	opts.Confirm = typed

	result, err := svc.Execute(cmd.Context(), opts, pre.Counts)
	printResetResult(cmd, result)
	if err != nil {
		return fmt.Errorf("resetting the library: %w", err)
	}
	return nil
}

// confirmDatabaseName obtains the typed confirmation: from --confirm-database
// when given, otherwise by prompting on the command's own streams. The value is
// returned as typed and checked by the service, which is where the comparison
// belongs — a caller that skips this prompt still cannot skip the check.
func confirmDatabaseName(cmd *cobra.Command, flags resetFlags, database string) (string, error) {
	if flags.confirm != "" {
		return flags.confirm, nil
	}
	cmd.Printf("Type the database name (%s) to confirm the wipe: ", database)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading the confirmation: %w", err)
		}
		return "", errResetNotConfirmed
	}
	typed := strings.TrimSpace(scanner.Text())
	if typed == "" {
		return "", errResetNotConfirmed
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
	printResetStoragePlan(cmd, pre.Storage)
	cmd.Println("preserved (never touched):")
	for _, table := range pre.Counts.Preserved {
		cmd.Printf("  %-22s %d\n", table.Table, table.Rows)
	}
}

// printResetStoragePlan prints the object counts per owned prefix, and — after a
// sweep — what the store actually holds and how many foreign keys it will leave
// alone.
func printResetStoragePlan(cmd *cobra.Command, plan reset.StoragePlan) {
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
