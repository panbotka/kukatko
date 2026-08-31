package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlDuplicatesCmd builds the "ctl duplicates" tree: the groups of photos the
// library thinks are the same shot (`internal/duplicates`), and the two opinions
// a human can record about a pair (`internal/feedbackapi`). All of it needs the
// editor or admin role.
//
// **Nothing here merges or archives anything.** Resolving a group by merging the
// copies into a keeper is destructive and stays out of `ctl` on purpose — it
// lives with the guarded local commands. Confirming and dismissing are opinions:
// they rank the duplicates page and keep a settled pair from coming back.
func newCtlDuplicatesCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "duplicates",
		Short: "List likely-duplicate groups and settle a pair as the same shot or as different ones",
	}
	cmd.AddCommand(
		newCtlDuplicatesListCmd(opts),
		newCtlDuplicatesPairCmd(opts, duplicatePairSpec{
			use:   "confirm <photo-uid> <photo-uid>",
			short: "Record that the two photos really are the same shot",
			long: "Record that the two photos really are the same shot.\n\n" +
				"It merges nothing and archives nothing: it is the agreement the duplicates\n" +
				"page ranks on, and what tells the library somebody has already looked. The\n" +
				"pair is unordered and the write is idempotent.",
			done: "confirmed as the same shot; nothing was merged",
			pick: func(c *ctl.Client) duplicatePair { return c.ConfirmDuplicate },
		}),
		newCtlDuplicatesPairCmd(opts, duplicatePairSpec{
			use:   "unconfirm <photo-uid> <photo-uid>",
			short: "Take a confirmation back, dropping the pair to a machine guess again",
			long:  "Take a confirmation back. The pair becomes an unjudged machine guess again.",
			done:  "no longer confirmed",
			pick:  func(c *ctl.Client) duplicatePair { return c.UnconfirmDuplicate },
		}),
		newCtlDuplicatesPairCmd(opts, duplicatePairSpec{
			use:   "dismiss <photo-uid> <photo-uid>",
			short: "Record that the two photos are NOT duplicates of each other",
			long: "Record that the two photos are NOT duplicates of each other.\n\n" +
				"The pair stops being offered for review. This is the exact opposite of\n" +
				"`confirm` — reaching for one meaning the other records the opposite of what\n" +
				"you decided. The pair is unordered and the write is idempotent.",
			done: "settled as not duplicates",
			pick: func(c *ctl.Client) duplicatePair { return c.DismissDuplicate },
		}),
		newCtlDuplicatesPairCmd(opts, duplicatePairSpec{
			use:   "undismiss <photo-uid> <photo-uid>",
			short: "Take a dismissal back, letting the pair be offered again",
			long:  "Take a dismissal back. The pair can be offered for review again.",
			done:  "no longer dismissed",
			pick:  func(c *ctl.Client) duplicatePair { return c.UndismissDuplicate },
		}),
	)
	return cmd
}

// newCtlDuplicatesListCmd builds "ctl duplicates list", a page of GET /duplicates.
func newCtlDuplicatesListCmd(opts *ctlOptions) *cobra.Command {
	var page ctl.PageOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the groups of photos the library thinks are the same shot",
		Long: "List the likely-duplicate groups, the ones somebody has already confirmed\n" +
			"first.\n\n" +
			"The scan is read-only and prints one row per member, because the member uids\n" +
			"are what `confirm` and `dismiss` take. DISTANCE names which detector measured\n" +
			"it: a Hamming distance between perceptual hashes and a cosine distance between\n" +
			"embeddings are not the same number.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListDuplicates(cmd.Context(), page)
			if err != nil {
				return fmt.Errorf("listing duplicate groups: %w", err)
			}
			return renderDuplicates(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&page.Limit, "limit", 0, "groups per page (0 = the server's default)")
	flags.IntVar(&page.Offset, "offset", 0, "how many groups to skip")
	return cmd
}

// duplicatePair is one of the four duplicate-opinion calls, bound to a resolved
// client.
type duplicatePair func(ctx context.Context, photoUID, otherUID string) error

// duplicatePairSpec is what distinguishes the four duplicate-opinion commands:
// their help text, the phrase their confirmation ends with, and the client call.
type duplicatePairSpec struct {
	use   string
	short string
	long  string
	done  string
	pick  func(*ctl.Client) duplicatePair
}

// newCtlDuplicatesPairCmd builds one duplicate-opinion command. All four take the
// same unordered pair, answer 204 and are idempotent.
func newCtlDuplicatesPairCmd(opts *ctlOptions, spec duplicatePairSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := spec.pick(client)(cmd.Context(), args[0], args[1]); err != nil {
				return fmt.Errorf("recording an opinion about %s and %s: %w", args[0], args[1], err)
			}
			return renderAck(cmd.OutOrStdout(), out,
				"photos "+args[0]+" and "+args[1]+" "+spec.done)
		},
	}
}
