package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
)

// undoFilePerm is the mode of a written undo file. It carries no secrets, but it
// is the only copy of what a detach removed, so it stays owner-writable.
const undoFilePerm = 0o600

// errUndoFileRequired is returned when --apply is used without --undo-file.
// Detaching is otherwise irreversible — the marker→subject links are set NULL and
// nothing else records what they were — so the command refuses to run destructively
// with nowhere to put the undo.
var errUndoFileRequired = errors.New("--apply requires --undo-file: refusing to detach without writing an undo file")

// namelessUndo is the on-disk undo file: every subject the run detached, with the
// markers and faces that pointed at it. `nameless-subjects --undo <file>` replays
// it to put everything back.
type namelessUndo struct {
	// Subjects are the detached subjects, in the order they were detached.
	Subjects []people.SubjectSnapshot `json:"subjects"`
}

// newMaintenanceNamelessCmd builds the "maintenance nameless-subjects"
// subcommand: the reporting and repair path for subjects whose name identifies
// nobody.
//
// Such a subject cannot be created deliberately — the subject API rejects a name
// with no letter or digit — so one in the catalogue was minted by an importer
// keying find-or-create on the fallback slug, and it acts as a catch-all every
// nameless face after it was assigned to (16 532 markers in production; see
// docs/OPERATIONS.md). Repairing that is data loss if it guesses wrong, so it is a
// deliberate CLI step rather than a migration: it reports by default, applies only
// with --apply, and always writes an undo file it can replay.
func newMaintenanceNamelessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nameless-subjects",
		Short: "Report (and optionally detach) subjects whose name identifies nobody",
		Long: "List every subject whose name identifies nobody — a catch-all an importer " +
			"minted — with the markers and faces assigned to it. Reports only unless " +
			"--apply is given, which deletes each such subject and leaves its markers " +
			"unassigned, writing an undo file that --undo replays.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMaintenanceNameless(cmd)
		},
	}
	cmd.Flags().Bool("apply", false, "detach the listed subjects (requires --undo-file)")
	cmd.Flags().String("undo-file", "", "path the undo file is written to")
	cmd.Flags().String("undo", "", "restore the subjects recorded in this undo file")
	return cmd
}

// namelessFlags is the parsed flag set of the nameless-subjects command.
type namelessFlags struct {
	apply    bool
	undoFile string
	undo     string
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
	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	store := people.NewStore(db.Pool())
	switch {
	case flags.undo != "":
		return restoreNamelessSubjects(ctx, cmd, store, flags.undo)
	case flags.apply:
		return detachNamelessSubjects(ctx, cmd, store, flags.undoFile)
	default:
		return reportNamelessSubjects(ctx, cmd, store)
	}
}

// reportNamelessSubjects prints the dry run: every nameless subject with its
// counts, and what running with --apply would do.
func reportNamelessSubjects(ctx context.Context, cmd *cobra.Command, store *people.Store) error {
	found, err := store.ListNamelessSubjects(ctx)
	if err != nil {
		return fmt.Errorf("listing nameless subjects: %w", err)
	}
	if len(found) == 0 {
		cmd.Println("no nameless subjects: nothing to repair")
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
	cmd.Printf("%d nameless subject(s), %d marker(s) and %d cached face(s) assigned to them\n",
		len(found), markers, faces)
	cmd.Println("dry run: re-run with --apply --undo-file <path> to detach them")
	return nil
}

// detachNamelessSubjects deletes every nameless subject, leaving its markers
// unassigned, and records each snapshot in the undo file. The file is created (and
// proven writable) before anything is deleted and rewritten after every detach, so
// an interrupted run leaves an undo file covering exactly what it managed to
// change.
func detachNamelessSubjects(ctx context.Context, cmd *cobra.Command, store *people.Store, undoPath string) error {
	found, err := store.ListNamelessSubjects(ctx)
	if err != nil {
		return fmt.Errorf("listing nameless subjects: %w", err)
	}
	if len(found) == 0 {
		cmd.Println("no nameless subjects: nothing to repair")
		return nil
	}
	undo := namelessUndo{Subjects: make([]people.SubjectSnapshot, 0, len(found))}
	if err := writeUndoFile(undoPath, undo); err != nil {
		return err
	}
	for _, ns := range found {
		entry := namelessAuditEntry(audit.ActionSubjectDelete, ns.UID, map[string]any{
			"reason": "nameless catch-all subject detached by maintenance nameless-subjects",
			"slug":   ns.Slug, "markers": ns.MarkerCount, "faces": ns.FaceCount,
		})
		snap, derr := store.DetachSubject(ctx, ns.UID, entry)
		if derr != nil {
			return fmt.Errorf("detaching subject %s: %w", ns.UID, derr)
		}
		undo.Subjects = append(undo.Subjects, snap)
		if werr := writeUndoFile(undoPath, undo); werr != nil {
			return errors.Join(werr, dumpSnapshot(cmd, snap))
		}
		cmd.Printf("detached %s: %d marker(s), %d cached face(s) left unassigned\n",
			ns.UID, len(snap.MarkerUIDs), len(snap.Faces))
	}
	cmd.Printf("%d subject(s) detached; undo written to %s\n", len(undo.Subjects), undoPath)
	return nil
}

// restoreNamelessSubjects replays an undo file, re-creating every subject it holds
// and re-assigning the markers and faces that pointed at it.
func restoreNamelessSubjects(ctx context.Context, cmd *cobra.Command, store *people.Store, undoPath string) error {
	raw, err := os.ReadFile(undoPath) //nolint:gosec // the operator names the undo file they wrote.
	if err != nil {
		return fmt.Errorf("reading undo file %s: %w", undoPath, err)
	}
	var undo namelessUndo
	if err := json.Unmarshal(raw, &undo); err != nil {
		return fmt.Errorf("parsing undo file %s: %w", undoPath, err)
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
		restored, rerr := store.RestoreSubject(ctx, snap, entry)
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
// links it records are not lost with the failed write. The returned error explains
// what to do with the printed JSON.
func dumpSnapshot(cmd *cobra.Command, snap people.SubjectSnapshot) error {
	raw, err := json.Marshal(namelessUndo{Subjects: []people.SubjectSnapshot{snap}})
	if err != nil {
		return fmt.Errorf("encoding the unsaved snapshot of %s: %w", snap.Subject.UID, err)
	}
	cmd.Println(string(raw))
	return fmt.Errorf(
		"the undo file could not be written; save the JSON above and replay it with --undo to restore %s",
		snap.Subject.UID)
}
