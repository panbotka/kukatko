package facejob

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// staticResolver returns a fixed absolute path regardless of input.
type staticResolver struct{ abs string }

// AbsPath returns the configured absolute path.
func (s staticResolver) AbsPath(_ string) string { return s.abs }

// writeTempJPEG writes some bytes to a temp file and returns its path.
func writeTempJPEG(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "img.jpg")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// TestStorageSource_passthrough opens the original directly when the decoder
// returns the source path unchanged, and runs the decoder cleanup on Close.
func TestStorageSource_passthrough(t *testing.T) {
	t.Parallel()

	path := writeTempJPEG(t, "jpeg-bytes")
	cleaned := false
	src := &StorageSource{
		storage: staticResolver{abs: path},
		decode: func(_ context.Context, p string) (string, func(), error) {
			return p, func() { cleaned = true }, nil
		},
	}

	rc, err := src.OpenDecodable(context.Background(), photos.Photo{UID: "ph1", FilePath: "2026/01/img.jpg"})
	if err != nil {
		t.Fatalf("OpenDecodable: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "jpeg-bytes" {
		t.Errorf("read %q, want %q", got, "jpeg-bytes")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !cleaned {
		t.Error("decoder cleanup was not run on Close")
	}
}

// TestStorageSource_convertedCleanup opens a temporary converted file and removes
// it on Close, mirroring the HEIC/RAW/video path.
func TestStorageSource_convertedCleanup(t *testing.T) {
	t.Parallel()

	temp := writeTempJPEG(t, "converted")
	src := &StorageSource{
		storage: staticResolver{abs: "/originals/photo.heic"},
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return temp, func() { _ = os.Remove(temp) }, nil
		},
	}

	rc, err := src.OpenDecodable(context.Background(), photos.Photo{UID: "ph1"})
	if err != nil {
		t.Fatalf("OpenDecodable: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp converted file still present after Close: %v", err)
	}
}

// TestStorageSource_decodeError surfaces a decoder failure.
func TestStorageSource_decodeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	src := &StorageSource{
		storage: staticResolver{abs: "/x"},
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return "", nil, wantErr
		},
	}
	if _, err := src.OpenDecodable(context.Background(), photos.Photo{UID: "ph1"}); !errors.Is(err, wantErr) {
		t.Errorf("OpenDecodable error = %v, want %v", err, wantErr)
	}
}

// TestStorageSource_openError runs cleanup when the decodable file cannot be
// opened, so a converted temp file is not leaked.
func TestStorageSource_openError(t *testing.T) {
	t.Parallel()

	cleaned := false
	src := &StorageSource{
		storage: staticResolver{abs: "/x"},
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return filepath.Join(t.TempDir(), "does-not-exist.jpg"), func() { cleaned = true }, nil
		},
	}
	if _, err := src.OpenDecodable(context.Background(), photos.Photo{UID: "ph1"}); err == nil {
		t.Fatal("OpenDecodable = nil, want an error opening a missing file")
	}
	if !cleaned {
		t.Error("cleanup was not run after an open failure")
	}
}
