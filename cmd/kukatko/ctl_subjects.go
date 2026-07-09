package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlSubjectsCmd builds the "ctl subjects" tree: the people, pets and other
// recurring subjects the face pipeline groups markers under, served by
// internal/peopleapi. The whole tree is read-only; creating and editing subjects
// belongs to the UI, where a face gallery makes the decision reviewable.
func newCtlSubjectsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subjects",
		Short: "List and inspect subjects, and browse a subject's photos",
	}
	cmd.AddCommand(newCtlSubjectsListCmd(opts), newCtlSubjectsGetCmd(opts), newCtlSubjectsPhotosCmd(opts))
	return cmd
}

// newCtlSubjectsListCmd builds "ctl subjects list", the bare {"subjects": […]}
// list in the API's name order. It is not paginated.
func newCtlSubjectsListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every subject with its face-marker count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListSubjects(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing subjects: %w", err)
			}
			return renderSubjects(cmd.OutOrStdout(), format, raw)
		},
	}
}

// newCtlSubjectsGetCmd builds "ctl subjects get <uid>", one subject's detail.
func newCtlSubjectsGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one subject's detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetSubject(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching subject %s: %w", args[0], err)
			}
			return renderSubject(cmd.OutOrStdout(), format, raw)
		},
	}
}

// newCtlSubjectsPhotosCmd builds "ctl subjects photos <uid>", the subject's photo
// gallery. It is the one subject endpoint that pages, and it answers with the
// /photos envelope, so it renders as a photo list.
func newCtlSubjectsPhotosCmd(opts *ctlOptions) *cobra.Command {
	var page ctl.PageOptions
	cmd := &cobra.Command{
		Use:   "photos <uid>",
		Short: "List the photos a subject appears in, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, format, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.SubjectPhotos(cmd.Context(), args[0], page)
			if err != nil {
				return fmt.Errorf("listing photos of subject %s: %w", args[0], err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), format, raw)
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&page.Limit, "limit", 0, "photos per page (0 = server default)")
	flags.IntVar(&page.Offset, "offset", 0, "photos to skip; the summary line prints the next offset")
	return cmd
}
