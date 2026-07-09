package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	// dirPerm is the permission mode for created directories (owner-only, the
	// service runs as a dedicated kukatko user).
	dirPerm = 0o750
	// sniffLen is the number of leading bytes captured for MIME content sniffing,
	// matching net/http's own detection window.
	sniffLen = 512
	// maxCollisionAttempts caps suffix resolution for a single target filename. It
	// is a defensive backstop against an unbounded loop; real collisions are rare.
	maxCollisionAttempts = 10000
	// tmpDirName is the subdirectory under the root that holds in-progress
	// uploads. Keeping it on the same filesystem as the originals lets the final
	// publish be an atomic hard link.
	tmpDirName = ".tmp"
)

// FS is a filesystem-backed Storage. It writes every upload to a temporary file
// on the same filesystem as the originals root, then publishes it with an atomic
// hard link, which both guarantees crash-safety (no half-written originals are
// ever visible at their final path) and makes concurrent identical writes
// converge race-free.
type FS struct {
	root   string
	tmpDir string
}

// compile-time assertion that *FS satisfies Storage.
var _ Storage = (*FS)(nil)

// NewFS returns an FS rooted at root, creating the root and its temporary upload
// directory if they do not yet exist. It returns a wrapped error if either
// directory cannot be created.
func NewFS(root string) (*FS, error) {
	tmpDir := filepath.Join(root, tmpDirName)
	if err := os.MkdirAll(tmpDir, dirPerm); err != nil {
		return nil, fmt.Errorf("storage: creating storage directories under %s: %w", root, err)
	}
	return &FS{root: root, tmpDir: tmpDir}, nil
}

// Store streams src into the store under YYYY/MM/<originalName> and returns the
// resulting StoredFile. See the Storage interface for the full contract,
// including the ErrAlreadyExists duplicate signal.
func (s *FS) Store(
	ctx context.Context, src io.Reader, takenAt time.Time, originalName string,
) (StoredFile, error) {
	tmpPath, hash, size, header, err := s.streamToTemp(ctx, src)
	if err != nil {
		return StoredFile{}, err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	name := sanitizeName(originalName, hash)
	relDir := relDirFor(takenAt)
	relPath, existed, err := s.linkIntoPlace(tmpPath, relDir, name, hash)
	if err != nil {
		return StoredFile{}, err
	}

	stored := StoredFile{Hash: hash, RelPath: relPath, Size: size, MIME: detectMIME(header, name)}
	if existed {
		return stored, ErrAlreadyExists
	}
	return stored, nil
}

// streamToTemp copies src into a fresh temporary file, computing the SHA256
// digest and capturing the leading bytes for MIME sniffing as it goes, without
// buffering the whole file. It returns the temp file path, the hex digest, the
// byte count, and the captured header. The caller owns the returned temp path
// and must remove it; on error the temp file is removed here.
func (s *FS) streamToTemp(
	ctx context.Context, src io.Reader,
) (tmpPath, hash string, size int64, header []byte, err error) {
	tmp, err := os.CreateTemp(s.tmpDir, "upload-*")
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("storage: creating temp file: %w", err)
	}
	// actualPath is the temp file's real name; the named tmpPath return is only
	// set on success, so the defer must clean up via this captured local instead.
	actualPath := tmp.Name()
	defer func() {
		closeErr := tmp.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("storage: closing temp file: %w", closeErr)
		}
		if err != nil {
			_ = os.Remove(actualPath)
		}
	}()

	hasher := sha256.New()
	sniffer := &headerSniffer{}
	written, err := io.Copy(io.MultiWriter(tmp, hasher, sniffer), ctxReader(ctx, src))
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("storage: streaming upload: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return "", "", 0, nil, fmt.Errorf("storage: flushing temp file: %w", err)
	}
	tmpPath = actualPath
	return tmpPath, hex.EncodeToString(hasher.Sum(nil)), written, sniffer.buf, nil
}

// linkIntoPlace publishes the temp file at tmpPath into relDir under name,
// resolving collisions: it hard-links the temp file to the first free candidate
// path, treating an identical-content occupant as a duplicate (existed=true) and
// a different-content occupant as a reason to try the next numeric suffix. It
// returns the chosen slash-separated relative path.
func (s *FS) linkIntoPlace(tmpPath, relDir, name, hash string) (relPath string, existed bool, err error) {
	absDir := filepath.Join(s.root, filepath.FromSlash(relDir))
	if mkErr := os.MkdirAll(absDir, dirPerm); mkErr != nil {
		return "", false, fmt.Errorf("storage: creating directory %s: %w", relDir, mkErr)
	}
	for attempt := range maxCollisionAttempts {
		candidate := suffixName(name, attempt)
		absPath := filepath.Join(absDir, candidate)
		linkErr := os.Link(tmpPath, absPath)
		if linkErr == nil {
			return path.Join(relDir, candidate), false, nil
		}
		if !os.IsExist(linkErr) {
			return "", false, fmt.Errorf("storage: linking %s: %w", candidate, linkErr)
		}
		same, hashErr := fileHasHash(absPath, hash)
		if hashErr != nil {
			return "", false, hashErr
		}
		if same {
			return path.Join(relDir, candidate), true, nil
		}
	}
	return "", false, fmt.Errorf("%w: %s", ErrTooManyCollisions, name)
}

// Open opens the file at relPath for reading.
func (s *FS) Open(_ context.Context, relPath string) (io.ReadCloser, error) {
	abs, err := s.safeAbs(relPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(abs) //nolint:gosec // G304: abs is confined to the storage root by safeAbs.
	if err != nil {
		return nil, fmt.Errorf("storage: opening %s: %w", relPath, err)
	}
	return file, nil
}

// Stat returns file information for relPath.
func (s *FS) Stat(_ context.Context, relPath string) (os.FileInfo, error) {
	abs, err := s.safeAbs(relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("storage: stat %s: %w", relPath, err)
	}
	return info, nil
}

// Delete removes the file at relPath.
func (s *FS) Delete(_ context.Context, relPath string) error {
	abs, err := s.safeAbs(relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("storage: deleting %s: %w", relPath, err)
	}
	return nil
}

// URL returns the empty string: originals under the storage root are not exposed
// over HTTP, so a client cannot fetch them directly and must go through the
// application's media routes instead. See Storage.URL.
func (s *FS) URL(_ string) string {
	return ""
}

// Materialize returns the original's own path under the storage root together
// with a no-op cleanup. The file is already local, so nothing is copied and
// nothing has to be removed afterwards — which is what keeps local development
// and the test suite zero-copy. See Storage.Materialize.
func (s *FS) Materialize(_ context.Context, relPath string) (string, func(), error) {
	abs, err := s.safeAbs(relPath)
	if err != nil {
		return "", noopCleanup, err
	}
	return abs, noopCleanup, nil
}

// noopCleanup is the cleanup returned by FS.Materialize. There is no temporary
// file to remove, and calling it any number of times does nothing.
func noopCleanup() {}

// safeAbs resolves relPath to an absolute path confined to the root, rejecting
// paths that resolve to the root directory itself with ErrInvalidPath.
func (s *FS) safeAbs(relPath string) (string, error) {
	confined := confine(relPath)
	if confined == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relPath)
	}
	return filepath.Join(s.root, filepath.FromSlash(confined)), nil
}

// confine cleans relPath as if rooted at "/" so that any "../" segments cannot
// escape above the storage root, then strips the leading slash. It returns the
// empty string when the result is the root directory itself.
func confine(relPath string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(relPath), "/"))
	return strings.TrimPrefix(cleaned, "/")
}

// relDirFor returns the slash-separated YYYY/MM directory for takenAt, falling
// back to the current time when takenAt is the zero value (taken_at unknown).
func relDirFor(takenAt time.Time) string {
	if takenAt.IsZero() {
		takenAt = time.Now()
	}
	return fmt.Sprintf("%04d/%02d", takenAt.Year(), int(takenAt.Month()))
}

// sanitizeName reduces originalName to a safe base filename, discarding any
// directory components. It returns fallback when the name is empty or resolves
// to a directory reference (".", "..").
func sanitizeName(originalName, fallback string) string {
	name := filepath.Base(filepath.FromSlash(originalName))
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return fallback
	}
	return name
}

// suffixName returns name for attempt 0 and "<stem>_<attempt><ext>" thereafter,
// inserting the disambiguating suffix before the extension.
func suffixName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", stem, attempt, ext)
}

// fileHasHash reports whether the file at absPath has the given hex SHA256
// digest, streaming the file through the hasher without buffering it.
func fileHasHash(absPath, want string) (bool, error) {
	file, err := os.Open(absPath) //nolint:gosec // G304: absPath is built from confined, store-internal paths.
	if err != nil {
		return false, fmt.Errorf("storage: opening %s for hashing: %w", absPath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, fmt.Errorf("storage: hashing %s: %w", absPath, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)) == want, nil
}

// headerSniffer is an io.Writer that retains the first sniffLen bytes written to
// it (for MIME content sniffing) and discards the rest.
type headerSniffer struct {
	buf []byte
}

// Write appends bytes from p to the retained header until sniffLen bytes have
// been captured, then ignores further input. It always reports the full length
// as written so it can sit in an io.MultiWriter without short-write errors.
func (h *headerSniffer) Write(p []byte) (int, error) {
	if remaining := sniffLen - len(h.buf); remaining > 0 {
		take := min(remaining, len(p))
		h.buf = append(h.buf, p[:take]...)
	}
	return len(p), nil
}

// readerFunc adapts a function to io.Reader, letting a closure satisfy the
// interface without a context-carrying struct.
type readerFunc func(p []byte) (int, error)

// Read calls the underlying function.
func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// ctxReader wraps reader so that reads abort once ctx is done, letting a very
// large or slow upload be cancelled mid-stream. The context is captured in the
// closure rather than stored in a struct field.
func ctxReader(ctx context.Context, reader io.Reader) io.Reader {
	return readerFunc(func(p []byte) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("storage: upload cancelled: %w", err)
		}
		n, err := reader.Read(p)
		if err != nil && !errors.Is(err, io.EOF) {
			return n, fmt.Errorf("storage: reading upload: %w", err)
		}
		return n, err //nolint:wrapcheck // io.EOF must pass through unwrapped for io.Copy.
	})
}
