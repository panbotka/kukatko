// Package trash implements the permanent deletion (purge) of soft-deleted
// photos. Photos are soft-deleted by setting photos.archived_at; this package
// hard-deletes those rows once their retention period elapses (the scheduled
// purge) or on an explicit admin/editor action (purge one, empty the trash).
//
// A purge removes the database row — cascading its embeddings, faces, markers,
// album/label memberships, phashes, edits and favorites via ON DELETE CASCADE —
// then deletes the originals, cached thumbnails and derived streaming segments
// and, when a remote object store is configured, the corresponding backup
// objects. Artifacts are always deleted before the row so an interrupted purge
// leaves a re-purgeable orphan row rather than dangling files. Every operation is idempotent and safe
// to re-run.
//
// # Projects
//
// Every operation works inside one project: PurgePhoto, EmptyTrash and
// PurgeOlderThan take the project uid after the context and never reach another
// project's archive (a foreign uid is photos.ErrPhotoNotFound, exactly like a
// missing one). The scheduled retention purge has no request and so no project;
// it lists the active projects and sweeps each one's trash in turn. The objects a
// purge deletes are named by the photo's own rows — the file_path of every
// stored file and the file_hash the derived media are keyed by, under the
// storage prefix of that file's own original (storekeys.PrefixOf(file_path)) —
// never recomputed from the project, so a photo is cleaned up where it actually
// lives, even in a project half-way through its storage move.
package trash

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/storyboard"
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
	// sourcePurgeOlder is an admin sweep of every archived photo older than a
	// caller-chosen age cutoff (the manual purge-older-than-N-days control). It is
	// distinct from sourceRetention so the audit trail tells a deliberate,
	// admin-attributed age purge apart from the scheduled system purge.
	sourcePurgeOlder = "purge_older"
	// sourceRetention is the scheduled, system-initiated retention purge.
	sourceRetention = "retention"
)

// PhotoStore is the subset of the photo repository the purge needs: resolving a
// photo and its files, enumerating the archived backlog, and deleting a row.
// Every method works inside the project it is given; a photo of another project
// is photos.ErrPhotoNotFound. *photos.Store satisfies it.
type PhotoStore interface {
	// GetByUIDInProject returns the project's photo with uid, or
	// photos.ErrPhotoNotFound.
	GetByUIDInProject(ctx context.Context, projectUID, uid string) (photos.Photo, error)
	// ListFiles returns every stored file (original and sidecars) of the
	// project's photo.
	ListFiles(ctx context.Context, projectUID, photoUID string) ([]photos.PhotoFile, error)
	// DeleteAudited removes the project's photo row (cascading its satellite
	// rows) and writes entry to the audit log in the same transaction, so the
	// permanent deletion and its audit record commit atomically. It returns
	// photos.ErrPhotoNotFound when the project holds no such photo, in which case
	// nothing is deleted and no audit row is written.
	DeleteAudited(ctx context.Context, projectUID, uid string, entry audit.Entry) error
	// ListArchivedUIDs returns the project's archived photo UIDs oldest-first;
	// before bounds the set to archived_at <= before (nil = all archived).
	ListArchivedUIDs(ctx context.Context, projectUID string, before *time.Time, limit, offset int) ([]string, error)
}

// ProjectLister enumerates the projects the scheduled retention purge sweeps.
// *project.Store satisfies it; deactivated projects are skipped by the purge.
type ProjectLister interface {
	// List returns every project of the instance.
	List(ctx context.Context) ([]project.Project, error)
}

// FileStorage is the subset of storage the purge needs: deleting an original by
// its relative path.
type FileStorage interface {
	// Delete removes the file at relPath. A missing file is not an error.
	Delete(ctx context.Context, relPath string) error
}

// ThumbStore is the subset of the thumbnailer the purge needs: removing every
// cached size for a file hash under a storage prefix.
type ThumbStore interface {
	// Remove deletes every cached thumbnail size for the given file hash under
	// the given storage prefix (that of the photo's own original).
	Remove(prefix, hash string) error
}

// StoryboardStore is the subset of the storyboard generator the purge needs:
// removing the cached scrub-preview sprite of a video by its file hash. It is
// optional — a wiring without it simply leaves the sprite behind, which is
// regenerable derived media and never catalogue state.
type StoryboardStore interface {
	// Remove deletes the cached storyboard sprite for the given file hash under
	// the given storage prefix (that of the video's own original). A hash that
	// never had one is not an error.
	Remove(prefix, hash string) error
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

// MoveLock keeps a storage move off one photo while it is purged. It is
// satisfied by *rekey.Gate.
type MoveLock interface {
	// Purge runs purge while no storage move can commit the photo or copy its
	// objects — waiting while a move is copying it right now — and returns
	// purge's error. A photo a crashed move has yet to finish is refused with an
	// error wrapping storekeys.ErrMoving, purge not called.
	Purge(ctx context.Context, projectUID, photoUID string, purge func() error) error
}

// Result reports the outcome of a batch purge: how many photos were permanently
// removed and how many failed (and were left in the trash for a later retry).
type Result struct {
	Purged int `json:"purged"`
	Failed int `json:"failed"`
}

// Config bundles the dependencies of New. Photos, Projects, Storage and
// Thumbnailer are required; Remote is optional (nil when no remote backup is
// configured).
type Config struct {
	// Photos is the photo repository.
	Photos PhotoStore
	// Projects lists the projects whose trash the scheduled retention purge
	// sweeps, one after another.
	Projects ProjectLister
	// Storage deletes originals from disk.
	Storage FileStorage
	// Thumbnailer removes cached thumbnails from disk.
	Thumbnailer ThumbStore
	// Storyboards, when non-nil, removes a purged video's cached scrub-preview
	// sprite so no derived media outlives its original.
	Storyboards StoryboardStore
	// Remote, when non-nil, deletes the corresponding backup objects.
	Remote RemoteRemover
	// Moves, when non-nil, keeps a storage move off each photo while it is
	// purged. Without it a purge racing a move could leave the copy the move
	// made of the photo's original under the new prefix, owned by no row.
	Moves MoveLock
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
	projects      ProjectLister
	storage       FileStorage
	thumbnailer   ThumbStore
	storyboards   StoryboardStore
	remote        RemoteRemover
	moves         MoveLock
	retentionDays int
	batchSize     int
	logger        *log.Logger
}

// New returns a purge Service from cfg. It panics if a required collaborator
// (Photos, Projects, Storage or Thumbnailer) is nil, since that is a wiring bug.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Projects == nil || cfg.Storage == nil || cfg.Thumbnailer == nil {
		panic("trash: Photos, Projects, Storage and Thumbnailer are required")
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
		projects:      cfg.Projects,
		storage:       cfg.Storage,
		thumbnailer:   cfg.Thumbnailer,
		storyboards:   cfg.Storyboards,
		remote:        cfg.Remote,
		moves:         cfg.Moves,
		retentionDays: cfg.RetentionDays,
		batchSize:     batch,
		logger:        logger,
	}
}

// PurgePhoto permanently deletes the project's archived photo identified by uid,
// recording meta (the acting admin/editor) on the purge's audit entry. It returns
// photos.ErrPhotoNotFound if the project holds no such photo — a photo of another
// project included, which is left untouched — or ErrNotArchived if the photo is
// live (not in the trash). On success the row and all its artifacts are gone.
func (s *Service) PurgePhoto(ctx context.Context, projectUID, uid string, meta audit.Meta) error {
	photo, err := s.photos.GetByUIDInProject(ctx, projectUID, uid)
	if err != nil {
		return fmt.Errorf("trash: resolving photo %s: %w", uid, err)
	}
	if photo.ArchivedAt == nil {
		return ErrNotArchived
	}
	return s.purgeOne(ctx, projectUID, uid, meta, sourceManual)
}

// EmptyTrash permanently deletes every archived photo of the project regardless
// of how long it has been in the trash, attributing each purge to meta (the
// acting admin/editor). Another project's trash is never touched. It returns the
// count purged and failed; a per-photo failure is recorded and the photo is left
// in the trash rather than aborting the whole run.
func (s *Service) EmptyTrash(ctx context.Context, projectUID string, meta audit.Meta) (Result, error) {
	return s.purgeArchived(ctx, projectUID, nil, meta, sourceEmptyTrash)
}

// PurgeOlderThan permanently deletes every archived photo of the project whose
// archived_at is older than now minus days, attributing each purge to meta (the acting admin)
// and tagging it with sourcePurgeOlder so a deliberate age-bounded purge is
// distinguishable in the audit trail from the scheduled retention purge. Unlike
// PurgeExpired it ignores the configured retention window — the caller chooses
// the age cutoff — so it works even when the scheduled purge is disabled.
//
// days must be >= 0 (the HTTP layer rejects a negative value); a negative value
// here would place the cutoff in the future and purge everything, so it is
// clamped to zero. days == 0 sets the cutoff at now, which matches every
// archived photo — equivalent to EmptyTrash. It returns the count purged and
// failed, with per-photo failures left in the trash for a later retry.
func (s *Service) PurgeOlderThan(ctx context.Context, projectUID string, days int, meta audit.Meta) (Result, error) {
	if days < 0 {
		days = 0
	}
	cutoff := time.Now().Add(-time.Duration(days) * hoursPerDay * time.Hour)
	return s.purgeArchived(ctx, projectUID, &cutoff, meta, sourcePurgeOlder)
}

// PurgeExpired permanently deletes, in every active project, each archived
// photo whose archived_at is older than the configured retention period. It
// lists the projects and sweeps one project's trash at a time — a deactivated
// project is skipped, its data kept as it is — and returns the counts summed
// over all of them. It runs without an HTTP actor, so each purge is audited
// against a system actor (empty ActorUID) under the photo's project. When
// retention is disabled (RetentionDays <= 0) it is a no-op returning a zero
// Result. A project whose sweep fails is logged and the next one still runs;
// only a listing failure or a cancelled context ends the pass early.
func (s *Service) PurgeExpired(ctx context.Context) (Result, error) {
	if s.retentionDays <= 0 {
		return Result{}, nil
	}
	cutoff := time.Now().Add(-time.Duration(s.retentionDays) * hoursPerDay * time.Hour)
	projects, err := s.projects.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("trash: listing projects: %w", err)
	}
	var total Result
	for _, p := range project.Active(projects) {
		res, err := s.purgeArchived(ctx, p.UID, &cutoff, audit.Meta{}, sourceRetention)
		total.Purged += res.Purged
		total.Failed += res.Failed
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return total, fmt.Errorf("trash: purge interrupted: %w", ctxErr)
			}
			s.logger.Printf("trash: retention purge of project %s failed: %v", p.UID, err)
		}
	}
	return total, nil
}

// purgeArchived purges the project's archived photos in oldest-first batches
// until none remain in scope, attributing each purge to meta with the given
// source. before bounds the set (nil = all archived). Failed photos are skipped
// via a growing offset so the loop always converges, and a cancelled context
// stops it promptly.
func (s *Service) purgeArchived(
	ctx context.Context, projectUID string, before *time.Time, meta audit.Meta, source string,
) (Result, error) {
	var res Result
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("trash: purge interrupted: %w", err)
		}
		uids, err := s.photos.ListArchivedUIDs(ctx, projectUID, before, s.batchSize, offset)
		if err != nil {
			return res, fmt.Errorf("trash: listing archived photos: %w", err)
		}
		if len(uids) == 0 {
			return res, nil
		}
		if err := s.purgeBatch(ctx, projectUID, uids, &res, &offset, meta, source); err != nil {
			return res, err
		}
	}
}

// purgeBatch purges one batch of the project's UIDs, accumulating counts into
// res and attributing each purge to meta with the given source. A per-photo
// failure is logged, counted and skipped (offset is advanced so the failed UID
// is stepped past on the next batch, keeping the loop convergent); only a
// cancelled context aborts the batch.
func (s *Service) purgeBatch(
	ctx context.Context, projectUID string, uids []string, res *Result, offset *int, meta audit.Meta, source string,
) error {
	for _, uid := range uids {
		if err := s.purgeOne(ctx, projectUID, uid, meta, source); err != nil {
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
// with source, recorded under the project) in the same transaction as the row
// deletion. The artifacts are found through the photo's own file rows — every
// key comes from a row's file_path or file_hash, never from the project — so a
// photo is cleaned up wherever its rows say it lives. Artifacts are removed
// first so an interrupted purge leaves a re-purgeable orphan row rather than
// dangling files; a hard artifact error aborts before the row is deleted so the
// next run retries it.
//
// With a MoveLock it all runs under the photo's storage-move lock: a purge and a
// storage move never interleave on one photo, so the purge deletes the objects
// of the key the row has once the move is through with it, and a move that has
// copied the original but not committed finds the row gone and deletes its
// copy. A photo a crashed move has yet to finish is not purged now (an error
// wrapping storekeys.ErrMoving); a later purge — the retention's next run, or
// the user's retry — takes it once the move has.
func (s *Service) purgeOne(ctx context.Context, projectUID, uid string, meta audit.Meta, source string) error {
	if s.moves == nil {
		return s.purgeUnlocked(ctx, projectUID, uid, meta, source)
	}
	//nolint:wrapcheck // the lock wraps its own errors and returns purgeUnlocked's as they are.
	return s.moves.Purge(ctx, projectUID, uid, func() error {
		return s.purgeUnlocked(ctx, projectUID, uid, meta, source)
	})
}

// purgeUnlocked is purgeOne once any storage move is kept off the photo.
func (s *Service) purgeUnlocked(ctx context.Context, projectUID, uid string, meta audit.Meta, source string) error {
	files, err := s.photos.ListFiles(ctx, projectUID, uid)
	if err != nil {
		return fmt.Errorf("listing files: %w", err)
	}
	for _, file := range files {
		if err := s.deleteArtifacts(ctx, file); err != nil {
			return err
		}
	}
	entry := meta.Entry(audit.ActionPhotoPurge, "photos", uid, map[string]any{"source": source})
	if err := s.photos.DeleteAudited(ctx, projectUID, uid, entry); err != nil {
		return fmt.Errorf("deleting row: %w", err)
	}
	return nil
}

// deleteArtifacts removes the on-disk original, its metadata sidecar, the cached
// thumbnails, the storyboard sprite, the HLS segments and (when configured) the
// remote backup object for a single stored file. A missing original is ignored;
// a malformed sidecar hash skips thumbnail removal rather than failing the purge.
// Every derived object is looked for under the storage prefix of the file's own
// original.
func (s *Service) deleteArtifacts(ctx context.Context, file photos.PhotoFile) error {
	prefix := storekeys.PrefixOf(file.FilePath)
	if err := s.storage.Delete(ctx, file.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting original %s: %w", file.FilePath, err)
	}
	if err := s.deleteSidecar(ctx, file.FilePath); err != nil {
		return err
	}
	if err := s.thumbnailer.Remove(prefix, file.FileHash); err != nil && !errors.Is(err, thumb.ErrInvalidHash) {
		return fmt.Errorf("removing thumbnails for %s: %w", file.FileHash, err)
	}
	if err := s.deleteStoryboard(prefix, file.FileHash); err != nil {
		return err
	}
	if err := s.deleteHLS(ctx, prefix, file.FileHash); err != nil {
		return err
	}
	if s.remote != nil {
		if err := s.remote.Remove(ctx, file.FilePath); err != nil {
			return fmt.Errorf("removing remote object %s: %w", file.FilePath, err)
		}
	}
	return nil
}

// deleteStoryboard removes the purged file's cached scrub-preview sprite. It is a
// no-op when no storyboard store is wired, and a malformed hash (the same
// condition that skips thumbnail removal) is skipped rather than failing the
// purge: the sprite is regenerable derived media, and an unrelated hash problem
// must not strand the row.
func (s *Service) deleteStoryboard(prefix, fileHash string) error {
	if s.storyboards == nil {
		return nil
	}
	if err := s.storyboards.Remove(prefix, fileHash); err != nil && !errors.Is(err, storyboard.ErrInvalidHash) {
		return fmt.Errorf("removing storyboard for %s: %w", fileHash, err)
	}
	return nil
}

// deleteHLS removes every HLS object of the purged file: the initialisation and
// media segments of each rendition, all of them under the
// <storagePrefix>hls/<file_hash>/ prefix (see internal/hls), storagePrefix being
// that of the file's own original. They are derived media like the thumbnails, but they live
// in the object store rather than a local cache, and unlike a thumbnail their
// keys cannot be enumerated from the catalogue — how many segments a video was
// cut into, and in which renditions, is known only to the store. So they are
// found by listing the prefix that holds them.
//
// A backend that cannot list by prefix, and a malformed hash (the same condition
// that skips thumbnail removal), leave the segments behind rather than failing
// the purge: they are regenerable derived media, and an unrelated storage
// limitation must not strand a row the user asked to delete permanently. A
// listing or a deletion that does fail is reported, because that is the store
// refusing work it can do.
//
// The keys are collected before anything is deleted rather than deleted inside
// the listing callback: the filesystem backend lists by walking the directory
// tree it would be deleting from underneath itself.
func (s *Service) deleteHLS(ctx context.Context, storagePrefix, fileHash string) error {
	lister, ok := s.storage.(storage.PrefixLister)
	if !ok {
		return nil
	}
	prefix, err := hls.PrefixFor(storagePrefix, fileHash)
	if err != nil {
		// A malformed hash addresses no segments; there is nothing to clean up.
		return nil //nolint:nilerr // an unusable hash is not a purge failure
	}
	var keys []string
	if listErr := lister.KeysWithPrefix(ctx, prefix, func(key string) error {
		keys = append(keys, key)
		return nil
	}); listErr != nil {
		return fmt.Errorf("listing HLS objects under %s: %w", prefix, listErr)
	}
	for _, key := range keys {
		if delErr := s.storage.Delete(ctx, key); delErr != nil && !errors.Is(delErr, os.ErrNotExist) {
			return fmt.Errorf("deleting HLS object %s: %w", key, delErr)
		}
	}
	return nil
}

// deleteSidecar removes the purged original's metadata sidecar — the YAML file
// under the sidecars/ prefix holding its curation (see internal/sidecarexport).
//
// It is unconditional, deliberately: it runs even when the export is switched
// off, because what must be cleaned up is what is *there*, not what the current
// config would write. A sidecar left behind by a photo that no longer exists is
// not an untidy leftover but an active hazard — it is precisely the file a
// rebuild reads, and a rebuild that reads it resurrects a photo the user chose to
// delete permanently.
//
// A sidecar that is already absent is the normal case (the export may never have
// run) and is not an error.
func (s *Service) deleteSidecar(ctx context.Context, fileKey string) error {
	key, err := sidecarexport.KeyFor(fileKey)
	if err != nil {
		// A row with no file path has no sidecar either; nothing to clean up.
		return nil //nolint:nilerr // a keyless file is not a purge failure
	}
	if err := s.storage.Delete(ctx, key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting sidecar %s: %w", key, err)
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
