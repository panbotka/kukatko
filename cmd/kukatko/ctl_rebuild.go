package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlPhotosRebuildCmd builds the "ctl photos rebuild" tree: one subcommand per
// per-photo computation that can be redone over the top of what is stored.
//
// It exists because the repair does not cover this. `POST /process/{step}` — and
// the same button in the UI — enqueues the ordinary job, and every one of those
// jobs skips a photo that already has the data: it answers 200, reports the step
// as done, and changes nothing. That is right for a photo the pipeline missed and
// useless for a photo whose stored answer is *wrong* — computed from a source
// that has since been fixed, or by a model that has since changed. These
// commands throw the stored answer away and compute it again.
//
// None of them touches an original, and none is gated: everything they discard is
// derived data the same command can produce again. What they do cost is real work
// on the box (and, for `place`, a mapy.com credit), so they run one photo at a
// time by design.
func newCtlPhotosRebuildCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Recompute one photo's derived data, replacing what is stored",
		Long: "Recompute one photo's derived data, replacing what is stored.\n\n" +
			"This is not the same as scheduling the step: an ordinary job skips a photo\n" +
			"that already has the data, so asking for it again looks like it worked and\n" +
			"changes nothing. A rebuild discards the stored answer and computes a new one,\n" +
			"which is what a photo whose derived data is wrong — rather than missing —\n" +
			"needs.\n\n" +
			"No original is ever touched, and everything a rebuild discards can be produced\n" +
			"again by the same command. Needs the maintainer role.\n\n" +
			"When the embeddings box (or mapy.com) is offline the work is queued instead of\n" +
			"failing, and the command says so.",
	}
	for _, spec := range ctl.RebuildSpecs {
		cmd.AddCommand(newCtlPhotosRebuildStepCmd(opts, spec))
	}
	return cmd
}

// newCtlPhotosRebuildStepCmd builds one rebuild subcommand. All four take a photo
// uid, post to their own endpoint and report what the recomputation produced.
func newCtlPhotosRebuildStepCmd(opts *ctlOptions, spec ctl.RebuildSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.Name + " <uid>",
		Short: spec.Short,
		Long:  spec.Short + ".\n\nThe stored result is replaced, not added to. POST /photos/{uid}/" + spec.Path + ".",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.RebuildPhoto(cmd.Context(), args[0], spec.Name)
			if err != nil {
				return fmt.Errorf("rebuilding the %s of photo %s: %w", spec.Name, args[0], err)
			}
			return renderPhotoRebuild(cmd.OutOrStdout(), out, raw, spec.Name)
		},
	}
}
