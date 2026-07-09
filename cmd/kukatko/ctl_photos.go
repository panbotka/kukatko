package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlPhotosCmd builds the "ctl photos" tree: the read side of the photo
// catalogue, served by internal/photoapi.
func newCtlPhotosCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "photos",
		Short: "Browse the photo catalogue on a running server",
	}
	cmd.AddCommand(newCtlPhotosListCmd(opts), newCtlPhotosGetCmd(opts), newCtlPhotosSearchCmd(opts))
	return cmd
}

// addPagingFlags registers the paging flags shared by list and search.
func addPagingFlags(cmd *cobra.Command, list *ctl.ListOptions) {
	flags := cmd.Flags()
	flags.IntVar(&list.Limit, "limit", 0, "photos per page (0 = server default 100, capped at 500)")
	flags.IntVar(&list.Offset, "offset", 0, "photos to skip; the summary line prints the next offset")
}

// addFilterFlags registers the catalogue filters shared by list and search.
//
// --favorite is deliberately absent: GET /search never reads the parameter, so
// offering the flag there would silently return unfiltered results. Only list
// registers it.
func addFilterFlags(cmd *cobra.Command, list *ctl.ListOptions) {
	flags := cmd.Flags()
	flags.IntVar(&list.Year, "year", 0, "keep only photos taken in this calendar year")
	flags.StringVar(&list.Album, "album", "", "keep only photos in this album (uid)")
	flags.StringVar(&list.Label, "label", "", "keep only photos carrying this label (uid)")
	flags.StringVar(&list.Archived, "archived", "",
		`archived photos: "false" (default), "true" to include, "only" for the trash`)
}

// newCtlPhotosListCmd builds "ctl photos list", a page of GET /photos.
func newCtlPhotosListCmd(opts *ctlOptions) *cobra.Command {
	var list ctl.ListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List photos with the catalogue filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListPhotos(cmd.Context(), list)
			if err != nil {
				return fmt.Errorf("listing photos: %w", err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), format, raw)
		},
	}
	addPagingFlags(cmd, &list)
	addFilterFlags(cmd, &list)
	flags := cmd.Flags()
	flags.BoolVar(&list.Favorite, "favorite", false, "keep only your own favorites")
	flags.StringVar(&list.Sort, "sort", "",
		"sort key: newest, oldest, taken_at, added, title, size or rating")
	flags.StringVar(&list.Order, "order", "", "sort direction: asc or desc (default: the sort key's own)")
	return cmd
}

// newCtlPhotosGetCmd builds "ctl photos get <uid>", the full detail of one photo.
func newCtlPhotosGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one photo's full detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetPhoto(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching photo %s: %w", args[0], err)
			}
			return renderPhotoDetail(cmd.OutOrStdout(), format, raw)
		},
	}
}

// newCtlPhotosSearchCmd builds "ctl photos search <query>", a page of GET /search.
// When the embeddings sidecar is offline the server falls back to full-text
// ranking; the summary line then says the result is degraded.
func newCtlPhotosSearchCmd(opts *ctlOptions) *cobra.Command {
	var search ctl.SearchOptions
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search photos by text, semantics, or both",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			search.Query = args[0]
			raw, err := client.SearchPhotos(cmd.Context(), search)
			if err != nil {
				return fmt.Errorf("searching photos: %w", err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), format, raw)
		},
	}
	addPagingFlags(cmd, &search.List)
	addFilterFlags(cmd, &search.List)
	cmd.Flags().StringVar(&search.Mode, "mode", ctl.SearchHybrid,
		"ranking mode: fulltext, semantic or hybrid")
	return cmd
}
