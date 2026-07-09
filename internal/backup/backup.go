// Package backup performs in-process, scheduled backups of the kukatko database
// and original media to a second, independent S3-compatible bucket.
//
// A backup run does three things, in order: it streams a pg_dump of the
// configured database to db/kukatko-<timestamp>.dump, it incrementally syncs the
// originals into the same bucket (skipping objects already present at the same
// key and size), and it prunes old database dumps down to the configured
// retention count. Pruning only ever touches the dump prefix, so originals are
// never expired — with no object versioning underneath, a deleted original is a
// lost photo — and it only runs after the new dump has been stored, so a failed
// dump never deletes existing backups.
//
// Where the originals are read from depends on the storage backend. DiskOriginals
// walks a local originals root and streams each file up. BucketOriginals reads
// the primary object store and has the backup service copy each object
// server-side, so a library that lives in a bucket is never dragged through this
// process to be uploaded again. Either way the sync is additive: an object
// deleted from the primary is left untouched in the backup bucket.
//
// Everything external sits behind an interface — ObjectStore for the backup
// bucket, Dumper for pg_dump, OriginalSource for the originals — so the
// orchestration is unit-testable with fakes and no live S3, database or
// filesystem. The concrete implementations live in s3.go, pgdump.go,
// originals.go and bucket.go. Secret keys are confined to the S3 client and
// never logged.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	// dumpPrefix is the bucket key prefix under which database dumps are stored.
	// Pruning is scoped to this prefix so it can never affect originals.
	dumpPrefix = "db/"
	// dumpBaseName is the leading component of a dump object's name.
	dumpBaseName = "kukatko"
	// dumpTimeLayout formats a dump timestamp so object names sort
	// lexicographically in chronological order (UTC, no separators that reorder).
	dumpTimeLayout = "20060102T150405Z"
	// contentTypeDump is the MIME type stored with a database dump object.
	contentTypeDump = "application/octet-stream"
	// streamUnknownSize tells ObjectStore.Put the object length is unknown, so it
	// streams the body without buffering the whole dump in memory.
	streamUnknownSize int64 = -1
)

// Sentinel errors returned by the package so callers can branch with errors.Is.
var (
	// ErrAlreadyRunning indicates a backup run was requested while one is already
	// in progress; runs are serialised so two never race on the same objects.
	ErrAlreadyRunning = errors.New("backup: a run is already in progress")
)

// Object identifies a stored object by its key and size; ETag is included when
// the store provides it.
type Object struct {
	// Key is the object's full key within the bucket.
	Key string
	// Size is the object's byte length.
	Size int64
	// ETag is the store's entity tag for the object, when available.
	ETag string
}

// ObjectStore is the subset of an S3-compatible bucket the backup needs:
// statting an object (for incremental skip), streaming an upload, copying an
// object in from another bucket, listing a prefix (for retention) and removing
// an object (for pruning).
type ObjectStore interface {
	// Stat returns the object at key with ok=true when present, or a zero Object
	// with ok=false and a nil error when it does not exist. A genuine transport or
	// service error is returned as a non-nil error.
	Stat(ctx context.Context, key string) (obj Object, ok bool, err error)
	// Put streams size bytes from reader to key. A negative size streams the body
	// without buffering it whole (multipart upload). contentType may be empty.
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	// CopyFrom copies srcKey out of srcBucket into key in this store, asking the
	// destination service to perform the transfer itself so the payload never
	// passes through this process. The destination service must therefore be able
	// to read srcBucket with the credentials this store was built with.
	CopyFrom(ctx context.Context, srcBucket, srcKey, key string) error
	// Open opens the object at key for streaming reads (used by restore). The
	// caller must close the returned reader.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// List returns every object whose key begins with prefix.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Remove deletes the object at key. A missing object is not an error.
	Remove(ctx context.Context, key string) error
}

// Dumper produces a streamed database dump. The returned reader must be closed
// by the caller, which also waits for the underlying dump process to finish and
// surfaces its exit error.
type Dumper interface {
	// Dump starts a database dump and returns a reader over its bytes. Closing the
	// reader waits for the dump to complete and returns a non-nil error if the
	// dump process failed.
	Dump(ctx context.Context) (io.ReadCloser, error)
}

// LocalOriginal identifies one stored original by its slash-separated key and
// byte size. The key is the path relative to the originals root for the local
// backend and the object key for the bucket backend; both layouts are identical,
// and the key is reused verbatim in the backup bucket.
type LocalOriginal struct {
	// Key is the slash-separated path relative to the originals root.
	Key string
	// Size is the file's byte length, used for the incremental skip check.
	Size int64
}

// OriginalSource enumerates the originals to back up and transfers one into the
// backup bucket. How the transfer happens is the source's business: the local
// backend streams the file up, the bucket backend has the destination copy it
// server-side.
type OriginalSource interface {
	// List returns every stored original. The order is unspecified.
	List(ctx context.Context) ([]LocalOriginal, error)
	// CopyTo transfers original into dst under the same key, overwriting whatever
	// occupies it.
	CopyTo(ctx context.Context, dst ObjectStore, original LocalOriginal) error
}

// Result reports what one backup run did: the dump object created, how many
// originals were uploaded versus skipped as already present, and how many old
// dumps were pruned.
type Result struct {
	DumpKey           string `json:"dump_key"`
	OriginalsUploaded int    `json:"originals_uploaded"`
	OriginalsSkipped  int    `json:"originals_skipped"`
	DumpsPruned       int    `json:"dumps_pruned"`
}

// Status is the readable state of the backup subsystem for the admin endpoint:
// whether a run is in progress and the outcome of the most recent run.
type Status struct {
	// Configured reports whether a backup destination is wired (always true for a
	// live Service; the HTTP layer reports false when no Service exists).
	Configured bool `json:"configured"`
	// Running is true while a run is in progress.
	Running bool `json:"running"`
	// LastStartedAt is when the most recent run began, or nil if none has run.
	LastStartedAt *time.Time `json:"last_started_at,omitempty"`
	// LastFinishedAt is when the most recent run finished, or nil if none has
	// finished.
	LastFinishedAt *time.Time `json:"last_finished_at,omitempty"`
	// LastError is the most recent run's error message, empty on success.
	LastError string `json:"last_error,omitempty"`
	// LastResult is the most recent completed run's result, or nil if none.
	LastResult *Result `json:"last_result,omitempty"`
}

// Config bundles the dependencies of New. Objects, Originals and Dumper are
// required; Retention <= 0 disables dump pruning and a nil Logger uses the
// standard logger.
type Config struct {
	// Objects is the destination bucket: a second bucket, independent of the one
	// originals normally live in.
	Objects ObjectStore
	// Originals enumerates the originals to sync and transfers each into Objects.
	Originals OriginalSource
	// Dumper produces the streamed database dump.
	Dumper Dumper
	// Retention is how many of the most recent dumps to keep; <= 0 keeps all.
	Retention int
	// Logger receives run progress; nil uses the standard logger.
	Logger *log.Logger
}

// Service runs and tracks S3 backups.
type Service struct {
	objects   ObjectStore
	originals OriginalSource
	dumper    Dumper
	retention int
	logger    *log.Logger

	mu     sync.Mutex
	status Status
}

// New returns a backup Service from cfg. It panics if a required collaborator
// (Objects, Originals or Dumper) is nil, since that is a wiring bug.
func New(cfg Config) *Service {
	if cfg.Objects == nil || cfg.Originals == nil || cfg.Dumper == nil {
		panic("backup: Objects, Originals and Dumper are required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Service{
		objects:   cfg.Objects,
		originals: cfg.Originals,
		dumper:    cfg.Dumper,
		retention: cfg.Retention,
		logger:    logger,
		status:    Status{Configured: true},
	}
}

// Status returns a snapshot of the subsystem state for the admin readout.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.status
	if s.status.LastResult != nil {
		result := *s.status.LastResult
		snapshot.LastResult = &result
	}
	return snapshot
}

// Run performs one full backup as of ts: dump, originals sync, then prune. The
// timestamp is supplied by the caller (the scheduler or the command) and names
// the dump object. It returns ErrAlreadyRunning if a run is already in progress.
func (s *Service) Run(ctx context.Context, ts time.Time) (Result, error) {
	if !s.reserve(ts) {
		return Result{}, ErrAlreadyRunning
	}
	return s.runReserved(ctx, ts)
}

// Trigger starts a backup in the background and returns immediately, so an HTTP
// handler need not block on a potentially long run. The run uses a context
// detached from ctx's cancellation so it outlives the request that started it.
// It returns ErrAlreadyRunning if a run is already in progress.
func (s *Service) Trigger(ctx context.Context, ts time.Time) error {
	if !s.reserve(ts) {
		return ErrAlreadyRunning
	}
	runCtx := context.WithoutCancel(ctx)
	go func() {
		if _, err := s.runReserved(runCtx, ts); err != nil {
			s.logger.Printf("backup: triggered run failed: %v", err)
		}
	}()
	return nil
}

// reserve atomically marks a run as started, returning false if one is already
// in progress. On success it stamps the start time and clears the previous
// error so Status reflects the in-progress run.
func (s *Service) reserve(ts time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return false
	}
	started := ts
	s.status.Running = true
	s.status.LastStartedAt = &started
	s.status.LastError = ""
	return true
}

// runReserved executes a run that has already been reserved, recording its
// outcome in the status before returning it.
func (s *Service) runReserved(ctx context.Context, ts time.Time) (Result, error) {
	res, err := s.execute(ctx, ts)
	s.finish(res, err)
	return res, err
}

// finish records the outcome of a run: clears the running flag, stamps the
// finish time, and stores the result and any error message.
func (s *Service) finish(res Result, runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Running = false
	finished := time.Now()
	s.status.LastFinishedAt = &finished
	result := res
	s.status.LastResult = &result
	if runErr != nil {
		s.status.LastError = runErr.Error()
	}
}

// execute does the actual backup work. The dump is mandatory: if it fails the
// run aborts before any pruning, so retention never deletes existing dumps
// without a fresh one in place. The originals sync and prune errors are joined
// so a partial failure in one does not hide the other.
func (s *Service) execute(ctx context.Context, ts time.Time) (Result, error) {
	var res Result
	key, err := s.backupDatabase(ctx, ts)
	if err != nil {
		return res, fmt.Errorf("backup: database dump: %w", err)
	}
	res.DumpKey = key

	uploaded, skipped, syncErr := s.SyncOriginals(ctx)
	res.OriginalsUploaded = uploaded
	res.OriginalsSkipped = skipped

	pruned, pruneErr := s.PruneDumps(ctx)
	res.DumpsPruned = pruned

	s.logger.Printf("backup: dump %s, originals uploaded=%d skipped=%d, dumps pruned=%d",
		key, uploaded, skipped, pruned)
	return res, errors.Join(syncErr, pruneErr)
}

// backupDatabase streams a pg_dump to the timestamped dump object and returns
// its key. The dump reader is always closed (which waits for pg_dump to finish);
// a non-nil close error means the dump process itself failed.
func (s *Service) backupDatabase(ctx context.Context, ts time.Time) (string, error) {
	key := dumpKey(ts)
	reader, err := s.dumper.Dump(ctx)
	if err != nil {
		return "", fmt.Errorf("starting dump: %w", err)
	}
	putErr := s.objects.Put(ctx, key, reader, streamUnknownSize, contentTypeDump)
	closeErr := reader.Close()
	if putErr != nil {
		return "", fmt.Errorf("uploading dump %s: %w", key, putErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("dump process for %s: %w", key, closeErr)
	}
	return key, nil
}

// SyncOriginals transfers every original that is not already in the backup
// bucket at the same key and size, and returns the number transferred and the
// number skipped as already present. The sync is purely additive: it never
// removes an object from the backup bucket, so an original deleted from the
// primary store survives here.
func (s *Service) SyncOriginals(ctx context.Context) (uploaded, skipped int, err error) {
	list, err := s.originals.List(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("backup: listing originals: %w", err)
	}
	for _, original := range list {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return uploaded, skipped, fmt.Errorf("backup: originals sync interrupted: %w", ctxErr)
		}
		present, statErr := s.originalPresent(ctx, original)
		if statErr != nil {
			return uploaded, skipped, statErr
		}
		if present {
			skipped++
			continue
		}
		if copyErr := s.copyOriginal(ctx, original); copyErr != nil {
			return uploaded, skipped, copyErr
		}
		uploaded++
	}
	return uploaded, skipped, nil
}

// originalPresent reports whether the bucket already holds original at the same
// key and byte size, in which case the upload can be skipped.
func (s *Service) originalPresent(ctx context.Context, original LocalOriginal) (bool, error) {
	existing, ok, err := s.objects.Stat(ctx, original.Key)
	if err != nil {
		return false, fmt.Errorf("backup: statting %s: %w", original.Key, err)
	}
	return ok && existing.Size == original.Size, nil
}

// copyOriginal transfers one original into the backup bucket, leaving it to the
// source to decide how: the local backend streams the bytes up, the bucket
// backend has the destination copy them across server-side.
func (s *Service) copyOriginal(ctx context.Context, original LocalOriginal) error {
	if err := s.originals.CopyTo(ctx, s.objects, original); err != nil {
		return fmt.Errorf("backup: syncing originals: %w", err)
	}
	return nil
}

// PruneDumps removes database dumps beyond the configured retention, keeping the
// newest Retention dumps and deleting the rest. It is a no-op when retention is
// disabled (<= 0) or there are no excess dumps. It only ever lists and removes
// objects under the dump prefix, so originals are never affected: retention
// applies to dumps alone, and an original in the backup bucket is never expired.
func (s *Service) PruneDumps(ctx context.Context) (int, error) {
	if s.retention <= 0 {
		return 0, nil
	}
	dumps, err := s.objects.List(ctx, dumpPrefix)
	if err != nil {
		return 0, fmt.Errorf("backup: listing dumps: %w", err)
	}
	if len(dumps) <= s.retention {
		return 0, nil
	}
	// Dump names sort lexicographically in chronological order, so descending
	// order puts the newest first; everything past the retention count is old.
	sort.Slice(dumps, func(i, j int) bool { return dumps[i].Key > dumps[j].Key })
	removed := 0
	for _, dump := range dumps[s.retention:] {
		if err := s.objects.Remove(ctx, dump.Key); err != nil {
			return removed, fmt.Errorf("backup: removing old dump %s: %w", dump.Key, err)
		}
		removed++
	}
	return removed, nil
}

// dumpKey returns the bucket key for the dump taken at ts, formatted so dump
// keys sort lexicographically in chronological order.
func dumpKey(ts time.Time) string {
	return fmt.Sprintf("%s%s-%s.dump", dumpPrefix, dumpBaseName, ts.UTC().Format(dumpTimeLayout))
}
