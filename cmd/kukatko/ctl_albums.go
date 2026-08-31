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
		Short: "List, inspect, create, edit and delete albums and their membership",
	}
	cmd.AddCommand(
		newCtlAlbumsListCmd(opts), newCtlAlbumsGetCmd(opts), newCtlAlbumsCreateCmd(opts),
		newCtlAlbumsUpdateCmd(opts), newCtlAlbumsDeleteCmd(opts),
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListAlbums(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing albums: %w", err)
			}
			return renderAlbums(cmd.OutOrStdout(), out, raw)
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetAlbum(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching album %s: %w", args[0], err)
			}
			return renderAlbum(cmd.OutOrStdout(), out, raw)
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
			client, out, err := opts.resolve()
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
			return renderAlbum(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Description, "description", "", "album description")
	flags.StringVar(&in.Type, "type", "",
		"album type: album (default), folder, moment, state or month")
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
			client, out, err := opts.resolve()
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
			return renderMembership(cmd.OutOrStdout(), out, raw, albumUID)
		},
	}
	addConfirmFlag(cmd, &assumeYes)
	return cmd
}

// newCtlAlbumsUpdateCmd builds "ctl albums update <uid>", the flag-per-field edit
// of an album's title, description, cover and privacy.
func newCtlAlbumsUpdateCmd(opts *ctlOptions) *cobra.Command {
	var (
		title       string
		description string
		cover       string
		private     bool
	)
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Rename an album or change its description, cover or privacy (editor or admin)",
		Long: "Edit an album's title, description, cover photo or privacy.\n\n" +
			"Only the flags you actually write are changed: the command reads the album\n" +
			"first and sends the rest back untouched, because PATCH /albums/{uid} rewrites\n" +
			"the whole record and a body that omitted the description would empty it.\n\n" +
			"An album's structural type (folder, moment, …) is not editable — the server\n" +
			"generates those groupings — so there is no flag for it. `--cover \"\"` removes\n" +
			"the cover photo. The photos stay where they are either way.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			patch := ctl.AlbumPatch{
				Title:       optionalString(cmd, "title", title),
				Description: optionalString(cmd, "description", description),
				Cover:       optionalString(cmd, "cover", cover),
				Private:     optionalBool(cmd, "private", private),
			}
			raw, err := client.UpdateAlbum(cmd.Context(), args[0], patch)
			if err != nil {
				return fmt.Errorf("updating album %s: %w", args[0], err)
			}
			return renderAlbum(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&title, "title", "", "new album title")
	flags.StringVar(&description, "description", "", "new album description")
	flags.StringVar(&cover, "cover", "", `uid of the cover photo; "" removes the cover`)
	flags.BoolVar(&private, "private", false, "hide the album from non-owners (--private=false unhides)")
	return cmd
}

// newCtlAlbumsDeleteCmd builds "ctl albums delete <uid>".
func newCtlAlbumsDeleteCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete an album, leaving its photos alone (editor or admin)",
		Long: "Delete an album.\n\n" +
			"The photos survive: an album is a grouping, so deleting it deletes only the\n" +
			"grouping. What is lost is the curation — which photos somebody chose, and the\n" +
			"order they chose them in — and that cannot be rebuilt from the library. Pass\n" +
			"--yes to confirm, or --dry-run to see which album would go.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlbumDelete(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runAlbumDelete names the album before removing it, so the confirmation says
// which album went rather than which uid did.
func runAlbumDelete(cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	which, err := client.DescribeAlbum(cmd.Context(), uid)
	if err != nil {
		return fmt.Errorf("fetching album %s: %w", uid, err)
	}
	action := "delete album " + which
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out,
			"dry run: would "+action+"; its photos would stay in the library, nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	if err := client.DeleteAlbum(cmd.Context(), uid); err != nil {
		return fmt.Errorf("deleting album %s: %w", uid, err)
	}
	return renderAck(cmd.OutOrStdout(), out, "album "+which+" deleted; its photos stayed in the library")
}
