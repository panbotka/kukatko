package familyexport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// fakeStore records what a Writer put, and can be told to refuse.
type fakeStore struct {
	file storage.StoredFile
	body string
	puts int
	err  error
}

// Put records the write, verifying nothing: the real backends do that, and this
// fake exists to show the Writer what it declared.
func (f *fakeStore) Put(_ context.Context, src io.Reader, file storage.StoredFile) error {
	f.puts++
	if f.err != nil {
		return f.err
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	f.file = file
	f.body = string(data)
	return nil
}

// TestWriter_Write writes the document at the export's key, declaring the size,
// digest and media type the store verifies it against.
func TestWriter_Write(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	doc := fullDocument()

	key, err := NewWriter(store).Write(t.Context(), doc)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if key != Key {
		t.Errorf("Write returned key %q, want %q", key, Key)
	}
	if store.file.RelPath != Key {
		t.Errorf("stored at %q, want %q", store.file.RelPath, Key)
	}
	if store.file.MIME != MIME {
		t.Errorf("stored MIME = %q, want %q", store.file.MIME, MIME)
	}
	want, err := Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if store.body != string(want) {
		t.Errorf("stored body = %q, want the marshaled document", store.body)
	}
	if store.file.Size != int64(len(want)) {
		t.Errorf("declared size = %d, want %d", store.file.Size, len(want))
	}
	sum := sha256.Sum256(want)
	if store.file.Hash != hex.EncodeToString(sum[:]) {
		t.Errorf("declared digest = %q, want the document's SHA256", store.file.Hash)
	}
}

// TestWriter_WriteOverwrites verifies a second write replaces the first rather
// than accumulating: there is one genealogy, so there is one file, always
// current.
func TestWriter_WriteOverwrites(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	writer := NewWriter(store)
	first := Build(Input{Now: time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)})
	second := Build(Input{Export: sampleExport(), Now: time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)})

	if _, err := writer.Write(t.Context(), first); err != nil {
		t.Fatalf("first Write returned error: %v", err)
	}
	if _, err := writer.Write(t.Context(), second); err != nil {
		t.Fatalf("second Write returned error: %v", err)
	}
	if store.puts != 2 {
		t.Errorf("puts = %d, want 2", store.puts)
	}
	if !strings.Contains(store.body, "fam1") {
		t.Errorf("the store holds the first document, want the second:\n%s", store.body)
	}
}

// TestWriter_WriteWrapsStoreFailure verifies a refused write is reported with the
// key in it, rather than swallowed into a silent "no tree on disk".
func TestWriter_WriteWrapsStoreFailure(t *testing.T) {
	t.Parallel()

	refused := errors.New("bucket unreachable")
	_, err := NewWriter(&fakeStore{err: refused}).Write(t.Context(), fullDocument())
	if !errors.Is(err, refused) {
		t.Fatalf("Write error = %v, want it to wrap %v", err, refused)
	}
	if !strings.Contains(err.Error(), Key) {
		t.Errorf("Write error = %q, want it to name %q", err, Key)
	}
}

// TestNewWriter_requiresAStore pins the wiring bug as a startup panic rather than
// a nil dereference on the first job.
func TestNewWriter_requiresAStore(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewWriter(nil) did not panic")
		}
	}()
	NewWriter(nil)
}
