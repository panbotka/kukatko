// Package storage is Kukátko's store for original media files. It owns a
// deterministic layout — originals live under the configured originals root as
// YYYY/MM/<filename>, where the date comes from a photo's taken_at timestamp
// (falling back to the import time when that is unknown) — and computes the
// SHA256 content hash of every file it writes.
//
// Two backends implement the same layout and contract: FS keeps originals on a
// local disk, and R2 keeps them in a private Cloudflare R2 bucket, where the
// relative path is the object key verbatim and clients fetch objects from an edge
// Worker with a short-lived signed URL. Which one runs is chosen by
// storage.backend at startup; nothing above this package knows the difference.
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
	// Put streams src into the store at file.RelPath, a key the caller chooses
	// rather than one the store derives, replacing whatever occupies it. It is how
	// an artifact whose key is fixed elsewhere — a thumbnail, whose cache path is
	// a function of its photo's hash and size — reaches the store, and how a bulk
	// migration re-creates an existing layout in a new backend verbatim.
	//
	// file declares the identity the store must end up holding: exactly file.Size
	// bytes, digesting to file.Hash, served as file.MIME. Implementations verify
	// the stream against that identity as they write it and return ErrSizeMismatch
	// or ErrHashMismatch without leaving a usable object behind, so a nil error
	// means the content is durably in place and is the content that was promised.
	// Nothing is buffered whole in memory.
	Put(ctx context.Context, src io.Reader, file StoredFile) error
	// Head returns the identity of the object at relPath as the store holds it:
	// its size, its content digest and its media type. It reads no content. The
	// Hash is the empty string when the store keeps no digest for the object,
	// which is what a foreign tool's object looks like — an object whose identity
	// is unknown, never one that may be assumed to match.
	//
	// It returns an error wrapping os.ErrNotExist when nothing is stored there.
	Head(ctx context.Context, relPath string) (StoredFile, error)
	// Check reports whether the store's destination is reachable and usable: the
	// root exists, or the bucket exists and the credentials open it. It exists so
	// a job that will run for hours fails in its first second on a typo rather
	// than on its first upload.
	Check(ctx context.Context) error
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

// KeyLister enumerates every key a store holds, whether or not the catalogue
// knows about it. Both backends implement it (*FS by walking the root, *R2 by
// listing the bucket).
//
// It is deliberately not part of Storage. Every consumer of Storage — the upload
// pipeline, the thumbnailer, the sidecar writer — addresses objects it already
// has the key of, and the fakes those packages test against would all have to
// grow a method they never call. Only the operations that reason about the store
// as a whole need this: reconciling the store against the catalogue, and sweeping
// the keys the catalogue no longer references. They type-assert for it and say so
// when a store cannot answer.
type KeyLister interface {
	// Keys calls yield once for every object the store holds, passing its
	// slash-separated key relative to the store root, in unspecified order. It
	// stops at the first error from yield and returns it unwrapped, so a caller
	// can end the walk with a sentinel of its own.
	//
	// Keys streams: it never materialises the whole key set, so a store holding
	// millions of objects is walked in bounded memory. What it enumerates is the
	// store's own content, including objects written by something other than
	// Kukátko — deciding which of them may be touched is the caller's job.
	Keys(ctx context.Context, yield func(key string) error) error
}

// PrefixLister narrows a key listing to one prefix, answering "what does the
// store already hold under here?" in a single round trip. Both backends
// implement it (*FS by walking the prefix's own directory, *R2 by listing the
// bucket with that prefix).
//
// It exists because the alternative — one Head per key — is a round trip per
// question, and the questions come in groups: the thumbnailer asks about a
// photo's eight sizes at once, all of them sharing the sharded key prefix
// derived from the photo's file hash. One listing answers all eight, which is
// what makes "is this already published?" cheap enough to ask before every
// encode.
//
// Like KeyLister it is deliberately not part of Storage: the packages that only
// ever address a key they already hold would all have to grow a method they
// never call. A caller type-asserts for it and falls back when a store cannot
// answer.
type PrefixLister interface {
	// KeysWithPrefix calls yield once for every object whose slash-separated key
	// (relative to the store root) starts with prefix, in unspecified order. An
	// empty prefix enumerates everything, exactly as KeyLister.Keys does. The
	// prefix is matched literally, not as a path component, so it may end
	// mid-filename — which is how a listing selects one photo's derived files out
	// of the shard directory they share.
	//
	// It stops at the first error from yield and returns it unwrapped, so a caller
	// can end the walk with a sentinel of its own. A prefix nothing matches — or
	// one naming a directory that does not exist — yields nothing and is not an
	// error. Like Keys it streams, never materialising the key set.
	KeysWithPrefix(ctx context.Context, prefix string, yield func(key string) error) error
}
