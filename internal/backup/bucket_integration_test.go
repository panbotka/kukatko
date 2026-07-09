//go:build integration

package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/panbotka/kukatko/internal/backup"
)

// These tests run only under `make test-integration` against the S3-compatible
// endpoint named by KUKATKO_TEST_S3_ENDPOINT (MinIO is what CI and local
// development use). They need two buckets — a primary holding the library and an
// independent backup bucket — and empty both between cases, so they intentionally
// do not run in parallel. With the variable unset they skip, keeping `make test`
// free of any object-storage dependency.

// Environment variables describing the integration-test buckets. Both are
// dedicated to the test suite and safe to empty.
const (
	envS3Endpoint  = "KUKATKO_TEST_S3_ENDPOINT"
	envS3Bucket    = "KUKATKO_TEST_S3_BUCKET"
	envS3Region    = "KUKATKO_TEST_S3_REGION"
	envS3AccessKey = "KUKATKO_TEST_S3_ACCESS_KEY"
	envS3SecretKey = "KUKATKO_TEST_S3_SECRET_KEY"
)

// bucketWipeTimeout bounds the between-test bucket wipe, which runs on its own
// context because the test's is already cancelled by then.
const bucketWipeTimeout = 30 * time.Second

// dumpPayload is the fixed archive the fake dumper streams, standing in for a
// real pg_dump.
var dumpPayload = []byte("PGDMP-fake-archive")

// staticDumper is a backup.Dumper that streams a fixed payload, so the
// orchestration can be exercised without a live database.
type staticDumper struct{}

// Dump returns a reader over the fixed dump payload.
func (staticDumper) Dump(context.Context) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(dumpPayload)), nil
}

// bucketFixture is the pair of live buckets one test case runs against, plus the
// service wired to copy the primary into the backup.
type bucketFixture struct {
	primary       backup.ObjectStore
	backupStore   backup.ObjectStore
	originals     *backup.BucketOriginals
	service       *backup.Service
	primaryBucket string
	backupBucket  string
}

// s3Env returns the endpoint and credentials of the integration-test S3 service,
// skipping the calling test when no endpoint is configured.
func s3Env(t *testing.T) (endpoint, region, accessKey, secretKey, baseBucket string) {
	t.Helper()
	endpoint = os.Getenv(envS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping integration test", envS3Endpoint)
	}
	baseBucket = os.Getenv(envS3Bucket)
	if baseBucket == "" {
		baseBucket = "kukatko-test"
	}
	return endpoint, os.Getenv(envS3Region), os.Getenv(envS3AccessKey), os.Getenv(envS3SecretKey), baseBucket
}

// newBucketFixture creates (if absent) and empties the primary and backup test
// buckets, and returns a backup service that copies originals from the former to
// the latter server-side. Both buckets are emptied again when the test ends.
//
// Both stores are built from the same credentials because MinIO serves both
// buckets: that is exactly the arrangement a server-side copy requires, and it
// leaves the two configurations independent of one another.
func newBucketFixture(t *testing.T, retention int) *bucketFixture {
	t.Helper()
	endpoint, region, accessKey, secretKey, base := s3Env(t)
	primaryBucket, backupBucket := base+"-primary", base+"-backup"

	opts := func(bucket string) backup.S3Options {
		return backup.S3Options{
			Endpoint:  endpoint,
			Region:    region,
			Bucket:    bucket,
			AccessKey: accessKey,
			SecretKey: secretKey,
			PathStyle: true,
		}
	}
	primary, err := backup.NewS3Store(opts(primaryBucket))
	if err != nil {
		t.Fatalf("primary store: %v", err)
	}
	backupStore, err := backup.NewS3Store(opts(backupBucket))
	if err != nil {
		t.Fatalf("backup store: %v", err)
	}

	admin := adminClient(t, endpoint, region, accessKey, secretKey)
	for _, bucket := range []string{primaryBucket, backupBucket} {
		ensureBucket(t, admin, bucket, region)
		emptyBucket(t, admin, bucket)
	}
	t.Cleanup(func() {
		for _, bucket := range []string{primaryBucket, backupBucket} {
			emptyBucket(t, admin, bucket)
		}
	})

	originals, err := backup.NewBucketOriginals(primary, primaryBucket)
	if err != nil {
		t.Fatalf("NewBucketOriginals: %v", err)
	}
	service := backup.New(backup.Config{
		Objects:   backupStore,
		Originals: originals,
		Dumper:    staticDumper{},
		Retention: retention,
	})
	return &bucketFixture{
		primary:       primary,
		backupStore:   backupStore,
		originals:     originals,
		service:       service,
		primaryBucket: primaryBucket,
		backupBucket:  backupBucket,
	}
}

// adminClient returns a raw minio client used only to create and empty the test
// buckets, which the ObjectStore interface deliberately cannot do.
func adminClient(t *testing.T, endpoint, region, accessKey, secretKey string) *minio.Client {
	t.Helper()
	host := endpoint
	secure := true
	if rest, ok := strings.CutPrefix(endpoint, "http://"); ok {
		host, secure = rest, false
	} else if rest, ok := strings.CutPrefix(endpoint, "https://"); ok {
		host = rest
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       secure,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	return client
}

// ensureBucket creates bucket when it does not exist yet.
func ensureBucket(t *testing.T, client *minio.Client, bucket, region string) {
	t.Helper()
	ctx := t.Context()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatalf("checking bucket %s: %v", bucket, err)
	}
	if exists {
		return
	}
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		t.Fatalf("creating bucket %s: %v", bucket, err)
	}
}

// emptyBucket removes every object from bucket. It runs on its own context rather
// than the test's, because it is also called from a t.Cleanup, by which time
// t.Context() has already been cancelled.
func emptyBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), bucketWipeTimeout)
	defer cancel()
	for info := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if info.Err != nil {
			t.Fatalf("listing %s: %v", bucket, info.Err)
		}
		if err := client.RemoveObject(ctx, bucket, info.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("removing %s/%s: %v", bucket, info.Key, err)
		}
	}
}

// seed writes an object into the primary bucket.
func seed(t *testing.T, store backup.ObjectStore, key, content string) {
	t.Helper()
	body := []byte(content)
	if err := store.Put(t.Context(), key, bytes.NewReader(body), int64(len(body)), ""); err != nil {
		t.Fatalf("seeding %s: %v", key, err)
	}
}

// readObject returns the bytes of the object at key, failing the test when absent.
func readObject(t *testing.T, store backup.ObjectStore, key string) []byte {
	t.Helper()
	reader, err := store.Open(t.Context(), key)
	if err != nil {
		t.Fatalf("opening %s: %v", key, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading %s: %v", key, err)
	}
	return data
}

// objectKeys returns every key in the store under prefix, for set assertions.
func objectKeys(t *testing.T, store backup.ObjectStore, prefix string) []string {
	t.Helper()
	objects, err := store.List(t.Context(), prefix)
	if err != nil {
		t.Fatalf("listing %q: %v", prefix, err)
	}
	keys := make([]string, 0, len(objects))
	for _, obj := range objects {
		keys = append(keys, obj.Key)
	}
	sort.Strings(keys)
	return keys
}

// TestBucketBackup_copiesPrimaryIntoBackupBucket runs a full backup against two
// live buckets and asserts the originals arrived byte-for-byte alongside the
// database dump, while the primary's own dumps and partial uploads were skipped.
func TestBucketBackup_copiesPrimaryIntoBackupBucket(t *testing.T) {
	fix := newBucketFixture(t, 0)
	seed(t, fix.primary, "2026/01/a.jpg", "photo-a")
	seed(t, fix.primary, "2026/02/b.jpg", "photo-b")
	// Neither of these is an original and neither may be copied.
	seed(t, fix.primary, ".tmp/upload-123", "partial")
	seed(t, fix.primary, "db/stray.dump", "not an original")

	res, err := fix.service.Run(t.Context(), time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.OriginalsUploaded != 2 || res.OriginalsSkipped != 0 {
		t.Errorf("Run() uploaded=%d skipped=%d, want 2/0", res.OriginalsUploaded, res.OriginalsSkipped)
	}

	if got := string(readObject(t, fix.backupStore, "2026/01/a.jpg")); got != "photo-a" {
		t.Errorf("copied 2026/01/a.jpg = %q, want photo-a", got)
	}
	if got := string(readObject(t, fix.backupStore, "2026/02/b.jpg")); got != "photo-b" {
		t.Errorf("copied 2026/02/b.jpg = %q, want photo-b", got)
	}

	// The database dump lands in the backup bucket alongside the originals.
	wantDump := "db/kukatko-20260701T020000Z.dump"
	if res.DumpKey != wantDump {
		t.Errorf("Run() dump key = %q, want %q", res.DumpKey, wantDump)
	}
	if got := readObject(t, fix.backupStore, wantDump); !bytes.Equal(got, dumpPayload) {
		t.Errorf("dump content = %q, want %q", got, dumpPayload)
	}

	// The primary's stray dump and partial upload were not treated as originals.
	wantKeys := []string{"2026/01/a.jpg", "2026/02/b.jpg", wantDump}
	if got := objectKeys(t, fix.backupStore, ""); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("backup bucket = %v, want %v", got, wantKeys)
	}
}

// TestBucketBackup_rerunCopiesNothingNew asserts the sync is incremental: a
// second pass over an unchanged primary copies nothing, and retention prunes the
// older dump without touching a single original.
func TestBucketBackup_rerunCopiesNothingNew(t *testing.T) {
	fix := newBucketFixture(t, 1)
	seed(t, fix.primary, "2026/01/a.jpg", "photo-a")
	seed(t, fix.primary, "2026/02/b.jpg", "photo-b")

	first, err := fix.service.Run(t.Context(), time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if first.OriginalsUploaded != 2 {
		t.Fatalf("first Run() uploaded=%d, want 2", first.OriginalsUploaded)
	}

	second, err := fix.service.Run(t.Context(), time.Date(2026, 7, 2, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.OriginalsUploaded != 0 || second.OriginalsSkipped != 2 {
		t.Errorf("second Run() uploaded=%d skipped=%d, want 0/2",
			second.OriginalsUploaded, second.OriginalsSkipped)
	}
	if second.DumpsPruned != 1 {
		t.Errorf("second Run() pruned %d dumps, want 1", second.DumpsPruned)
	}

	// Retention applies to dumps only: both originals survive, and only the newest
	// dump remains.
	wantKeys := []string{"2026/01/a.jpg", "2026/02/b.jpg", "db/kukatko-20260702T020000Z.dump"}
	if got := objectKeys(t, fix.backupStore, ""); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("backup bucket = %v, want %v", got, wantKeys)
	}
}

// TestBucketBackup_deletionDoesNotPropagate asserts the copy is additive: an
// original removed from the primary bucket stays in the backup bucket, which is
// the only protection there is against an accidental or malicious delete.
func TestBucketBackup_deletionDoesNotPropagate(t *testing.T) {
	fix := newBucketFixture(t, 0)
	seed(t, fix.primary, "2026/01/a.jpg", "photo-a")
	seed(t, fix.primary, "2026/02/b.jpg", "photo-b")

	if _, _, err := fix.service.SyncOriginals(t.Context()); err != nil {
		t.Fatalf("first SyncOriginals: %v", err)
	}
	if err := fix.primary.Remove(t.Context(), "2026/01/a.jpg"); err != nil {
		t.Fatalf("removing from primary: %v", err)
	}

	uploaded, skipped, err := fix.service.SyncOriginals(t.Context())
	if err != nil {
		t.Fatalf("second SyncOriginals: %v", err)
	}
	if uploaded != 0 || skipped != 1 {
		t.Errorf("SyncOriginals() after delete: uploaded=%d skipped=%d, want 0/1", uploaded, skipped)
	}

	if _, ok, err := fix.backupStore.Stat(t.Context(), "2026/01/a.jpg"); err != nil || !ok {
		t.Errorf("deleted original: Stat in backup bucket ok=%v err=%v, want present", ok, err)
	}
	if got := string(readObject(t, fix.backupStore, "2026/01/a.jpg")); got != "photo-a" {
		t.Errorf("surviving backup copy = %q, want photo-a", got)
	}
}

// TestBucketBackup_unconfiguredTargetFailsLoudly asserts that a missing backup
// target or a missing primary bucket is an error, never a backup that quietly
// copies nothing and reports success.
func TestBucketBackup_unconfiguredTargetFailsLoudly(t *testing.T) {
	endpoint, region, accessKey, secretKey, _ := s3Env(t)
	base := backup.S3Options{
		Endpoint:  endpoint,
		Region:    region,
		AccessKey: accessKey,
		SecretKey: secretKey,
		PathStyle: true,
	}

	if _, err := backup.NewS3Store(base); !errors.Is(err, backup.ErrNotConfigured) {
		t.Errorf("NewS3Store() with no bucket error = %v, want ErrNotConfigured", err)
	}
	noEndpoint := base
	noEndpoint.Endpoint, noEndpoint.Bucket = "", "kukatko-backups"
	if _, err := backup.NewS3Store(noEndpoint); !errors.Is(err, backup.ErrNotConfigured) {
		t.Errorf("NewS3Store() with no endpoint error = %v, want ErrNotConfigured", err)
	}

	// A configured destination with an unconfigured primary is just as loud: an
	// empty source bucket must never read as an empty library.
	withBucket := base
	withBucket.Bucket = "kukatko-backups"
	store, err := backup.NewS3Store(withBucket)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	if _, err := backup.NewBucketOriginals(store, ""); !errors.Is(err, backup.ErrNoSourceBucket) {
		t.Errorf("NewBucketOriginals() with no source bucket error = %v, want ErrNoSourceBucket", err)
	}
	if _, err := backup.NewBucketOriginals(nil, "kukatko-originals"); !errors.Is(err, backup.ErrNoSourceStore) {
		t.Errorf("NewBucketOriginals() with no source store error = %v, want ErrNoSourceStore", err)
	}
}
