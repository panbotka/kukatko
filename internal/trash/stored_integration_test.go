//go:build integration

package trash_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/trash"
)

// These tests cover the derived objects a purge removes from the store itself —
// published thumbnails and streaming segments, under the layout the photo's row
// names and, for a photo a storage move committed, under the root it left them
// at — over the filesystem store and, when KUKATKO_TEST_S3_ENDPOINT names an
// S3-compatible endpoint (MinIO, see `make dev-storage`), over the R2 backend.

// Environment variables describing the integration-test bucket, the names
// internal/storage's own integration suite uses. The bucket is dedicated to the
// test suite and safe to empty.
const (
	envTestS3Endpoint  = "KUKATKO_TEST_S3_ENDPOINT"
	envTestS3Bucket    = "KUKATKO_TEST_S3_BUCKET"
	envTestS3Region    = "KUKATKO_TEST_S3_REGION"
	envTestS3AccessKey = "KUKATKO_TEST_S3_ACCESS_KEY"
	envTestS3SecretKey = "KUKATKO_TEST_S3_SECRET_KEY"
)

// bucketCleanupTimeout bounds the bucket wipe, which runs on its own context
// because the test's is already cancelled by the time a cleanup runs.
const bucketCleanupTimeout = 30 * time.Second

// storedObject is one object a test puts into the store for a photo.
type storedObject struct {
	key  string
	data []byte
}

// derivedEnv is a purge service over one store and the integration database.
type derivedEnv struct {
	db     *database.DB
	store  storage.Storage
	photos *photos.Store
	svc    *trash.Service
}

// newDerivedEnv truncates the database and wires the purge over store, with a
// real thumbnailer over the same store and the storage move's photo lock.
func newDerivedEnv(t *testing.T, store storage.Storage) *derivedEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	photoStore := photos.NewStore(db.Pool())
	svc := trash.New(trash.Config{
		Photos: photoStore, Projects: project.NewStore(db.Pool()), Storage: store,
		Thumbnailer: thumb.New(store, t.TempDir()), Moves: rekey.NewGate(db.Pool()),
	})
	return &derivedEnv{db: db, store: store, photos: photoStore, svc: svc}
}

// put stores data at key through the store's verified Put.
func (e *derivedEnv) put(t *testing.T, key string, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	file := storage.StoredFile{
		Hash: hex.EncodeToString(sum[:]), RelPath: key, Size: int64(len(data)), MIME: "application/octet-stream",
	}
	if err := e.store.Put(t.Context(), bytes.NewReader(data), file); err != nil {
		t.Fatalf("putting %s: %v", key, err)
	}
}

// exists reports whether the store holds key.
func (e *derivedEnv) exists(t *testing.T, key string) bool {
	t.Helper()
	_, err := e.store.Stat(t.Context(), key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("statting %s: %v", key, err)
	}
	return err == nil
}

// seed catalogues an archived photo of the bootstrap project whose original is
// stored at filePath, stores the original, and returns its content hash.
func (e *derivedEnv) seed(t *testing.T, uid, filePath string, archived bool) string {
	t.Helper()
	content := []byte("original bytes of " + uid)
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	e.put(t, filePath, content)
	photo, err := e.photos.Create(t.Context(), photos.Photo{
		UID: uid, ProjectID: project.BootstrapUID, FileHash: hash, FilePath: filePath,
		FileName: uid + ".mp4", FileSize: int64(len(content)), FileMime: "video/mp4", TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("photos.Create: %v", err)
	}
	if _, err := e.photos.CreateFile(t.Context(), project.BootstrapUID, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: filePath, FileHash: hash, FileSize: int64(len(content)),
		FileMime: "video/mp4", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("photos.CreateFile: %v", err)
	}
	if archived {
		if _, err := e.db.Pool().Exec(t.Context(),
			`UPDATE photos SET archived_at = now() - interval '2 days' WHERE uid = $1`, photo.UID); err != nil {
			t.Fatalf("archiving %s: %v", photo.UID, err)
		}
	}
	return hash
}

// derived stores two thumbnail sizes and a streaming rendition of hash under
// prefix, returning their keys.
func (e *derivedEnv) derived(t *testing.T, prefix, hash string) []string {
	t.Helper()
	var keys []string
	for _, size := range thumb.SizeNames()[:2] {
		key, err := thumb.RelPath(prefix, hash, size)
		if err != nil {
			t.Fatalf("thumb.RelPath: %v", err)
		}
		keys = append(keys, key)
	}
	for _, name := range []string{hls.InitName, "00000.m4s"} {
		key, err := hls.Key(prefix, hash, hls.Rendition1080p, name)
		if err != nil {
			t.Fatalf("hls.Key: %v", err)
		}
		keys = append(keys, key)
	}
	for _, key := range keys {
		e.put(t, key, []byte("derived "+key))
	}
	return keys
}

// setPrefix sets a project's storage prefix, as a storage move's Start does.
func (e *derivedEnv) setPrefix(t *testing.T, projectUID, prefix string) {
	t.Helper()
	if _, err := e.db.Pool().Exec(t.Context(),
		`UPDATE projects SET storage_prefix = $2 WHERE uid = $1`, projectUID, prefix); err != nil {
		t.Fatalf("setting the storage prefix of %s: %v", projectUID, err)
	}
}

// purge purges the bootstrap project's photo uid.
func (e *derivedEnv) purge(t *testing.T, uid string) {
	t.Helper()
	if err := e.svc.PurgePhoto(t.Context(), project.BootstrapUID, uid, audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto(%s): %v", uid, err)
	}
}

// assertGone fails the test for every key the store still holds.
func (e *derivedEnv) assertGone(t *testing.T, what string, keys []string) {
	t.Helper()
	for _, key := range keys {
		if e.exists(t, key) {
			t.Errorf("%s: %s survives the purge", what, key)
		}
	}
}

// assertKept fails the test for every key the store no longer holds.
func (e *derivedEnv) assertKept(t *testing.T, what string, keys []string) {
	t.Helper()
	for _, key := range keys {
		if !e.exists(t, key) {
			t.Errorf("%s: %s was deleted", what, key)
		}
	}
}

// testStoredDerivedPurge runs the derived-object scenarios over store.
//
// A photo at the root loses its published thumbnails and segments. A photo a
// storage move committed under the bootstrap project's prefix, with derived
// objects left at the root by a failed copy, loses them under both layouts. A
// live photo's objects stay. With another project still in the root layout,
// the moved photo's root keys are left for it.
func testStoredDerivedPurge(t *testing.T, newStore func(t *testing.T) storage.Storage) {
	t.Run("root photo and moved photo", func(t *testing.T) {
		env := newDerivedEnv(t, newStore(t))
		prefix := project.StoragePrefixFor(project.BootstrapUID)
		env.setPrefix(t, project.BootstrapUID, prefix)

		rootHash := env.seed(t, "ph_atroot", "2023/01/atroot.mp4", true)
		rootDerived := env.derived(t, "", rootHash)
		movedHash := env.seed(t, "ph_moved", prefix+"2023/02/moved.mp4", true)
		movedDerived := append(env.derived(t, prefix, movedHash), env.derived(t, "", movedHash)...)
		liveHash := env.seed(t, "ph_live", prefix+"2023/03/live.mp4", false)
		liveDerived := env.derived(t, prefix, liveHash)

		env.purge(t, "ph_atroot")
		env.purge(t, "ph_moved")
		env.assertGone(t, "a root photo's derived object", rootDerived)
		env.assertGone(t, "a moved photo's derived object", movedDerived)
		env.assertGone(t, "an original", []string{"2023/01/atroot.mp4", prefix + "2023/02/moved.mp4"})
		env.assertKept(t, "a live photo's derived object", liveDerived)
	})

	t.Run("root still in use", func(t *testing.T) {
		env := newDerivedEnv(t, newStore(t))
		prefix := project.StoragePrefixFor(project.BootstrapUID)
		// The bootstrap project keeps the root layout; a photo of it under a
		// prefix stands for a moved one while the root is not the move's alone.
		movedHash := env.seed(t, "ph_moved", prefix+"2023/02/moved.mp4", true)
		prefixed := env.derived(t, prefix, movedHash)
		root := env.derived(t, "", movedHash)

		env.purge(t, "ph_moved")
		env.assertGone(t, "a moved photo's prefixed object", prefixed)
		env.assertKept(t, "a root object another project may own", root)
	})
}

// TestPurgePhoto_storedDerivedObjectsFS runs the scenarios over the filesystem
// store.
func TestPurgePhoto_storedDerivedObjectsFS(t *testing.T) {
	testStoredDerivedPurge(t, func(t *testing.T) storage.Storage {
		t.Helper()
		fs, err := storage.NewFS(t.TempDir())
		if err != nil {
			t.Fatalf("storage.NewFS: %v", err)
		}
		return fs
	})
}

// TestPurgePhoto_storedDerivedObjectsR2 runs the scenarios over the R2 backend
// against the integration-test bucket. It is skipped when
// KUKATKO_TEST_S3_ENDPOINT is unset.
func TestPurgePhoto_storedDerivedObjectsR2(t *testing.T) {
	endpoint := os.Getenv(envTestS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping object-store integration test", envTestS3Endpoint)
	}
	testStoredDerivedPurge(t, func(t *testing.T) storage.Storage {
		t.Helper()
		return newBucketStore(t, endpoint)
	})
}

// newBucketStore returns the R2 backend over the emptied integration-test
// bucket, emptied again after the test. A media base URL and signing secret are
// set so the thumbnailer treats it as the publishing backend it is in
// production.
func newBucketStore(t *testing.T, endpoint string) *storage.R2 {
	t.Helper()
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	store, err := storage.NewR2(storage.R2Options{
		Endpoint: endpoint, Region: os.Getenv(envTestS3Region), Bucket: bucket,
		AccessKey: os.Getenv(envTestS3AccessKey), SecretKey: os.Getenv(envTestS3SecretKey),
		MediaBaseURL: "https://media.example.test", URLSigningSecret: "integration signing secret",
		URLTTL: time.Hour, TempPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("storage.NewR2: %v", err)
	}
	client := bucketClient(t, endpoint)
	ensureBucket(t, client, bucket)
	emptyBucket(t, client, bucket)
	t.Cleanup(func() { emptyBucket(t, client, bucket) })
	return store
}

// bucketClient returns a raw minio client for the endpoint, used to set the
// bucket up — deliberately not through the code under test.
func bucketClient(t *testing.T, endpoint string) *minio.Client {
	t.Helper()
	host, secure := endpoint, true
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		host, secure = strings.TrimPrefix(endpoint, "http://"), false
	case strings.HasPrefix(endpoint, "https://"):
		host = strings.TrimPrefix(endpoint, "https://")
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv(envTestS3AccessKey), os.Getenv(envTestS3SecretKey), ""),
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
	exists, err := client.BucketExists(t.Context(), bucket)
	if err != nil {
		t.Fatalf("checking bucket %s: %v", bucket, err)
	}
	if exists {
		return
	}
	if err := client.MakeBucket(t.Context(), bucket,
		minio.MakeBucketOptions{Region: os.Getenv(envTestS3Region)}); err != nil {
		t.Fatalf("creating bucket %s: %v", bucket, err)
	}
}

// emptyBucket removes every object from the test bucket, on its own context.
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
