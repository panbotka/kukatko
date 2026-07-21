//go:build integration

package storagemigrate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storagemigrate"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These tests run only under `make test-integration`, against a real
// S3-compatible endpoint (MinIO is what CI and local development use) and the
// real integration-test database. They exercise what the in-memory suite in
// storagemigrate_test.go cannot: the SQL that finds and stamps pending photos,
// and an object store that answers over the network and can be killed mid-run.
// With either environment variable unset they skip, so `make test` stays free of
// both dependencies.

// Environment variables describing the integration-test bucket, matching the
// names internal/storage's own integration suite uses. The bucket is dedicated
// to the test suite and safe to empty.
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

// killAfterPuts is how many objects the interrupted run is allowed to upload
// before it is killed. With a serial migrator and two objects per photo it lands
// squarely inside the third photo: its original is in the bucket, its row is not
// yet stamped. That is the state a resumed run has to recognise, and the reason
// the kill is scripted rather than timed.
const killAfterPuts = 5

// bucketStore opens the R2 backend against the integration-test bucket, creating
// the bucket when absent and emptying it both now and after the test. The calling
// test is skipped when KUKATKO_TEST_S3_ENDPOINT is unset.
func bucketStore(t *testing.T) *storage.R2 {
	t.Helper()

	endpoint := os.Getenv(envTestS3Endpoint)
	if endpoint == "" {
		t.Skipf("%s not set; skipping integration test", envTestS3Endpoint)
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
		TempPath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewR2: %v", err)
	}
	client := bucketClient(t, endpoint)
	ensureBucket(t, client, bucket)
	emptyBucket(t, client, bucket)
	t.Cleanup(func() { emptyBucket(t, client, bucket) })
	return store
}

// bucketClient returns a raw minio client for the endpoint, used to set the
// bucket up and to read back what the migration actually left in it — deliberately
// not through the code under test.
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

// bucketKeys lists everything in the bucket, sorted, read with a plain client so
// the assertion does not depend on the storage package it is checking.
func bucketKeys(t *testing.T, client *minio.Client, bucket string) []string {
	t.Helper()
	var keys []string
	for info := range client.ListObjects(t.Context(), bucket, minio.ListObjectsOptions{Recursive: true}) {
		if info.Err != nil {
			t.Fatalf("listing bucket: %v", info.Err)
		}
		keys = append(keys, info.Key)
	}
	sort.Strings(keys)
	return keys
}

// countingDestination wraps the real bucket, tallying the writes that succeeded
// per key and killing the run once killAfter of them have landed.
//
// Only a successful Put is counted, which is the whole point: a Put rejected for
// a digest mismatch leaves no object behind, so it is not something that
// "landed", and the photo it belonged to must be free to be retried.
type countingDestination struct {
	inner     storagemigrate.Destination
	killAfter int
	kill      context.CancelFunc

	mu   sync.Mutex
	puts map[string]int
}

// newCountingDestination wraps inner, seeding the tally with puts so a resumed
// run keeps counting where the killed one stopped. A non-positive killAfter never
// kills.
func newCountingDestination(
	inner storagemigrate.Destination, killAfter int, kill context.CancelFunc, puts map[string]int,
) *countingDestination {
	if puts == nil {
		puts = map[string]int{}
	}
	return &countingDestination{inner: inner, killAfter: killAfter, kill: kill, puts: puts}
}

// Check delegates. A transparent decorator must not reshape the store's errors:
// os.ErrNotExist and the mismatch sentinels have to reach the migrator intact.
func (d *countingDestination) Check(ctx context.Context) error {
	return d.inner.Check(ctx)
}

// Head delegates.
func (d *countingDestination) Head(ctx context.Context, relPath string) (storage.StoredFile, error) {
	return d.inner.Head(ctx, relPath)
}

// Put delegates, counts the write when it succeeded, and cancels the run's
// context once killAfter objects have landed — simulating the operator's Ctrl-C,
// or the VPS being rebooted, at a point that is inside a photo rather than
// conveniently between two.
func (d *countingDestination) Put(ctx context.Context, src io.Reader, file storage.StoredFile) error {
	if err := d.inner.Put(ctx, src, file); err != nil {
		return err
	}
	d.mu.Lock()
	d.puts[file.RelPath]++
	landed := 0
	for _, count := range d.puts {
		landed += count
	}
	d.mu.Unlock()

	if d.killAfter > 0 && landed >= d.killAfter && d.kill != nil {
		d.kill()
	}
	return nil
}

// putCounts returns a copy of the per-key tally of successful writes.
func (d *countingDestination) putCounts() map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.puts)
}

// libraryPhoto is one fixture photo: the row the catalogue holds and the bytes
// the disk holds. They agree, unless the photo is the poisoned one.
type libraryPhoto struct {
	uid string
	// relPath is the original's path under the originals root, and its object key.
	relPath string
	// catalogueHash is what the photos row claims the original hashes to.
	catalogueHash string
	// size is what the photos row claims the original's length is.
	size int64
	// thumbKey is the cached thumbnail's key, empty when none was generated.
	thumbKey string
	// poisoned marks the photo whose catalogue digest does not describe the bytes
	// on disk. Its upload must fail verification, and its local original must
	// survive.
	poisoned bool
}

// library is a small fixture library: originals on local disk, thumbnails in the
// cache, rows in the real catalogue.
type library struct {
	photos     []libraryPhoto
	sourceRoot string
	cacheDir   string
	store      *storagemigrate.Store
	db         *database.DB
}

// newLibrary builds n photos on disk and in the database. The photo at index
// poisonAt (negative for none) gets a catalogue digest that describes different
// bytes than the ones written to disk, which is how a real library carries a row
// whose hash was computed before the file was touched by something else.
func newLibrary(t *testing.T, n, poisonAt int) *library {
	t.Helper()

	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	lib := &library{
		sourceRoot: t.TempDir(),
		cacheDir:   t.TempDir(),
		store:      storagemigrate.NewStore(db.Pool()),
		db:         db,
	}
	for i := range n {
		lib.photos = append(lib.photos, lib.addPhoto(t, i, i == poisonAt))
	}
	return lib
}

// addPhoto writes one photo's original (and, unless it is poisoned, its grid
// thumbnail) and inserts its catalogue row.
func (l *library) addPhoto(t *testing.T, i int, poisoned bool) libraryPhoto {
	t.Helper()

	uid := fmt.Sprintf("photo%02d", i)
	content := []byte("original bytes for " + uid)
	relPath := "2024/05/" + uid + ".jpg"
	writeFile(t, filepath.Join(l.sourceRoot, filepath.FromSlash(relPath)), content)

	photo := libraryPhoto{
		uid: uid, relPath: relPath, catalogueHash: hashOf(content),
		size: int64(len(content)), poisoned: poisoned,
	}
	if poisoned {
		// The row's digest describes bytes that are not the bytes on disk. No
		// thumbnail: the cache is keyed by the digest, so nothing would be found
		// under this one anyway.
		photo.catalogueHash = hashOf([]byte("what the catalogue wrongly believes " + uid))
	} else {
		absThumb := thumbPath(t, l.cacheDir, photo.catalogueHash)
		writeFile(t, absThumb, []byte("thumbnail bytes for "+uid))
		rel, err := thumb.RelPath(photo.catalogueHash, gridSize)
		if err != nil {
			t.Fatalf("thumb.RelPath: %v", err)
		}
		photo.thumbKey = rel
	}
	l.insertRow(t, photo)
	return photo
}

// insertRow inserts the photo's catalogue row.
func (l *library) insertRow(t *testing.T, photo libraryPhoto) {
	t.Helper()
	const insertSQL = `
		INSERT INTO photos (uid, file_hash, file_path, file_name, file_size, file_mime)
		VALUES ($1, $2, $3, $4, $5, 'image/jpeg')`
	_, err := l.db.Pool().Exec(t.Context(), insertSQL,
		photo.uid, photo.catalogueHash, photo.relPath, filepath.Base(photo.relPath), photo.size)
	if err != nil {
		t.Fatalf("inserting photo %s: %v", photo.uid, err)
	}
}

// migratedUIDs returns the uids the catalogue considers present in the object
// store, read straight out of the column rather than through the Store.
func (l *library) migratedUIDs(t *testing.T) []string {
	t.Helper()
	rows, err := l.db.Pool().Query(t.Context(),
		`SELECT uid FROM photos WHERE storage_migrated_at IS NOT NULL ORDER BY uid`)
	if err != nil {
		t.Fatalf("reading migrated uids: %v", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatalf("scanning uid: %v", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating uids: %v", err)
	}
	return uids
}

// originalExists reports whether the photo's original is still on the local disk.
func (l *library) originalExists(t *testing.T, photo libraryPhoto) bool {
	t.Helper()
	return exists(t, filepath.Join(l.sourceRoot, filepath.FromSlash(photo.relPath)))
}

// poisoned returns the one photo whose catalogue digest lies about its bytes.
func (l *library) poisoned(t *testing.T) libraryPhoto {
	t.Helper()
	for _, photo := range l.photos {
		if photo.poisoned {
			return photo
		}
	}
	t.Fatal("the fixture library has no poisoned photo")
	return libraryPhoto{}
}

// TestMigrateToR2_KilledMidRunResumesAndEveryObjectLandsExactlyOnce is the
// migration's whole contract in one test, against a real bucket and a real
// catalogue.
//
// A six-photo library is migrated with --delete-local. One photo's catalogue
// digest lies about its bytes. The first run is killed after five objects have
// landed — inside the third photo, after its original but before its thumbnail —
// and a second run resumes it.
//
// What must then hold: every object in the bucket was written exactly once (the
// resumed run recognised the original the killed run had already uploaded and
// did not pay a second Class A operation for it); the poisoned photo has no
// object, no stamp, and — the rule the whole design exists to protect — still has
// its local original on disk; and every other photo is stamped, in the bucket,
// and gone from local disk.
func TestMigrateToR2_KilledMidRunResumesAndEveryObjectLandsExactlyOnce(t *testing.T) {
	const (
		photoCount = 6
		poisonAt   = 3
	)
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	destination := bucketStore(t)
	lib := newLibrary(t, photoCount, poisonAt)
	source, err := storage.NewFS(lib.sourceRoot)
	if err != nil {
		t.Fatalf("NewFS(source): %v", err)
	}

	// The first run: killed from inside the destination, after killAfterPuts
	// objects have landed.
	killableCtx, kill := context.WithCancel(t.Context())
	defer kill()
	killed := newCountingDestination(destination, killAfterPuts, kill, nil)
	first, err := newMigrator(t, lib, source, killed)
	if err != nil {
		t.Fatalf("New(first run): %v", err)
	}
	firstResult, firstErr := first.Run(killableCtx)
	if firstErr == nil {
		t.Fatal("the killed run returned no error; it was supposed to be interrupted")
	}
	if firstResult.Photos >= photoCount {
		t.Fatalf("the killed run finished %d photos; it was supposed to stop early", firstResult.Photos)
	}

	// The second run resumes: a fresh context, because the first one is cancelled,
	// and the killed run's tally carried over, so "exactly once" is counted across
	// both runs rather than within either.
	resumed := newCountingDestination(destination, 0, nil, killed.putCounts())
	second, err := newMigrator(t, lib, source, resumed)
	if err != nil {
		t.Fatalf("New(second run): %v", err)
	}
	secondResult, secondErr := second.Run(t.Context())
	if secondErr != nil {
		t.Fatalf("the resumed run aborted: %v", secondErr)
	}

	keys := bucketKeys(t, bucketClient(t, os.Getenv(envTestS3Endpoint)), bucket)
	assertPoisonedPhotoFailed(t, lib, secondResult)
	assertBucketHoldsEveryGoodObjectOnce(t, lib, keys, resumed.putCounts())
	assertResumeSkippedTheAlreadyUploadedOriginal(t, secondResult)
	assertLocalDiskMatchesTheCommits(t, lib)
}

// newMigrator builds a serial migrator over the library, reading three photos per
// batch so the run also crosses a catalogue page boundary, and deleting each
// local original once its row is committed.
func newMigrator(
	t *testing.T, lib *library, source storagemigrate.Source, destination storagemigrate.Destination,
) (*storagemigrate.Migrator, error) {
	t.Helper()
	return storagemigrate.New(storagemigrate.Config{
		Catalogue:   lib.store,
		Source:      source,
		Destination: destination,
		CacheDir:    lib.cacheDir,
		Concurrency: 1,
		BatchSize:   3,
		DeleteLocal: true,
	})
}

// assertPoisonedPhotoFailed checks the run collected the poisoned photo as a
// failure rather than aborting on it, and named it.
func assertPoisonedPhotoFailed(t *testing.T, lib *library, result storagemigrate.Result) {
	t.Helper()
	poisoned := lib.poisoned(t)
	if len(result.Failures) != 1 {
		t.Fatalf("resumed run reported %d failures, want exactly 1: %v", len(result.Failures), result.Failures)
	}
	failure := result.Failures[0]
	if failure.UID != poisoned.uid {
		t.Errorf("failed photo = %s, want %s", failure.UID, poisoned.uid)
	}
	if !errors.Is(failure.Err, storage.ErrHashMismatch) {
		t.Errorf("failure = %v, want ErrHashMismatch", failure.Err)
	}
}

// assertBucketHoldsEveryGoodObjectOnce checks that the bucket holds exactly the
// objects of the photos that verified — original and thumbnail apiece — that the
// poisoned photo left nothing behind, and that no key was written twice across
// the killed run and the resumed one.
func assertBucketHoldsEveryGoodObjectOnce(
	t *testing.T, lib *library, keys []string, puts map[string]int,
) {
	t.Helper()

	var want []string
	for _, photo := range lib.photos {
		if photo.poisoned {
			continue
		}
		want = append(want, photo.relPath, photo.thumbKey)
	}
	sort.Strings(want)
	if !slices.Equal(keys, want) {
		t.Errorf("bucket holds\n %v\nwant\n %v", keys, want)
	}

	poisoned := lib.poisoned(t)
	if slices.Contains(keys, poisoned.relPath) {
		t.Errorf("the bucket holds %s, the object of a photo that failed verification", poisoned.relPath)
	}
	for _, key := range want {
		if puts[key] != 1 {
			t.Errorf("%s landed %d times, want exactly 1", key, puts[key])
		}
	}
	// The poisoned original was streamed and rejected, never landed, so it is not
	// in the successful tally at all.
	if puts[poisoned.relPath] != 0 {
		t.Errorf("%s counted %d successful writes, want 0", poisoned.relPath, puts[poisoned.relPath])
	}
}

// assertResumeSkippedTheAlreadyUploadedOriginal checks the resumed run recognised
// the object the killed run had already put there instead of re-uploading it.
// That skip is what keeps a resumed migration inside R2's free tier.
func assertResumeSkippedTheAlreadyUploadedOriginal(t *testing.T, result storagemigrate.Result) {
	t.Helper()
	if result.Skipped < 1 {
		t.Errorf("resumed run skipped %d objects, want at least the original the killed run uploaded",
			result.Skipped)
	}
}

// assertLocalDiskMatchesTheCommits checks the invariant the command exists to
// keep: an original is gone from local disk if and only if its row says its
// objects are in the bucket.
func assertLocalDiskMatchesTheCommits(t *testing.T, lib *library) {
	t.Helper()

	var wantMigrated []string
	for _, photo := range lib.photos {
		if !photo.poisoned {
			wantMigrated = append(wantMigrated, photo.uid)
		}
	}
	if migrated := lib.migratedUIDs(t); !slices.Equal(migrated, wantMigrated) {
		t.Errorf("stamped photos = %v, want %v", migrated, wantMigrated)
	}
	for _, photo := range lib.photos {
		onDisk := lib.originalExists(t, photo)
		switch {
		case photo.poisoned && !onDisk:
			t.Errorf("%s failed verification but its local original was removed", photo.uid)
		case !photo.poisoned && onDisk:
			t.Errorf("%s was committed but its local original was not removed", photo.uid)
		}
	}
}

// TestMigrateToR2_CarriesTheSidecarAndReclaimsItLocally proves the migration
// moves each photo's metadata sidecar — the disaster-recovery artifact the
// catalogue can be rebuilt from — into the bucket alongside its original, and,
// under --delete-local, removes the local sidecar too. That is the whole point:
// once the disk is reclaimed, the sidecar must not be stranded on it.
func TestMigrateToR2_CarriesTheSidecarAndReclaimsItLocally(t *testing.T) {
	const photoCount = 3
	bucket := os.Getenv(envTestS3Bucket)
	if bucket == "" {
		bucket = "kukatko-test"
	}
	destination := bucketStore(t)
	lib := newLibrary(t, photoCount, -1)
	source, err := storage.NewFS(lib.sourceRoot)
	if err != nil {
		t.Fatalf("NewFS(source): %v", err)
	}

	// Give every photo a sidecar on local disk, in the parallel sidecars/ tree.
	sidecarKeys := make(map[string]string, photoCount)
	for _, photo := range lib.photos {
		sidecarKeys[photo.uid] = writeSidecar(t, lib.sourceRoot, photo.relPath, []byte("sidecar for "+photo.uid))
	}

	migrator, err := newMigrator(t, lib, source, destination)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := migrator.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("Run reported failures: %v", result.Failures)
	}

	keys := bucketKeys(t, bucketClient(t, os.Getenv(envTestS3Endpoint)), bucket)
	for _, photo := range lib.photos {
		key := sidecarKeys[photo.uid]
		if !slices.Contains(keys, key) {
			t.Errorf("sidecar %s of %s did not land in the bucket; got %v", key, photo.uid, keys)
		}
		if exists(t, filepath.Join(lib.sourceRoot, filepath.FromSlash(key))) {
			t.Errorf("local sidecar of %s survived --delete-local", photo.uid)
		}
		if lib.originalExists(t, photo) {
			t.Errorf("local original of %s survived --delete-local", photo.uid)
		}
	}
}
