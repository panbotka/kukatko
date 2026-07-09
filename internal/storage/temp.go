package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// tempFile describes a fully written temporary file: where it lives, the SHA256
// digest of its content, its size in bytes, and its leading bytes captured for
// MIME sniffing.
type tempFile struct {
	Path   string
	Hash   string
	Size   int64
	Header []byte
}

// streamToTemp copies src into a fresh temporary file inside dir, computing the
// SHA256 digest and capturing the leading bytes for MIME sniffing as it goes,
// without ever buffering the whole file in memory. The caller owns the returned
// path and must remove it; on error the temp file is removed here and no path is
// returned.
func streamToTemp(ctx context.Context, dir string, src io.Reader) (tf tempFile, err error) {
	file, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return tempFile{}, fmt.Errorf("storage: creating temp file: %w", err)
	}
	tmpPath := file.Name()
	defer func() {
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("storage: closing temp file: %w", closeErr)
		}
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	sniffer := &headerSniffer{}
	written, err := io.Copy(io.MultiWriter(file, hasher, sniffer), ctxReader(ctx, src))
	if err != nil {
		return tempFile{}, fmt.Errorf("storage: streaming upload: %w", err)
	}
	if err = file.Sync(); err != nil {
		return tempFile{}, fmt.Errorf("storage: flushing temp file: %w", err)
	}
	return tempFile{
		Path:   tmpPath,
		Hash:   hex.EncodeToString(hasher.Sum(nil)),
		Size:   written,
		Header: sniffer.buf,
	}, nil
}

// removeOnce returns a cleanup that deletes the file at path the first time it is
// called and does nothing on every later call, so a caller may defer it and still
// call it explicitly on an error path.
func removeOnce(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { _ = os.Remove(path) })
	}
}

// noopCleanup is the cleanup returned when there is no temporary file to remove.
// Calling it any number of times does nothing.
func noopCleanup() {}

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
