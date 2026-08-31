package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlStacksCmd builds the "ctl stacks" tree: grouping the several files one
// shot was stored as — a RAW beside its JPEG, an edit beside its original — into
// one tile (`internal/stacks`). All of it needs the editor or admin role.
//
// A stack **groups, it never merges.** Every member keeps its own uid, its own
// file and its own metadata; the group only decides which one a listing shows.
// That is why ungrouping loses nothing and why none of these commands deletes
// anything.
func newCtlStacksCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "Group the variants of one shot into a single tile, and ungroup them again",
	}
	cmd.AddCommand(
		newCtlStacksGroupCmd(opts), newCtlStacksSetPrimaryCmd(opts),
		newCtlStacksUngroupCmd(opts), newCtlStacksUngroupAllCmd(opts),
	)
	return cmd
}

// newCtlStacksGroupCmd builds "ctl stacks group <photo-uid> <photo-uid> […]".
func newCtlStacksGroupCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "group <photo-uid> <photo-uid> [<photo-uid>…]",
		Short: "Group two or more photos as variants of one shot (editor or admin)",
		Long: "Group two or more photos as variants of one shot.\n\n" +
			"Nothing is merged and nothing is deleted: each photo keeps its uid, its file\n" +
			"and its metadata, and the group only decides which of them the library shows.\n" +
			"`ctl stacks ungroup-all` puts them back side by side.\n\n" +
			"This is the manual path, for the pairs the detection rules miss.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.StackPhotos(cmd.Context(), args)
			if err != nil {
				return fmt.Errorf("grouping %d photos into a stack: %w", len(args), err)
			}
			return renderStack(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlStacksSetPrimaryCmd builds "ctl stacks set-primary <photo-uid>".
func newCtlStacksSetPrimaryCmd(opts *ctlOptions) *cobra.Command {
	return newCtlStackMutationCmd(opts, stackSpec{
		use:   "set-primary <photo-uid>",
		short: "Make one variant the one its stack is shown as (editor or admin)",
		long: "Make one variant the one its stack is shown as.\n\n" +
			"The other members are untouched; only which of them a listing, a search and\n" +
			"the map show changes. A photo that is not in a stack is refused (409).",
		pick: func(c *ctl.Client) stackMutation { return c.SetStackPrimary },
	})
}

// newCtlStacksUngroupCmd builds "ctl stacks ungroup <photo-uid>".
func newCtlStacksUngroupCmd(opts *ctlOptions) *cobra.Command {
	return newCtlStackMutationCmd(opts, stackSpec{
		use:   "ungroup <photo-uid>",
		short: "Take one photo out of its stack, leaving the rest grouped (editor or admin)",
		long: "Take one photo out of its stack.\n\n" +
			"The rest of the group stays as it was and the photo becomes standalone again,\n" +
			"visible in its own right. Nothing is deleted — the stack never held the file,\n" +
			"only the grouping.",
		pick: func(c *ctl.Client) stackMutation { return c.UnstackPhoto },
	})
}

// newCtlStacksUngroupAllCmd builds "ctl stacks ungroup-all <photo-uid>".
func newCtlStacksUngroupAllCmd(opts *ctlOptions) *cobra.Command {
	return newCtlStackMutationCmd(opts, stackSpec{
		use:   "ungroup-all <photo-uid>",
		short: "Dissolve the whole stack the photo belongs to (editor or admin)",
		long: "Dissolve the whole stack the photo belongs to.\n\n" +
			"Every member becomes a standalone photo again, each visible in its own right.\n" +
			"Nothing is deleted; the grouping is.",
		pick: func(c *ctl.Client) stackMutation { return c.UnstackAll },
	})
}

// stackMutation is one of the three per-photo stacking calls, bound to a
// resolved client.
type stackMutation func(ctx context.Context, photoUID string) (json.RawMessage, error)

// stackSpec is what distinguishes the three per-photo stacking commands: their
// help text and the client call. They otherwise share an argument, an output and
// an error path.
type stackSpec struct {
	use   string
	short string
	long  string
	pick  func(*ctl.Client) stackMutation
}

// newCtlStackMutationCmd builds one per-photo stacking command.
func newCtlStackMutationCmd(opts *ctlOptions, spec stackSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := spec.pick(client)(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("restacking photo %s: %w", args[0], err)
			}
			return renderStack(cmd.OutOrStdout(), out, raw)
		},
	}
}
