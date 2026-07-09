//go:build integration

package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// These tests run only under `make test-integration` against the S3-compatible
// endpoint named by KUKATKO_TEST_S3_ENDPOINT (MinIO is what CI and local
// development use). They share one bucket, which is emptied between cases, so
// they intentionally do not run in parallel. With the variable unset they skip,
// keeping `make test` free of any object-storage dependency.

// Environment variables describing the integration-test bucket. The bucket is
// dedicated to the test suite and safe to empty.
const (
	envTestS3Endpoint  = "KUKATKO_TEST_S3_ENDPOINT"
	envTestS3Bucket    = "KUKATKO_TEST_S3_BUCKET"
	envTestS3Region    = "KUKATKO_TEST_S3_REGION"
	envTestS3AccessKey = "KUKATKO_TEST_S3_ACCESS_KEY"
	envTestS3SecretKey = "KUKATKO_TEST_S3_SECRET_KEY"
)

// bucketCleanupTimeout bounds the between-test bucket wipe, which runs on its own
// context because the test's is already cancelled by then.
const bucketCleanupTimeout = 30 * time.Second

// takenAt is the capture timestamp every stored fixture carries, so its object
// key lands in a predictable YYYY/MM prefix.
var takenAt = time.Date(2024, time.May, 3, 9, 30, 0, 0, time.UTC)

// jpegBytes returns a byte slice that sniffs as image/jpeg, padded with filler so
// that different fixtures have different content and different digests.
func jpegBytes(filler string) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte(filler)...)
}

// newTestR2 returns an R2 backend against the integration-test bucket, together
// with the local temp directory it stages uploads and downloads through. The
// bucket is created if absent and emptied both now and after the test, so each
// case starts from a clean slate. The calling test is skipped when
// KUKATKO_TEST_S3_ENDPOINT is unset.
func newTestR2(t *testing.T) (*R2, string) {
	t.Helper()

	endpoint := os.Getenv(envTestS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping integration test", envTestS3Endpoint)
	}
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	opts := R2Options{
		Endpoint:         endpoint,
		Region:           os.Getenv(envTestS3Region),
		Bucket:           bucket,
		AccessKey:        os.Getenv(envTestS3AccessKey),
		SecretKey:        os.Getenv(envTestS3SecretKey),
		MediaBaseURL:     testBaseURL,
		URLSigningSecret: testSecret,
		URLTTL:           time.Hour,
		TempPath:         t.TempDir(),
	}
	store, err := NewR2(opts)
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}
	ensureBucket(t, opts)
	emptyBucket(t, store)
	t.Cleanup(func() { emptyBucket(t, store) })
	return store, opts.TempPath
}

// ensureBucket creates the test bucket when it does not exist yet.
func ensureBucket(t *testing.T, opts R2Options) {
	t.Helper()
	host, secure, err := parseR2Endpoint(opts.Endpoint)
	if err != nil {
		t.Fatalf("parsing %s: %v", envTestS3Endpoint, err)
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: secure,
		Region: opts.Region,
	})
	if err != nil {
		t.Fatalf("initialising test client: %v", err)
	}
	ctx := t.Context()
	exists, err := client.BucketExists(ctx, opts.Bucket)
	if err != nil {
		t.Fatalf("checking bucket %s: %v", opts.Bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, opts.Bucket, minio.MakeBucketOptions{Region: opts.Region}); err != nil {
			t.Fatalf("creating bucket %s: %v", opts.Bucket, err)
		}
	}
}

// emptyBucket removes every object from the test bucket. It runs on its own
// context rather than the test's, because it is also called from a t.Cleanup, by
// which time t.Context() has already been cancelled.
func emptyBucket(t *testing.T, store *R2) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), bucketCleanupTimeout)
	defer cancel()
	for info := range store.client.ListObjects(ctx, store.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if info.Err != nil {
			t.Fatalf("listing bucket: %v", info.Err)
		}
		if err := store.client.RemoveObject(ctx, store.bucket, info.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("removing %s: %v", info.Key, err)
		}
	}
}

// storeFixture stores content under name and fails the test on any error other
// than the expected duplicate signal.
func storeFixture(t *testing.T, store *R2, name string, content []byte) StoredFile {
	t.Helper()
	stored, err := store.Store(t.Context(), bytes.NewReader(content), takenAt, name)
	if err != nil {
		t.Fatalf("Store(%s): %v", name, err)
	}
	return stored
}

// assertTempDirEmpty fails the test when the staging directory still holds a
// file: a leaked temp file fills the small disk this backend exists to avoid.
func assertTempDirEmpty(t *testing.T, tempDir string) {
	t.Helper()
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("reading temp dir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("temp dir %s leaked %d file(s): %s", tempDir, len(names), strings.Join(names, ", "))
	}
}

func TestR2StoreOpenStatDelete(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()
	content := jpegBytes("store-open-stat-delete")

	stored := storeFixture(t, store, "IMG_0001.jpg", content)
	if got, want := stored.RelPath, "2024/05/IMG_0001.jpg"; got != want {
		t.Errorf("RelPath = %q, want %q", got, want)
	}
	if got, want := stored.Hash, hashOf(content); got != want {
		t.Errorf("Hash = %q, want %q", got, want)
	}
	if got, want := stored.Size, int64(len(content)); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
	if got, want := stored.MIME, "image/jpeg"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}
	assertTempDirEmpty(t, tempDir)

	reader, err := store.Open(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	roundTripped, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing object: %v", err)
	}
	if !bytes.Equal(roundTripped, content) {
		t.Errorf("Open returned %d bytes, want the %d stored", len(roundTripped), len(content))
	}

	info, err := store.Stat(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(content)) || info.Name() != "IMG_0001.jpg" || info.IsDir() {
		t.Errorf("Stat = (%q, %d, dir=%t), want (IMG_0001.jpg, %d, dir=false)",
			info.Name(), info.Size(), info.IsDir(), len(content))
	}

	if err := store.Delete(ctx, stored.RelPath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(ctx, stored.RelPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat after Delete = %v, want os.ErrNotExist", err)
	}
	if _, err := store.Open(ctx, stored.RelPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open after Delete = %v, want os.ErrNotExist", err)
	}
	if err := store.Delete(ctx, stored.RelPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Delete of a missing object = %v, want os.ErrNotExist", err)
	}
}

func TestR2StoreDeduplicatesIdenticalContent(t *testing.T) {
	store, tempDir := newTestR2(t)
	content := jpegBytes("identical")

	first := storeFixture(t, store, "IMG_0002.jpg", content)

	second, err := store.Store(t.Context(), bytes.NewReader(content), takenAt, "IMG_0002.jpg")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Store(duplicate) = %v, want ErrAlreadyExists", err)
	}
	if second.RelPath != first.RelPath || second.Hash != first.Hash || second.Size != first.Size {
		t.Errorf("duplicate StoredFile = %+v, want %+v", second, first)
	}
	assertTempDirEmpty(t, tempDir)
}

func TestR2StoreSuffixesCollidingContent(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()

	first := storeFixture(t, store, "IMG_0003.jpg", jpegBytes("first"))
	second := storeFixture(t, store, "IMG_0003.jpg", jpegBytes("second"))

	if got, want := first.RelPath, "2024/05/IMG_0003.jpg"; got != want {
		t.Errorf("first RelPath = %q, want %q", got, want)
	}
	if got, want := second.RelPath, "2024/05/IMG_0003_1.jpg"; got != want {
		t.Errorf("second RelPath = %q, want %q — a colliding upload must never overwrite", got, want)
	}
	// The original object must still hold its own bytes.
	reader, err := store.Open(ctx, first.RelPath)
	if err != nil {
		t.Fatalf("Open(first): %v", err)
	}
	defer reader.Close()
	roundTripped, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading first object: %v", err)
	}
	if !bytes.Equal(roundTripped, jpegBytes("first")) {
		t.Error("the first object was overwritten by the colliding upload")
	}
	assertTempDirEmpty(t, tempDir)
}

func TestR2HandlesUTF8FilenameKey(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()
	const name = "Šťastné Vánoce 🎄.jpg"
	content := jpegBytes("utf-8")

	stored := storeFixture(t, store, name, content)
	if got, want := stored.RelPath, "2024/05/"+name; got != want {
		t.Fatalf("RelPath = %q, want %q", got, want)
	}

	reader, err := store.Open(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("Open(utf-8 key): %v", err)
	}
	roundTripped, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing object: %v", err)
	}
	if !bytes.Equal(roundTripped, content) {
		t.Error("Open(utf-8 key) returned different bytes than were stored")
	}
	if _, err := store.Stat(ctx, stored.RelPath); err != nil {
		t.Errorf("Stat(utf-8 key): %v", err)
	}

	local, cleanup, err := store.Materialize(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("Materialize(utf-8 key): %v", err)
	}
	defer cleanup()
	materialized, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("reading materialized file: %v", err)
	}
	if !bytes.Equal(materialized, content) {
		t.Error("Materialize(utf-8 key) wrote different bytes than were stored")
	}
	if err := store.Delete(ctx, stored.RelPath); err != nil {
		t.Errorf("Delete(utf-8 key): %v", err)
	}
	cleanup()
	assertTempDirEmpty(t, tempDir)
}

func TestR2MaterializeAndCleanup(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()
	content := jpegBytes("materialize")
	stored := storeFixture(t, store, "IMG_0004.jpg", content)

	local, cleanup, err := store.Materialize(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if filepath.Dir(local) != tempDir {
		t.Errorf("Materialize wrote to %s, want a file under the configured temp path %s", local, tempDir)
	}
	if got, want := filepath.Ext(local), ".jpg"; got != want {
		t.Errorf("materialized extension = %q, want %q (imgconvert dispatches on it)", got, want)
	}
	materialized, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("reading materialized file: %v", err)
	}
	if !bytes.Equal(materialized, content) {
		t.Error("Materialize wrote different bytes than were stored")
	}

	cleanup()
	if _, err := os.Stat(local); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cleanup left %s behind: %v", local, err)
	}
	// The cleanup is idempotent: a caller may defer it and still call it early.
	cleanup()
	assertTempDirEmpty(t, tempDir)
}

func TestR2MaterializeMissingObjectLeavesNoTempFile(t *testing.T) {
	store, tempDir := newTestR2(t)

	local, cleanup, err := store.Materialize(t.Context(), "2024/05/absent.jpg")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Materialize(missing) = %v, want os.ErrNotExist", err)
	}
	if local != "" {
		t.Errorf("Materialize(missing) returned path %q, want empty", local)
	}
	if cleanup == nil {
		t.Fatal("Materialize(missing) returned a nil cleanup; the contract promises a no-op")
	}
	cleanup()
	// The partially created download must be gone: on a small disk a leaked temp
	// file per failed materialize is a real failure, not a nuisance.
	assertTempDirEmpty(t, tempDir)
}

func TestR2PutHeadRoundTripAtACallerChosenKey(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()
	content := jpegBytes("put-head")
	// A thumbnail cache path: a key no Store call could ever derive.
	const key = "thumb/ab/cd/ef/abcdef_tile_500.jpg"
	want := StoredFile{Hash: hashOf(content), RelPath: key, Size: int64(len(content)), MIME: "image/jpeg"}

	if err := store.Put(ctx, bytes.NewReader(content), want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	head, err := store.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash != want.Hash || head.Size != want.Size || head.MIME != want.MIME {
		t.Errorf("Head = %+v, want %+v", head, want)
	}

	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	roundTripped, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}
	if !bytes.Equal(roundTripped, content) {
		t.Error("Put stored different bytes than it was given")
	}
	// Nothing is staged: Put streams straight into the bucket.
	assertTempDirEmpty(t, tempDir)
}

func TestR2PutOverwritesAndHeadOfAMissingObjectIsNotExist(t *testing.T) {
	store, _ := newTestR2(t)
	ctx := t.Context()
	const key = "2024/05/overwrite.jpg"

	if _, err := store.Head(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Head(missing) = %v, want os.ErrNotExist", err)
	}

	// An interrupted run leaves a half-written object; the resumed run must be able
	// to replace it at the same key.
	first := jpegBytes("first-attempt")
	second := jpegBytes("second attempt, different length")
	for _, content := range [][]byte{first, second} {
		file := StoredFile{
			Hash: hashOf(content), RelPath: key, Size: int64(len(content)), MIME: "image/jpeg",
		}
		if err := store.Put(ctx, bytes.NewReader(content), file); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	head, err := store.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash != hashOf(second) || head.Size != int64(len(second)) {
		t.Errorf("Head = %+v, want the second content", head)
	}
}

func TestR2PutRemovesTheObjectWhenTheContentDoesNotMatchItsDigest(t *testing.T) {
	store, tempDir := newTestR2(t)
	ctx := t.Context()
	const key = "2024/05/lying.jpg"
	actual := jpegBytes("what the disk holds")
	declared := StoredFile{
		Hash:    hashOf(jpegBytes("what the catalogue believes")),
		RelPath: key,
		Size:    int64(len(actual)),
		MIME:    "image/jpeg",
	}

	err := store.Put(ctx, bytes.NewReader(actual), declared)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("Put(wrong digest) = %v, want ErrHashMismatch", err)
	}
	// The object carried metadata that lied about its own bytes; it must be gone,
	// or a later reader — or a migration deciding it may delete the local original
	// — would trust it.
	if _, err := store.Head(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Head after a rejected Put = %v, want os.ErrNotExist", err)
	}
	assertTempDirEmpty(t, tempDir)
}

func TestR2PutRejectsATruncatedStream(t *testing.T) {
	store, _ := newTestR2(t)
	content := jpegBytes("shorter than declared")
	declared := StoredFile{
		Hash:    hashOf(content),
		RelPath: "2024/05/truncated.jpg",
		Size:    int64(len(content)) + 100,
		MIME:    "image/jpeg",
	}

	if err := store.Put(t.Context(), bytes.NewReader(content), declared); err == nil {
		t.Fatal("Put(short stream) = nil, want an error")
	}
	if _, err := store.Head(t.Context(), declared.RelPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a short Put left an object behind: %v", err)
	}
}

func TestR2Check(t *testing.T) {
	store, _ := newTestR2(t)

	if err := store.Check(t.Context()); err != nil {
		t.Errorf("Check on the test bucket: %v", err)
	}

	absent := *store
	absent.bucket = "kukatko-bucket-that-does-not-exist"
	err := absent.Check(t.Context())
	if !errors.Is(err, ErrBucketNotFound) {
		t.Errorf("Check(missing bucket) = %v, want ErrBucketNotFound", err)
	}
	if !IsSystemic(err) {
		t.Error("a missing bucket must be systemic: no retry can create it")
	}
}

func TestR2CheckRejectsBadCredentialsSystemically(t *testing.T) {
	endpoint := os.Getenv(envTestS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping integration test", envTestS3Endpoint)
	}
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	store, err := NewR2(R2Options{
		Endpoint:  endpoint,
		Region:    os.Getenv(envTestS3Region),
		Bucket:    bucket,
		AccessKey: "wrong-access-key",
		SecretKey: "wrong-secret-key",
		TempPath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}

	// Credentials that cannot open the bucket must stop a migration in its first
	// second, not on its hundred-thousandth upload.
	checkErr := store.Check(t.Context())
	if checkErr == nil {
		t.Fatal("Check with bad credentials = nil, want an error")
	}
	if !IsSystemic(checkErr) {
		t.Errorf("IsSystemic(%v) = false, want true", checkErr)
	}
}

func TestR2StoreRecordsContentHashMetadata(t *testing.T) {
	store, _ := newTestR2(t)
	content := jpegBytes("metadata")
	stored := storeFixture(t, store, "IMG_0005.jpg", content)

	info, err := store.client.StatObject(t.Context(), store.bucket, stored.RelPath, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	if got, want := objectHash(info), stored.Hash; got != want {
		t.Errorf("object %s metadata hash = %q, want %q — Store's duplicate detection depends on it",
			stored.RelPath, got, want)
	}
	if got, want := info.ContentType, "image/jpeg"; got != want {
		t.Errorf("ContentType = %q, want %q", got, want)
	}
}
