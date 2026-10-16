package privacyguard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// SpecsDir is the directory of task specs. Its files are scanned whether git
// tracks them yet or not: a task's spec lands there before it is committed.
const SpecsDir = "docs/specs"

// FileListEnv names an environment variable holding the path of a file list
// (one slash-separated path per line) that replaces `git ls-files` — the build
// box's synced copy of the tree has no .git, so scripts/check-on-box.sh ships
// the list computed on this side.
const FileListEnv = "KUKATKO_PRIVACY_FILES"

// sniffLen is how much of a file is read to decide whether it is binary.
const sniffLen = 8000

// Finding is one line holding private data.
type Finding struct {
	// Path is slash-separated, relative to the repository root.
	Path string
	// Line is 1-based.
	Line int
	// Rule is the name of the rule that matched.
	Rule string
}

// Key is the `path:line` the allow-list addresses the finding by.
func (f Finding) Key() string { return fmt.Sprintf("%s:%d", f.Path, f.Line) }

// String renders the finding as `path:line: <rule name>` — deliberately
// without the matched text, which is exactly what must not be printed.
func (f Finding) String() string { return f.Key() + ": " + f.Rule }

// Files lists the files to scan under root: the file list named by
// FileListEnv when it is set, otherwise every file git tracks; plus, either
// way, every file under SpecsDir. The result is sorted and duplicate-free.
// It also stats git's index so `go test`'s cache is invalidated by a `git add`.
func Files(ctx context.Context, root string) ([]string, error) {
	listed, err := listedFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	specs, err := specFiles(root)
	if err != nil {
		return nil, err
	}
	all := slices.Concat(listed, specs)
	slices.Sort(all)
	return slices.Compact(all), nil
}

// listedFiles is the FileListEnv list when that is set, otherwise git's.
func listedFiles(ctx context.Context, root string) ([]string, error) {
	if list := os.Getenv(FileListEnv); list != "" {
		return readFileList(list)
	}
	return gitFiles(ctx, root)
}

// git runs git with args in the work tree at root and returns its stdout.
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	//nolint:gosec // a fixed binary; the arguments are the guard's own, root is the repository
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// readFileList reads one slash-separated path per line from the file at path.
func readFileList(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the gate's own file list, set by check-on-box.sh
	if err != nil {
		return nil, fmt.Errorf("read %s file list: %w", FileListEnv, err)
	}
	var files []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// gitFiles runs `git ls-files` under root. A root that is not a git work tree
// is an error that names the way out (FileListEnv), never an empty list.
func gitFiles(ctx context.Context, root string) ([]string, error) {
	out, err := git(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s (not a work tree? set %s to a file list): %w",
			root, FileListEnv, err)
	}
	idx, err := git(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err == nil {
		// Only for the test cache: a stat of the index ties a cached pass to it.
		_, _ = os.Stat(strings.TrimSpace(string(idx)))
	}
	var files []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// specFiles walks SpecsDir under root; a missing directory lists nothing.
func specFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(root, SpecsDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("relative path of %s: %w", path, err)
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", SpecsDir, err)
	}
	return files, nil
}

// Check scans every file of files (slash-separated, relative to root) against
// rules and returns the findings in file order. A listed file that no longer
// exists (deleted, not yet committed), a symlink or a binary file (a NUL byte
// in its first bytes) is skipped. A line may yield several findings, one per
// rule it breaks. The files are scanned by one worker per CPU.
func Check(root string, files []string, rules []Rule) ([]Finding, error) {
	perFile := make([][]Finding, len(files))
	errs := make([]error, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				perFile[i], errs[i] = checkFile(root, files[i], rules)
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return slices.Concat(perFile...), nil
}

// checkFile scans one file of Check; see Check for what is skipped.
func checkFile(root, name string, rules []Rule) ([]Finding, error) {
	path := filepath.Join(root, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is a repository file listed by git
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if bytes.IndexByte(data[:min(len(data), sniffLen)], 0) >= 0 {
		return nil, nil
	}
	return CheckText(name, data, rules), nil
}

// CheckText scans the text of the file at path (slash-separated, relative to
// the repository root) and returns one finding per (line, rule) hit. A rule
// first runs once over the whole text; only one that matches somewhere is run
// line by line, which spares the per-line pass for nearly every rule and file.
func CheckText(path string, data []byte, rules []Rule) []Finding {
	candidates := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Applies(path) && r.MayMatch(data) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	var findings []Finding
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), len(data)+1)
	n := 0
	for scanner.Scan() {
		n++
		line := scanner.Text()
		for _, r := range candidates {
			if r.Matches(line) {
				findings = append(findings, Finding{Path: path, Line: n, Rule: r.Name})
			}
		}
	}
	return findings
}
