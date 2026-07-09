// Package storage is Kukátko's on-disk store for original media files. It owns a
// deterministic layout — originals live under the configured originals root as
// YYYY/MM/<filename>, where the date comes from a photo's taken_at timestamp
// (falling back to the import time when that is unknown) — and computes the
// SHA256 content hash of every file it writes.
//
// Content identity is the SHA256 hex digest. Filename collisions within a month
// directory are resolved safely: an incoming file whose bytes are identical to
// the file already occupying its path is reported as a duplicate (ErrAlreadyExists)
// rather than rewritten, while a same-name-but-different-content file is stored
// under a numeric suffix so nothing is ever overwritten. Authoritative,
// catalogue-wide deduplication is the database's job (the photos.file_hash unique
// constraint); the ErrAlreadyExists signal here only covers the filename clash.
//
// All operations stream: files are never buffered whole in memory, so arbitrarily
// large originals and videos can be stored and served.
package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// Sentinel errors returned by the store so callers (importers, upload handlers,
// tests) can branch with errors.Is.
var (
	// ErrAlreadyExists indicates that the target path is already occupied by a
	// file with byte-identical content. Store returns it together with a fully
	// populated StoredFile describing the existing file, so callers can treat the
	// write as a deduplicated no-op.
	ErrAlreadyExists = errors.New("storage: file already exists")
	// ErrInvalidPath indicates a relative path that escapes the storage root or is
	// otherwise unusable (empty, or pointing at the root directory itself).
	ErrInvalidPath = errors.New("storage: invalid relative path")
	// ErrTooManyCollisions indicates that suffix resolution exhausted its attempt
	// budget for a single target filename (effectively never under normal use).
	ErrTooManyCollisions = errors.New("storage: too many filename collisions")
)

// StoredFile describes a file as it lives in the store. RelPath is always
// slash-separated (YYYY/MM/<name>) for portability and direct use as the
// photos.file_path column value.
type StoredFile struct {
	// Hash is the lowercase hex SHA256 digest of the file's content.
	Hash string `json:"hash"`
	// RelPath is the slash-separated path relative to the storage root.
	RelPath string `json:"rel_path"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// MIME is the detected media type (content sniffing with extension as hint).
	MIME string `json:"mime"`
}

// Storage is the on-disk store for original media files. Implementations must be
// safe for concurrent use: simultaneous writes of identical content must not
// corrupt each other and must converge on a single stored file.
type Storage interface {
	// Store streams src to disk under YYYY/MM/<originalName> (the date taken from
	// takenAt, or the current time when takenAt is the zero value), computing the
	// SHA256 digest as it writes without buffering the whole file in memory. It
	// returns the resulting StoredFile. When the target path is already occupied
	// by byte-identical content it returns that StoredFile together with
	// ErrAlreadyExists; when occupied by different content it stores under a
	// numeric suffix and returns a nil error.
	Store(ctx context.Context, src io.Reader, takenAt time.Time, originalName string) (StoredFile, error)
	// Open opens the file at relPath for reading. The caller owns the returned
	// reader and must close it.
	Open(ctx context.Context, relPath string) (io.ReadCloser, error)
	// Stat returns file information for relPath.
	Stat(ctx context.Context, relPath string) (os.FileInfo, error)
	// Delete removes the file at relPath. It returns an error wrapping
	// os.ErrNotExist when the file is absent.
	Delete(ctx context.Context, relPath string) error
	// URL returns the address at which a client can fetch the object at relPath
	// directly, bypassing the application. It returns the empty string when the
	// backend exposes no such address — as the filesystem backend does, its
	// originals living on a disk no browser can reach — in which case the caller
	// must serve the bytes itself through the application's own media routes,
	// which stream the file via Open.
	URL(relPath string) string
	// Materialize yields a real local file for relPath, for the external tools
	// (exiftool, ffprobe, ffmpeg, heif-convert, vipsthumbnail) that take a
	// filename and cannot read an io.Reader. A backend whose objects are already
	// local returns their path as-is and copies nothing; a remote backend
	// downloads to a temporary file.
	//
	// The caller must always call cleanup once it is done with the file,
	// including on its own error paths, or a remote backend leaks a temp file.
	// cleanup is never nil — it is a no-op even when Materialize fails — and is
	// safe to call more than once.
	Materialize(ctx context.Context, relPath string) (path string, cleanup func(), err error)
}
