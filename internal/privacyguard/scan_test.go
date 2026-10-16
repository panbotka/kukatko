package privacyguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sshHit is a synthetic ssh-target line, assembled so this source never
// matches the rule itself.
const sshHit = "ssh someone" + "@somehost"

// phoneHit is a synthetic Czech phone number, assembled the same way.
const phoneHit = "call 6" + "01 234 567 for access"

// writeFile writes content to root/name, creating its directories.
func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestCheckText_reportsEveryRuleOnItsLine(t *testing.T) {
	t.Parallel()
	text := "clean line\n" + sshHit + "\nstill clean\n" + sshHit + " or mail x" + "@mailhost.cz\n"
	got := CheckText("docs/a.md", []byte(text), GenericRules())
	want := []string{
		"docs/a.md:2: ssh-target",
		"docs/a.md:4: ssh-target",
		"docs/a.md:4: email-address",
	}
	gotStr := make([]string, 0, len(got))
	for _, f := range got {
		gotStr = append(gotStr, f.String())
	}
	if !slices.Equal(gotStr, want) {
		t.Errorf("findings = %v, want %v", gotStr, want)
	}
}

func TestCheckText_miss(t *testing.T) {
	t.Parallel()
	text := "ssh someone@host.example.org\nhost 192.0.2.1\nmail jan@example.org\n"
	if got := CheckText("docs/a.md", []byte(text), GenericRules()); len(got) != 0 {
		t.Errorf("findings = %v, want none", got)
	}
}

func TestCheck_skipsBinaryAndMissingFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "docs/a.md", "intro\n"+sshHit+"\n")
	writeFile(t, root, "img.bin", "\x00\x01"+sshHit+"\n")
	files := []string{"docs/a.md", "img.bin", "deleted.md"}
	got, err := Check(root, files, GenericRules())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(got) != 1 || got[0].String() != "docs/a.md:2: ssh-target" {
		t.Errorf("findings = %v, want only docs/a.md:2: ssh-target", got)
	}
}

func TestFinding_stringOmitsTheText(t *testing.T) {
	t.Parallel()
	got := CheckText("docs/a.md", []byte(sshHit+"\n"), GenericRules())
	if len(got) != 1 {
		t.Fatalf("findings = %v, want one", got)
	}
	if s := got[0].String(); strings.Contains(s, "someone") || s != "docs/a.md:1: ssh-target" {
		t.Errorf("String() = %q, want only path:line: rule", s)
	}
}

// gitRepo creates a git work tree in a temp dir with one tracked file; it
// skips the test when no git binary is installed. It clears FileListEnv —
// scripts/check-on-box.sh exports it for the whole gate — so Files asks git;
// a caller therefore cannot be parallel.
func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv(FileListEnv, "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	writeFile(t, root, "README.md", "a clean tracked file\n")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "README.md"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func TestFiles_tracksPlusUntrackedSpecs(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "docs/specs/task-new.md", "a fresh spec\n")
	writeFile(t, root, "scratch.txt", "an untracked file outside docs/specs\n")
	got, err := Files(t.Context(), root)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	want := []string{"README.md", "docs/specs/task-new.md"}
	if !slices.Equal(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
}

// TestFiles_untrackedSpecHitFails is the end-to-end promise: a private value
// in a spec that git does not track yet still fails the guard.
func TestFiles_untrackedSpecHitFails(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "docs/specs/task-new.md", "# Task\n\n"+phoneHit+"\n")
	files, err := Files(t.Context(), root)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	got, err := Check(root, files, GenericRules())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(got) != 1 || got[0].String() != "docs/specs/task-new.md:3: phone-number" {
		t.Errorf("findings = %v, want docs/specs/task-new.md:3: phone-number", got)
	}
}

func TestFiles_notAWorkTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv(FileListEnv, "")
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	_, err := Files(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), FileListEnv) {
		t.Errorf("Files outside a work tree: err = %v, want one naming %s", err, FileListEnv)
	}
}

func TestFiles_fileListEnv(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/specs/task-new.md", "a spec only on disk\n")
	list := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(list, []byte("b.go\n\na.md\n"), 0o600); err != nil {
		t.Fatalf("write list: %v", err)
	}
	t.Setenv(FileListEnv, list)
	got, err := Files(t.Context(), root)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	want := []string{"a.md", "b.go", "docs/specs/task-new.md"}
	if !slices.Equal(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
}
