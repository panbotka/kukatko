package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
)

// undoFilePerm is the mode of a written undo file. It carries no secrets, but it
// is the only copy of what a detach removed, so it stays owner-writable.
const undoFilePerm = 0o600

// errUndoFileRequired is returned when --apply is used without --undo-file.
// Detaching is otherwise irreversible — the marker→subject links are set NULL and
// nothing else records what they were — so the command refuses to run destructively
// with nowhere to put the undo.
var errUndoFileRequired = errors.New("--apply requires --undo-file: refusing to detach without writing an undo file")

// errProjectRequired is returned when --project is missing. Each project has its
// own catch-all, if any, and the command never guesses which library it is about
// to change — not even the report.
var errProjectRequired = errors.New("--project <slug> is required: the repair runs inside one project")

// namelessUndo is the on-disk undo file: every subject the run detached, with the
// markers and faces that pointed at it. `nameless-subjects --undo <file>` replays
// it to put everything back.
//
// It is namelessjob.Undo — the very format the admin Maintenance page hands to
// the browser and takes back — so an undo file written here can be uploaded
// there and one downloaded there can be replayed here.
type namelessUndo = namelessjob.Undo

// newMaintenanceNamelessCmd builds the "maintenance nameless-subjects"
// subcommand: the reporting and repair path for subjects whose name identifies
// nobody.
//
// Such a subject cannot be created deliberately — the subject API rejects a name
// with no letter or digit — so one in the catalogue was minted by an importer
// keying find-or-create on the fallback slug, and it acts as a catch-all every
// nameless face after it was assigned to (~16k markers in production; see
// docs/OPERATIONS.md). Repairing that is data loss if it guesses wrong, so it is a
// deliberate CLI step rather than a migration: it reports by default, applies only
// with --apply, and always writes an undo file it can replay.
//
// It works on one project, named by the required --project slug. The undo file
// records that project (uid and slug), and --undo refuses a file of another
// project; a file written before projects existed belongs to the bootstrap one.
func newMaintenanceNamelessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nameless-subjects",
		Short: "Report (and optionally detach) subjects whose name identifies nobody",
		Long: "List every subject of the --project whose name identifies nobody — a catch-all " +
			"an importer minted — with the markers and faces assigned to it. Reports only unless " +
			"--apply is given, which deletes each such subject and leaves its markers " +
			"unassigned, writing an undo file that --undo replays into the same project.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMaintenanceNameless(cmd)
		},
	}
	cmd.Flags().Bool("apply", false, "detach the listed subjects (requires --undo-file)")
	cmd.Flags().String("undo-file", "", "path the undo file is written to")
	cmd.Flags().String("undo", "", "restore the subjects recorded in this undo file")
	cmd.Flags().String("project", "", "slug of the project to repair (required)")
	return cmd
}

// namelessFlags is the parsed flag set of the nameless-subjects command.
type namelessFlags struct {
	apply    bool
	undoFile string
	undo     string
	project  string
}

// namelessFlagsFrom reads and validates the command's flags, rejecting the
// combinations that would either be irreversible (--apply with no undo file) or
// ambiguous (--apply together with --undo).
func namelessFlagsFrom(cmd *cobra.Command) (namelessFlags, error) {
	var f namelessFlags
	var err error
	if f.apply, err = cmd.Flags().GetBool("apply"); err != nil {
		return namelessFlags{}, fmt.Errorf("reading --apply: %w", err)
	}
	if f.undoFile, err = cmd.Flags().GetString("undo-file"); err != nil {
		return namelessFlags{}, fmt.Errorf("reading --undo-file: %w", err)
	}
	if f.undo, err = cmd.Flags().GetString("undo"); err != nil {
		return namelessFlags{}, fmt.Errorf("reading --undo: %w", err)
	}
	if f.project, err = cmd.Flags().GetString("project"); err != nil {
		return namelessFlags{}, fmt.Errorf("reading --project: %w", err)
	}
	if f.project == "" {
		return namelessFlags{}, errProjectRequired
	}
	if f.apply && f.undo != "" {
		return namelessFlags{}, errors.New("--apply and --undo are mutually exclusive")
	}
	if f.apply && f.undoFile == "" {
		return namelessFlags{}, errUndoFileRequired
	}
	return f, nil
}

// runMaintenanceNameless dispatches the command's three modes — restore, apply, or
// the read-only default — over an opened database.
func runMaintenanceNameless(cmd *cobra.Command) error {
	flags, err := namelessFlagsFrom(cmd)
	if err != nil {
		return err
	}
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	db, err := openCurrentDB(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	p, err := project.NewStore(db.Pool()).GetBySlug(ctx, flags.project)
	if err != nil {
		return fmt.Errorf("looking up project %q: %w", flags.project, err)
	}
	target := namelessTarget{store: people.NewStore(db.Pool()), project: p}
	switch {
	case flags.undo != "":
		return restoreNamelessSubjects(ctx, cmd, target, flags.undo)
	case flags.apply:
		return detachNamelessSubjects(ctx, cmd, target, flags.undoFile)
	default:
		return reportNamelessSubjects(ctx, cmd, target)
	}
}

// namelessTarget is what the three modes work on: the people store and the one
// project the repair is confined to.
type namelessTarget struct {
	store   *people.Store
	project *project.Project
}

// reportNamelessSubjects prints the dry run: every nameless subject with its
// counts, and what running with --apply would do.
func reportNamelessSubjects(ctx context.Context, cmd *cobra.Command, target namelessTarget) error {
	found, err := target.store.ListNamelessSubjects(ctx, target.project.UID)
	if err != nil {
		return fmt.Errorf("listing nameless subjects: %w", err)
	}
	if len(found) == 0 {
		cmd.Printf("no nameless subjects in project %s: nothing to repair\n", target.project.Slug)
		return nil
	}
	markers, faces := 0, 0
	for _, ns := range found {
		cmd.Printf("uid=%s slug=%q name=%q type=%s created=%s markers=%d faces=%d\n",
			ns.UID, ns.Slug, ns.Name, ns.Type, ns.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
			ns.MarkerCount, ns.FaceCount)
		markers += ns.MarkerCount
		faces += ns.FaceCount
	}
	cmd.Printf("%d nameless subject(s) in project %s, %d marker(s) and %d cached face(s) assigned to them\n",
		len(found), target.project.Slug, markers, faces)
	cmd.Println("dry run: re-run with --apply --undo-file <path> to detach them")
	return nil
}

// detachNamelessSubjects deletes every nameless subject, leaving its markers
// unassigned, and records each snapshot in the undo file. The file is created (and
// proven writable) before anything is deleted and rewritten after every detach, so
// an interrupted run leaves an undo file covering exactly what it managed to
// change.
func detachNamelessSubjects(
	ctx context.Context, cmd *cobra.Command, target namelessTarget, undoPath string,
) error {
	found, err := target.store.ListNamelessSubjects(ctx, target.project.UID)
	if err != nil {
		return fmt.Errorf("listing nameless subjects: %w", err)
	}
	if len(found) == 0 {
		cmd.Printf("no nameless subjects in project %s: nothing to repair\n", target.project.Slug)
		return nil
	}
	undo := namelessUndo{
		ProjectUID: target.project.UID, ProjectSlug: target.project.Slug,
		Subjects: make([]people.SubjectSnapshot, 0, len(found)),
	}
	if err := writeUndoFile(undoPath, undo); err != nil {
		return err
	}
	for _, ns := range found {
		entry := namelessAuditEntry(audit.ActionSubjectDelete, ns.UID, map[string]any{
			"reason": "nameless catch-all subject detached by maintenance nameless-subjects",
			"slug":   ns.Slug, "markers": ns.MarkerCount, "faces": ns.FaceCount,
		})
		snap, derr := target.store.DetachSubject(ctx, target.project.UID, ns.UID, entry)
		if derr != nil {
			return fmt.Errorf("detaching subject %s: %w", ns.UID, derr)
		}
		undo.Subjects = append(undo.Subjects, snap)
		if werr := writeUndoFile(undoPath, undo); werr != nil {
			return errors.Join(werr, dumpSnapshot(cmd, undo, snap))
		}
		cmd.Printf("detached %s: %d marker(s), %d cached face(s) left unassigned\n",
			ns.UID, len(snap.MarkerUIDs), len(snap.Faces))
	}
	cmd.Printf("%d subject(s) detached; undo written to %s\n", len(undo.Subjects), undoPath)
	return nil
}

// restoreNamelessSubjects replays an undo file into its project, re-creating every
// subject it holds and re-assigning the markers and faces that pointed at it. A
// file of another project than --project is refused before anything changes.
func restoreNamelessSubjects(
	ctx context.Context, cmd *cobra.Command, target namelessTarget, undoPath string,
) error {
	raw, err := os.ReadFile(undoPath) //nolint:gosec // the operator names the undo file they wrote.
	if err != nil {
		return fmt.Errorf("reading undo file %s: %w", undoPath, err)
	}
	var undo namelessUndo
	if err := json.Unmarshal(raw, &undo); err != nil {
		return fmt.Errorf("parsing undo file %s: %w", undoPath, err)
	}
	if err := undo.CheckProject(target.project.UID); err != nil {
		return fmt.Errorf("undo file %s: %w", undoPath, err)
	}
	if len(undo.Subjects) == 0 {
		cmd.Printf("undo file %s records no detached subject: nothing to restore\n", undoPath)
		return nil
	}
	for _, snap := range undo.Subjects {
		entry := namelessAuditEntry(audit.ActionSubjectCreate, snap.Subject.UID, map[string]any{
			"reason":  "nameless catch-all subject restored from an undo file",
			"markers": len(snap.MarkerUIDs), "faces": len(snap.Faces),
		})
		restored, rerr := target.store.RestoreSubject(ctx, target.project.UID, snap, entry)
		if rerr != nil {
			return fmt.Errorf("restoring subject %s: %w", snap.Subject.UID, rerr)
		}
		cmd.Printf("restored %s (slug %q): %d marker(s), %d cached face(s) reassigned\n",
			restored.UID, restored.Slug, len(snap.MarkerUIDs), len(snap.Faces))
	}
	cmd.Printf("%d subject(s) restored from %s\n", len(undo.Subjects), undoPath)
	return nil
}

// namelessAuditEntry builds the audit entry for one detach or restore. The command
// runs offline with no logged-in user, so the entry carries no actor (stored NULL)
// — the same convention batch state transitions already use — while still recording
// what changed and why.
func namelessAuditEntry(action, subjectUID string, details map[string]any) audit.Entry {
	return audit.Entry{
		Action:     action,
		TargetType: "subject",
		TargetUID:  subjectUID,
		Details:    details,
	}
}

// writeUndoFile serialises undo to path, replacing whatever is there. It is called
// once before the first detach (so an unwritable path fails before any data
// changes) and again after each one.
func writeUndoFile(path string, undo namelessUndo) error {
	raw, err := json.MarshalIndent(undo, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding undo file: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), undoFilePerm); err != nil {
		return fmt.Errorf("writing undo file %s: %w", path, err)
	}
	return nil
}

// dumpSnapshot prints a snapshot the undo file could not be written with, so the
// links it records are not lost with the failed write. The printed file names
// undo's project, so it replays into the right one. The returned error explains
// what to do with the printed JSON.
func dumpSnapshot(cmd *cobra.Command, undo namelessUndo, snap people.SubjectSnapshot) error {
	raw, err := json.Marshal(namelessUndo{
		ProjectUID: undo.ProjectUID, ProjectSlug: undo.ProjectSlug, Subjects: []people.SubjectSnapshot{snap},
	})
	if err != nil {
		return fmt.Errorf("encoding the unsaved snapshot of %s: %w", snap.Subject.UID, err)
	}
	cmd.Println(string(raw))
	return fmt.Errorf(
		"the undo file could not be written; save the JSON above and replay it with --undo to restore %s",
		snap.Subject.UID)
}
