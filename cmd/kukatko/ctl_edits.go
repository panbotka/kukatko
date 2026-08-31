package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlEditsCmd builds the "ctl edits" tree: the **non-destructive image edit**
// — crop, rotation, brightness, contrast — that decides how the library renders
// a photo (`internal/photoedit`). Reading needs any role, writing editor or admin.
//
// It is not `ctl photos edit`, which writes the photo's **metadata** (title,
// date, place). Nothing here touches the original file: the edit is stored beside
// the photo and the derived thumbnails are re-rendered from it.
func newCtlEditsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edits",
		Short: "Read and write a photo's non-destructive image edit (crop, rotation, brightness, contrast)",
		Long: "The non-destructive image edit: how the library renders a photo, without ever\n" +
			"rewriting the original file.\n\n" +
			"This is not `ctl photos edit`, which writes the photo's metadata. Here the\n" +
			"crop, the rotation and the two adjustments are stored beside the photo, and\n" +
			"the thumbnails the grid, the search results and the map show are re-rendered\n" +
			"from them in the background.",
	}
	cmd.AddCommand(newCtlEditsGetCmd(opts), newCtlEditsSetCmd(opts), newCtlEditsResetCmd(opts))
	return cmd
}

// newCtlEditsGetCmd builds "ctl edits get <uid>".
func newCtlEditsGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show a photo's stored image edit",
		Long: "Show a photo's stored image edit.\n\n" +
			"A photo nobody has edited reads back as the neutral edit — no crop, no\n" +
			"rotation, no adjustment — which the table says outright, so \"everything is\n" +
			"zero\" cannot be mistaken for \"nobody has looked\".",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetImageEdit(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching the image edit of photo %s: %w", args[0], err)
			}
			return renderImageEdit(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlEditsSetCmd builds "ctl edits set <uid> [flags]".
func newCtlEditsSetCmd(opts *ctlOptions) *cobra.Command {
	var (
		crop       string
		clearCrop  bool
		rotation   int
		brightness float64
		contrast   float64
	)
	cmd := &cobra.Command{
		Use:   "set <uid>",
		Short: "Change a photo's crop, rotation, brightness or contrast (editor or admin)",
		Long: "Change how a photo renders.\n\n" +
			"Only the flags you actually write are changed: the command reads the stored\n" +
			"edit first and sends the rest back untouched, because PUT /photos/{uid}/edit\n" +
			"replaces the whole edit and a body carrying only a rotation would silently\n" +
			"drop an existing crop.\n\n" +
			"--crop takes x,y,w,h as fractions of the whole image, so the same rectangle\n" +
			"survives a thumbnail of any size; --clear-crop removes it and leaves the rest\n" +
			"alone. To clear everything at once use `ctl edits reset`.\n\n" +
			"The original file is never rewritten.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			patch, err := imageEditPatch(cmd, crop, clearCrop, rotation, brightness, contrast)
			if err != nil {
				return err
			}
			raw, err := client.SetImageEdit(cmd.Context(), args[0], patch)
			if err != nil {
				return fmt.Errorf("editing photo %s: %w", args[0], err)
			}
			return renderImageEdit(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&crop, "crop", "", "crop rectangle as x,y,w,h in 0..1 (e.g. 0.1,0.1,0.8,0.8)")
	flags.BoolVar(&clearCrop, "clear-crop", false, "remove the crop, leaving the rest of the edit alone")
	flags.IntVar(&rotation, "rotate", 0, "rotation in degrees clockwise: 0, 90, 180 or 270")
	flags.Float64Var(&brightness, "brightness", 0, "brightness between -1 and 1 (0 is neutral)")
	flags.Float64Var(&contrast, "contrast", 0, "contrast between -1 and 1 (0 is neutral)")
	return cmd
}

// imageEditPatch builds the patch from the flags the caller actually wrote,
// rejecting the one contradiction that needs no round trip.
func imageEditPatch(
	cmd *cobra.Command, crop string, clearCrop bool, rotation int, brightness, contrast float64,
) (ctl.ImageEditPatch, error) {
	patch := ctl.ImageEditPatch{
		ClearCrop:  clearCrop,
		Rotation:   optionalInt(cmd, "rotate", rotation),
		Brightness: optionalFloat(cmd, "brightness", brightness),
		Contrast:   optionalFloat(cmd, "contrast", contrast),
	}
	if cmd.Flags().Changed("crop") {
		if clearCrop {
			return ctl.ImageEditPatch{}, fmt.Errorf(
				"%w: --crop and --clear-crop say opposite things", ctl.ErrInvalidCrop)
		}
		parsed, err := ctl.ParseCrop(crop)
		if err != nil {
			return ctl.ImageEditPatch{}, fmt.Errorf("reading --crop: %w", err)
		}
		patch.Crop = &parsed
	}
	return patch, nil
}

// newCtlEditsResetCmd builds "ctl edits reset <uid>".
func newCtlEditsResetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "reset <uid>",
		Short: "Save the neutral edit, rendering the photo exactly as the file has it (editor or admin)",
		Long: "Save the neutral edit: no crop, no rotation, no adjustment.\n\n" +
			"This is a write, not the absence of one. The thumbnails the library shows were\n" +
			"rendered through the previous edit and are cached against the original's hash,\n" +
			"so only saving the neutral edit actually puts the photo back the way the file\n" +
			"has it. The original was never changed in the first place.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ResetImageEdit(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("resetting the image edit of photo %s: %w", args[0], err)
			}
			return renderImageEdit(cmd.OutOrStdout(), out, raw)
		},
	}
}
