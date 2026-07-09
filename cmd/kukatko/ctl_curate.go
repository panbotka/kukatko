package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlFavoritesCmd builds the "ctl favorites" tree. Favorites are per-user, not
// global: the list is scoped to whoever the token belongs to, and so is every
// toggle. Any logged-in role may curate its own.
func newCtlFavoritesCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "favorites",
		Short: "List and edit your own favorite photos",
	}
	cmd.AddCommand(newCtlFavoritesListCmd(opts), newCtlFavoritesAddCmd(opts), newCtlFavoritesRemoveCmd(opts))
	return cmd
}

// newCtlFavoritesListCmd builds "ctl favorites list", a page of GET /favorites.
// It shares the /photos envelope and the catalogue filters, minus --favorite,
// which the endpoint applies by itself.
func newCtlFavoritesListCmd(opts *ctlOptions) *cobra.Command {
	var list ctl.ListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the photos you have favorited",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListFavorites(cmd.Context(), list)
			if err != nil {
				return fmt.Errorf("listing favorites: %w", err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), format, raw)
		},
	}
	addPagingFlags(cmd, &list)
	addFilterFlags(cmd, &list)
	flags := cmd.Flags()
	flags.StringVar(&list.Sort, "sort", "",
		"sort key: newest, oldest, taken_at, added, title, size or rating")
	flags.StringVar(&list.Order, "order", "", "sort direction: asc or desc (default: the sort key's own)")
	return cmd
}

// newCtlFavoritesAddCmd builds "ctl favorites add <uid>". It is idempotent.
func newCtlFavoritesAddCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "add <uid>",
		Short: "Favorite one photo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.AddFavorite(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("favoriting photo %s: %w", args[0], err)
			}
			return renderAck(cmd.OutOrStdout(), format, "photo "+args[0]+" favorited")
		},
	}
}

// newCtlFavoritesRemoveCmd builds "ctl favorites remove <uid>". It is idempotent.
func newCtlFavoritesRemoveCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <uid>",
		Short: "Unfavorite one photo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.RemoveFavorite(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("unfavoriting photo %s: %w", args[0], err)
			}
			return renderAck(cmd.OutOrStdout(), format, "photo "+args[0]+" unfavorited")
		},
	}
}

// newCtlRatingCmd builds the "ctl rating" tree. Star ratings and pick/reject flags
// are per-user, exactly like favorites, so any logged-in role may set its own.
func newCtlRatingCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rating",
		Short: "Set and clear your own star ratings and cull flags",
	}
	cmd.AddCommand(newCtlRatingSetCmd(opts), newCtlRatingClearCmd(opts))
	return cmd
}

// newCtlRatingSetCmd builds "ctl rating set <uid> [<0-5>] [--flag …]". The star
// value and the flag are independent: setting one leaves the other untouched, and
// at least one must be given.
func newCtlRatingSetCmd(opts *ctlOptions) *cobra.Command {
	var flag string
	cmd := &cobra.Command{
		Use:   "set <uid> [<0-5>]",
		Short: "Set a photo's star rating and/or cull flag",
		Long: "Set a photo's star rating and/or cull flag.\n\n" +
			"Give the stars as the second argument, the flag with --flag, or both. Whichever\n" +
			"you omit is left as it was.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			rating, err := parseStars(args)
			if err != nil {
				return err
			}
			flagPtr := optionalString(cmd, "flag", flag)
			if err := client.SetRating(cmd.Context(), args[0], rating, flagPtr); err != nil {
				return fmt.Errorf("rating photo %s: %w", args[0], err)
			}
			return renderAck(cmd.OutOrStdout(), format, "photo "+args[0]+" rated "+ratingSummary(rating, flagPtr))
		},
	}
	cmd.Flags().StringVar(&flag, "flag", "", "cull flag: none, pick or reject")
	return cmd
}

// parseStars reads the optional star argument, returning nil when it is absent so
// the API leaves the existing rating alone.
func parseStars(args []string) (*int, error) {
	if len(args) < 2 {
		return nil, nil //nolint:nilnil // no stars given: nil means "leave the rating unchanged".
	}
	stars, err := strconv.Atoi(args[1])
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a number", ctl.ErrInvalidRating, args[1])
	}
	return &stars, nil
}

// optionalString returns a pointer to value only when the named flag was actually
// given, so an unset flag reaches the API as "leave this alone" rather than as an
// empty string that would clear it.
func optionalString(cmd *cobra.Command, name, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

// ratingSummary describes what a rating command changed, for its confirmation line.
func ratingSummary(rating *int, flag *string) string {
	switch {
	case rating != nil && flag != nil:
		return strconv.Itoa(*rating) + "/5, flag " + *flag
	case rating != nil:
		return strconv.Itoa(*rating) + "/5"
	case flag != nil:
		return "flag " + *flag
	default:
		return "unchanged"
	}
}

// newCtlRatingClearCmd builds "ctl rating clear <uid>", which removes both the
// stars and the flag. It is idempotent.
func newCtlRatingClearCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "clear <uid>",
		Short: "Clear a photo's star rating and cull flag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.ClearRating(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("clearing the rating of photo %s: %w", args[0], err)
			}
			return renderAck(cmd.OutOrStdout(), format, "photo "+args[0]+" rating cleared")
		},
	}
}
