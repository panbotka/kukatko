package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlPhotosRebuildVideoCmd builds "ctl photos rebuild video": prepare one
// video for playback.
//
// It sits with the other four rebuilds because it is one — the `hls_transcode`
// job re-encodes the clip into every configured quality whatever is already
// recorded, replacing each rendition rather than adding to it, so the same
// command both prepares a video the encoder never reached and redoes one encoded
// before a quality level was configured or the segment length changed.
//
// Unlike those four it schedules rather than runs: a streaming encode is the most
// expensive thing this application does and drains one clip at a time in the
// worker, so the answer is a step and its state, not a result. Watch it land with
// `ctl photos get <uid>` (the processing report) or `ctl photos renditions <uid>`.
func newCtlPhotosRebuildVideoCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   ctl.RebuildVideo + " <uid>",
		Short: "Encode a video into the streaming renditions a browser plays",
		Long: "Encode a video into the streaming renditions a browser plays.\n\n" +
			"The encode replaces whatever was recorded before, so this both prepares a clip\n" +
			"the encoder never reached and redoes one encoded under an older configuration.\n" +
			"It is queued, not run here: the worker encodes one clip at a time and a long\n" +
			"video is minutes of work. Needs the maintainer role.\n\n" +
			"POST /photos/{uid}/process/hls_transcode.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.PrepareVideo(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("preparing video %s for playback: %w", args[0], err)
			}
			return renderPhotoStep(cmd.OutOrStdout(), out, raw, ctl.RebuildVideo)
		},
	}
}

// newCtlPhotosRebuildStoryboardCmd builds "ctl photos rebuild storyboard": re-cut
// one video's scrub preview.
//
// The discard is the reason it exists. The sprite is rendered lazily on first
// playback and the renderer is a no-op over one that is already cached, so a
// preview cut before the clip's duration was corrected — or from an original that
// has since been replaced — has no other way back. This throws the cached sprite
// away first and only then queues the render.
func newCtlPhotosRebuildStoryboardCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   ctl.RebuildStoryboard + " <uid>",
		Short: "Discard a video's scrub-preview sprite and cut a fresh one",
		Long: "Discard a video's scrub-preview sprite and cut a fresh one.\n\n" +
			"Asking for the preview again would not do this: the renderer skips a sprite\n" +
			"that already exists, so a wrong preview stays wrong. The cached one is deleted\n" +
			"here and the render queued — one full decode of the clip. Needs the maintainer\n" +
			"role.\n\n" +
			"A photo that can never have a preview — a still, a live photo, a clip of\n" +
			"unknown length — is refused rather than queued.\n\n" +
			"POST /photos/{uid}/regenerate-storyboard.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.RebuildStoryboard(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("rebuilding the scrub preview of %s: %w", args[0], err)
			}
			return renderPhotoStep(cmd.OutOrStdout(), out, raw, ctl.RebuildStoryboard)
		},
	}
}

// newCtlPhotosRenditionsCmd builds "ctl photos renditions": what the streaming
// encode actually produced for one video.
//
// It answers the question no other command can. The photo detail says only whether
// the clip streams at all, the master playlist names the qualities but not when
// they were made, and "is what is stored still current?" — after a changed
// rendition plan, a re-encode, a restore — needs the encode stamp beside each one.
func newCtlPhotosRenditionsCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "renditions <uid>",
		Short: "List a video's encoded streaming qualities",
		Long: "List a video's encoded streaming qualities: the picture each one carries, the\n" +
			"bitrate it asks of the connection, how many segments it was cut into, its\n" +
			"measured length and when it was encoded.\n\n" +
			"A still, a video the encoder has not reached and an instance with streaming\n" +
			"switched off all report no renditions; `photos rebuild video <uid>` is what\n" +
			"produces them.\n\n" +
			"GET /photos/{uid}/renditions.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.VideoRenditions(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("listing the renditions of %s: %w", args[0], err)
			}
			return renderVideoRenditions(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlProcessCmd builds the "ctl process" tree: the library-wide backfills that
// schedule a per-photo job over everything that is missing one.
//
// Only the streaming encode is here, and deliberately so. The other `/process/*`
// backfills all have a local counterpart on the machine that runs the instance
// (`kukatko maintenance repair --embeddings --faces --places …`, `kukatko sidecar
// backfill`), and that is where an operator reaches for them. The encode had
// neither: no flag, no subcommand, nothing but curl.
func newCtlProcessCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "process",
		Short: "Schedule a library-wide backfill of derived data",
	}
	cmd.AddCommand(newCtlProcessHLSCmd(opts))
	return cmd
}

// newCtlProcessHLSCmd builds "ctl process hls", the library-wide streaming
// encode.
//
// The default is the repair: only the videos with no rendition at all, which is
// what a restore leaves behind (the backup carries no segments) and what an import
// of videos onto an instance whose box was asleep leaves behind. `--all` is the
// other thing entirely — every live video re-encoded, which is how a library picks
// up a newly configured quality level and is hours of work on anything but a
// handful of clips. That is why it asks for --yes: nothing is destroyed, but a
// mistyped flag would occupy the worker for a day.
func newCtlProcessHLSCmd(opts *ctlOptions) *cobra.Command {
	var all, assumeYes bool
	cmd := &cobra.Command{
		Use:   "hls",
		Short: "Encode every video that has no streaming rendition",
		Long: "Encode every video that has no streaming rendition yet.\n\n" +
			"With --all it schedules every live video instead — a full re-encode of the\n" +
			"library, which is how a newly configured quality level reaches videos that\n" +
			"predate it. That is hours of background work, so it needs --yes.\n\n" +
			"Either way this only fills the queue: the worker encodes one clip at a time,\n" +
			"and the number reported is jobs scheduled, not videos finished. Needs the\n" +
			"maintainer role.\n\n" +
			"POST /process/hls.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && !assumeYes {
				return fmt.Errorf("%w: refusing to re-encode every video in the library "+
					"without --yes; drop --all to schedule only the ones with no rendition",
					errNotConfirmed)
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.BackfillHLS(cmd.Context(), all)
			if err != nil {
				return fmt.Errorf("scheduling the streaming encode: %w", err)
			}
			return renderBackfill(cmd.OutOrStdout(), out, raw, "video")
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&all, "all", false, "re-encode every video, not only the ones with no rendition")
	flags.BoolVarP(&assumeYes, "yes", "y", false, "confirm the full re-encode --all asks for")
	return cmd
}
