package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlAlbumsCmd builds the "ctl albums" tree: albums and their membership,
// served by internal/organizeapi. Listing needs any role; creating and editing
// membership need the editor or admin role.
func newCtlAlbumsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "albums",
		Short: "List, inspect, create albums and edit their membership",
	}
	cmd.AddCommand(
		newCtlAlbumsListCmd(opts), newCtlAlbumsGetCmd(opts), newCtlAlbumsCreateCmd(opts),
		newCtlAlbumsAddPhotosCmd(opts), newCtlAlbumsRemovePhotosCmd(opts),
	)
	return cmd
}

// newCtlAlbumsListCmd builds "ctl albums list", the bare {"albums": […]} list.
// It is not paginated: GET /albums returns every album, each with its photo count.
func newCtlAlbumsListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every album with its photo count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListAlbums(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing albums: %w", err)
			}
			return renderAlbums(cmd.OutOrStdout(), format, raw)
		},
	}
}

// newCtlAlbumsGetCmd builds "ctl albums get <uid>", one album's detail.
func newCtlAlbumsGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one album's detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetAlbum(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching album %s: %w", args[0], err)
			}
			return renderAlbum(cmd.OutOrStdout(), format, raw)
		},
	}
}

// newCtlAlbumsCreateCmd builds "ctl albums create <title>". The server derives the
// uid and a unique slug; everything but the title is optional.
func newCtlAlbumsCreateCmd(opts *ctlOptions) *cobra.Command {
	var (
		in    ctl.AlbumInput
		cover string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create an album (editor or admin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			in.Title = args[0]
			if cover != "" {
				in.CoverPhotoUID = &cover
			}
			raw, err := client.CreateAlbum(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating album %q: %w", in.Title, err)
			}
			return renderAlbum(cmd.OutOrStdout(), format, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Description, "description", "", "album description")
	flags.StringVar(&in.Type, "type", "",
		"album type: album (default), folder, moment, state or month")
	flags.StringVar(&in.OrderBy, "order-by", "", "photo order inside the album (default: added)")
	flags.StringVar(&cover, "cover", "", "uid of the photo to use as the cover")
	flags.BoolVar(&in.Private, "private", false, "hide the album from non-owners")
	return cmd
}

// albumMembership is one of the two membership calls, bound to a resolved client.
type albumMembership func(ctx context.Context, uid string, photoUIDs []string) (json.RawMessage, error)

// newCtlAlbumsAddPhotosCmd builds "ctl albums add-photos <album-uid> [<photo-uid>…]",
// which appends photos after the ones already in the album.
func newCtlAlbumsAddPhotosCmd(opts *ctlOptions) *cobra.Command {
	return newCtlAlbumMembershipCmd(opts, membershipSpec{
		use:         "add-photos <album-uid> [<photo-uid>…]",
		short:       "Add photos to an album (editor or admin)",
		verb:        "add",
		preposition: "to",
		pick:        func(c *ctl.Client) albumMembership { return c.AddAlbumPhotos },
	})
}

// newCtlAlbumsRemovePhotosCmd builds "ctl albums remove-photos <album-uid> [<photo-uid>…]".
// Removing a photo that is not a member is a no-op.
func newCtlAlbumsRemovePhotosCmd(opts *ctlOptions) *cobra.Command {
	return newCtlAlbumMembershipCmd(opts, membershipSpec{
		use:         "remove-photos <album-uid> [<photo-uid>…]",
		short:       "Remove photos from an album (editor or admin)",
		verb:        "remove",
		preposition: "from",
		pick:        func(c *ctl.Client) albumMembership { return c.RemoveAlbumPhotos },
	})
}

// membershipSpec is what distinguishes the add and remove membership commands:
// their help text, the phrase the confirmation prompt uses, and the client call.
type membershipSpec struct {
	use         string
	short       string
	verb        string
	preposition string
	pick        func(*ctl.Client) albumMembership
}

// confirmPhrase renders what this command is about to do, for the batch prompt.
func (s membershipSpec) confirmPhrase(count int, albumUID string) string {
	return fmt.Sprintf("%s %d photos %s album %s", s.verb, count, s.preposition, albumUID)
}

// newCtlAlbumMembershipCmd builds one membership command. Both read the photo uids
// from the arguments after the album uid, or from stdin when none are given, and
// both ask before touching more than ctl.ConfirmThreshold photos.
func newCtlAlbumMembershipCmd(opts *ctlOptions, spec membershipSpec) *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long: spec.short + ".\n\n" +
			"Photo uids are read from the arguments, or from stdin when none are given, so this\n" +
			"composes with `kukatkoctl photos list -o json`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			albumUID := args[0]
			photoUIDs, fromStdin, err := photoUIDsFromArgs(cmd, args[1:])
			if err != nil {
				return err
			}
			action := spec.confirmPhrase(len(photoUIDs), albumUID)
			if err := confirmBatch(cmd, len(photoUIDs), assumeYes, fromStdin, action); err != nil {
				return err
			}
			raw, err := spec.pick(client)(cmd.Context(), albumUID, photoUIDs)
			if err != nil {
				return fmt.Errorf("updating album %s: %w", albumUID, err)
			}
			return renderMembership(cmd.OutOrStdout(), format, raw, albumUID)
		},
	}
	addConfirmFlag(cmd, &assumeYes)
	return cmd
}
