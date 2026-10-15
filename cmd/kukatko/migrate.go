package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
)

// errSchemaBehind is returned by every subcommand other than `migrate` and
// `serve` that finds the database short of a migration this binary embeds.
// Only those two change the schema: an operator who runs, say, `maintenance
// scan` with a new image while the old release still serves the database must
// not break that release as a side effect.
var errSchemaBehind = errors.New("the database schema is older than this binary")

// errOtherClients is returned by `migrate` when the database is behind and
// other sessions are connected to it — most likely the release being replaced,
// which the migration would break under it — and --force was not given.
var errOtherClients = errors.New("other clients are connected to the database")

// newMigrateCmd builds the "migrate" subcommand, which opens the configured
// database, applies any pending embedded SQL migrations in order, prints a
// summary, and exits. It is the standalone counterpart to the automatic
// migration that "serve" runs on startup, and with it the only command that
// changes the schema.
func newMigrateCmd() *cobra.Command {
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations and exit",
		Long: "Open the configured database and apply any pending embedded SQL migrations in order. " +
			"Only `migrate` and `serve` change the schema; every other subcommand refuses to run " +
			"against a database that is behind.\n\n" +
			"When migrations are pending, migrate first looks for other sessions on the database: " +
			"any session announcing a Kukátko application_name, and any session of the same database " +
			"user (Kukátko 0.31.0 and older announce none). If there is one it lists them and refuses — " +
			"stop the old server first, then migrate. --force migrates anyway (an unrelated client of " +
			"the same user, say an admin's psql).",
		Args: cobra.NoArgs,
		RunE: runMigrate,
	}
	migrateCmd.Flags().Bool("force", false,
		"migrate even though other clients are connected to the database (stop the old server first)")
	return migrateCmd
}

// runMigrate is the migrate command: open the database, refuse while other
// clients are connected to a database that is behind (unless --force), then
// apply the pending migrations and report them.
func runMigrate(cmd *cobra.Command, _ []string) error {
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return fmt.Errorf("reading --force flag: %w", err)
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

	if err := checkOtherClients(cmd.Context(), cmd, db, force); err != nil {
		return err
	}
	applied, err := db.Migrate(cmd.Context())
	if err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	reportMigrations(cmd, applied)
	return nil
}

// migrationTarget is the slice of *database.DB the pre-migration checks need:
// where the schema stands and who else is connected.
type migrationTarget interface {
	SchemaStatus(ctx context.Context) (database.SchemaStatus, error)
	OtherClients(ctx context.Context) ([]database.Client, error)
}

// checkOtherClients is migrate's guard: on a database with pending migrations
// it lists every other client on stderr and returns errOtherClients unless
// force is set. An up-to-date database is not checked at all — migrate has
// nothing to change there.
func checkOtherClients(ctx context.Context, cmd *cobra.Command, db migrationTarget, force bool) error {
	status, clients, err := otherClientsIfBehind(ctx, db)
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		return nil
	}
	cmd.PrintErrf("database is at schema %d and %d migration(s) would bring it to %d, "+
		"but %d other client(s) are connected to it:\n",
		status.Current, len(status.Pending), status.Expected, len(clients))
	for _, c := range clients {
		cmd.PrintErrf("  - %s\n", c)
	}
	if force {
		cmd.PrintErrln("--force: migrating anyway")
		return nil
	}
	return fmt.Errorf("%w: an older server still on it breaks once the schema changes; "+
		"stop it and run `kukatko migrate` again, or pass --force if they are not Kukátko", errOtherClients)
}

// otherClientsIfBehind returns the schema status and — only when the database
// is behind — the other clients connected to it.
func otherClientsIfBehind(ctx context.Context, db migrationTarget) (database.SchemaStatus, []database.Client, error) {
	status, err := db.SchemaStatus(ctx)
	if err != nil {
		return status, nil, fmt.Errorf("checking the database schema: %w", err)
	}
	if !status.Behind() {
		return status, nil, nil
	}
	clients, err := db.OtherClients(ctx)
	if err != nil {
		return status, nil, fmt.Errorf("looking for other database clients: %w", err)
	}
	return status, clients, nil
}

// warnOtherClients is serve's counterpart to checkOtherClients: it never stops
// the start (a compose deploy stops the old container before starting the new
// one, so normally nobody else is there, and a server that refused would only
// restart into the same refusal), but on a database with pending migrations it
// logs every other client at error level, so a migration run under a live old
// server is in the log next to the 500s it causes. A failing check is logged
// and ignored for the same reason.
func warnOtherClients(ctx context.Context, logger *slog.Logger, db migrationTarget) {
	status, clients, err := otherClientsIfBehind(ctx, db)
	if err != nil {
		logger.WarnContext(ctx, "could not check for other clients before migrating", "error", err)
		return
	}
	if len(clients) == 0 {
		return
	}
	logger.ErrorContext(ctx, "migrating a database other clients are connected to; "+
		"an older Kukátko still serving it will fail once the schema changes — stop it",
		"schema", status.Current, "expected", status.Expected,
		"pending", len(status.Pending), "clients", len(clients))
	for _, c := range clients {
		logger.ErrorContext(ctx, "other database client", "client", c.String(), "kukatko", c.Kukatko())
	}
}

// startMigrator is the slice of *database.DB serve's start-up migration needs.
type startMigrator interface {
	migrationTarget
	Migrate(ctx context.Context) ([]string, error)
}

// migrateOnStart is serve's migration: warn about other clients connected to a
// database that is behind (warnOtherClients), then apply the pending
// migrations. Serve is, with `migrate`, the only command that migrates.
func migrateOnStart(ctx context.Context, logger *slog.Logger, db startMigrator) error {
	warnOtherClients(ctx, logger, db)
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// reportMigrations prints a human-readable summary of the migrations applied by
// a migrate run to the command's output stream.
func reportMigrations(cmd *cobra.Command, applied []string) {
	if len(applied) == 0 {
		cmd.Println("database is up to date; no migrations applied")
		return
	}
	cmd.Printf("applied %d migration(s):\n", len(applied))
	for _, name := range applied {
		cmd.Printf("  - %s\n", name)
	}
}

// schemaChecker is the slice of *database.DB requireCurrentSchema needs.
type schemaChecker interface {
	SchemaStatus(ctx context.Context) (database.SchemaStatus, error)
}

// requireCurrentSchema refuses a subcommand whose job is something else than
// migrating (a scan, an import, a repair…) when the database lacks a migration
// this binary embeds. It returns errSchemaBehind naming both versions and the
// way out; an up-to-date database (or one ahead of the binary) passes. It never
// migrates: that is `kukatko migrate`'s job, run once the old server is stopped
// (docs/OPERATIONS.md, "Upgrading — migrations, the inventory and rollback").
func requireCurrentSchema(ctx context.Context, db schemaChecker) error {
	status, err := db.SchemaStatus(ctx)
	if err != nil {
		return fmt.Errorf("checking the database schema: %w", err)
	}
	if !status.Behind() {
		return nil
	}
	return fmt.Errorf("%w: database is at schema %d, this binary expects %d (%d pending migration(s): %s … %s); "+
		"run `kukatko migrate` after stopping the old server",
		errSchemaBehind, status.Current, status.Expected, len(status.Pending),
		status.Pending[0], status.Pending[len(status.Pending)-1])
}

// openCurrentDB connects to the configured database and checks that its schema
// is current (requireCurrentSchema). It is how every subcommand other than
// `migrate`, `serve` and `maintenance inventory` opens the database. The caller
// owns the returned DB; on an error nothing is left open.
func openCurrentDB(ctx context.Context, cfg config.DatabaseConfig) (*database.DB, error) {
	db, err := database.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := requireCurrentSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
