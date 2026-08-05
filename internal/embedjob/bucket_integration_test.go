//go:build integration

package embedjob_test

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` and additionally need an
// S3-compatible endpoint (MinIO is what CI and local development use, see
// `make dev-storage`). They exist because the bug they cover cannot happen on the
// filesystem backend the rest of this package's integration tests use: only a
// backend that publishes its objects skips encoding a size the bucket already
// holds, and only there does a preview exist with no local cache file.

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

// bucketHarness builds a harness whose originals backend is the real object store
// against the integration-test bucket, creating the bucket when absent and
// emptying it both now and after the test. The calling test is skipped when
// KUKATKO_TEST_S3_ENDPOINT is unset.
func bucketHarness(t *testing.T) *harness {
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
		// A media base URL and signing secret are what make URL() answer, and
		// URL() is how the thumbnailer recognises a backend whose thumbnails live
		// in the bucket. Nothing here fetches that URL.
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

	return newHarnessOver(t, store, filepath.Join(t.TempDir(), "cache"))
}

// bucketClient returns a raw minio client for the endpoint, used to set the
// bucket up and to read back what actually landed in it — deliberately not
// through the code under test.
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

// TestEmbed_previewOnlyInTheBucket is the production failure, reproduced end to
// end against a real object store: the thumbnail job publishes the preview, the
// local cache is then pruned (thumbnails outgrow the disk, so the production host
// prunes it every five minutes), and the image_embed job runs afterwards.
//
// Before the fix this failed deterministically — Generate correctly declined to
// re-encode a size the bucket already held, and the handler then went looking for
// a cache file that had never existed, so every image_embed job on R2 retried five
// times and dead-lettered with "thumb: thumbnail not cached".
func TestEmbed_previewOnlyInTheBucket(t *testing.T) {
	h := bucketHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "bucket-only")

	// The thumbnail job's work: every size encoded once and published.
	if _, err := h.thumbnailer.GenerateAll(ctx, photo); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if err := os.RemoveAll(h.cacheDir); err != nil {
		t.Fatalf("pruning the cache: %v", err)
	}
	// The state the failing jobs ran in: the size is available, but not locally.
	if _, err := h.thumbnailer.OpenCached(photo.FileHash, embedjob.DefaultPreviewSize); err == nil {
		t.Fatal("the cache was not actually pruned; the test would prove nothing")
	}

	client, calls := fakeSidecar(t, 0)
	if err := h.newService(client).Embed(ctx, photo.UID); err != nil {
		t.Fatalf("Embed with the preview only in the bucket: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("sidecar calls = %d, want 1", calls.Load())
	}
	if _, err := h.vectors.GetEmbedding(ctx, photo.UID); err != nil {
		t.Fatalf("GetEmbedding after Embed: %v", err)
	}

	// Nothing was re-encoded to make that work: the bytes came from the bucket.
	abs, err := h.thumbnailer.Path(photo.FileHash, embedjob.DefaultPreviewSize)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("the preview was re-encoded into %s; want it read from the bucket", abs)
	}
}

// TestEmbed_previewInNeitherPlace proves the fallback still holds on the object
// store: a photo whose preview was never generated is embedded anyway, the size
// being encoded and published on the way.
func TestEmbed_previewInNeitherPlace(t *testing.T) {
	h := bucketHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "never-thumbed")

	client, calls := fakeSidecar(t, 0)
	if err := h.newService(client).Embed(ctx, photo.UID); err != nil {
		t.Fatalf("Embed without any preview: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("sidecar calls = %d, want 1", calls.Load())
	}

	// The preview it had to encode is now in the bucket, readable and decodable.
	reader, err := h.thumbnailer.OpenOrGenerate(ctx, photo, embedjob.DefaultPreviewSize)
	if err != nil {
		t.Fatalf("OpenOrGenerate after Embed: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if _, _, err := image.Decode(reader); err != nil {
		t.Errorf("the published preview is not a decodable image: %v", err)
	}
}
