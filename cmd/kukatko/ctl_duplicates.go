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
// Confirming and dismissing are opinions: they rank the duplicates page and keep
// a settled pair from coming back, and neither of them touches a photo. `merge`
// is the one command here that does — it archives the copies it did not keep —
// so it is gated like every other irreversible step: --yes to run it, --dry-run
// to see what it would move first.
func newCtlDuplicatesCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "duplicates",
		Short: "List likely-duplicate groups, settle a pair, or resolve a group into one keeper",
	}
	cmd.AddCommand(
		newCtlDuplicatesListCmd(opts), newCtlDuplicatesMergeCmd(opts),
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

// newCtlDuplicatesMergeCmd builds "ctl duplicates merge <keeper-uid>
// <other-uid>…", the one command in this tree that changes the library.
func newCtlDuplicatesMergeCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "merge <keeper-uid> <other-uid>…",
		Short: "Resolve a duplicate group into one keeper, archiving the copies (editor or admin)",
		Long: "Resolve a duplicate group: everything the other members carried — their\n" +
			"albums, their labels, the people on them — moves onto the keeper, whose empty\n" +
			"metadata fields are filled from theirs, and **the copies are archived**.\n\n" +
			"That last part is why this is gated while `confirm` and `dismiss` are not: an\n" +
			"opinion about a pair can be taken back, an archived photo is on the retention\n" +
			"clock and will eventually be destroyed. Nothing is deleted here and no original\n" +
			"is touched — the copies land in the trash, where `ctl trash info` accounts for\n" +
			"them and only a purge ends them.\n\n" +
			"The keeper is named first and is added to the group automatically, so it\n" +
			"cannot be left out of its own merge. Pass --yes to confirm, or --dry-run to\n" +
			"see what would move — the rehearsal is the server's own preview of this exact\n" +
			"merge, not a second guess at it, and it needs no --yes.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuplicateMerge(cmd, opts, args[0], args[1:], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runDuplicateMerge previews or performs a merge. The dry run goes to the server
// too: POST /duplicates/merge with dry_run computes the whole result and writes
// nothing, so what the rehearsal prints is what the merge would do.
func runDuplicateMerge(
	cmd *cobra.Command, opts *ctlOptions, keeperUID string, others []string, assumeYes, dryRun bool,
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	if !dryRun {
		action := fmt.Sprintf("merge %d photos into %s, archiving the copies", len(others), keeperUID)
		if err := confirmIrreversible(assumeYes, action); err != nil {
			return err
		}
	}
	raw, err := client.MergeGroup(cmd.Context(), keeperUID, others, dryRun)
	if err != nil {
		return fmt.Errorf("merging a duplicate group into %s: %w", keeperUID, err)
	}
	return renderDuplicateMerge(cmd.OutOrStdout(), out, raw)
}
