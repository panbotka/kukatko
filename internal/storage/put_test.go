package storage

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/minio/minio-go/v7"
)

// storedFor returns the identity a Put must be handed to write content at
// relPath, with a media type the sniffer would agree with.
func storedFor(relPath string, content []byte) StoredFile {
	return StoredFile{Hash: hashOf(content), RelPath: relPath, Size: int64(len(content)), MIME: "image/jpeg"}
}

func TestFSPut_writesAtTheCallerChosenKey(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("thumbnail bytes")
	const relPath = "thumb/ab/cd/ef/abcdef_tile_500.jpg"

	if err := fs.Put(t.Context(), bytes.NewReader(content), storedFor(relPath, content)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	onDisk, err := os.ReadFile(filepath.Join(fs.root, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("reading the put file: %v", err)
	}
	if !bytes.Equal(onDisk, content) {
		t.Errorf("Put wrote %q, want %q", onDisk, content)
	}
}

func TestFSPut_overwritesAnExistingFile(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	const relPath = "2024/05/photo.jpg"
	first, second := []byte("first"), []byte("second, longer")

	if err := fs.Put(t.Context(), bytes.NewReader(first), storedFor(relPath, first)); err != nil {
		t.Fatalf("Put(first): %v", err)
	}
	if err := fs.Put(t.Context(), bytes.NewReader(second), storedFor(relPath, second)); err != nil {
		t.Fatalf("Put(second): %v", err)
	}

	// Overwriting is the point: a killed run must be able to replace the object it
	// left half-written, and Put's key is the caller's, not one it may suffix.
	head, err := fs.Head(t.Context(), relPath)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash != hashOf(second) {
		t.Errorf("Head.Hash = %q, want the second content's digest", head.Hash)
	}
}

func TestFSPut_rejectsAWrongDigestAndLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	const relPath = "2024/05/corrupt.jpg"
	declared := storedFor(relPath, []byte("what the catalogue believes"))
	actual := []byte("what the disk actually holds!")
	declared.Size = int64(len(actual)) // only the digest disagrees.

	err := fs.Put(t.Context(), bytes.NewReader(actual), declared)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("Put(wrong digest) = %v, want ErrHashMismatch", err)
	}
	if _, err := fs.Stat(t.Context(), relPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a rejected Put published %s anyway: %v", relPath, err)
	}
	// The staged file must not survive either: this backend's temp dir sits on the
	// same small disk as the originals.
	entries, err := os.ReadDir(fs.tmpDir)
	if err != nil {
		t.Fatalf("reading temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected Put leaked %d temp file(s)", len(entries))
	}
}

func TestFSPut_rejectsAWrongSize(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	const relPath = "2024/05/short.jpg"
	content := []byte("eight...")
	declared := storedFor(relPath, content)
	declared.Size = int64(len(content)) + 10

	err := fs.Put(t.Context(), bytes.NewReader(content), declared)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("Put(wrong size) = %v, want ErrSizeMismatch", err)
	}
	if _, err := fs.Stat(t.Context(), relPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a rejected Put published %s anyway: %v", relPath, err)
	}
}

func TestFSPut_confinesAnEscapingKeyToTheRoot(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("not your passwd")

	if err := fs.Put(t.Context(), bytes.NewReader(content), storedFor("../../etc/passwd", content)); err != nil {
		t.Fatalf("Put(escaping key): %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fs.root), "etc", "passwd")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Put escaped the storage root")
	}
	if _, err := os.Stat(filepath.Join(fs.root, "etc", "passwd")); err != nil {
		t.Errorf("the confined key did not land under the root: %v", err)
	}

	// A key that resolves to the root itself has nowhere to write.
	if err := fs.Put(t.Context(), bytes.NewReader(nil), StoredFile{RelPath: "/"}); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Put(root) = %v, want ErrInvalidPath", err)
	}
}

func TestFSHead_reportsIdentityAndMissingFiles(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'j', 'p', 'e', 'g'}

	stored, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "head.jpg")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	head, err := fs.Head(t.Context(), stored.RelPath)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash != stored.Hash || head.Size != stored.Size || head.MIME != stored.MIME {
		t.Errorf("Head = %+v, want the identity Store reported %+v", head, stored)
	}
	if head.RelPath != stored.RelPath {
		t.Errorf("Head.RelPath = %q, want %q", head.RelPath, stored.RelPath)
	}

	if _, err := fs.Head(t.Context(), "2024/05/absent.jpg"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Head(missing) = %v, want os.ErrNotExist", err)
	}
}

func TestFSCheck_acceptsTheRootAndRejectsAFile(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	if err := fs.Check(t.Context()); err != nil {
		t.Errorf("Check on a real root: %v", err)
	}

	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := (&FS{root: file}).Check(t.Context()); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Check on a file = %v, want ErrInvalidPath", err)
	}
	if err := (&FS{root: filepath.Join(t.TempDir(), "gone")}).Check(t.Context()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Check on a missing root = %v, want os.ErrNotExist", err)
	}
}

func TestHashingReader_digestsAndCountsEverythingRead(t *testing.T) {
	t.Parallel()
	content := []byte("streamed straight into the bucket")

	reader := newHashingReader(bytes.NewReader(content))
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("draining: %v", err)
	}
	if got, want := reader.sum(), hashOf(content); got != want {
		t.Errorf("sum() = %q, want %q", got, want)
	}
	if got, want := reader.read, int64(len(content)); got != want {
		t.Errorf("read = %d, want %d", got, want)
	}
}

func TestVerifyContent_sizeBeatsDigestAndAnUnknownDigestIsSkipped(t *testing.T) {
	t.Parallel()
	want := StoredFile{RelPath: "2024/05/x.jpg", Size: 10, Hash: "cafe"}

	// A truncated stream explains a wrong digest, so the size is the diagnosis.
	if err := verifyContent(want, "beef", 9); !errors.Is(err, ErrSizeMismatch) {
		t.Errorf("verifyContent(short, wrong digest) = %v, want ErrSizeMismatch", err)
	}
	if err := verifyContent(want, "beef", 10); !errors.Is(err, ErrHashMismatch) {
		t.Errorf("verifyContent(wrong digest) = %v, want ErrHashMismatch", err)
	}
	if err := verifyContent(want, "cafe", 10); err != nil {
		t.Errorf("verifyContent(matching) = %v, want nil", err)
	}
	// An empty declared digest means "identity unknown": only the size is checked.
	unknown := StoredFile{RelPath: want.RelPath, Size: 10}
	if err := verifyContent(unknown, "anything", 10); err != nil {
		t.Errorf("verifyContent(no declared digest) = %v, want nil", err)
	}
}

func TestIsSystemic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"missing bucket sentinel", ErrBucketNotFound, true},
		{"wrapped missing bucket", errors.Join(errors.New("ctx"), ErrBucketNotFound), true},
		{"unconfigured destination", ErrR2NotConfigured, true},
		{"invalid endpoint", ErrInvalidEndpoint, true},
		{"bad credentials", minio.ErrorResponse{Code: "InvalidAccessKeyId", StatusCode: 403}, true},
		{"bad signature", minio.ErrorResponse{Code: "SignatureDoesNotMatch", StatusCode: 403}, true},
		{"no such bucket", minio.ErrorResponse{Code: noSuchBucketCode, StatusCode: 404}, true},
		{"forbidden without a code", minio.ErrorResponse{StatusCode: http.StatusForbidden}, true},
		{"missing object", minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}, false},
		{"throttled", minio.ErrorResponse{Code: "SlowDown", StatusCode: 503}, false},
		{"a torn connection", io.ErrUnexpectedEOF, false},
		{"a local hash mismatch", ErrHashMismatch, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsSystemic(tc.err); got != tc.want {
				t.Errorf("IsSystemic(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsSystemic_seesThroughWrapping(t *testing.T) {
	t.Parallel()

	// objectError is how every R2 method reports a failure; a systemic cause must
	// survive that wrapping, or a bulk job would grind through the whole library.
	wrapped := objectError("stat", "2024/05/x.jpg", minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403})
	if !IsSystemic(wrapped) {
		t.Errorf("IsSystemic(%v) = false, want true", wrapped)
	}
}

func TestIsNotFound_doesNotMistakeAMissingBucketForAMissingObject(t *testing.T) {
	t.Parallel()

	missingObject := minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound}
	if !isNotFound(missingObject) {
		t.Error("isNotFound(NoSuchKey) = false, want true")
	}
	// Both answer 404. Reading the second as the first would let resolveKey
	// conclude the key is free and "store" into a bucket that is not there.
	missingBucket := minio.ErrorResponse{Code: noSuchBucketCode, StatusCode: http.StatusNotFound}
	if isNotFound(missingBucket) {
		t.Error("isNotFound(NoSuchBucket) = true, want false")
	}
}
