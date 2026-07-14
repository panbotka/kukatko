package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/dirimport"
)

// runDirFlags parses args with the "import dir" command's flags (without running
// it) and returns the options they produce.
func runDirFlags(t *testing.T, args ...string) (dirImportOptions, error) {
	t.Helper()
	cmd := newImportDirCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Flags().Parse(args); err != nil {
		return dirImportOptions{}, err
	}
	return dirImportOptionsFromFlags(cmd, "/photos")
}

// TestDirImportOptionsFromFlags checks the flag defaults and how --no-recursive
// negates the recursive default.
func TestDirImportOptionsFromFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want dirImportOptions
	}{
		{
			name: "defaults are recursive, no album, small fan-out",
			args: nil,
			want: dirImportOptions{root: "/photos", recursive: true, concurrency: dirimport.DefaultConcurrency},
		},
		{
			name: "no-recursive turns the recursive default off",
			args: []string{"--no-recursive"},
			want: dirImportOptions{root: "/photos", recursive: false, concurrency: dirimport.DefaultConcurrency},
		},
		{
			name: "album, labels, dry run and concurrency are read",
			args: []string{"--album", "Scans", "--labels", "scan,1985", "--dry-run", "--concurrency", "2"},
			want: dirImportOptions{
				root: "/photos", recursive: true, album: "Scans",
				labels: []string{"scan", "1985"}, dryRun: true, concurrency: 2,
			},
		},
		{
			name: "uploader is read",
			args: []string{"--uploader", "botka"},
			want: dirImportOptions{
				root: "/photos", recursive: true,
				concurrency: dirimport.DefaultConcurrency, uploader: "botka",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := runDirFlags(t, tt.args...)
			if err != nil {
				t.Fatalf("parsing %v: %v", tt.args, err)
			}
			if got.root != tt.want.root || got.recursive != tt.want.recursive ||
				got.album != tt.want.album || got.dryRun != tt.want.dryRun ||
				got.concurrency != tt.want.concurrency || got.uploader != tt.want.uploader ||
				strings.Join(got.labels, ",") != strings.Join(tt.want.labels, ",") {
				t.Errorf("options = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestImportDirCmd_recursiveFlagsAreExclusive checks that asking for both
// --recursive and --no-recursive is rejected rather than silently resolved.
func TestImportDirCmd_recursiveFlagsAreExclusive(t *testing.T) {
	t.Parallel()

	out, err := executeCmd(t, "import", "dir", "/photos", "--recursive", "--no-recursive")
	if err == nil {
		t.Fatalf("executing both flags succeeded, want an error (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "recursive") {
		t.Errorf("error = %v, want it to name the conflicting flags", err)
	}
}

// TestImportDirCmd_registered checks "import dir" is wired under the import
// group and takes exactly one path.
func TestImportDirCmd_registered(t *testing.T) {
	t.Parallel()

	var dir *cobra.Command
	for _, c := range newImportCmd().Commands() {
		if c.Name() == "dir" {
			dir = c
		}
	}
	if dir == nil {
		t.Fatal("import dir is not registered under import")
	}
	if err := dir.Args(dir, []string{}); err == nil {
		t.Error("import dir accepted no path, want exactly one")
	}
	if err := dir.Args(dir, []string{"/photos", "/more"}); err == nil {
		t.Error("import dir accepted two paths, want exactly one")
	}
}

// TestPrintFileResult checks each outcome prints the one thing a user needs from
// it: the counter, the verdict, the file, and — for a duplicate — what it
// collided with, or why it was skipped, or why it failed.
func TestPrintFileResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		res  dirimport.FileResult
		want []string
	}{
		{
			name: "imported",
			res:  dirimport.FileResult{Path: "a.jpg", Outcome: dirimport.OutcomeImported},
			want: []string{"[1/9]", "imported", "a.jpg"},
		},
		{
			name: "imported with warnings names the codes",
			res: dirimport.FileResult{
				Path: "weird.jpg", Outcome: dirimport.OutcomeImported,
				Warnings: []string{"thumbnail_failed", "phash_failed"},
			},
			want: []string{"imported", "weird.jpg", "(warnings: thumbnail_failed, phash_failed)"},
		},
		{
			name: "duplicate names the file it collided with",
			res: dirimport.FileResult{
				Path: "b.jpg", Outcome: dirimport.OutcomeDuplicate, ExistingPath: "2014/06/x.jpg",
			},
			want: []string{"duplicate", "b.jpg", "already in the library as 2014/06/x.jpg"},
		},
		{
			name: "skipped names the reason",
			res: dirimport.FileResult{
				Path: "Thumbs.db", Outcome: dirimport.OutcomeSkipped, Reason: dirimport.SkipJunk,
			},
			want: []string{"skipped", "Thumbs.db", "(junk)"},
		},
		{
			name: "failed names the error",
			res: dirimport.FileResult{
				Path: "bad.jpg", Outcome: dirimport.OutcomeFailed, Err: errors.New("corrupt"),
			},
			want: []string{"failed", "bad.jpg", "corrupt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			printFileResult(cmd, tt.res, 1, 9)
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
		})
	}
}

// TestPrintImportSummary checks the summary reports every bucket, breaks the
// skips down by reason, and says that the queued embedding work is expected
// rather than broken.
func TestPrintImportSummary(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	printImportSummary(cmd, dirimport.Result{
		RunID: 7,
		Counts: dirimport.Counts{
			Imported: 10, Duplicates: 2, Skipped: 3, Failed: 1,
			ByReason: map[dirimport.SkipReason]int{dirimport.SkipJunk: 2, dirimport.SkipSidecar: 1},
		},
	}, 90*time.Second)

	got := out.String()
	for _, want := range []string{
		"folder import run 7", "imported=10", "duplicates=2", "skipped=3", "failed=1", "1m30s",
		"skipped: junk=2 sidecar=1", "embedding and face-detection jobs are queued",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q does not contain %q", got, want)
		}
	}
}

// TestPrintImportSummary_dryRun checks a dry run says plainly that it wrote
// nothing and reports no run id.
func TestPrintImportSummary_dryRun(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	printImportSummary(cmd, dirimport.Result{
		DryRun: true,
		Counts: dirimport.Counts{Imported: 4, ByReason: map[dirimport.SkipReason]int{}},
	}, time.Second)

	got := out.String()
	if !strings.Contains(got, "dry run — nothing was written") {
		t.Errorf("dry-run summary %q does not say that nothing was written", got)
	}
	if strings.Contains(got, "run 0") || strings.Contains(got, "jobs are queued") {
		t.Errorf("dry-run summary %q claims a run or queued jobs", got)
	}
}

// TestSkipBreakdown checks the breakdown is stable (alphabetical) and empty when
// nothing was skipped, so the summary of a clean import stays quiet.
func TestSkipBreakdown(t *testing.T) {
	t.Parallel()

	if got := skipBreakdown(nil); got != "" {
		t.Errorf("skipBreakdown(nil) = %q, want empty", got)
	}
	got := skipBreakdown(map[dirimport.SkipReason]int{
		dirimport.SkipUnsupported: 3,
		dirimport.SkipHidden:      1,
		dirimport.SkipJunk:        2,
	})
	if want := "hidden=1 junk=2 unsupported=3"; got != want {
		t.Errorf("skipBreakdown() = %q, want %q", got, want)
	}
}
