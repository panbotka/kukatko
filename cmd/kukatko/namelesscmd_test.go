package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
)

// TestMaintenanceCmd_hasNamelessChild verifies the nameless-subject repair is
// wired onto the maintenance group, since it is the only entry point to it.
func TestMaintenanceCmd_hasNamelessChild(t *testing.T) {
	t.Parallel()

	var found bool
	for _, c := range newMaintenanceCmd().Commands() {
		if c.Name() == "nameless-subjects" {
			found = true
		}
	}
	if !found {
		t.Error("maintenance command has no nameless-subjects subcommand")
	}
}

// TestNamelessFlags_validation checks the flag combinations that guard the
// destructive path. --apply without --undo-file must be refused *before* the
// database is opened: detaching sets the marker→subject links NULL and nothing
// else records what they were, so the undo file is the only way back.
func TestNamelessFlags_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr error
		wantMsg string
	}{
		{name: "no flags is the dry run", args: nil},
		{name: "apply with an undo file", args: []string{"--apply", "--undo-file", "u.json"}},
		{name: "undo alone", args: []string{"--undo", "u.json"}},
		{name: "apply without an undo file", args: []string{"--apply"}, wantErr: errUndoFileRequired},
		{
			name:    "apply together with undo",
			args:    []string{"--apply", "--undo", "u.json", "--undo-file", "u.json"},
			wantMsg: "mutually exclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := newMaintenanceNamelessCmd()
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tt.args, err)
			}
			flags, err := namelessFlagsFrom(cmd)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("err = %v, want one mentioning %q", err, tt.wantMsg)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want none", err)
				}
				if flags.apply && flags.undoFile == "" {
					t.Error("accepted --apply with no undo file")
				}
			}
		})
	}
}

// TestWriteUndoFile_roundTrip checks the undo file keeps everything a restore
// needs, and that an unwritable path is reported rather than silently dropped —
// the pre-flight write is what lets the command fail before it deletes anything.
func TestWriteUndoFile_roundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "undo.json")
	undo := namelessUndo{Subjects: []people.SubjectSnapshot{{
		Subject:    people.Subject{UID: "su_catchall", Slug: "subject", Name: ""},
		MarkerUIDs: []string{"mk_1", "mk_2"},
		Faces:      []people.FaceRef{{PhotoUID: "ph_1", FaceIndex: 0}},
	}}}
	if err := writeUndoFile(path, undo); err != nil {
		t.Fatalf("writeUndoFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	var got namelessUndo
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(got.Subjects) != 1 {
		t.Fatalf("subjects = %d, want 1", len(got.Subjects))
	}
	snap := got.Subjects[0]
	if snap.Subject.UID != "su_catchall" || len(snap.MarkerUIDs) != 2 || len(snap.Faces) != 1 {
		t.Errorf("round trip lost data: %+v", snap)
	}
	if snap.Faces[0].PhotoUID != "ph_1" || snap.Faces[0].FaceIndex != 0 {
		t.Errorf("face ref = %+v, want ph_1/0", snap.Faces[0])
	}

	if err := writeUndoFile(filepath.Join(path, "nested.json"), undo); err == nil {
		t.Error("writing under a file path succeeded, want an error")
	}
}
