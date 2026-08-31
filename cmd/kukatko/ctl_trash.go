package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlTrashCmd builds the "ctl trash" tree: what is in the trash, and the two
// ways to empty it for good.
//
// The trash is where the reversible half of a photo's life ends and the
// irreversible half begins, so the tree is deliberately shaped around that: one
// command that only reads (`info`), and two that destroy and therefore refuse to
// run without --yes. Both of those take --dry-run, which lists exactly what
// would be lost and needs no confirmation at all — showing what is at stake is
// how the decision gets made, so it cannot be behind the same gate as the
// decision.
func newCtlTrashCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "Inspect the trash and permanently delete what is in it",
	}
	cmd.AddCommand(newCtlTrashInfoCmd(opts), newCtlTrashEmptyCmd(opts), newCtlTrashPurgeOlderCmd(opts))
	return cmd
}

// newCtlTrashInfoCmd builds "ctl trash info", the read that informs the gate.
func newCtlTrashInfoCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Report what is in the trash and what retention will remove next",
		Long: "Report what is in the trash: how many photos, how much they weigh, the\n" +
			"configured retention window, and every archived photo with the date retention\n" +
			"will destroy it on — oldest first, which is the order it takes them in.\n\n" +
			"Nothing here changes anything. It exists so that emptying the trash is a\n" +
			"decision somebody made having seen what is in it, rather than a blind one.\n\n" +
			"With retention switched off (`trash.retention_days` <= 0) there is no purge\n" +
			"date to print and the summary says so: an archived photo then stays in the\n" +
			"trash until somebody destroys it by hand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			trash, err := client.FetchTrash(cmd.Context())
			if err != nil {
				return fmt.Errorf("reading the trash: %w", err)
			}
			view := ctl.TrashView{Heading: "in the trash now, oldest first:", Trash: trash}
			return renderTrash(cmd.OutOrStdout(), out, view)
		},
	}
}

// newCtlTrashEmptyCmd builds "ctl trash empty".
func newCtlTrashEmptyCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "empty",
		Short: "Permanently delete every archived photo (admin; cannot be undone)",
		Long: "Permanently delete every photo in the trash.\n\n" +
			"**This cannot be undone.** For each photo the catalogue row, the original file,\n" +
			"every cached thumbnail and the backup object are destroyed. There is no second\n" +
			"trash behind this one, and no photo that is not archived is touched.\n\n" +
			"Pass --yes to confirm, or --dry-run to list exactly which photos would be\n" +
			"destroyed. --dry-run needs no --yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrashEmpty(cmd, opts, assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runTrashEmpty lists the trash first, so both the rehearsal and the
// confirmation are about photos somebody could have looked at.
func runTrashEmpty(cmd *cobra.Command, opts *ctlOptions, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	if dryRun {
		trash, err := client.FetchTrash(cmd.Context())
		if err != nil {
			return fmt.Errorf("reading the trash: %w", err)
		}
		view := ctl.TrashView{
			Heading: "dry run: emptying the trash would permanently delete these photos:",
			DryRun:  true,
			Trash:   trash,
		}
		return renderTrash(cmd.OutOrStdout(), out, view)
	}
	if err := confirmIrreversible(assumeYes, "permanently delete every photo in the trash"); err != nil {
		return err
	}
	raw, err := client.EmptyTrash(cmd.Context())
	if err != nil {
		return fmt.Errorf("emptying the trash: %w", err)
	}
	return renderPurgeResult(cmd.OutOrStdout(), out, raw)
}

// newCtlTrashPurgeOlderCmd builds "ctl trash purge-older", the age-bounded
// purge: the manual half of what retention does on its own.
func newCtlTrashPurgeOlderCmd(opts *ctlOptions) *cobra.Command {
	var (
		days      int
		assumeYes bool
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "purge-older",
		Short: "Permanently delete photos archived longer ago than --days (admin; cannot be undone)",
		Long: "Permanently delete every photo that has been in the trash for longer than\n" +
			"--days days.\n\n" +
			"**This cannot be undone**, and it takes the same path as the scheduled\n" +
			"retention purge — it is that purge run by hand, on a window you choose, and\n" +
			"the audit trail records it as yours rather than the system's.\n\n" +
			"--days 0 is the whole trash, exactly the same as `trash empty`.\n\n" +
			"Pass --yes to confirm, or --dry-run to list exactly which photos would be\n" +
			"destroyed. --dry-run needs no --yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrashPurgeOlder(cmd, opts, days, assumeYes, dryRun)
		},
	}
	cmd.Flags().IntVar(&days, "days", 0,
		"delete photos archived more than this many days ago (0 = the whole trash)")
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runTrashPurgeOlder rehearses or runs the age-bounded purge. The rehearsal
// applies the cutoff to the listing itself, so what it prints is the set the
// server will act on and not the whole trash.
func runTrashPurgeOlder(cmd *cobra.Command, opts *ctlOptions, days int, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	if days < 0 {
		return fmt.Errorf("reading --days: %w: %d", ctl.ErrInvalidRetentionDays, days)
	}
	action := "permanently delete every photo archived more than " + strconv.Itoa(days) + " days ago"
	if dryRun {
		trash, err := client.FetchTrash(cmd.Context())
		if err != nil {
			return fmt.Errorf("reading the trash: %w", err)
		}
		view := ctl.TrashView{
			Heading: "dry run: this would " + action + ":",
			DryRun:  true,
			Trash:   trash.ArchivedBefore(ctl.PurgeCutoff(time.Now(), days)),
		}
		return renderTrash(cmd.OutOrStdout(), out, view)
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	raw, err := client.PurgeOlderThan(cmd.Context(), days)
	if err != nil {
		return fmt.Errorf("purging the trash older than %d days: %w", days, err)
	}
	return renderPurgeResult(cmd.OutOrStdout(), out, raw)
}
