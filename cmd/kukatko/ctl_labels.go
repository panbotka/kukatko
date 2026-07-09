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
		Short: "List, inspect, create labels and attach them to photos",
	}
	cmd.AddCommand(
		newCtlLabelsListCmd(opts), newCtlLabelsGetCmd(opts), newCtlLabelsCreateCmd(opts),
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
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListLabels(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing labels: %w", err)
			}
			return renderLabels(cmd.OutOrStdout(), format, raw)
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
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetLabel(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching label %s: %w", args[0], err)
			}
			return renderLabel(cmd.OutOrStdout(), format, raw)
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
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			in.Name = args[0]
			raw, err := client.CreateLabel(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating label %q: %w", in.Name, err)
			}
			return renderLabel(cmd.OutOrStdout(), format, raw)
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
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.AttachLabel(cmd.Context(), args[0], args[1], source, uncertainty); err != nil {
				return fmt.Errorf("attaching label %s to photo %s: %w", args[0], args[1], err)
			}
			return renderAck(cmd.OutOrStdout(), format,
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
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.DetachLabel(cmd.Context(), args[0], args[1]); err != nil {
				return fmt.Errorf("detaching label %s from photo %s: %w", args[0], args[1], err)
			}
			return renderAck(cmd.OutOrStdout(), format,
				"label "+args[0]+" detached from photo "+args[1])
		},
	}
}
