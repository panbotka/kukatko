package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlSubjectsCmd builds the "ctl subjects" tree: the people, pets and other
// recurring subjects the face pipeline groups markers under, served by
// internal/peopleapi.
//
// Reading needs any role, writing the editor or admin one. Merging and deleting
// destroy something the library cannot get back, so both carry --yes and
// --dry-run; there is no `edit`, because PATCH /subjects/{uid} rewrites the whole
// record and a partial edit offered as a flag would quietly erase what it did not
// mention. `rename` is that edit done safely, by reading the record first.
func newCtlSubjectsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subjects",
		Short: "List, inspect, create, rename, merge and delete the people in the library",
	}
	cmd.AddCommand(
		newCtlSubjectsListCmd(opts), newCtlSubjectsGetCmd(opts), newCtlSubjectsPhotosCmd(opts),
		newCtlSubjectsCreateCmd(opts), newCtlSubjectsRenameCmd(opts),
		newCtlSubjectsMergeCmd(opts), newCtlSubjectsDeleteCmd(opts),
	)
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListSubjects(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing subjects: %w", err)
			}
			return renderSubjects(cmd.OutOrStdout(), out, raw)
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetSubject(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching subject %s: %w", args[0], err)
			}
			return renderSubject(cmd.OutOrStdout(), out, raw)
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
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.SubjectPhotos(cmd.Context(), args[0], page)
			if err != nil {
				return fmt.Errorf("listing photos of subject %s: %w", args[0], err)
			}
			return renderPhotoPage(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&page.Limit, "limit", 0, "photos per page (0 = server default)")
	flags.IntVar(&page.Offset, "offset", 0, "photos to skip; the summary line prints the next offset")
	return cmd
}

// newCtlSubjectsCreateCmd builds "ctl subjects create <name>". The server derives
// the uid and a unique slug; everything but the name is optional.
func newCtlSubjectsCreateCmd(opts *ctlOptions) *cobra.Command {
	var (
		in        ctl.SubjectInput
		cover     string
		birthYear int
		deathYear int
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a subject — a person, an animal or anything else recurring (editor or admin)",
		Long: "Create a subject.\n\n" +
			"A name that identifies nobody (punctuation alone, no letter and no digit) is\n" +
			"refused by the server: it would have no slug of its own and become a magnet for\n" +
			"every later find-or-create by name.\n\n" +
			"Naming a face is usually enough to create the person — `ctl faces assign\n" +
			"--name` and `ctl clusters assign --name` create one on the spot. Use this when\n" +
			"the record needs more than a name.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			in.Name = args[0]
			if cover != "" {
				in.CoverPhotoUID = &cover
			}
			in.BirthYear = optionalInt(cmd, "birth-year", birthYear)
			in.DeathYear = optionalInt(cmd, "death-year", deathYear)
			raw, err := client.CreateSubject(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating subject %q: %w", in.Name, err)
			}
			return renderSubject(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Type, "type", "", "subject type: person (default), pet or other")
	flags.StringVar(&in.Notes, "notes", "", "free-text note about the subject")
	flags.StringVar(&cover, "cover", "", "uid of the photo to illustrate the subject with")
	flags.BoolVar(&in.Favorite, "favorite", false, "mark the subject as a favorite")
	flags.BoolVar(&in.Private, "private", false, "hide the subject from non-owners")
	flags.IntVar(&birthYear, "birth-year", 0, "the year the person was born (1800…this year)")
	flags.IntVar(&deathYear, "death-year", 0, "the year the person died (not before --birth-year)")
	return cmd
}

// optionalInt returns a pointer to value only when the named flag was actually
// given, so an unset year reaches the API as "unknown" rather than as the year 0
// its validation would refuse.
func optionalInt(cmd *cobra.Command, name string, value int) *int {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

// newCtlSubjectsRenameCmd builds "ctl subjects rename <uid> <name>".
func newCtlSubjectsRenameCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <uid> <name>",
		Short: "Change a subject's name, leaving the rest of the record alone (editor or admin)",
		Long: "Change a subject's name.\n\n" +
			"The record is read before it is written, because PATCH /subjects/{uid} rewrites\n" +
			"the whole editable set: a body carrying the new name alone would reclassify a\n" +
			"pet as a person and erase the notes, the cover photo and the life years with it.\n\n" +
			"The slug is re-derived server-side and the cached name on every one of the\n" +
			"subject's faces is refreshed; the markers themselves do not move.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.RenameSubject(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("renaming subject %s: %w", args[0], err)
			}
			return renderSubject(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlSubjectsMergeCmd builds "ctl subjects merge <source-uid> <keeper-uid>",
// the repair for the same person recorded twice.
func newCtlSubjectsMergeCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "merge <source-uid> <keeper-uid>",
		Short: "Merge one person into another; the first is deleted (editor or admin)",
		Long: "Merge the first subject into the second. Everything the source carried —\n" +
			"markers, the faces cache, confirmations, rejections, dismissals — moves onto\n" +
			"the keeper, whose empty fields are filled from it, and the source is deleted in\n" +
			"the same transaction.\n\n" +
			"**This cannot be undone.** The source's name survives nowhere afterwards, which\n" +
			"is why the result names both people rather than echoing the server's uids. Pass\n" +
			"--yes to confirm, or --dry-run to see who would be merged into whom.\n\n" +
			"Markers are never deduplicated: a photo carrying both people keeps both, and\n" +
			"becomes a repeated-marker group `GET /duplicate-markers` surfaces.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubjectMerge(cmd, opts, args[0], args[1], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runSubjectMerge resolves both people by name, gates the merge, and reports what
// it moved. Both subjects are fetched first: it turns a mistyped uid into a 404
// before anything is destroyed, and it is the only chance to learn the source's
// name, which the merge itself deletes.
func runSubjectMerge(
	cmd *cobra.Command, opts *ctlOptions, sourceUID, keeperUID string, assumeYes, dryRun bool,
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	source, keeper, err := fetchMergePair(cmd, client, sourceUID, keeperUID)
	if err != nil {
		return err
	}
	action := "merge " + ctl.SubjectLabel(source.Name, source.UID) +
		" into " + ctl.SubjectLabel(keeper.Name, keeper.UID)
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out, "dry run: would "+action+
			"; the merged-away subject would be deleted and nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	raw, err := client.MergeSubjects(cmd.Context(), sourceUID, keeperUID)
	if err != nil {
		return fmt.Errorf("merging subject %s into %s: %w", sourceUID, keeperUID, err)
	}
	result, err := ctl.DecodeMergeResult(raw)
	if err != nil {
		return fmt.Errorf("merging subject %s into %s: %w", sourceUID, keeperUID, err)
	}
	report := ctl.MergeReport{MergeResult: result, SourceName: source.Name, KeeperName: keeper.Name}
	return renderMergeReport(cmd.OutOrStdout(), out, report)
}

// fetchMergePair reads both subjects of a merge, refusing a merge of a subject
// into itself before either lookup can make it look meaningful.
func fetchMergePair(
	cmd *cobra.Command, client *ctl.Client, sourceUID, keeperUID string,
) (ctl.Subject, ctl.Subject, error) {
	if sourceUID == keeperUID {
		return ctl.Subject{}, ctl.Subject{}, fmt.Errorf("%w: %s", ctl.ErrMergeIntoSelf, sourceUID)
	}
	source, err := client.FetchSubject(cmd.Context(), sourceUID)
	if err != nil {
		return ctl.Subject{}, ctl.Subject{}, fmt.Errorf("fetching subject %s: %w", sourceUID, err)
	}
	keeper, err := client.FetchSubject(cmd.Context(), keeperUID)
	if err != nil {
		return ctl.Subject{}, ctl.Subject{}, fmt.Errorf("fetching subject %s: %w", keeperUID, err)
	}
	return source, keeper, nil
}

// newCtlSubjectsDeleteCmd builds "ctl subjects delete <uid>".
func newCtlSubjectsDeleteCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a subject, detaching every face that named them (editor or admin)",
		Long: "Delete a subject.\n\n" +
			"The markers survive, unnamed: the photos keep their faces, they simply stop\n" +
			"naming anybody. Nothing is archived and no photo is touched.\n\n" +
			"**This cannot be undone** — the name, the notes and the life years go with it,\n" +
			"and re-creating the person will not re-attach a single face. If the two records\n" +
			"are the same person, `ctl subjects merge` is what you want instead. Pass --yes\n" +
			"to confirm, or --dry-run to see who would go.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubjectDelete(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runSubjectDelete names the subject before removing it, so the confirmation says
// who went rather than which uid did.
func runSubjectDelete(
	cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool,
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	who, err := client.DescribeSubject(cmd.Context(), uid)
	if err != nil {
		return fmt.Errorf("fetching subject %s: %w", uid, err)
	}
	action := "delete " + who
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out, "dry run: would "+action+
			", detaching every face that names them; nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	if err := client.DeleteSubject(cmd.Context(), uid); err != nil {
		return fmt.Errorf("deleting subject %s: %w", uid, err)
	}
	return renderAck(cmd.OutOrStdout(), out, "subject "+who+" deleted; the faces that named them are unnamed")
}
