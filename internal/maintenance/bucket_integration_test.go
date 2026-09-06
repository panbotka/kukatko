//go:build integration

package maintenance_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` and additionally need an
// S3-compatible endpoint (MinIO is what CI and local development use, see
// `make dev-storage`). They exist because the defect they cover cannot happen on
// the filesystem backend the rest of this package's integration tests use: with
// the originals in a bucket, the local originals root is empty, and a scan that
// inventories the root reports a full library as nought files and calls it
// consistent.

// Environment variables describing the integration-test bucket, matching the
// names internal/storage's own integration suite uses. The bucket is dedicated to
// the test suite and safe to empty.
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

// bucketHarness builds a harness whose originals backend is the real object
// store against the integration-test bucket, creating the bucket when absent and
// emptying it both now and after the test. The calling test is skipped when
// KUKATKO_TEST_S3_ENDPOINT is unset.
func bucketHarness(t *testing.T) (*harness, *minio.Client, string) {
	t.Helper()

	endpoint := os.Getenv(envTestS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping object-store integration test", envTestS3Endpoint)
	}
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	store, err := storage.NewR2(storage.R2Options{
		Endpoint:  endpoint,
		Region:    os.Getenv(envTestS3Region),
		Bucket:    bucket,
		AccessKey: os.Getenv(envTestS3AccessKey),
		SecretKey: os.Getenv(envTestS3SecretKey),
		// A media base URL and signing secret are what make URL() answer, which
		// is how the thumbnailer recognises a backend whose thumbnails live in
		// the bucket. Nothing here fetches that URL.
		MediaBaseURL:     "https://media.example.test",
		URLSigningSecret: "integration signing secret",
		URLTTL:           time.Hour,
		TempPath:         t.TempDir(),
	})
	if err != nil {
		t.Fatalf("storage.NewR2: %v", err)
	}
	client := bucketClient(t, endpoint)
	ensureBucket(t, client, bucket)
	emptyBucket(t, client, bucket)
	t.Cleanup(func() { emptyBucket(t, client, bucket) })

	return newHarnessOver(t, store, maintenance.StoreObject), client, bucket
}

// bucketClient returns a raw minio client for the endpoint, used to set the
// bucket up and to put objects into it — deliberately not through the code under
// test.
func bucketClient(t *testing.T, endpoint string) *minio.Client {
	t.Helper()

	host := endpoint
	secure := true
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		host, secure = strings.TrimPrefix(endpoint, "http://"), false
	case strings.HasPrefix(endpoint, "https://"):
		host = strings.TrimPrefix(endpoint, "https://")
	}
	client, err := minio.New(host, &minio.Options{
		Creds: credentials.NewStaticV4(
			os.Getenv(envTestS3AccessKey), os.Getenv(envTestS3SecretKey), ""),
		Secure: secure,
		Region: os.Getenv(envTestS3Region),
	})
	if err != nil {
		t.Fatalf("initialising test client: %v", err)
	}
	return client
}

// ensureBucket creates the test bucket when it does not exist yet.
func ensureBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx := t.Context()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatalf("checking bucket %s: %v", bucket, err)
	}
	if exists {
		return
	}
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: os.Getenv(envTestS3Region)}); err != nil {
		t.Fatalf("creating bucket %s: %v", bucket, err)
	}
}

// emptyBucket removes every object from the test bucket. It runs on its own
// context rather than the test's, because it is also called from a t.Cleanup, by
// which time t.Context() has already been cancelled.
func emptyBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), bucketCleanupTimeout)
	defer cancel()
	for info := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if info.Err != nil {
			t.Fatalf("listing bucket: %v", info.Err)
		}
		if err := client.RemoveObject(ctx, bucket, info.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("removing %s: %v", info.Key, err)
		}
	}
}

// putObject writes one object into the test bucket directly, bypassing the
// storage layer, so the scan meets keys nothing in Kukátko put there.
func putObject(t *testing.T, client *minio.Client, bucket, key, body string) {
	t.Helper()
	if _, err := client.PutObject(t.Context(), bucket, key,
		strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatalf("putting %s: %v", key, err)
	}
}

// TestScanInventoriesTheBucket verifies the scan on an object-store instance
// counts the bucket's originals rather than the empty local root, says so in the
// report, and finds the orphan that only a real listing can see. The sidecar, the
// published thumbnail and the foreign object share the bucket and must not be
// counted as originals — an orphan is a file the repair offers to ingest.
func TestScanInventoriesTheBucket(t *testing.T) {
	h, client, bucket := bucketHarness(t)
	ctx := context.Background()

	h.storeRealPhoto(t, "one", 0x10)
	h.storeRealPhoto(t, "two", 0x20)
	putObject(t, client, bucket, "2023/06/orphan.jpg", "not catalogued")
	putObject(t, client, bucket, "sidecars/2023/06/one.jpg.yaml", "version: 1\n")
	putObject(t, client, bucket, "thumb/ab/cd/ef/abcdef_tile_224.jpg", "thumbnail bytes")
	putObject(t, client, bucket, "exports/somebody-elses.pdf", "foreign")

	report, err := h.svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.Store.Kind != maintenance.StoreObject || !report.Store.Listed() {
		t.Fatalf("store inventory = %+v, want a listed object-store inventory", report.Store)
	}
	if report.Store.Originals != 3 {
		t.Errorf("originals in the store = %d, want 3 (two catalogued plus the orphan)",
			report.Store.Originals)
	}
	assertFinding(t, "orphan files", report.OrphanFiles, 1, "2023/06/orphan.jpg")
	if report.MissingOriginals.Count != 0 {
		t.Errorf("missing originals = %d, want 0 (both originals are in the bucket)",
			report.MissingOriginals.Count)
	}
	if report.Clean() {
		t.Error("a library with an orphan must not scan clean")
	}
}

// TestScanEmptyBucketIsListedNotUnknown verifies an empty bucket reports zero
// originals as a listing that ran: with no catalogue rows either, that library
// really is consistent, and the report may say so.
func TestScanEmptyBucketIsListedNotUnknown(t *testing.T) {
	h, _, _ := bucketHarness(t)

	report, err := h.svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !report.Store.Listed() || report.Store.Originals != 0 {
		t.Errorf("store inventory = %+v, want a listed inventory of 0", report.Store)
	}
	if !report.Clean() {
		t.Error("an empty library over an empty bucket should scan clean")
	}
}

// TestScanUnreachableBucketReportsTheFailure verifies a bucket that cannot be
// listed is reported as a failure rather than as zero originals, and that such a
// report is never clean. This is the whole point of carrying the error: an
// unreadable store must not pass for an empty one.
func TestScanUnreachableBucketReportsTheFailure(t *testing.T) {
	if os.Getenv(envTestS3Endpoint) == "" {
		t.Skipf("%s not set; skipping object-store integration test", envTestS3Endpoint)
	}
	// A bucket that does not exist stands in for any listing the backend cannot
	// answer; nothing else about the wiring changes.
	store, err := storage.NewR2(storage.R2Options{
		Endpoint:         os.Getenv(envTestS3Endpoint),
		Region:           os.Getenv(envTestS3Region),
		Bucket:           "kukatko-test-no-such-bucket",
		AccessKey:        os.Getenv(envTestS3AccessKey),
		SecretKey:        os.Getenv(envTestS3SecretKey),
		MediaBaseURL:     "https://media.example.test",
		URLSigningSecret: "integration signing secret",
		URLTTL:           time.Hour,
		TempPath:         t.TempDir(),
	})
	if err != nil {
		t.Fatalf("storage.NewR2: %v", err)
	}
	h := newHarnessOver(t, store, maintenance.StoreObject)

	report, err := h.svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if report.Store.Listed() {
		t.Fatalf("store inventory = %+v, want the listing failure recorded", report.Store)
	}
	if report.Clean() {
		t.Error("a scan whose bucket could not be listed must not report itself clean")
	}
}
