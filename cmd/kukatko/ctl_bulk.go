package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/panbotka/kukatko/internal/ctl"
)

// bulkFlags is the raw flag state of "ctl bulk", before the flags that mean
// "leave this alone unless given" are resolved against pflag's Changed.
type bulkFlags struct {
	addAlbums    []string
	removeAlbums []string
	addLabels    []string
	removeLabels []string

	caption          string
	clearCaption     bool
	description      string
	clearDescription bool
	location         string
	clearLocation    bool

	private   bool
	favorite  bool
	archive   bool
	unarchive bool
	rating    int
	flag      string
}

// newCtlBulkCmd builds "ctl bulk", one metadata edit applied to many photos.
//
// The uids come from the arguments or, when there are none, from stdin — so the
// command is the natural sink of `ctl photos list -o json`, which it parses
// directly, as it does a bare uid array or a plain newline-separated list. The
// whole batch travels in a single POST /photos/bulk, matching the server's
// one-transaction contract; there is deliberately no per-photo loop.
func newCtlBulkCmd(opts *ctlOptions) *cobra.Command {
	var (
		flags     bulkFlags
		assumeYes bool
	)
	cmd := &cobra.Command{
		Use:   "bulk [<photo-uid>…]",
		Short: "Apply one metadata edit to many photos in a single transaction (editor or admin)",
		Long: "Apply one metadata edit to many photos in a single transaction.\n\n" +
			"Photo uids are read from the arguments, or from stdin when none are given, so this\n" +
			"composes with `kukatkoctl photos list -o json`, with a bare JSON array of uids, or\n" +
			"with a plain newline-separated list.\n\n" +
			"The whole batch is one request: the server applies it in one transaction, so it\n" +
			"either lands as a unit or not at all.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			// Resolve and check the operations before touching stdin: a typo in a flag
			// must not consume the uid list the operator piped in.
			ops, err := flags.operations(cmd.Flags())
			if err != nil {
				return err
			}
			if err := ops.Validate(); err != nil {
				return fmt.Errorf("checking the bulk operations: %w", err)
			}
			uids, fromStdin, err := photoUIDsFromArgs(cmd, args)
			if err != nil {
				return err
			}
			action := fmt.Sprintf("apply this edit to %d photos", len(uids))
			if err := confirmBatch(cmd, len(uids), assumeYes, fromStdin, action); err != nil {
				return err
			}
			raw, err := client.Bulk(cmd.Context(), uids, ops)
			if err != nil {
				return fmt.Errorf("applying the bulk edit: %w", err)
			}
			return renderBulkResult(cmd.OutOrStdout(), format, raw)
		},
	}
	flags.register(cmd)
	addConfirmFlag(cmd, &assumeYes)
	return cmd
}

// register declares every operation flag. The names mirror the API's own operation
// keys, so `-o json` output and the flag that produced it read the same.
func (f *bulkFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringArrayVar(&f.addAlbums, "add-album", nil, "add the photos to this album uid (repeatable)")
	flags.StringArrayVar(&f.removeAlbums, "remove-album", nil, "remove the photos from this album uid (repeatable)")
	flags.StringArrayVar(&f.addLabels, "add-label", nil, "attach this label uid to the photos (repeatable)")
	flags.StringArrayVar(&f.removeLabels, "remove-label", nil, "detach this label uid from the photos (repeatable)")

	flags.StringVar(&f.caption, "set-caption", "", "set the photo title")
	flags.BoolVar(&f.clearCaption, "clear-caption", false, "empty the photo title")
	flags.StringVar(&f.description, "set-description", "", "set the photo description")
	flags.BoolVar(&f.clearDescription, "clear-description", false, "empty the photo description")
	flags.StringVar(&f.location, "location", "", `set the GPS position, as "lat,lng"`)
	flags.BoolVar(&f.clearLocation, "clear-location", false, "remove the GPS position")

	flags.BoolVar(&f.private, "private", false, "mark the photos private (--private=false unmarks them)")
	flags.BoolVar(&f.favorite, "favorite", false, "favorite the photos (--favorite=false unfavorites them)")
	flags.BoolVar(&f.archive, "archive", false, "move the photos to the trash")
	flags.BoolVar(&f.unarchive, "unarchive", false, "restore the photos from the trash")
	flags.IntVar(&f.rating, "rating", 0, "set the star rating, 0 to 5")
	flags.StringVar(&f.flag, "flag", "", "set the cull flag: none, pick or reject")
}

// operations resolves the flag state into the API's operation set.
//
// The flags that carry a value which is also a meaningful "unset" — a false bool,
// a zero rating, an empty flag — are only sent when pflag saw them on the command
// line. Otherwise `ctl bulk --add-label x` would silently also unfavorite every
// photo it touched and set its rating to zero.
func (f *bulkFlags) operations(flags *pflag.FlagSet) (ctl.BulkOperations, error) {
	ops := ctl.BulkOperations{
		AddAlbums:        f.addAlbums,
		RemoveAlbums:     f.removeAlbums,
		AddLabels:        f.addLabels,
		RemoveLabels:     f.removeLabels,
		ClearCaption:     f.clearCaption,
		ClearDescription: f.clearDescription,
		ClearLocation:    f.clearLocation,
		Archive:          f.archive,
		Unarchive:        f.unarchive,
	}
	if flags.Changed("set-caption") {
		ops.SetCaption = &f.caption
	}
	if flags.Changed("set-description") {
		ops.SetDescription = &f.description
	}
	if flags.Changed("private") {
		ops.SetPrivate = &f.private
	}
	if flags.Changed("favorite") {
		ops.SetFavorite = &f.favorite
	}
	if flags.Changed("rating") {
		ops.SetRating = &f.rating
	}
	if flags.Changed("flag") {
		ops.SetFlag = &f.flag
	}
	if flags.Changed("location") {
		location, err := ctl.ParseLocation(f.location)
		if err != nil {
			return ctl.BulkOperations{}, fmt.Errorf("reading --location: %w", err)
		}
		ops.SetLocation = location
	}
	return ops, nil
}
