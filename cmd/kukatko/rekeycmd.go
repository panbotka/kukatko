package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
)

// Flag names of the rekey subcommand that are its own; --dry-run is
// flagDryRun, shared with migrate-to-r2.
const (
	flagRekeyStatus = "status"
	flagRekeyPause  = "pause"
	flagRekeyResume = "resume"
	flagRekeyRetry  = "retry"
)

// Output limits of the rekey subcommand.
const (
	// rekeyPreviewPhotos is how many planned moves a dry run lists.
	rekeyPreviewPhotos = 20
	// rekeyShownErrors is how many of a run's most recent errors a report lists.
	rekeyShownErrors = 5
)

// Errors the rekey subcommand ends on.
var (
	// errRekeyFlagsExclusive is returned, before any connection is opened, for
	// more than one of the action flags.
	errRekeyFlagsExclusive = errors.New("--dry-run, --status, --pause, --resume and --retry are mutually exclusive")
	// errRekeyFailed is how a run in the failed state ends the command, so a
	// shell or a cron notices.
	errRekeyFailed = errors.New("the storage move failed: fix the cause, then --retry")
)

// storageRekeyLong is the subcommand's help text.
const storageRekeyLong = `Move one project's objects from the store root under its own prefix,
projects/<uid>/: originals, thumbnails, streaming segments, sidecars and its
families.yaml. Only a project that lives in the store root needs it — the
bootstrap project of an instance that predates projects; a created project
always had its own prefix and is refused.

Without a flag the move is STARTED: the project's storage prefix switches at
once (new uploads land in the new layout), a run is recorded, and the
storage_rekey job is queued for the worker of the running server, which moves
storage.rekey.per_second photos a second, one photo at a time, copy → verify →
point the row at the copy → only then delete the old objects. It survives a
restart and resumes where it stopped.

  --dry-run   change nothing; print how many photos and bytes would move and
              the first 20 planned moves
  --status    print the project's latest run
  --pause     pause the running move (the job stops at the next photo)
  --resume    resume a paused move
  --retry     start another pass of a failed move over what is left

Reports go to stderr. The command exits non-zero when the run it reports is
failed. Read the runbook in docs/OPERATIONS.md before starting a move.`

// rekeyAction is what one invocation of the rekey subcommand does.
type rekeyAction string

// The rekey subcommand's actions; the zero action starts the move.
const (
	rekeyStart  rekeyAction = ""
	rekeyDryRun rekeyAction = flagDryRun
	rekeyStatus rekeyAction = flagRekeyStatus
	rekeyPause  rekeyAction = flagRekeyPause
	rekeyResume rekeyAction = flagRekeyResume
	rekeyRetry  rekeyAction = flagRekeyRetry
)

// rekeyController is the part of the storage move the subcommand drives. It is
// satisfied by *rekey.Service.
type rekeyController interface {
	Preview(ctx context.Context, projectUID string, limit int) (rekey.Preview, error)
	Status(ctx context.Context, projectUID string) (rekey.Run, error)
	Start(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	Pause(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	Resume(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	Retry(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
}

// compile-time assertion that the real service is what the subcommand drives.
var _ rekeyController = (*rekey.Service)(nil)

// newStorageRekeyCmd builds "storage rekey <slug>": start, inspect and steer
// the storage move of one project.
func newStorageRekeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey <slug>",
		Short: "Move one project's objects under its own storage prefix (resumable)",
		Long:  storageRekeyLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageRekey(cmd, args[0])
		},
	}
	flags := cmd.Flags()
	flags.Bool(flagDryRun, false, "print what the move would do and change nothing")
	flags.Bool(flagRekeyStatus, false, "print the project's latest run")
	flags.Bool(flagRekeyPause, false, "pause the running move")
	flags.Bool(flagRekeyResume, false, "resume a paused move")
	flags.Bool(flagRekeyRetry, false, "retry a failed move")
	return cmd
}

// readRekeyAction returns the one action the flags ask for, or the start when
// none does. More than one is errRekeyFlagsExclusive.
func readRekeyAction(cmd *cobra.Command) (rekeyAction, error) {
	action := rekeyStart
	for _, candidate := range []rekeyAction{rekeyDryRun, rekeyStatus, rekeyPause, rekeyResume, rekeyRetry} {
		set, err := cmd.Flags().GetBool(string(candidate))
		if err != nil {
			return "", fmt.Errorf("reading --%s: %w", candidate, err)
		}
		if !set {
			continue
		}
		if action != rekeyStart {
			return "", errRekeyFlagsExclusive
		}
		action = candidate
	}
	return action, nil
}

// runStorageRekey resolves the project slug names and runs the action its flags
// ask for against the configured database and store.
func runStorageRekey(cmd *cobra.Command, slug string) error {
	action, err := readRekeyAction(cmd)
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
	proj, err := project.NewStore(db.Pool()).GetBySlug(cmd.Context(), slug)
	if err != nil {
		return fmt.Errorf("finding project %s: %w", slug, err)
	}
	mover, err := buildRekeyService(cfg, db)
	if err != nil {
		return err
	}
	return runRekey(cmd, mover, *proj, action)
}

// runRekey performs action on proj's storage move through mover and reports on
// stderr. It returns errRekeyFailed when the run it reports is failed.
func runRekey(cmd *cobra.Command, mover rekeyController, proj project.Project, action rekeyAction) error {
	ctx := cmd.Context()
	cmd.PrintErrf("project: %s (%s) uid %s\n", proj.Slug, proj.Name, proj.UID)
	if action == rekeyDryRun {
		preview, err := mover.Preview(ctx, proj.UID, rekeyPreviewPhotos)
		if err != nil {
			return fmt.Errorf("previewing the move: %w", err)
		}
		printRekeyPreview(cmd, preview)
		return nil
	}
	run, err := applyRekeyAction(ctx, mover, proj.UID, action)
	if err != nil {
		return err
	}
	printRekeyRun(cmd, run)
	if action == rekeyStart || action == rekeyResume || action == rekeyRetry {
		cmd.PrintErrln("the worker of the running server moves it; watch it with --status")
	}
	if run.State == rekey.StateFailed {
		return errRekeyFailed
	}
	return nil
}

// applyRekeyAction runs one of the actions that answer a run.
func applyRekeyAction(
	ctx context.Context, mover rekeyController, projectUID string, action rekeyAction,
) (rekey.Run, error) {
	meta := audit.Meta{UserAgent: "kukatko storage rekey (" + operatorIdentity() + ")"}
	var (
		run rekey.Run
		err error
	)
	switch action {
	case rekeyStatus:
		run, err = mover.Status(ctx, projectUID)
	case rekeyPause:
		run, err = mover.Pause(ctx, meta, projectUID)
	case rekeyResume:
		run, err = mover.Resume(ctx, meta, projectUID)
	case rekeyRetry:
		run, err = mover.Retry(ctx, meta, projectUID)
	case rekeyStart, rekeyDryRun:
		run, err = mover.Start(ctx, meta, projectUID)
	}
	if err != nil {
		return rekey.Run{}, fmt.Errorf("storage move (%s): %w", actionName(action), err)
	}
	return run, nil
}

// actionName names an action for an error message.
func actionName(action rekeyAction) string {
	if action == rekeyStart {
		return "start"
	}
	return string(action)
}

// printRekeyPreview prints, on stderr, what a dry run found.
func printRekeyPreview(cmd *cobra.Command, preview rekey.Preview) {
	cmd.PrintErrf("dry run: %d photo(s), %s of originals would move from the store root under %s\n",
		preview.Photos, humanBytes(preview.Bytes), preview.TargetPrefix)
	if len(preview.First) > 0 {
		cmd.PrintErrf("the first %d photo(s), in the order the move takes them:\n", len(preview.First))
	}
	for _, photo := range preview.First {
		cmd.PrintErrf("  %s → %s (+%d derived key(s), moved where they exist)\n",
			photo.Original.From, photo.Original.To, photo.Derived)
	}
	cmd.PrintErrln("nothing was changed — run without --dry-run to start the move")
}

// printRekeyRun prints, on stderr, one run: its state, its progress and its
// most recent errors.
func printRekeyRun(cmd *cobra.Command, run rekey.Run) {
	cmd.PrintErrf("run %s: %s, moving under %s\n", run.UID, run.State, run.TargetPrefix)
	cmd.PrintErrf("photos: %d of %d moved (%s)\n", run.DonePhotos, run.TotalPhotos,
		percent(run.DonePhotos, run.TotalPhotos))
	cmd.PrintErrf("originals: %s of %s\n", humanBytes(run.DoneBytes), humanBytes(run.TotalBytes))
	finished := "-"
	if run.FinishedAt != nil {
		finished = run.FinishedAt.UTC().Format("2006-01-02 15:04:05Z")
	}
	cmd.PrintErrf("started %s, updated %s, finished %s\n",
		run.StartedAt.UTC().Format("2006-01-02 15:04:05Z"), run.UpdatedAt.UTC().Format("2006-01-02 15:04:05Z"),
		finished)
	cmd.PrintErrf("errors: %d\n", run.ErrorCount)
	shown := run.Errors[max(len(run.Errors)-rekeyShownErrors, 0):]
	for _, failure := range shown {
		key := failure.Key
		if key == "" {
			key = "(run)"
		}
		cmd.PrintErrf("  %s %s: %s\n", failure.At.UTC().Format("2006-01-02 15:04:05Z"), key, failure.Message)
	}
}

// percent renders done of total as a percentage, 100% for nothing to do.
func percent(done, total int64) string {
	if total <= 0 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", float64(done)*100/float64(total))
}
