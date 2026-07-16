package sidecarexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// TestKeyFor maps an original's storage key to its sidecar's.
func TestKeyFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fileKey string
		want    string
		wantErr error
	}{
		{"typical original", "2024/05/IMG_1234.jpg", "sidecars/2024/05/IMG_1234.jpg.yml", nil},
		{"video", "2019/12/VID_0001.mp4", "sidecars/2019/12/VID_0001.mp4.yml", nil},
		{"no directory", "loose.jpg", "sidecars/loose.jpg.yml", nil},
		{"leading slash is stripped", "/2024/05/a.jpg", "sidecars/2024/05/a.jpg.yml", nil},
		{"surrounding space", "  2024/05/a.jpg  ", "sidecars/2024/05/a.jpg.yml", nil},
		{"empty", "", "", ErrEmptyKey},
		{"only space", "   ", "", ErrEmptyKey},
		{"only a slash", "/", "", ErrEmptyKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := KeyFor(tt.fileKey)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("KeyFor(%q) error = %v, want %v", tt.fileKey, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("KeyFor(%q) = %q, want %q", tt.fileKey, got, tt.want)
			}
		})
	}
}

// TestKeyFor_distinguishesSameBaseName pins the reason the extension is appended
// rather than replaced: two photos sharing a base name must not collide on one
// sidecar, or each would silently overwrite the other's curation.
func TestKeyFor_distinguishesSameBaseName(t *testing.T) {
	t.Parallel()

	jpg, err := KeyFor("2024/05/IMG_1.jpg")
	if err != nil {
		t.Fatalf("KeyFor returned error: %v", err)
	}
	png, err := KeyFor("2024/05/IMG_1.png")
	if err != nil {
		t.Fatalf("KeyFor returned error: %v", err)
	}
	if jpg == png {
		t.Errorf("IMG_1.jpg and IMG_1.png share sidecar key %q", jpg)
	}
}

// verifyingStore is a fake ObjectStore that enforces the real store's contract:
// the bytes must match the declared size and digest. It is what proves the Writer
// tells the store the truth about what it is writing — the promise the store's
// atomicity is built on.
type verifyingStore struct {
	objects  map[string][]byte
	putErr   error
	deleteEr error
	deleted  []string
}

// newVerifyingStore returns an empty verifyingStore.
func newVerifyingStore() *verifyingStore {
	return &verifyingStore{objects: map[string][]byte{}}
}

// Put verifies src against file's declared identity and stores it, mimicking the
// real backends: on a mismatch nothing usable is left behind.
func (s *verifyingStore) Put(_ context.Context, src io.Reader, file storage.StoredFile) error {
	if s.putErr != nil {
		return s.putErr
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	if int64(len(data)) != file.Size {
		return errors.New("size mismatch")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != file.Hash {
		return errors.New("hash mismatch")
	}
	s.objects[file.RelPath] = data
	return nil
}

// Delete removes the object at relPath, reporting os.ErrNotExist when absent.
func (s *verifyingStore) Delete(_ context.Context, relPath string) error {
	if s.deleteEr != nil {
		return s.deleteEr
	}
	s.deleted = append(s.deleted, relPath)
	if _, ok := s.objects[relPath]; !ok {
		return os.ErrNotExist
	}
	delete(s.objects, relPath)
	return nil
}

// TestWriter_Write stores the document at the sidecar key with an honest
// size/digest, which is what the store's atomicity guarantee rests on.
func TestWriter_Write(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	key, err := NewWriter(store).Write(context.Background(), "2024/05/IMG_1234.jpg", fullDocument())
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if want := "sidecars/2024/05/IMG_1234.jpg.yml"; key != want {
		t.Errorf("Write returned key %q, want %q", key, want)
	}
	data, ok := store.objects[key]
	if !ok {
		t.Fatalf("nothing stored at %q; stored: %v", key, store.objects)
	}
	doc, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("stored bytes do not parse: %v", err)
	}
	if doc.Identity.UID != "pht000000000001" {
		t.Errorf("stored document uid = %q, want pht000000000001", doc.Identity.UID)
	}
}

// TestWriter_Write_replacesPreviousSidecar asserts a rewrite replaces the old
// file rather than appending to or merging with it — the handler is a "write the
// current truth" operation, not an accumulator.
func TestWriter_Write_replacesPreviousSidecar(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	writer := NewWriter(store)
	doc := fullDocument()

	if _, err := writer.Write(context.Background(), "2024/05/a.jpg", doc); err != nil {
		t.Fatalf("first Write returned error: %v", err)
	}
	doc.Descriptive.Title = "Nový titulek"
	key, err := writer.Write(context.Background(), "2024/05/a.jpg", doc)
	if err != nil {
		t.Fatalf("second Write returned error: %v", err)
	}
	got, err := Unmarshal(store.objects[key])
	if err != nil {
		t.Fatalf("stored bytes do not parse: %v", err)
	}
	if got.Descriptive.Title != "Nový titulek" {
		t.Errorf("title = %q, want the rewritten value", got.Descriptive.Title)
	}
	if len(store.objects) != 1 {
		t.Errorf("stored %d objects, want 1 (the rewrite must replace, not accumulate)", len(store.objects))
	}
}

// TestWriter_Write_emptyFileKey refuses a photo with no file path rather than
// inventing a key for it.
func TestWriter_Write_emptyFileKey(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	if _, err := NewWriter(store).Write(context.Background(), "", fullDocument()); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("Write error = %v, want ErrEmptyKey", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("stored %d objects for an empty key, want 0", len(store.objects))
	}
}

// TestWriter_Write_storeFailurePropagates asserts a store failure surfaces (so
// the marker is not stamped and the queue retries) and leaves nothing behind.
func TestWriter_Write_storeFailurePropagates(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	store.putErr = errors.New("bucket on fire")
	_, err := NewWriter(store).Write(context.Background(), "2024/05/a.jpg", fullDocument())
	if err == nil {
		t.Fatal("Write returned nil, want the store's error")
	}
	if !strings.Contains(err.Error(), "bucket on fire") {
		t.Errorf("error %v does not wrap the store's error", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("stored %d objects after a failed put, want 0", len(store.objects))
	}
}

// TestWriter_Delete removes the sidecar, and treats an already-absent one as
// success — two callers racing to purge one photo must both succeed.
func TestWriter_Delete(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	writer := NewWriter(store)
	if _, err := writer.Write(context.Background(), "2024/05/a.jpg", fullDocument()); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if err := writer.Delete(context.Background(), "2024/05/a.jpg"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("sidecar survived Delete: %v", store.objects)
	}
	if err := writer.Delete(context.Background(), "2024/05/a.jpg"); err != nil {
		t.Errorf("second Delete returned %v, want nil (an absent sidecar is not an error)", err)
	}
}

// TestWriter_Delete_realErrorPropagates asserts a genuine store failure is not
// swallowed along with the benign not-found.
func TestWriter_Delete_realErrorPropagates(t *testing.T) {
	t.Parallel()

	store := newVerifyingStore()
	store.deleteEr = errors.New("permission denied")
	if err := NewWriter(store).Delete(context.Background(), "2024/05/a.jpg"); err == nil {
		t.Error("Delete returned nil, want the store's error")
	}
}

// TestNewWriter_requiresStore asserts a nil store is a wiring bug caught at
// startup rather than a nil dereference on the first job.
func TestNewWriter_requiresStore(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewWriter(nil) did not panic")
		}
	}()
	NewWriter(nil)
}

// TestWriter_Write_overFilesystemBackend runs the Writer against the real
// filesystem storage backend, end to end.
//
// It is the test that proves the atomic-write claim is not merely asserted: the
// FS backend streams to a temp file and renames, so a successful write must leave
// the sidecar at its final key and no temp residue anywhere under the root.
func TestWriter_Write_overFilesystemBackend(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS returned error: %v", err)
	}
	key, err := NewWriter(fs).Write(context.Background(), "2024/05/IMG_1234.jpg", fullDocument())
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatalf("sidecar not at its final path: %v", err)
	}
	if _, err := Unmarshal(data); err != nil {
		t.Errorf("sidecar on disk does not parse: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("# Kukátko metadata sidecar.")) {
		t.Error("sidecar on disk is missing its header")
	}
	assertNoTempResidue(t, root)
}

// assertNoTempResidue fails when any file under root looks like a partial write —
// the residue a non-atomic writer leaves behind.
func assertNoTempResidue(t *testing.T, root string) {
	t.Helper()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if name := info.Name(); strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".tmp") {
			t.Errorf("temp residue left behind at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestWriter_Delete_overFilesystemBackend removes a real sidecar from the real
// backend and is idempotent, which is what the purge path relies on.
func TestWriter_Delete_overFilesystemBackend(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS returned error: %v", err)
	}
	writer := NewWriter(fs)
	if _, err := writer.Write(context.Background(), "2024/05/a.jpg", fullDocument()); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if err := writer.Delete(context.Background(), "2024/05/a.jpg"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sidecars", "2024", "05", "a.jpg.yml")); !os.IsNotExist(err) {
		t.Errorf("sidecar still on disk after Delete (stat err = %v)", err)
	}
	if err := writer.Delete(context.Background(), "2024/05/a.jpg"); err != nil {
		t.Errorf("Delete of an absent sidecar returned %v, want nil", err)
	}
}

// TestBuild_usesWallClockWhenNowIsZero asserts the generation timestamp defaults
// to now, so a document never claims to have been generated at the zero time.
func TestBuild_usesWallClockWhenNowIsZero(t *testing.T) {
	t.Parallel()

	before := time.Now().Add(-time.Second)
	doc := Build(Input{})
	if doc.GeneratedAt.Before(before) {
		t.Errorf("GeneratedAt = %v, want approximately now", doc.GeneratedAt)
	}
}
