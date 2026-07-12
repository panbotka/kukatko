// Package trash implements the permanent deletion (purge) of soft-deleted
// photos. Photos are soft-deleted by setting photos.archived_at; this package
// hard-deletes those rows once their retention period elapses (the scheduled
// purge) or on an explicit admin/editor action (purge one, empty the trash).
//
// A purge removes the database row — cascading its embeddings, faces, markers,
// album/label memberships, phashes, edits and favorites via ON DELETE CASCADE —
// then deletes the originals and cached thumbnails from disk and, when a remote
// object store is configured, the corresponding backup objects. Artifacts are
// always deleted before the row so an interrupted purge leaves a re-purgeable
// orphan row rather than dangling files. Every operation is idempotent and safe
// to re-run.
package trash

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
)

// ErrNotArchived is returned by PurgePhoto when the target photo exists but is
// not archived: only items already in the trash may be permanently deleted, so
// a live photo cannot be purged by mistake.
var ErrNotArchived = errors.New("trash: photo is not archived")

// hoursPerDay converts the configured retention, expressed in days, to a
// duration cutoff.
const hoursPerDay = 24

// defaultBatchSize bounds how many archived UIDs are loaded per purge batch so a
// large trash is processed in bounded memory.
const defaultBatchSize = 200

// Purge sources, recorded in each purge's audit entry (details["source"]) so the
// trail distinguishes a manual single purge, an empty-trash sweep, and the
// scheduled retention purge.
const (
	// sourceManual is a single admin/editor purge of one photo.
	sourceManual = "manual"
	// sourceEmptyTrash is an admin/editor sweep of the whole trash.
	sourceEmptyTrash = "empty_trash"
	// sourceRetention is the scheduled, system-initiated retention purge.
	sourceRetention = "retention"
)

// PhotoStore is the subset of the photo repository the purge needs: resolving a
// photo and its files, enumerating the archived backlog, and deleting a row.
type PhotoStore interface {
	// GetByUID returns the photo with uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// ListFiles returns every stored file (original and sidecars) for the photo.
	ListFiles(ctx context.Context, photoUID string) ([]photos.PhotoFile, error)
	// DeleteAudited removes the photo row (cascading its satellite rows) and writes
	// entry to the audit log in the same transaction, so the permanent deletion and
	// its audit record commit atomically. It returns photos.ErrPhotoNotFound when no
	// such photo exists, in which case nothing is deleted and no audit row is written.
	DeleteAudited(ctx context.Context, uid string, entry audit.Entry) error
	// ListArchivedUIDs returns archived photo UIDs oldest-first; before bounds the
	// set to archived_at <= before (nil = all archived).
	ListArchivedUIDs(ctx context.Context, before *time.Time, limit, offset int) ([]string, error)
}

// FileStorage is the subset of storage the purge needs: deleting an original by
// its relative path.
type FileStorage interface {
	// Delete removes the file at relPath. A missing file is not an error.
	Delete(ctx context.Context, relPath string) error
}

// ThumbStore is the subset of the thumbnailer the purge needs: removing every
// cached size for a file hash.
type ThumbStore interface {
	// Remove deletes every cached thumbnail size for the given file hash.
	Remove(hash string) error
}

// RemoteRemover deletes a backup object from a configured remote (S3-compatible)
// store. It is optional: when no remote backup is configured the purge skips it
// entirely. The key is the object's relative path, matching the original's
// storage path.
type RemoteRemover interface {
	// Remove deletes the remote object identified by key. A missing object is not
	// an error.
	Remove(ctx context.Context, key string) error
}

// Result reports the outcome of a batch purge: how many photos were permanently
// removed and how many failed (and were left in the trash for a later retry).
type Result struct {
	Purged int `json:"purged"`
	Failed int `json:"failed"`
}

// Config bundles the dependencies of New. Photos, Storage and Thumbnailer are
// required; Remote is optional (nil when no remote backup is configured).
type Config struct {
	// Photos is the photo repository.
	Photos PhotoStore
	// Storage deletes originals from disk.
	Storage FileStorage
	// Thumbnailer removes cached thumbnails from disk.
	Thumbnailer ThumbStore
	// Remote, when non-nil, deletes the corresponding backup objects.
	Remote RemoteRemover
	// RetentionDays is how long an archived photo is kept before the scheduled
	// purge removes it. A value <= 0 disables the scheduled purge (manual purge
	// and empty-trash still work).
	RetentionDays int
	// BatchSize bounds how many UIDs are loaded per batch; <= 0 uses the default.
	BatchSize int
	// Logger receives purge progress; nil uses the standard logger.
	Logger *log.Logger
}

// Service purges soft-deleted photos.
type Service struct {
	photos        PhotoStore
	storage       FileStorage
	thumbnailer   ThumbStore
	remote        RemoteRemover
	retentionDays int
	batchSize     int
	logger        *log.Logger
}

// New returns a purge Service from cfg. It panics if a required collaborator
// (Photos, Storage or Thumbnailer) is nil, since that is a wiring bug.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Storage == nil || cfg.Thumbnailer == nil {
		panic("trash: Photos, Storage and Thumbnailer are required")
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Service{
		photos:        cfg.Photos,
		storage:       cfg.Storage,
		thumbnailer:   cfg.Thumbnailer,
		remote:        cfg.Remote,
		retentionDays: cfg.RetentionDays,
		batchSize:     batch,
		logger:        logger,
	}
}

// PurgePhoto permanently deletes the archived photo identified by uid, recording
// meta (the acting admin/editor) on the purge's audit entry. It returns
// photos.ErrPhotoNotFound if no such photo exists, or ErrNotArchived if the photo
// is live (not in the trash). On success the row and all its artifacts are gone.
func (s *Service) PurgePhoto(ctx context.Context, uid string, meta audit.Meta) error {
	photo, err := s.photos.GetByUID(ctx, uid)
	if err != nil {
		return fmt.Errorf("trash: resolving photo %s: %w", uid, err)
	}
	if photo.ArchivedAt == nil {
		return ErrNotArchived
	}
	return s.purgeOne(ctx, uid, meta, sourceManual)
}

// EmptyTrash permanently deletes every archived photo regardless of how long it
// has been in the trash, attributing each purge to meta (the acting
// admin/editor). It returns the count purged and failed; a per-photo failure is
// recorded and the photo is left in the trash rather than aborting the whole run.
func (s *Service) EmptyTrash(ctx context.Context, meta audit.Meta) (Result, error) {
	return s.purgeArchived(ctx, nil, meta, sourceEmptyTrash)
}

// PurgeExpired permanently deletes every archived photo whose archived_at is
// older than the configured retention period. It runs without an HTTP actor, so
// each purge is audited against a system actor (empty ActorUID). When retention
// is disabled (RetentionDays <= 0) it is a no-op returning a zero Result.
func (s *Service) PurgeExpired(ctx context.Context) (Result, error) {
	if s.retentionDays <= 0 {
		return Result{}, nil
	}
	cutoff := time.Now().Add(-time.Duration(s.retentionDays) * hoursPerDay * time.Hour)
	return s.purgeArchived(ctx, &cutoff, audit.Meta{}, sourceRetention)
}

// purgeArchived purges archived photos in oldest-first batches until none remain
// in scope, attributing each purge to meta with the given source. before bounds
// the set (nil = all archived). Failed photos are skipped via a growing offset so
// the loop always converges, and a cancelled context stops it promptly.
func (s *Service) purgeArchived(
	ctx context.Context, before *time.Time, meta audit.Meta, source string,
) (Result, error) {
	var res Result
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("trash: purge interrupted: %w", err)
		}
		uids, err := s.photos.ListArchivedUIDs(ctx, before, s.batchSize, offset)
		if err != nil {
			return res, fmt.Errorf("trash: listing archived photos: %w", err)
		}
		if len(uids) == 0 {
			return res, nil
		}
		if err := s.purgeBatch(ctx, uids, &res, &offset, meta, source); err != nil {
			return res, err
		}
	}
}

// purgeBatch purges one batch of UIDs, accumulating counts into res and
// attributing each purge to meta with the given source. A per-photo failure is
// logged, counted and skipped (offset is advanced so the failed UID is stepped
// past on the next batch, keeping the loop convergent); only a cancelled context
// aborts the batch.
func (s *Service) purgeBatch(
	ctx context.Context, uids []string, res *Result, offset *int, meta audit.Meta, source string,
) error {
	for _, uid := range uids {
		if err := s.purgeOne(ctx, uid, meta, source); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("trash: purge interrupted: %w", ctxErr)
			}
			s.logger.Printf("trash: purging %s failed: %v", uid, err)
			res.Failed++
			*offset++ // leave the failure in place; step past it next batch.
			continue
		}
		res.Purged++
	}
	return nil
}

// purgeOne deletes a single photo's artifacts (originals, thumbnails and remote
// objects) and then its row, writing an audit entry (attributed to meta, tagged
// with source) in the same transaction as the row deletion. Artifacts are removed
// first so an interrupted purge leaves a re-purgeable orphan row rather than
// dangling files; a hard artifact error aborts before the row is deleted so the
// next run retries it.
func (s *Service) purgeOne(ctx context.Context, uid string, meta audit.Meta, source string) error {
	files, err := s.photos.ListFiles(ctx, uid)
	if err != nil {
		return fmt.Errorf("listing files: %w", err)
	}
	for _, file := range files {
		if err := s.deleteArtifacts(ctx, file); err != nil {
			return err
		}
	}
	entry := meta.Entry(audit.ActionPhotoPurge, "photos", uid, map[string]any{"source": source})
	if err := s.photos.DeleteAudited(ctx, uid, entry); err != nil {
		return fmt.Errorf("deleting row: %w", err)
	}
	return nil
}

// deleteArtifacts removes the on-disk original, the cached thumbnails and (when
// configured) the remote backup object for a single stored file. A missing
// original is ignored; a malformed sidecar hash skips thumbnail removal rather
// than failing the purge.
func (s *Service) deleteArtifacts(ctx context.Context, file photos.PhotoFile) error {
	if err := s.storage.Delete(ctx, file.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting original %s: %w", file.FilePath, err)
	}
	if err := s.thumbnailer.Remove(file.FileHash); err != nil && !errors.Is(err, thumb.ErrInvalidHash) {
		return fmt.Errorf("removing thumbnails for %s: %w", file.FileHash, err)
	}
	if s.remote != nil {
		if err := s.remote.Remove(ctx, file.FilePath); err != nil {
			return fmt.Errorf("removing remote object %s: %w", file.FilePath, err)
		}
	}
	return nil
}

// RunPurge runs the scheduled retention purge: once immediately, then every
// interval until ctx is cancelled. It is a no-op (returns at once) when the
// scheduled purge is disabled (RetentionDays <= 0). Intended to be launched in a
// goroutine for the lifetime of the server process.
func (s *Service) RunPurge(ctx context.Context, interval time.Duration) {
	if s.retentionDays <= 0 {
		s.logger.Printf("trash: scheduled purge disabled (retention_days <= 0)")
		return
	}
	s.purgeTick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeTick(ctx)
		}
	}
}

// purgeTick runs one scheduled PurgeExpired pass and logs its outcome, swallowing
// errors (other than to log them) so a transient failure never stops the loop.
func (s *Service) purgeTick(ctx context.Context) {
	res, err := s.PurgeExpired(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logger.Printf("trash: scheduled purge failed: %v", err)
		return
	}
	if res.Purged > 0 || res.Failed > 0 {
		s.logger.Printf("trash: scheduled purge removed %d photo(s), %d failed", res.Purged, res.Failed)
	}
}
