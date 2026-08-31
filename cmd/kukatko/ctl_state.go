package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// photoStateSpec is what distinguishes the four reversible state commands: the
// state endpoint they post to and their help text.
type photoStateSpec struct {
	state string
	short string
	long  string
}

// photoStateSpecs describes archive, unarchive, hide and unhide — the whole
// reversible half of a photo's lifecycle.
//
// None of them is gated, and that is the point of the distinction this command
// tree draws: they are undone by the command that reverses them, they delete
// nothing, and no original is touched. Only what cannot be taken back — the
// purge, the trash, a merge that archives copies — asks for --yes.
var photoStateSpecs = []photoStateSpec{
	{
		state: ctl.StateArchive,
		short: "Move a photo to the trash (reversible)",
		long: "Move a photo to the trash.\n\n" +
			"This is a soft delete: the row, the original and everything about the photo\n" +
			"stay exactly as they were, and it simply leaves the default listings. " +
			"`unarchive`\nbrings it back, unchanged.\n\n" +
			"What it does start is a clock: the trash is purged by retention, and " +
			"`ctl trash\ninfo` says how long a photo has. Permanent deletion is `ctl photos purge`,\n" +
			"which only works on a photo that is already here.",
	},
	{
		state: ctl.StateUnarchive,
		short: "Restore a photo from the trash",
		long: "Restore a photo from the trash, exactly as it was: nothing about an archived\n" +
			"photo was changed, so nothing has to be put back.",
	},
	{
		state: ctl.StateHide,
		short: "Hide a photo from the library grid (reversible)",
		long: "Hide a photo from the library grid.\n\n" +
			"This is **not** archiving and nothing is deleted or scheduled for deletion.\n" +
			"The photo leaves the library grid and its counts, the timeline, the map, the\n" +
			"slideshow, the review game and the default search — and stays fully visible in\n" +
			"its albums and labels, in favourites, and at its own uid. It is for the photo\n" +
			"that is worth keeping but not worth meeting again by accident.\n\n" +
			"`q=hidden:yes` lists the hidden ones; `unhide` brings one back.",
	},
	{
		state: ctl.StateUnhide,
		short: "Bring a hidden photo back into the library",
		long:  "Bring a hidden photo back into the library grid, the timeline and the search.",
	},
}

// newCtlPhotoStateCmds builds the four reversible lifecycle commands.
func newCtlPhotoStateCmds(opts *ctlOptions) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(photoStateSpecs))
	for _, spec := range photoStateSpecs {
		cmds = append(cmds, newCtlPhotoStateCmd(opts, spec))
	}
	return cmds
}

// newCtlPhotoStateCmd builds one reversible state command. All four post to
// their own endpoint, answer with the refreshed photo, and are idempotent.
func newCtlPhotoStateCmd(opts *ctlOptions, spec photoStateSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.state + " <uid>",
		Short: spec.short,
		Long:  spec.long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.SetPhotoState(cmd.Context(), args[0], spec.state)
			if err != nil {
				return fmt.Errorf("%s photo %s: %w", spec.state, args[0], err)
			}
			return renderPhotoState(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlPhotosPurgeCmd builds "ctl photos purge <uid>", the end of a photo's
// life: the row, the original, the thumbnails and the backup object all go.
func newCtlPhotosPurgeCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "purge <uid>",
		Short: "Permanently delete one archived photo (admin; cannot be undone)",
		Long: "Permanently delete one archived photo.\n\n" +
			"**This cannot be undone.** The catalogue row, the original file, every cached\n" +
			"thumbnail and the photo's object in the backup bucket are all destroyed. There\n" +
			"is no second trash behind this one.\n\n" +
			"Only an archived photo can be purged: a live photo answers 409, so nothing in\n" +
			"the library is ever one command away from gone. It needs the admin role, not\n" +
			"merely write access.\n\n" +
			"Pass --yes to confirm, or --dry-run to see exactly which photo would be\n" +
			"destroyed. --dry-run needs no --yes: seeing what would be lost is how the\n" +
			"decision gets made.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPhotoPurge(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runPhotoPurge reads the photo before destroying it, so both the rehearsal and
// the confirmation name a file rather than a uid — and so a mistyped uid is a
// 404 while everything is still there.
func runPhotoPurge(
	cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool,
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	raw, err := client.GetPhoto(cmd.Context(), uid, ctl.PhotoDetailOptions{})
	if err != nil {
		return fmt.Errorf("fetching photo %s: %w", uid, err)
	}
	detail, err := ctl.DecodePhotoDetail(raw)
	if err != nil {
		return fmt.Errorf("fetching photo %s: %w", uid, err)
	}
	action := "permanently delete " + ctl.NamedUID(purgeLabel(detail), detail.UID)
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out, "dry run: would "+action+
			" ("+ctl.DescribePurgeTarget(detail)+"); nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	if err := client.PurgePhoto(cmd.Context(), uid); err != nil {
		return fmt.Errorf("purging photo %s: %w", uid, err)
	}
	return renderAck(cmd.OutOrStdout(), out, "photo "+ctl.NamedUID(purgeLabel(detail), detail.UID)+
		" is permanently deleted, along with its original and its thumbnails")
}

// purgeLabel names the photo about to be destroyed: its title when it has one,
// otherwise the file it came from.
func purgeLabel(detail ctl.PhotoDetail) string {
	if detail.Title != "" {
		return detail.Title
	}
	return detail.FileName
}
