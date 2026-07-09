package backup

import (
	"errors"
	"reflect"
	"testing"
)

// primaryBucket is the name the fake primary store is registered under in the
// backup store's peers, standing in for "the backup endpoint can read this
// bucket".
const primaryBucket = "kukatko-originals"

// newBucketPair returns a fake primary store seeded with seed, a fake backup
// store that can copy from it, and a BucketOriginals over the primary.
func newBucketPair(t *testing.T, seed map[string][]byte) (*fakeStore, *fakeStore, *BucketOriginals) {
	t.Helper()
	primary := newFakeStore(seed)
	backupStore := newFakeStore(nil)
	backupStore.peers[primaryBucket] = primary
	originals, err := NewBucketOriginals(primary, primaryBucket)
	if err != nil {
		t.Fatalf("NewBucketOriginals: %v", err)
	}
	return primary, backupStore, originals
}

func TestNewBucketOriginals_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  ObjectStore
		bucket  string
		wantErr error
	}{
		{name: "source and bucket succeed", source: newFakeStore(nil), bucket: primaryBucket},
		{name: "nil source is a wiring bug", source: nil, bucket: primaryBucket, wantErr: ErrNoSourceStore},
		{name: "empty bucket fails loudly", source: newFakeStore(nil), bucket: "", wantErr: ErrNoSourceBucket},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewBucketOriginals(tt.source, tt.bucket)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewBucketOriginals(_, %q) error = %v, want %v", tt.bucket, err, tt.wantErr)
			}
			if tt.wantErr == nil && got.Bucket() != tt.bucket {
				t.Errorf("Bucket() = %q, want %q", got.Bucket(), tt.bucket)
			}
		})
	}
}

func TestBucketOriginals_List_skipsDumpsAndPartialUploads(t *testing.T) {
	t.Parallel()
	_, _, originals := newBucketPair(t, map[string][]byte{
		"2026/01/a.jpg":                    []byte("aaa"),
		"2026/02/b.jpg":                    []byte("bb"),
		"db/kukatko-20260101T000000Z.dump": []byte("dump"),
		".tmp/upload-123":                  []byte("partial"),
	})

	got, err := originals.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []LocalOriginal{
		{Key: "2026/01/a.jpg", Size: 3},
		{Key: "2026/02/b.jpg", Size: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestBucketOriginals_List_error(t *testing.T) {
	t.Parallel()
	primary, _, originals := newBucketPair(t, nil)
	sentinel := errors.New("bucket unreachable")
	primary.listErr = sentinel

	if _, err := originals.List(t.Context()); !errors.Is(err, sentinel) {
		t.Errorf("List() error = %v, want %v", err, sentinel)
	}
}

func TestBucketOriginals_CopyTo_isServerSide(t *testing.T) {
	t.Parallel()
	_, backupStore, originals := newBucketPair(t, map[string][]byte{"2026/01/a.jpg": []byte("photo")})

	err := originals.CopyTo(t.Context(), backupStore, LocalOriginal{Key: "2026/01/a.jpg", Size: 5})
	if err != nil {
		t.Fatalf("CopyTo: %v", err)
	}

	wantCopies := []copyCall{{srcBucket: primaryBucket, srcKey: "2026/01/a.jpg", dstKey: "2026/01/a.jpg"}}
	if !reflect.DeepEqual(backupStore.copied, wantCopies) {
		t.Errorf("copied = %v, want %v", backupStore.copied, wantCopies)
	}
	// The payload must not have been streamed through this process, so no Put was
	// issued — the whole point of the bucket source.
	if len(backupStore.putSizes) != 0 {
		t.Errorf("CopyTo issued Put(s) %v; the copy must be server-side", backupStore.putSizes)
	}
	if got := string(backupStore.objects["2026/01/a.jpg"].data); got != "photo" {
		t.Errorf("backup object = %q, want %q", got, "photo")
	}
}

func TestBucketOriginals_CopyTo_error(t *testing.T) {
	t.Parallel()
	_, backupStore, originals := newBucketPair(t, map[string][]byte{"2026/01/a.jpg": []byte("photo")})
	sentinel := errors.New("access denied on source bucket")
	backupStore.copyErr = sentinel

	err := originals.CopyTo(t.Context(), backupStore, LocalOriginal{Key: "2026/01/a.jpg", Size: 5})
	if !errors.Is(err, sentinel) {
		t.Errorf("CopyTo() error = %v, want %v", err, sentinel)
	}
}

func TestService_SyncOriginals_copiesBucketToBucketAdditively(t *testing.T) {
	t.Parallel()
	primary, backupStore, originals := newBucketPair(t, map[string][]byte{
		"2026/01/a.jpg": []byte("aaa"),
		"2026/02/b.jpg": []byte("bb"),
	})
	svc := New(Config{Objects: backupStore, Originals: originals, Dumper: &fakeDumper{}})

	uploaded, skipped, err := svc.SyncOriginals(t.Context())
	if err != nil {
		t.Fatalf("SyncOriginals: %v", err)
	}
	if uploaded != 2 || skipped != 0 {
		t.Fatalf("first pass: uploaded=%d skipped=%d, want 2/0", uploaded, skipped)
	}

	// A second pass finds both objects already present at the same size and copies
	// nothing new.
	uploaded, skipped, err = svc.SyncOriginals(t.Context())
	if err != nil {
		t.Fatalf("second SyncOriginals: %v", err)
	}
	if uploaded != 0 || skipped != 2 {
		t.Fatalf("second pass: uploaded=%d skipped=%d, want 0/2", uploaded, skipped)
	}

	// An original deleted from the primary must survive in the backup: the sync is
	// additive and never mirrors a deletion.
	if err := primary.Remove(t.Context(), "2026/01/a.jpg"); err != nil {
		t.Fatalf("Remove from primary: %v", err)
	}
	uploaded, skipped, err = svc.SyncOriginals(t.Context())
	if err != nil {
		t.Fatalf("third SyncOriginals: %v", err)
	}
	if uploaded != 0 || skipped != 1 {
		t.Fatalf("third pass: uploaded=%d skipped=%d, want 0/1", uploaded, skipped)
	}
	if _, ok := backupStore.objects["2026/01/a.jpg"]; !ok {
		t.Error("original deleted from the primary was also removed from the backup bucket")
	}
}

func TestSkipKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{key: "2026/01/a.jpg", want: false},
		{key: "db/kukatko-20260101T000000Z.dump", want: true},
		{key: ".tmp/upload-123", want: true},
		{key: "dbx/not-a-dump.jpg", want: false},
		{key: ".tmpfile.jpg", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			if got := skipKey(tt.key); got != tt.want {
				t.Errorf("skipKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// compile-time assertion that the fake destination satisfies ObjectStore,
// including the server-side copy the bucket source relies on.
var _ ObjectStore = (*fakeStore)(nil)
