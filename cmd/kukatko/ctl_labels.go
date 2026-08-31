package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlLabelsCmd builds the "ctl labels" tree: labels and the photos they are
// attached to, served by internal/organizeapi. Listing needs any role; creating,
// attaching and detaching need the editor or admin role.
func newCtlLabelsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List, inspect, create, edit and delete labels and attach them to photos",
	}
	cmd.AddCommand(
		newCtlLabelsListCmd(opts), newCtlLabelsGetCmd(opts), newCtlLabelsCreateCmd(opts),
		newCtlLabelsUpdateCmd(opts), newCtlLabelsDeleteCmd(opts),
		newCtlLabelsAttachCmd(opts), newCtlLabelsDetachCmd(opts),
	)
	return cmd
}

// newCtlLabelsListCmd builds "ctl labels list", the bare {"labels": […]} list in
// the API's priority order. It is not paginated.
func newCtlLabelsListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every label with its photo count, highest priority first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListLabels(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing labels: %w", err)
			}
			return renderLabels(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlLabelsGetCmd builds "ctl labels get <uid>", one label's detail.
func newCtlLabelsGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one label's detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetLabel(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching label %s: %w", args[0], err)
			}
			return renderLabel(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlLabelsCreateCmd builds "ctl labels create <name>". The server derives the
// uid and a unique slug.
func newCtlLabelsCreateCmd(opts *ctlOptions) *cobra.Command {
	var in ctl.LabelInput
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a label (editor or admin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			in.Name = args[0]
			raw, err := client.CreateLabel(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating label %q: %w", in.Name, err)
			}
			return renderLabel(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().IntVar(&in.Priority, "priority", 0, "float the label up the UI's list")
	return cmd
}

// newCtlLabelsAttachCmd builds "ctl labels attach <label-uid> <photo-uid>".
func newCtlLabelsAttachCmd(opts *ctlOptions) *cobra.Command {
	var (
		source      string
		uncertainty int
	)
	cmd := &cobra.Command{
		Use:   "attach <label-uid> <photo-uid>",
		Short: "Attach a label to one photo (editor or admin)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.AttachLabel(cmd.Context(), args[0], args[1], source, uncertainty); err != nil {
				return fmt.Errorf("attaching label %s to photo %s: %w", args[0], args[1], err)
			}
			return renderAck(cmd.OutOrStdout(), out,
				"label "+args[0]+" attached to photo "+args[1])
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&source, "source", "", "where the attachment came from: manual (default), ai or import")
	flags.IntVar(&uncertainty, "uncertainty", 0, "how uncertain the attachment is, for an ai source")
	return cmd
}

// newCtlLabelsDetachCmd builds "ctl labels detach <label-uid> <photo-uid>".
// Detaching a label that is not attached is a no-op, so the command is idempotent.
func newCtlLabelsDetachCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "detach <label-uid> <photo-uid>",
		Short: "Detach a label from one photo (editor or admin)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.DetachLabel(cmd.Context(), args[0], args[1]); err != nil {
				return fmt.Errorf("detaching label %s from photo %s: %w", args[0], args[1], err)
			}
			return renderAck(cmd.OutOrStdout(), out,
				"label "+args[0]+" detached from photo "+args[1])
		},
	}
}

// newCtlLabelsUpdateCmd builds "ctl labels update <uid>", the flag-per-field edit
// of a label's name, priority and review-game participation.
func newCtlLabelsUpdateCmd(opts *ctlOptions) *cobra.Command {
	var (
		name     string
		priority int
		review   bool
	)
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Rename a label or change its priority or review setting (editor or admin)",
		Long: "Edit a label's name, priority or review-game participation.\n\n" +
			"Only the flags you actually write are changed: the command reads the label\n" +
			"first and sends the rest back untouched, because PATCH /labels/{uid} rewrites\n" +
			"the whole record and a body carrying only a new name would reset the priority\n" +
			"to zero.\n\n" +
			"Renaming a label renames it everywhere — the photos it is attached to keep it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			patch := ctl.LabelPatch{
				Name:     optionalString(cmd, "name", name),
				Priority: optionalInt(cmd, "priority", priority),
				Review:   optionalBool(cmd, "review", review),
			}
			raw, err := client.UpdateLabel(cmd.Context(), args[0], patch)
			if err != nil {
				return fmt.Errorf("updating label %s: %w", args[0], err)
			}
			return renderLabel(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&name, "name", "", "new label name")
	flags.IntVar(&priority, "priority", 0, "float the label up the UI's list")
	flags.BoolVar(&review, "review", true, "let the review game ask about this label (--review=false stops it)")
	return cmd
}

// newCtlLabelsDeleteCmd builds "ctl labels delete <uid>".
func newCtlLabelsDeleteCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a label, detaching it from every photo (editor or admin)",
		Long: "Delete a label.\n\n" +
			"The photos survive and nothing is archived; they simply stop carrying the\n" +
			"label. What is lost is which photos somebody decided it applied to, and\n" +
			"re-creating a label of the same name re-attaches none of them. Pass --yes to\n" +
			"confirm, or --dry-run to see which label would go.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLabelDelete(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runLabelDelete names the label before removing it, so the confirmation says
// which label went rather than which uid did.
func runLabelDelete(cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	which, err := client.DescribeLabel(cmd.Context(), uid)
	if err != nil {
		return fmt.Errorf("fetching label %s: %w", uid, err)
	}
	action := "delete label " + which
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out,
			"dry run: would "+action+", detaching it from every photo; nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	if err := client.DeleteLabel(cmd.Context(), uid); err != nil {
		return fmt.Errorf("deleting label %s: %w", uid, err)
	}
	return renderAck(cmd.OutOrStdout(), out,
		"label "+which+" deleted; the photos that carried it are untouched")
}
