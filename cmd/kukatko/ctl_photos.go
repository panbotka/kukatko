package main

import (
	"fmt"
	"strings"

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
	cmd.AddCommand(
		newCtlPhotosListCmd(opts), newCtlPhotosGetCmd(opts), newCtlPhotosSearchCmd(opts),
		newCtlPhotosImageCmd(opts), newCtlPhotosEditCmd(opts), newCtlPhotosFacesCmd(opts),
		newCtlPhotosSimilarCmd(opts),
	)
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListPhotos(cmd.Context(), list)
			if err != nil {
				return fmt.Errorf("listing photos: %w", err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), out, raw)
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

// newCtlPhotosGetCmd builds "ctl photos get <uid>": the whole photo in one
// request — its metadata with the provenance of the date and the location, its
// memberships, the text the recogniser read in it, and who is on it.
func newCtlPhotosGetCmd(opts *ctlOptions) *cobra.Command {
	detail := ctl.PhotoDetailOptions{People: true}
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one photo's full detail, including who is on it",
		Long: "Show one photo's full detail: its metadata with the provenance of the date and\n" +
			"the location, its albums and labels, the text the recogniser read in it, and who\n" +
			"is on it — the named subjects plus however many detections nobody has named yet.\n\n" +
			"Reading the photo whole is the point of this command, so the roll-call is asked\n" +
			"for by default. The server assembles it only on request (matching the detections\n" +
			"against the markers is work a plain read should not pay for), so\n" +
			"--people=false skips it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetPhoto(cmd.Context(), args[0], detail)
			if err != nil {
				return fmt.Errorf("fetching photo %s: %w", args[0], err)
			}
			return renderPhotoDetail(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().BoolVar(&detail.People, "people", true,
		"report who is on the photo; --people=false skips the face↔marker match")
	return cmd
}

// newCtlPhotosImageCmd builds "ctl photos image <uid>", which saves one rendition
// of a photo to a file so an agent can actually look at it.
func newCtlPhotosImageCmd(opts *ctlOptions) *cobra.Command {
	var (
		size   string
		output string
	)
	cmd := &cobra.Command{
		Use:   "image <uid>",
		Short: "Save a rendition of a photo to a file and print its path",
		Long: "Save one rendition of a photo to a file and print the path.\n\n" +
			"--size takes one of:\n  " + strings.Join(ctl.RenditionSizes(), ", ") + "\n" +
			"The last of them is the stored file itself, at full size and in its own format,\n" +
			"a video included; the rest are cached thumbnails.\n\n" +
			"The bytes are streamed from the socket to the file and are never held in memory,\n" +
			"and the file appears at its final name only once the download is complete.\n\n" +
			"The flag is --output-file, not --output: -o/--output is already the output\n" +
			"format, and a local flag of that name would shadow it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			saved, err := client.SaveRendition(cmd.Context(), args[0], size, output)
			if err != nil {
				return fmt.Errorf("saving photo %s: %w", args[0], err)
			}
			return renderRendition(cmd.OutOrStdout(), out, saved)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&size, "size", ctl.DefaultRenditionSize,
		"rendition: a thumbnail size or "+ctl.RenditionOriginal)
	flags.StringVarP(&output, "output-file", "f", "",
		"where to write it (default: the working directory, named after the response)")
	return cmd
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			search.Query = args[0]
			raw, err := client.SearchPhotos(cmd.Context(), search)
			if err != nil {
				return fmt.Errorf("searching photos: %w", err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), out, raw)
		},
	}
	addPagingFlags(cmd, &search.List)
	addFilterFlags(cmd, &search.List)
	cmd.Flags().StringVar(&search.Mode, "mode", ctl.SearchHybrid,
		"ranking mode: fulltext, semantic or hybrid")
	return cmd
}

// newCtlPhotosSimilarCmd builds "ctl photos similar <uid>", the photo's visual
// neighbourhood.
func newCtlPhotosSimilarCmd(opts *ctlOptions) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "similar <uid>",
		Short: "List the photos that look most like this one, nearest first",
		Long: "List the photos nearest this one in the embedding space, nearest first, with\n" +
			"the cosine distance to each — without the distance a neighbour list is just a\n" +
			"list, and \"how alike\" is the whole question. The photo itself is excluded, and\n" +
			"so is every non-primary member of a stack.\n\n" +
			"An empty answer means \"nothing to compare with\" as often as \"nothing alike\":\n" +
			"a photo the box has not embedded yet, and an instance with no embeddings\n" +
			"backend at all, both answer with an empty list rather than an error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListSimilar(cmd.Context(), args[0], limit)
			if err != nil {
				return fmt.Errorf("finding photos similar to %s: %w", args[0], err)
			}
			return renderSimilar(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0,
		"how many neighbours to return, 1…100 (0 = the server's default of 24)")
	return cmd
}
