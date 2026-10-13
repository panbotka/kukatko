package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// errCtlRekeyFlagsExclusive is returned, before any request, for more than one
// of the action flags of "ctl projects rekey".
var errCtlRekeyFlagsExclusive = errors.New("--status, --pause, --resume and --retry are mutually exclusive")

// ctlRekeyFlags are the action flags of "ctl projects rekey".
type ctlRekeyFlags struct {
	status, pause, resume, retry bool
}

// action returns the transition the flags ask for, "" for none, and whether
// only the status is wanted. More than one flag is errCtlRekeyFlagsExclusive.
func (f ctlRekeyFlags) action() (ctl.RekeyAction, bool, error) {
	set := 0
	var action ctl.RekeyAction
	for _, flag := range []struct {
		on     bool
		action ctl.RekeyAction
	}{{f.status, ""}, {f.pause, ctl.RekeyPause}, {f.resume, ctl.RekeyResume}, {f.retry, ctl.RekeyRetry}} {
		if flag.on {
			set++
			action = flag.action
		}
	}
	if set > 1 {
		return "", false, errCtlRekeyFlagsExclusive
	}
	return action, f.status, nil
}

// newCtlProjectsRekeyCmd builds "ctl projects rekey": start, inspect and steer
// a project's storage move over the API.
func newCtlProjectsRekeyCmd(opts *ctlOptions) *cobra.Command {
	var f ctlRekeyFlags
	cmd := &cobra.Command{
		Use:   "rekey <slug>",
		Short: "Start, inspect or steer a project's storage move",
		Long: "Move a project's objects from the store root under its own prefix,\n" +
			"projects/<uid>/. Without a flag the move is started; the server's worker\n" +
			"moves it. --status shows the latest run, --pause/--resume/--retry steer it.\n" +
			"Read the runbook in docs/OPERATIONS.md before starting a move.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCtlProjectsRekey(cmd, opts, args[0], f)
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&f.status, "status", false, "show the project's latest run")
	flags.BoolVar(&f.pause, "pause", false, "pause the running move")
	flags.BoolVar(&f.resume, "resume", false, "resume a paused move")
	flags.BoolVar(&f.retry, "retry", false, "retry a failed move")
	return cmd
}

// runCtlProjectsRekey resolves the project slug names and applies what the
// flags ask for, rendering the run the server answers.
func runCtlProjectsRekey(cmd *cobra.Command, opts *ctlOptions, slug string, f ctlRekeyFlags) error {
	action, statusOnly, err := f.action()
	if err != nil {
		return err
	}
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	found, err := client.ResolveProject(cmd.Context(), slug)
	if err != nil {
		return fmt.Errorf("finding project %s: %w", slug, err)
	}
	raw, err := rekeyRequest(cmd.Context(), client, found.UID, action, statusOnly)
	if err != nil {
		return fmt.Errorf("storage move of project %s: %w", slug, err)
	}
	return renderRaw(cmd.OutOrStdout(), out, raw, "storage move", ctl.DecodeRekeyRun, ctl.WriteRekeyRun)
}

// rekeyRequest sends the one request the flags ask for.
func rekeyRequest(
	ctx context.Context, client *ctl.Client, projectUID string, action ctl.RekeyAction, statusOnly bool,
) (json.RawMessage, error) {
	switch {
	case statusOnly:
		return client.RekeyStatus(ctx, projectUID) //nolint:wrapcheck // the caller wraps it with the slug.
	case action != "":
		return client.ChangeRekey(ctx, projectUID, action) //nolint:wrapcheck // the caller wraps it.
	}
	return client.StartRekey(ctx, projectUID) //nolint:wrapcheck // the caller wraps it with the slug.
}
