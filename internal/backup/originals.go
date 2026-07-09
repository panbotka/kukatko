package backup

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// tmpDirName is the in-progress upload subdirectory of the originals root
// (storage.FS writes partial uploads here). It is skipped by the sync so
// half-written files are never backed up. It mirrors storage's own constant.
const tmpDirName = ".tmp"

// DiskOriginals is an OriginalSource backed by the on-disk originals root. It
// walks the directory tree, exposing each regular file by its slash-separated
// path relative to the root — the same key layout the bucket uses — and skips
// the temporary upload directory.
type DiskOriginals struct {
	root string
}

// compile-time assertion that DiskOriginals satisfies OriginalSource.
var _ OriginalSource = (*DiskOriginals)(nil)

// NewDiskOriginals returns a DiskOriginals rooted at root (the configured
// storage.originals_path).
func NewDiskOriginals(root string) *DiskOriginals {
	return &DiskOriginals{root: root}
}

// List walks the originals root and returns every regular file as a
// LocalOriginal keyed by its slash-separated path relative to the root. The
// temporary upload directory is skipped. A missing root yields an empty list
// rather than an error, so a fresh install with no originals backs up cleanly.
func (d *DiskOriginals) List(ctx context.Context) ([]LocalOriginal, error) {
	var originals []LocalOriginal
	walkErr := filepath.WalkDir(d.root, func(absPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("interrupted: %w", ctxErr)
		}
		return d.visit(absPath, entry, &originals)
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: walking originals %s: %w", d.root, walkErr)
	}
	return originals, nil
}

// visit handles one entry of the originals walk: it skips the temporary upload
// directory wholesale, ignores non-regular files, and appends a LocalOriginal
// for each regular file keyed by its slash-separated relative path.
func (d *DiskOriginals) visit(absPath string, entry fs.DirEntry, originals *[]LocalOriginal) error {
	if entry.IsDir() {
		if entry.Name() == tmpDirName {
			return filepath.SkipDir
		}
		return nil
	}
	if !entry.Type().IsRegular() {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("statting %s: %w", absPath, err)
	}
	rel, err := filepath.Rel(d.root, absPath)
	if err != nil {
		return fmt.Errorf("relativising %s: %w", absPath, err)
	}
	*originals = append(*originals, LocalOriginal{Key: filepath.ToSlash(rel), Size: info.Size()})
	return nil
}

// Open opens the original at key (a slash-separated path relative to the root)
// for reading, confining the resolved path to the root so a crafted key cannot
// escape it. The caller must close the returned reader.
func (d *DiskOriginals) Open(_ context.Context, key string) (io.ReadCloser, error) {
	abs := filepath.Join(d.root, filepath.FromSlash(confineKey(key)))
	file, err := os.Open(abs) //nolint:gosec // G304: abs is confined to the originals root by confineKey.
	if err != nil {
		return nil, fmt.Errorf("backup: opening %s: %w", key, err)
	}
	return file, nil
}

// CopyTo streams the original at original.Key from disk into dst under the same
// key. Unlike the bucket source there is no server-side shortcut available: the
// bytes exist only on this host, so they necessarily pass through this process.
// The stream is never buffered whole.
func (d *DiskOriginals) CopyTo(ctx context.Context, dst ObjectStore, original LocalOriginal) error {
	reader, err := d.Open(ctx, original.Key)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	if err := dst.Put(ctx, original.Key, reader, original.Size, ""); err != nil {
		return fmt.Errorf("backup: uploading %s: %w", original.Key, err)
	}
	return nil
}

// Stat reports whether the original at key already exists on disk and, when it
// does, its byte size, so the restore download can skip files already present at
// the same size. A missing file yields ok=false with a nil error; the path is
// confined to the root.
func (d *DiskOriginals) Stat(_ context.Context, key string) (LocalOriginal, bool, error) {
	abs := filepath.Join(d.root, filepath.FromSlash(confineKey(key)))
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return LocalOriginal{}, false, nil
		}
		return LocalOriginal{}, false, fmt.Errorf("backup: statting %s: %w", key, err)
	}
	return LocalOriginal{Key: key, Size: info.Size()}, true, nil
}

// Write streams reader to the original at key, creating parent directories and
// publishing the file atomically: it writes to a temporary file under the
// root's .tmp directory and renames it into place only once fully written. An
// interrupted download therefore never leaves a partial file at the final path,
// so re-running the restore safely resumes (the partial temp file is discarded).
// The path is confined to the root.
func (d *DiskOriginals) Write(_ context.Context, key string, reader io.Reader) error {
	rel := confineKey(key)
	abs := filepath.Join(d.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return fmt.Errorf("backup: creating directory for %s: %w", key, err)
	}
	tmpDir := filepath.Join(d.root, tmpDirName)
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return fmt.Errorf("backup: creating temp directory: %w", err)
	}
	tmp, err := os.CreateTemp(tmpDir, "restore-*")
	if err != nil {
		return fmt.Errorf("backup: creating temp file for %s: %w", key, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed away.
	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: writing %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backup: closing temp file for %s: %w", key, err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("backup: publishing %s: %w", key, err)
	}
	return nil
}

// confineKey cleans key as if rooted at "/" so that any "../" segments cannot
// escape above the originals root, then strips the leading slash.
func confineKey(key string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(key), "/"))
	return strings.TrimPrefix(cleaned, "/")
}
