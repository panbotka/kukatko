package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// errPersonDetails indicates the details of a person to create were given
// alongside the uid of a person who already exists, where they could only be
// ignored.
var errPersonDetails = errors.New(
	"--type, --birth-year, --death-year and --notes describe a person this command creates, so they need --name")

// personFlags are the flags that describe a person `family add` may create. They
// are listed once, because both the guard and the help text must agree on them.
var personFlags = []string{"type", "birth-year", "death-year", "notes"}

// newCtlFamilyCmd builds the "ctl family" tree: the genealogy over subjects,
// served by internal/familyapi.
//
// The family is the node, not the edge — a couple (or a lone parent) plus their
// children — so parents, siblings, partners and children are all derived from it
// and cannot contradict each other. That is why there is no `sibling` role and
// nothing to remove between two siblings: the way to record one is to give the
// two children the same parent.
//
// Reading needs any role, writing the editor or admin one. Removing a relation
// carries --yes and --dry-run; adding one does not, because nothing is lost by it.
func newCtlFamilyCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "family",
		Short: "Read and record the genealogy: parents, siblings, partners and children",
	}
	cmd.AddCommand(
		newCtlFamilyRelationsCmd(opts), newCtlFamilyAddCmd(opts),
		newCtlFamilyRemoveCmd(opts), newCtlFamilyEditCmd(opts),
	)
	return cmd
}

// newCtlFamilyRelationsCmd builds "ctl family relations <subject-uid>", the four
// derived lists of one person's immediate family.
func newCtlFamilyRelationsCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "relations <subject-uid>",
		Aliases: []string{"show"},
		Short:   "Show one person's parents, siblings, partners and children",
		Long: "Show one person's immediate family.\n\n" +
			"All four lists are derived from the family rows rather than stored, so they\n" +
			"cannot disagree with each other: a sibling is another child of the family this\n" +
			"person is a child in, and a half-sibling a child of another family one of their\n" +
			"parents is a partner in.\n\n" +
			"FAMILY names the family row each relation is recorded in — it is the uid\n" +
			"`ctl family edit` takes — and KIND what the relation is recorded as: how a child\n" +
			"belongs to their family, or what tied a couple together.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetRelations(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("reading the relations of subject %s: %w", args[0], err)
			}
			return renderRelations(cmd.OutOrStdout(), out, raw)
		},
	}
}

// familyAddRequest is one recorded relation as the command line stated it: on
// whom, in which role, to whom, and — for a person the library has never heard
// of — what little a tree needs to know about them.
type familyAddRequest struct {
	subjectUID string
	role       string
	ref        ctl.SubjectRef
	childKind  string
	person     ctl.NewPerson
}

// input resolves the reference into the body the API takes. A uid is sent as it
// is; a name is looked up among the subjects first, and only a name that matches
// nobody creates a person — the inline half of the endpoint *always* creates, so
// sending a name that already exists would quietly split one person into two.
func (req familyAddRequest) input(ctx context.Context, client *ctl.Client) (ctl.RelationInput, error) {
	in := ctl.RelationInput{Role: req.role, ChildKind: req.childKind}
	if req.ref.UID != "" {
		in.SubjectUID = req.ref.UID
		return in, nil
	}
	found, ok, err := client.FindSubjectByName(ctx, req.ref.Name)
	if err != nil {
		return ctl.RelationInput{}, fmt.Errorf("looking up %s: %w", req.ref, err)
	}
	if ok {
		in.SubjectUID = found.UID
		return in, nil
	}
	person := req.person
	person.Name = req.ref.Name
	in.New = &person
	return in, nil
}

// newCtlFamilyAddCmd builds "ctl family add <subject-uid> <role> [<uid>]".
func newCtlFamilyAddCmd(opts *ctlOptions) *cobra.Command {
	var (
		req                  familyAddRequest
		name                 string
		birthYear, deathYear int
	)
	cmd := &cobra.Command{
		Use:   "add <subject-uid> <role> [<related-subject-uid>]",
		Short: "Record a relation: somebody's parent, child or partner (editor or admin)",
		Long: "Record one relation on a person.\n\n" +
			"The role is the *other* person's side of it: `parent` makes them the subject's\n" +
			"parent, `child` their child, `partner` their partner. There is no `sibling`:\n" +
			"siblings are derived, so the way to record one is to give the two children the\n" +
			"same parent.\n\n" +
			"Name the other person by uid, or with --name. A name that matches a subject —\n" +
			"case-insensitively, by name or by slug — relates to that person; a name that\n" +
			"matches nobody creates them, together with the relation, in one transaction, so\n" +
			"a refused relation leaves no orphan person behind. That is what makes filling a\n" +
			"tree bearable: a great-grandmother nobody photographed is otherwise a trip to\n" +
			"another screen and back. --type, --birth-year, --death-year and --notes describe\n" +
			"the person being created and are ignored for one who already exists.\n\n" +
			"Nothing is destroyed by a relation, so there is no --yes; a relation the tree\n" +
			"cannot hold — a cycle, a second parentage, a second family for one couple — is\n" +
			"refused by the server with a 409 and changes nothing.",
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := ctl.SubjectRefFromArgs(positional(args, 2), name)
			if err != nil {
				return fmt.Errorf("reading the other person: %w", err)
			}
			if ref.UID != "" && anyFlagChanged(cmd, personFlags...) {
				return errPersonDetails
			}
			req.subjectUID, req.role, req.ref = args[0], args[1], ref
			req.person.BirthYear = optionalInt(cmd, "birth-year", birthYear)
			req.person.DeathYear = optionalInt(cmd, "death-year", deathYear)
			return runFamilyAdd(cmd, opts, req)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&name, "name", "", "name the other person instead of giving their uid")
	flags.StringVar(&req.childKind, "child-kind", "",
		"how the child belongs to the family: birth (default), adopted or step; ignored for a partner")
	flags.StringVar(&req.person.Type, "type", "", "type of a person being created: person (default), pet or other")
	flags.StringVar(&req.person.Notes, "notes", "", "note about a person being created")
	flags.IntVar(&birthYear, "birth-year", 0, "birth year of a person being created")
	flags.IntVar(&deathYear, "death-year", 0, "death year of a person being created")
	return cmd
}

// runFamilyAdd names the subject, resolves the other person and records the
// relation, reporting both by name. The subject is read first for the same reason
// a merge reads both people: a mistyped uid becomes a 404 before a person is
// created for it, and the answer carries no name for the person the relation was
// recorded on.
func runFamilyAdd(cmd *cobra.Command, opts *ctlOptions, req familyAddRequest) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	subject, err := client.FetchSubject(cmd.Context(), req.subjectUID)
	if err != nil {
		return fmt.Errorf("fetching subject %s: %w", req.subjectUID, err)
	}
	in, err := req.input(cmd.Context(), client)
	if err != nil {
		return err
	}
	raw, err := client.AddRelation(cmd.Context(), req.subjectUID, in)
	if err != nil {
		return fmt.Errorf("recording the %s of subject %s: %w", req.role, req.subjectUID, err)
	}
	result, err := ctl.DecodeRelationResult(raw)
	if err != nil {
		return fmt.Errorf("recording the %s of subject %s: %w", req.role, req.subjectUID, err)
	}
	report := ctl.RelationReport{
		RelationResult: result,
		Role:           req.role,
		SubjectUID:     subject.UID,
		SubjectName:    subject.Name,
	}
	return renderRelationReport(cmd.OutOrStdout(), out, report)
}

// newCtlFamilyRemoveCmd builds "ctl family remove <subject-uid> <uid>".
func newCtlFamilyRemoveCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "remove <subject-uid> <related-subject-uid>",
		Short: "Remove whatever relates two people (editor or admin)",
		Long: "Remove the relation between two people.\n\n" +
			"Which relation that is follows from the rows rather than from the command line:\n" +
			"one is the other's parent, or their child, or their partner. Both people survive\n" +
			"with every photo they are on — only the line between them goes.\n\n" +
			"Nothing records who was whose parent once it is gone, so this needs --yes;\n" +
			"--dry-run says who would be removed as whose what and writes nothing. Both read\n" +
			"the relation first, which is what lets two people who are not related fail\n" +
			"before a request is spent, and lets a sibling say so: siblings are derived from\n" +
			"a shared parent, and the way to part them is to remove that parent.\n\n" +
			"Removing one parent leaves the other: the child keeps the parentage it still\n" +
			"has, because \"X is no longer Y's father\" must not quietly take Y's mother away.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFamilyRemove(cmd, opts, args[0], args[1], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runFamilyRemove describes what would go, gates the removal and reports it.
func runFamilyRemove(
	cmd *cobra.Command, opts *ctlOptions, subjectUID, otherUID string, assumeYes, dryRun bool,
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	relation, err := describeRelation(cmd, client, subjectUID, otherUID)
	if err != nil {
		return err
	}
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out, "dry run: would remove "+relation+
			"; both people and their photos would stay, and nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, "remove "+relation); err != nil {
		return err
	}
	if err := client.RemoveRelation(cmd.Context(), subjectUID, otherUID); err != nil {
		return fmt.Errorf("removing the relation between %s and %s: %w", subjectUID, otherUID, err)
	}
	return renderAck(cmd.OutOrStdout(), out, "removed "+relation+"; both people and their photos are untouched")
}

// describeRelation reads how the two are related and renders it as the phrase
// every confirmation of this command is built from — "Marie Nečasová (sub02) as
// the parent of Anna Nečasová (sub01)". A pair that is not related and a pair of
// siblings are both refused here, before a request is spent on a 404.
func describeRelation(cmd *cobra.Command, client *ctl.Client, subjectUID, otherUID string) (string, error) {
	subject, err := client.FetchSubject(cmd.Context(), subjectUID)
	if err != nil {
		return "", fmt.Errorf("fetching subject %s: %w", subjectUID, err)
	}
	relations, err := client.FetchRelations(cmd.Context(), subjectUID)
	if err != nil {
		return "", fmt.Errorf("reading the relations of subject %s: %w", subjectUID, err)
	}
	role, other := relations.Role(otherUID), relations.Find(otherUID)
	switch {
	case role == ctl.RoleSibling:
		return "", fmt.Errorf("%w: %s and %s are siblings", ctl.ErrSiblingsDerived, subjectUID, otherUID)
	case role == "" || other == nil:
		return "", fmt.Errorf("%w: %s and %s", ctl.ErrNotRelated, subjectUID, otherUID)
	}
	return ctl.SubjectLabel(other.Name, other.UID) + " as the " + role + " of " +
		ctl.SubjectLabel(subject.Name, subject.UID), nil
}

// newCtlFamilyEditCmd builds "ctl family edit <family-uid>".
func newCtlFamilyEditCmd(opts *ctlOptions) *cobra.Command {
	var (
		in                 ctl.FamilyUpdate
		fromYear, toYear   int
		editableFamilyKeys = []string{"kind", "from-year", "to-year", "note"}
	)
	cmd := &cobra.Command{
		Use:   "edit <family-uid>",
		Short: "Edit what tied a couple together and when (editor or admin)",
		Long: "Edit a family's own record — its kind, its years and its note — as opposed to\n" +
			"who is in it, which is what `family add` and `family remove` are for. The uid is\n" +
			"the FAMILY column of `ctl family relations`.\n\n" +
			"**It rewrites the whole record**, exactly as PATCH /families/{uid} does: a year\n" +
			"or a note you do not pass is cleared, and an omitted --kind falls back to the\n" +
			"server's default, `partnership`. Pass everything the family should end up\n" +
			"carrying, not only what changed. An edit naming nothing at all is refused\n" +
			"rather than run, because it would erase rather than do nothing.\n\n" +
			"`partnership` is the default because it is what the archive can honestly claim\n" +
			"about most of the pairs in it; `unknown` admits nobody knows which it was.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !anyFlagChanged(cmd, editableFamilyKeys...) {
				return ctl.ErrNoFamilyEdits
			}
			in.FromYear = optionalInt(cmd, "from-year", fromYear)
			in.ToYear = optionalInt(cmd, "to-year", toYear)
			return runFamilyEdit(cmd, opts, args[0], in)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Kind, "kind", "", "what tied the pair together: marriage, partnership (default) or unknown")
	flags.StringVar(&in.Note, "note", "", "free-text note about the union")
	flags.IntVar(&fromYear, "from-year", 0, "the year the union began (1800…this year)")
	flags.IntVar(&toYear, "to-year", 0, "the year it ended (not before --from-year)")
	return cmd
}

// runFamilyEdit writes the family record and prints it back with its partners
// named, which costs a lookup per partner the API's answer does not carry.
func runFamilyEdit(cmd *cobra.Command, opts *ctlOptions, familyUID string, in ctl.FamilyUpdate) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	raw, err := client.UpdateFamily(cmd.Context(), familyUID, in)
	if err != nil {
		return fmt.Errorf("editing family %s: %w", familyUID, err)
	}
	names := map[string]string{}
	if out.Format == ctl.FormatTable {
		if family, decodeErr := ctl.DecodeFamily(raw); decodeErr == nil {
			names = partnerNames(cmd, client, family)
		}
	}
	return renderRaw(cmd.OutOrStdout(), out, raw, "family", ctl.DecodeFamily,
		func(w io.Writer, family ctl.Family) error { return ctl.WriteFamily(w, family, names) })
}

// partnerNames resolves a family's partners to their names, best effort: a
// lookup that fails leaves the uid to be printed as it is, because a write that
// has already happened must not be reported as a failure over a missing name.
func partnerNames(cmd *cobra.Command, client *ctl.Client, family ctl.Family) map[string]string {
	names := make(map[string]string, 2)
	for _, uid := range family.Partners() {
		subject, err := client.FetchSubject(cmd.Context(), uid)
		if err != nil {
			continue
		}
		names[uid] = subject.Name
	}
	return names
}

// anyFlagChanged reports whether the command line carried any of these flags, so
// a whole-record write can refuse an edit that names nothing instead of clearing
// everything it was not told about.
func anyFlagChanged(cmd *cobra.Command, names ...string) bool {
	return slices.ContainsFunc(names, cmd.Flags().Changed)
}
