package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
)

// Sentinel errors returned by the restore service.
var (
	// ErrNoDumps indicates the bucket holds no database dumps to restore from.
	ErrNoDumps = errors.New("backup: no database dumps found in bucket")
	// ErrDumpNotFound indicates the requested dump key is not present in the bucket.
	ErrDumpNotFound = errors.New("backup: dump not found")
)

// DumpInfo describes one restorable database dump in the bucket.
type DumpInfo struct {
	// Key is the dump object's full key within the bucket.
	Key string `json:"key"`
	// Size is the dump's byte length.
	Size int64 `json:"size"`
}

// LocalOriginals is the local originals directory seen as a restore destination
// and verification source: listing what is already present, statting one entry
// for the skip-existing check, and writing a downloaded original atomically.
// DiskOriginals satisfies it.
type LocalOriginals interface {
	// List returns every original currently on disk.
	List(ctx context.Context) ([]LocalOriginal, error)
	// Stat reports whether key exists on disk and, if so, its size.
	Stat(ctx context.Context, key string) (LocalOriginal, bool, error)
	// Write streams reader to key, publishing the file atomically.
	Write(ctx context.Context, key string, reader io.Reader) error
}

// PhotoCatalog is the subset of the photo catalogue the integrity check needs:
// the total photo count and the storage key of every catalogued file.
type PhotoCatalog interface {
	// CountPhotos returns the number of photos in the catalogue.
	CountPhotos(ctx context.Context) (int, error)
	// ListFilePaths returns the storage key of every catalogued file.
	ListFilePaths(ctx context.Context) ([]string, error)
}

// RestoreOriginalsResult reports how many originals a download pass fetched
// versus skipped as already present at the same key and size.
type RestoreOriginalsResult struct {
	Downloaded int `json:"downloaded"`
	Skipped    int `json:"skipped"`
}

// VerifyReport is the post-restore integrity reconciliation between the
// catalogue in the database and the originals on disk.
type VerifyReport struct {
	// PhotosInDB is the total number of photos in the catalogue.
	PhotosInDB int `json:"photos_in_db"`
	// FilesInDB is the number of catalogued files (originals plus sidecars).
	FilesInDB int `json:"files_in_db"`
	// OriginalsOnDisk is the number of files found under the originals root.
	OriginalsOnDisk int `json:"originals_on_disk"`
	// MissingOnDisk lists catalogued file keys with no file on disk.
	MissingOnDisk []string `json:"missing_on_disk"`
	// ExtraOnDisk lists files on disk with no catalogue row.
	ExtraOnDisk []string `json:"extra_on_disk"`
	// Consistent is true when neither mismatch list has any entries.
	Consistent bool `json:"consistent"`
}

// RestoreConfig bundles the dependencies of NewRestoreService. Objects and
// Restorer are required for a database restore; Originals is required to download
// originals; Photos is required for the integrity check. A nil Logger uses the
// standard logger.
type RestoreConfig struct {
	// Objects is the source bucket.
	Objects ObjectStore
	// Restorer runs pg_restore against the target database.
	Restorer Restorer
	// Originals is the local originals directory as a download destination and
	// verification source.
	Originals LocalOriginals
	// Photos is the catalogue queried by the integrity check.
	Photos PhotoCatalog
	// Logger receives progress; nil uses the standard logger.
	Logger *log.Logger
}

// RestoreService restores the database and originals from an S3 backup and
// verifies the result. Everything external sits behind an interface so the
// orchestration is unit-testable with fakes and no live S3, database or
// filesystem.
type RestoreService struct {
	objects   ObjectStore
	restorer  Restorer
	originals LocalOriginals
	photos    PhotoCatalog
	logger    *log.Logger
}

// NewRestoreService returns a RestoreService from cfg. It panics if Objects is
// nil, since the bucket source is required by every operation.
func NewRestoreService(cfg RestoreConfig) *RestoreService {
	if cfg.Objects == nil {
		panic("backup: Objects is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &RestoreService{
		objects:   cfg.Objects,
		restorer:  cfg.Restorer,
		originals: cfg.Originals,
		photos:    cfg.Photos,
		logger:    logger,
	}
}

// ListDumps returns the database dumps available in the bucket, newest first.
// Dump keys sort lexicographically in chronological order, so a descending sort
// puts the most recent dump first.
func (s *RestoreService) ListDumps(ctx context.Context) ([]DumpInfo, error) {
	objects, err := s.objects.List(ctx, dumpPrefix)
	if err != nil {
		return nil, fmt.Errorf("backup: listing dumps: %w", err)
	}
	dumps := make([]DumpInfo, 0, len(objects))
	for _, obj := range objects {
		if strings.HasSuffix(obj.Key, ".dump") {
			dumps = append(dumps, DumpInfo{Key: obj.Key, Size: obj.Size})
		}
	}
	sort.Slice(dumps, func(i, j int) bool { return dumps[i].Key > dumps[j].Key })
	return dumps, nil
}

// LatestDump returns the most recent dump in the bucket, or ErrNoDumps when none
// exist.
func (s *RestoreService) LatestDump(ctx context.Context) (DumpInfo, error) {
	dumps, err := s.ListDumps(ctx)
	if err != nil {
		return DumpInfo{}, err
	}
	if len(dumps) == 0 {
		return DumpInfo{}, ErrNoDumps
	}
	return dumps[0], nil
}

// RestoreDatabase restores the database from the dump at key, streaming the
// archive from the bucket straight into pg_restore. An empty key restores the
// most recent dump. It returns ErrNoDumps when no key is given and the bucket
// holds no dumps, ErrDumpNotFound when the named key is absent, and the
// restorer's error when the restore itself fails. This is destructive: it
// overwrites the target database.
func (s *RestoreService) RestoreDatabase(ctx context.Context, key string) (string, error) {
	if s.restorer == nil {
		return "", errors.New("backup: no restorer configured")
	}
	resolved, err := s.resolveDump(ctx, key)
	if err != nil {
		return "", err
	}
	reader, err := s.objects.Open(ctx, resolved)
	if err != nil {
		return "", fmt.Errorf("backup: opening dump %s: %w", resolved, err)
	}
	defer func() { _ = reader.Close() }()
	s.logger.Printf("backup: restoring database from %s", resolved)
	if err := s.restorer.Restore(ctx, reader); err != nil {
		return "", fmt.Errorf("backup: restoring %s: %w", resolved, err)
	}
	s.logger.Printf("backup: database restored from %s", resolved)
	return resolved, nil
}

// resolveDump turns a requested key into a concrete dump key: an empty key
// resolves to the latest dump, and a non-empty key is verified to exist.
func (s *RestoreService) resolveDump(ctx context.Context, key string) (string, error) {
	if key == "" {
		latest, err := s.LatestDump(ctx)
		if err != nil {
			return "", err
		}
		return latest.Key, nil
	}
	dumps, err := s.ListDumps(ctx)
	if err != nil {
		return "", err
	}
	for _, dump := range dumps {
		if dump.Key == key {
			return key, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrDumpNotFound, key)
}

// RestoreOriginals downloads every original in the bucket that is not already on
// disk at the same key and size, streaming each one and writing it atomically so
// an interrupted run resumes safely. Database dumps (under the dump prefix) are
// skipped. It returns how many originals were downloaded versus skipped.
func (s *RestoreService) RestoreOriginals(ctx context.Context) (RestoreOriginalsResult, error) {
	var res RestoreOriginalsResult
	if s.originals == nil {
		return res, errors.New("backup: no originals destination configured")
	}
	objects, err := s.objects.List(ctx, "")
	if err != nil {
		return res, fmt.Errorf("backup: listing bucket originals: %w", err)
	}
	for _, obj := range objects {
		if strings.HasPrefix(obj.Key, dumpPrefix) {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, fmt.Errorf("backup: originals restore interrupted: %w", ctxErr)
		}
		present, statErr := s.originalPresentLocally(ctx, obj)
		if statErr != nil {
			return res, statErr
		}
		if present {
			res.Skipped++
			continue
		}
		if dlErr := s.downloadOriginal(ctx, obj.Key); dlErr != nil {
			return res, dlErr
		}
		res.Downloaded++
	}
	s.logger.Printf("backup: originals restore downloaded=%d skipped=%d", res.Downloaded, res.Skipped)
	return res, nil
}

// originalPresentLocally reports whether the originals directory already holds
// obj at the same key and byte size, in which case the download is skipped.
func (s *RestoreService) originalPresentLocally(ctx context.Context, obj Object) (bool, error) {
	local, ok, err := s.originals.Stat(ctx, obj.Key)
	if err != nil {
		return false, fmt.Errorf("backup: statting local %s: %w", obj.Key, err)
	}
	return ok && local.Size == obj.Size, nil
}

// downloadOriginal streams one bucket object to the originals directory at its
// key, writing it atomically.
func (s *RestoreService) downloadOriginal(ctx context.Context, key string) error {
	reader, err := s.objects.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("backup: opening %s: %w", key, err)
	}
	defer func() { _ = reader.Close() }()
	if err := s.originals.Write(ctx, key, reader); err != nil {
		return fmt.Errorf("backup: writing %s: %w", key, err)
	}
	return nil
}

// Verify reconciles the catalogue against the originals on disk and reports the
// counts plus any mismatches (catalogued files missing on disk, and files on
// disk with no catalogue row). It is read-only.
func (s *RestoreService) Verify(ctx context.Context) (VerifyReport, error) {
	if s.photos == nil || s.originals == nil {
		return VerifyReport{}, errors.New("backup: integrity check needs both catalogue and originals")
	}
	photoCount, err := s.photos.CountPhotos(ctx)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("backup: counting photos: %w", err)
	}
	dbPaths, err := s.photos.ListFilePaths(ctx)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("backup: listing catalogued files: %w", err)
	}
	diskFiles, err := s.originals.List(ctx)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("backup: listing originals on disk: %w", err)
	}
	diskKeys := make([]string, len(diskFiles))
	for i, f := range diskFiles {
		diskKeys[i] = f.Key
	}
	missing, extra := reconcile(dbPaths, diskKeys)
	return VerifyReport{
		PhotosInDB:      photoCount,
		FilesInDB:       len(dbPaths),
		OriginalsOnDisk: len(diskKeys),
		MissingOnDisk:   missing,
		ExtraOnDisk:     extra,
		Consistent:      len(missing) == 0 && len(extra) == 0,
	}, nil
}

// reconcile compares the catalogued file keys against the keys found on disk and
// returns the sorted set differences: missing (in the catalogue but not on disk)
// and extra (on disk but not catalogued). It is a pure function so the
// comparison logic is exercised without any I/O.
func reconcile(dbPaths, diskKeys []string) (missing, extra []string) {
	diskSet := make(map[string]struct{}, len(diskKeys))
	for _, key := range diskKeys {
		diskSet[key] = struct{}{}
	}
	dbSet := make(map[string]struct{}, len(dbPaths))
	for _, path := range dbPaths {
		dbSet[path] = struct{}{}
		if _, ok := diskSet[path]; !ok {
			missing = append(missing, path)
		}
	}
	for _, key := range diskKeys {
		if _, ok := dbSet[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
