package dirimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// ErrNotDirectory is returned when the import root is not a directory (a file, a
// device, or a path that does not exist).
var ErrNotDirectory = errors.New("dirimport: import root is not a directory")

// junkNames are filesystem artefacts, matched case-insensitively on the whole
// name. The directory entries (@eaDir, __MACOSX) are pruned wholesale; the file
// entries are skipped one by one.
var junkNames = map[string]struct{}{
	"@eadir":      {},
	"__macosx":    {},
	"thumbs.db":   {},
	".ds_store":   {},
	"desktop.ini": {},
	"picasa.ini":  {},
	".picasa.ini": {},
}

// sidecarExts are metadata companions of a real photo — not media in their own
// right. They are skipped with their own reason so a folder full of Google
// Takeout .json or Apple .aae files does not read as "unsupported junk"; mining
// them for metadata is a separate task.
var sidecarExts = map[string]struct{}{
	".xmp":  {},
	".json": {},
	".aae":  {},
	".thm":  {},
}

// planEntry is one file the walk decided about: either a candidate for ingest
// (skip empty) or an already-decided skip.
type planEntry struct {
	// abs is the absolute path to read the file from.
	abs string
	// rel is the path relative to the import root, as reported to the user.
	rel string
	// skip is the rule that excluded the file; empty means the file is media and
	// should be ingested.
	skip SkipReason
}

// plan walks root and classifies every file below it, returning the entries in
// walk order (lexical, so a run's output is deterministic). It returns
// ErrNotDirectory when root is not a directory.
//
// Directories are pruned rather than descended into when they are hidden (a
// leading dot), junk (@eaDir, __MACOSX), or — when recursive is false — simply
// below root. Symlinks *inside* the tree are never followed, so the walk cannot
// loop; each is reported as a skip. Only root itself is resolved, so pointing the
// import at a symlinked directory works as the user expects.
//
// A walk error on a single entry (an unreadable subdirectory, a file that
// vanished mid-walk) is not fatal: the entry is dropped and the walk continues.
// Only a failure to read root itself aborts.
func plan(root string, recursive bool) ([]planEntry, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("dirimport: resolving import root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("dirimport: reading import root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDirectory, root)
	}

	var entries []planEntry
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry is reported and stepped over, not fatal: a 2000-file
			// import must not die on one permission-denied subdirectory.
			return skipUnreadable(root, path, entry, err)
		}
		rel := relative(root, path)
		if entry.IsDir() {
			return pruneDir(rel, entry.Name(), recursive)
		}
		if reason, ok := classify(entry); ok {
			entries = append(entries, planEntry{abs: path, rel: rel, skip: reason})
			return nil
		}
		entries = append(entries, planEntry{abs: path, rel: rel})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("dirimport: walking %q: %w", root, walkErr)
	}
	return entries, nil
}

// skipUnreadable turns a walk error on one entry into a "step over it" decision:
// a directory is pruned, a file is dropped, and the walk carries on. An error on
// root itself is returned, aborting the walk.
func skipUnreadable(root, path string, entry fs.DirEntry, err error) error {
	if path == root {
		return err
	}
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// pruneDir decides whether the walk descends into a directory: root always, a
// subdirectory only in a recursive run and only when it is neither hidden nor
// junk. rel is the directory's path relative to root ("." for root itself).
func pruneDir(rel, name string, recursive bool) error {
	if rel == "." {
		return nil
	}
	if !recursive || isHidden(name) || isJunk(name) {
		return fs.SkipDir
	}
	return nil
}

// classify applies the skip rules to one file entry, in the order a user would
// explain them: links are never followed, junk and hidden files are noise, empty
// files hold nothing, sidecars are metadata rather than media, and whatever is
// left must be a format the pipeline can actually decode. It reports the reason
// and true when the file is skipped, and false when it is media to ingest.
func classify(entry fs.DirEntry) (SkipReason, bool) {
	name := entry.Name()
	switch {
	case entry.Type()&fs.ModeSymlink != 0:
		return SkipSymlink, true
	case !entry.Type().IsRegular():
		return SkipUnsupported, true
	case isJunk(name):
		return SkipJunk, true
	case isHidden(name):
		return SkipHidden, true
	case isSidecar(name):
		return SkipSidecar, true
	case !imgconvert.IsSupportedFormat(filepath.Ext(name)):
		return SkipUnsupported, true
	case isEmpty(entry):
		return SkipEmpty, true
	}
	return "", false
}

// isHidden reports whether a file or directory name is a dotfile.
func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

// isJunk reports whether a name is a known filesystem artefact, matched
// case-insensitively (a FAT-formatted card yields THUMBS.DB).
func isJunk(name string) bool {
	_, ok := junkNames[strings.ToLower(name)]
	return ok
}

// isSidecar reports whether a name is a metadata sidecar rather than media.
func isSidecar(name string) bool {
	_, ok := sidecarExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

// isEmpty reports whether the entry is a zero-byte file. An entry whose info
// cannot be read (it vanished mid-walk) is treated as empty, so it is skipped
// rather than failed.
func isEmpty(entry fs.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return true
	}
	return info.Size() == 0
}

// relative returns path as seen from root, falling back to the absolute path if
// the two are unrelated (which filepath.WalkDir never produces).
func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
