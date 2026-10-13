package rekey

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// memStore is an in-memory object store with S3-style ETags. Its knobs make one
// key misbehave: corrupt makes a copy onto the key write different bytes,
// failCopy and failDelete make the operation on the key fail, and multipart
// gives an object an opaque multipart ETag so verification has to read it.
type memStore struct {
	mu         sync.Mutex
	objects    map[string][]byte
	corrupt    map[string]bool
	failCopy   map[string]error
	failDelete map[string]error
	multipart  map[string]bool
	copies     int
}

// compile-time assertions that the fake is a store a move can run over, with
// entity tags.
var (
	_ ObjectStore        = (*memStore)(nil)
	_ storage.ETagStater = (*memStore)(nil)
)

// newMemStore returns an empty fake.
func newMemStore() *memStore {
	return &memStore{
		objects:    map[string][]byte{},
		corrupt:    map[string]bool{},
		failCopy:   map[string]error{},
		failDelete: map[string]error{},
		multipart:  map[string]bool{},
	}
}

// put stores data at key.
func (m *memStore) put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = slices.Clone(data)
}

// get returns the bytes at key.
func (m *memStore) get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	return data, ok
}

// keys returns every key, sorted.
func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for key := range m.objects {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// missing is the not-found error the real backends return.
func missing(key string) error {
	return fmt.Errorf("fake: %s: %w", key, os.ErrNotExist)
}

// Copy duplicates from to to, honouring the corrupt and failCopy knobs.
func (m *memStore) Copy(_ context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failCopy[to]; err != nil {
		return err
	}
	data, ok := m.objects[from]
	if !ok {
		return missing(from)
	}
	m.copies++
	copied := slices.Clone(data)
	if m.corrupt[to] {
		copied = append(copied, '!')
	}
	m.objects[to] = copied
	return nil
}

// Stat reports the object's size.
func (m *memStore) Stat(_ context.Context, key string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, missing(key)
	}
	return fileInfo{name: path.Base(key), size: int64(len(data))}, nil
}

// StatETag reports the object's size and an S3-style ETag.
func (m *memStore) StatETag(_ context.Context, key string) (storage.ObjectStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return storage.ObjectStat{}, missing(key)
	}
	sum := md5.Sum(data)
	etag := hex.EncodeToString(sum[:])
	if m.multipart[key] {
		etag += "-2"
	}
	return storage.ObjectStat{Size: int64(len(data)), ETag: etag}, nil
}

// Open returns the object's bytes.
func (m *memStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.get(key)
	if !ok {
		return nil, missing(key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Delete removes the object, honouring the failDelete knob.
func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failDelete[key]; err != nil {
		return err
	}
	if _, ok := m.objects[key]; !ok {
		return missing(key)
	}
	delete(m.objects, key)
	return nil
}

// KeysWithPrefix yields every key starting with prefix, sorted.
func (m *memStore) KeysWithPrefix(_ context.Context, prefix string, yield func(string) error) error {
	for _, key := range m.keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

// fileInfo is the fake's os.FileInfo.
type fileInfo struct {
	name string
	size int64
}

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return 0o444 }
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Sys() any           { return nil }

// sha256Hex returns the hex SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
