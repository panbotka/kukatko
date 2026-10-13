package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/reset"
)

// Flag names of the purge subcommand.
const (
	flagPurgeYes            = "yes"
	flagPurgeConfirm        = "confirm"
	flagPurgeAllowBootstrap = "allow-bootstrap"
	flagPurgeReport         = "report"
)

// purgeReportPerm is the mode of a written purge report. It holds no secrets,
// but it names the project and what it held, so it stays the operator's.
const purgeReportPerm = 0o600

// purgeLong is the purge subcommand's help text.
const purgeLong = `Remove one project of this instance outright: every catalogue row of it
(photos and everything hanging off them, people, albums, labels, tasks, upload
links, saved searches, notifications, import history and its queued jobs), its
memberships and invitations, its settings and announcement, the project row
itself, and every object under its storage prefix (projects/<uid>/) — originals,
sidecars, thumbnails, streaming segments and its families.yaml — plus its tree in
the local cache. Every other project is left exactly as it was.

The accounts are kept: a member of the purged project stays an account of the
instance. The audit trail is kept too, and gains a project.purge entry.

Guards:

  * Without --yes nothing is deleted. The default run prints, on stderr, the row
    count per table and the number and size of the objects under the prefix.
  * --yes needs the slug typed again as --confirm <slug>.
  * The bootstrap project is refused unless --allow-bootstrap is passed.
  * A project whose storage prefix is empty (it lives in the store root, as the
    bootstrap project does until its storage move) is refused outright: its
    "prefix" would be the whole store.
  * The rows, the project row and the audit entry are deleted in one
    transaction; the objects are deleted only after it has committed.

After the run a JSON report of what was deleted is written to --report (by
default project-purge-<slug>-<time>.json in the current directory).

Deactivate the project first, so nobody uploads into it while it is removed. A
running job of the project is left to fail on its own.`

// newProjectCmd builds the "project" command group: instance-level operations on
// one project that have no place in the HTTP API.
func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Operate on one project of this instance",
	}
	cmd.AddCommand(newProjectPurgeCmd())
	return cmd
}

// newProjectPurgeCmd builds "project purge <slug>": the guarded removal of one
// project and everything it owns.
func newProjectPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge <slug>",
		Short: "Remove one project — its catalogue, members and stored objects (DESTRUCTIVE)",
		Long:  purgeLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectPurge(cmd, args[0])
		},
	}
	flags := cmd.Flags()
	flags.Bool(flagPurgeYes, false, "actually delete; without it the run is a dry run that changes nothing")
	flags.String(flagPurgeConfirm, "", "the project's slug typed again (required with --yes)")
	flags.Bool(flagPurgeAllowBootstrap, false, "allow purging the bootstrap project")
	flags.String(flagPurgeReport, "",
		"path of the JSON report written after the purge (default project-purge-<slug>-<time>.json)")
	return cmd
}

// purgeFlags is the parsed flag set of one purge invocation.
type purgeFlags struct {
	yes            bool
	confirm        string
	allowBootstrap bool
	report         string
}

// errPurgeConfirmMissing is returned for --yes without --confirm, before any
// connection is opened.
var errPurgeConfirmMissing = errors.New("--yes requires --confirm <slug>: type the project's slug again")

// readPurgeFlags parses the subcommand's flags.
func readPurgeFlags(cmd *cobra.Command) (purgeFlags, error) {
	var (
		parsed purgeFlags
		err    error
	)
	if parsed.yes, err = cmd.Flags().GetBool(flagPurgeYes); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagPurgeYes, err)
	}
	if parsed.confirm, err = cmd.Flags().GetString(flagPurgeConfirm); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagPurgeConfirm, err)
	}
	if parsed.allowBootstrap, err = cmd.Flags().GetBool(flagPurgeAllowBootstrap); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagPurgeAllowBootstrap, err)
	}
	if parsed.report, err = cmd.Flags().GetString(flagPurgeReport); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagPurgeReport, err)
	}
	if parsed.yes && parsed.confirm == "" {
		return parsed, errPurgeConfirmMissing
	}
	return parsed, nil
}

// runProjectPurge plans the purge of slug, prints the plan and — only with --yes
// and the slug confirmed — executes it, prints the outcome and writes the report.
func runProjectPurge(cmd *cobra.Command, slug string) error {
	flags, err := readPurgeFlags(cmd)
	if err != nil {
		return err
	}
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	purger, err := buildPurger(cfg, db)
	if err != nil {
		return err
	}
	return runPurge(cmd, purger, slug, flags)
}

// buildPurger assembles the purge over the configured store, the local cache
// and the pool of the database the config names.
func buildPurger(cfg *config.Config, db *database.DB) (*reset.Purger, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	purgeStore, ok := store.(reset.PurgeStore)
	if !ok {
		return nil, fmt.Errorf("storage backend %q cannot list its objects with their sizes", cfg.Storage.Backend)
	}
	return reset.NewPurger(reset.PurgeConfig{
		Pool: db.Pool(), Store: purgeStore, CacheDir: cfg.Storage.CachePath,
	}), nil
}

// runPurge drives the plan, the guard and the purge itself over purger.
func runPurge(cmd *cobra.Command, purger *reset.Purger, slug string, flags purgeFlags) error {
	opts := reset.PurgeOptions{
		Slug:           slug,
		Execute:        flags.yes,
		Confirm:        flags.confirm,
		AllowBootstrap: flags.allowBootstrap,
		Operator:       operatorIdentity(),
	}
	plan, err := purger.Plan(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("planning the purge: %w", err)
	}
	printPurgePlan(cmd, plan)
	if !flags.yes {
		cmd.PrintErrf("dry run: nothing was deleted — re-run with --yes --confirm %s to purge the project\n", slug)
		return nil
	}
	result, err := purger.Execute(cmd.Context(), opts, plan)
	if result.PurgedAt.IsZero() {
		// The database half did not commit: nothing was deleted, nothing to report.
		return fmt.Errorf("purging the project: %w", err)
	}
	printPurgeResult(cmd, result)
	path, writeErr := writePurgeReport(flags.report, result)
	if writeErr != nil {
		return errors.Join(err, writeErr)
	}
	cmd.PrintErrf("report written to %s\n", path)
	if err != nil {
		return fmt.Errorf("purging the project's objects: %w", err)
	}
	return nil
}

// printPurgePlan prints, on stderr, the project a purge is about and what it
// would delete: the rows per table and the objects under its prefix.
func printPurgePlan(cmd *cobra.Command, plan reset.PurgePlan) {
	cmd.PrintErrf("project: %s (%s) uid %s, storage prefix %s\n",
		plan.Project.Slug, plan.Project.Name, plan.Project.UID, plan.Project.StoragePrefix)
	cmd.PrintErrf("database: %d row(s) would be deleted (rows of tables below these go with them)\n", plan.Rows())
	for _, table := range plan.Tables {
		cmd.PrintErrf("  %-22s %d\n", table.Table, table.Rows)
	}
	cmd.PrintErrf("store: %d object(s), %s under %s would be deleted\n",
		plan.Objects, humanBytes(plan.Bytes), plan.Project.StoragePrefix)
}

// printPurgeResult prints, on stderr, what the purge did to the store.
func printPurgeResult(cmd *cobra.Command, result reset.PurgeResult) {
	stored := result.Storage
	cmd.PrintErrf("database: project %s and its %d row(s) deleted\n", result.Plan.Project.Slug, result.Plan.Rows())
	cmd.PrintErrf("store: %d object(s) deleted, %d already absent, %d failed\n",
		stored.Deleted, stored.Missing, stored.Failed)
	for _, failure := range stored.Failures {
		cmd.PrintErrf("  - %s\n", failure)
	}
	if result.CacheRemoved {
		cmd.PrintErrln("local cache: the project's tree removed")
	}
}

// writePurgeReport writes result as indented JSON to path, or to the default
// project-purge-<slug>-<time>.json in the current directory when path is empty,
// and returns the path written.
func writePurgeReport(path string, result reset.PurgeResult) (string, error) {
	if path == "" {
		path = fmt.Sprintf("project-purge-%s-%s.json",
			result.Plan.Project.Slug, result.PurgedAt.UTC().Format("20060102T150405Z"))
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return path, fmt.Errorf("encoding the purge report: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), purgeReportPerm); err != nil {
		return path, fmt.Errorf("writing the purge report %s: %w", path, err)
	}
	return path, nil
}
