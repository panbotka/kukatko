// Package maintenance keeps a large, long-lived photo library consistent. It
// scans for drift between the database catalogue and the files on disk — photos
// whose original is missing, originals on disk with no catalogue row (orphans),
// photos missing thumbnails, and photos missing embeddings, faces or perceptual
// hashes — and repairs what it can: regenerating thumbnails and pHashes through
// the job queue, backfilling embeddings and faces, and optionally importing
// orphan originals into the catalogue.
//
// It mirrors photo-sorter's "cache build-thumbs" but is broader and safer:
// repairs run through the persistent job queue (bounded concurrency, resumable),
// every operation is idempotent, and it never deletes an original — reclaiming
// disk space is the trash/purge subsystem's job. Everything external sits behind
// an interface so the orchestration is unit-testable with fakes and no live
// database, filesystem or queue.
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/panbotka/kukatko/internal/photos"
)

// defaultSampleLimit is the number of affected identifiers retained per Finding
// when no SampleLimit is configured.
const defaultSampleLimit = 20

// representativeThumbSize is the thumbnail size whose presence the scan treats as
// proof a photo's thumbnails are cached. It is the grid tile size, generated for
// every photo, so its absence reliably signals an unprocessed or evicted cache.
const representativeThumbSize = "tile_224"

// PhotoCatalog is the subset of the photo catalogue the maintenance scan and
// repairs need. It is satisfied by *photos.Store.
type PhotoCatalog interface {
	// CountPhotos returns the total number of catalogued photos.
	CountPhotos(ctx context.Context) (int, error)
	// ListPrimaryFiles returns every photo's primary original file reference.
	ListPrimaryFiles(ctx context.Context) ([]photos.PrimaryFile, error)
	// ListFilePaths returns the storage key of every catalogued file.
	ListFilePaths(ctx context.Context) ([]string, error)
	// ListPhotosMissingPhash returns the uids of photos with no perceptual hashes.
	ListPhotosMissingPhash(ctx context.Context, limit int) ([]string, error)
}

// VectorCatalog is the subset of the embeddings/faces store the scan and the
// embedding/face backfills need. It is satisfied by *vectors.Store.
type VectorCatalog interface {
	// ListPhotosMissingEmbedding returns the uids of photos with no embedding.
	ListPhotosMissingEmbedding(ctx context.Context, limit int) ([]string, error)
	// ListPhotosMissingFaces returns the uids of photos with no face detection.
	ListPhotosMissingFaces(ctx context.Context, limit int) ([]string, error)
}

// OriginalStore reports whether a stored original is present on disk. It is the
// presence-check subset of storage.Storage (Stat).
type OriginalStore interface {
	// Stat returns file information for the original at relPath, or an error
	// wrapping os.ErrNotExist when it is absent.
	Stat(ctx context.Context, relPath string) (os.FileInfo, error)
}

// DiskFile is one regular file found under the originals root, keyed by its
// slash-separated path relative to the root.
type DiskFile struct {
	// Key is the storage key (slash path relative to the originals root).
	Key string
	// Size is the file's byte length.
	Size int64
}

// DiskScanner walks the originals root and lists the files actually on disk,
// backing the orphan-detection half of the scan and the orphan-import repair.
type DiskScanner interface {
	// List returns every original currently on disk.
	List(ctx context.Context) ([]DiskFile, error)
}

// ThumbChecker reports whether a photo's representative thumbnail is cached.
type ThumbChecker interface {
	// HasThumbnail reports whether the representative thumbnail for fileHash exists
	// in the cache.
	HasThumbnail(fileHash string) (bool, error)
}

// Enqueuer schedules thumbnail regeneration jobs (which also recompute a missing
// pHash). It is satisfied by *jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueThumbnail schedules thumbnail regeneration for the photo identified by
	// photoUID, treating a pre-existing active job as a no-op.
	EnqueueThumbnail(ctx context.Context, photoUID string) error
}

// EmbedBackfiller enqueues image_embed jobs for photos missing an embedding. It
// is satisfied by *embedjob.Service.
type EmbedBackfiller interface {
	// BackfillEmbeddings enqueues an image_embed job per photo missing an embedding.
	BackfillEmbeddings(ctx context.Context) (int, error)
}

// FaceBackfiller enqueues face_detect jobs for photos missing face detection. It
// is satisfied by *facejob.Service.
type FaceBackfiller interface {
	// BackfillFaces enqueues a face_detect job per unprocessed photo.
	BackfillFaces(ctx context.Context) (int, error)
}

// ImportOutcome classifies what happened when an orphan original was imported.
type ImportOutcome int

const (
	// ImportCreated means the orphan was catalogued as a new photo.
	ImportCreated ImportOutcome = iota
	// ImportDuplicate means the orphan's content was already catalogued.
	ImportDuplicate
)

// OrphanImporter catalogues an orphan original already on disk by running it
// through the upload pipeline. A nil importer disables the orphan-import repair.
type OrphanImporter interface {
	// ImportOriginal catalogues the original at key, reporting whether a new photo
	// was created or the content was already catalogued.
	ImportOriginal(ctx context.Context, key string) (ImportOutcome, error)
}

// Config bundles the collaborators and tunables of a Service. Every interface is
// required except OrphanImporter (nil disables orphan import). A non-positive
// SampleLimit uses defaultSampleLimit.
type Config struct {
	Photos      PhotoCatalog
	Vectors     VectorCatalog
	Originals   OriginalStore
	Disk        DiskScanner
	Thumbs      ThumbChecker
	Enqueuer    Enqueuer
	Embed       EmbedBackfiller
	Faces       FaceBackfiller
	Importer    OrphanImporter
	SampleLimit int
}

// Service runs library integrity scans and repairs over the injected
// collaborators.
type Service struct {
	photos      PhotoCatalog
	vectors     VectorCatalog
	originals   OriginalStore
	disk        DiskScanner
	thumbs      ThumbChecker
	enqueuer    Enqueuer
	embed       EmbedBackfiller
	faces       FaceBackfiller
	importer    OrphanImporter
	sampleLimit int
}

// New returns a Service from cfg. It panics if any required collaborator is nil,
// since a scan needs all of them; the optional OrphanImporter may be nil.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Vectors == nil || cfg.Originals == nil ||
		cfg.Disk == nil || cfg.Thumbs == nil || cfg.Enqueuer == nil ||
		cfg.Embed == nil || cfg.Faces == nil {
		panic("maintenance: Photos, Vectors, Originals, Disk, Thumbs, Enqueuer, Embed and Faces are required")
	}
	limit := cfg.SampleLimit
	if limit <= 0 {
		limit = defaultSampleLimit
	}
	return &Service{
		photos:      cfg.Photos,
		vectors:     cfg.Vectors,
		originals:   cfg.Originals,
		disk:        cfg.Disk,
		thumbs:      cfg.Thumbs,
		enqueuer:    cfg.Enqueuer,
		embed:       cfg.Embed,
		faces:       cfg.Faces,
		importer:    cfg.Importer,
		sampleLimit: limit,
	}
}

// Scan reconciles the catalogue against the files on disk and the derived data,
// returning a Report of every problem class with counts and bounded samples. It
// is read-only and safe to run at any time.
func (s *Service) Scan(ctx context.Context) (Report, error) {
	photoCount, err := s.photos.CountPhotos(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("maintenance: counting photos: %w", err)
	}
	missingOriginals, missingThumbs, err := s.scanFiles(ctx)
	if err != nil {
		return Report{}, err
	}
	orphans, filesInDB, originalsOnDisk, err := s.scanOrphans(ctx)
	if err != nil {
		return Report{}, err
	}
	derived, err := s.scanDerived(ctx)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Photos:            photoCount,
		FilesInDB:         filesInDB,
		OriginalsOnDisk:   originalsOnDisk,
		MissingOriginals:  missingOriginals,
		OrphanFiles:       findingFrom(orphans, s.sampleLimit),
		MissingThumbnails: missingThumbs,
		MissingEmbeddings: derived.embeddings,
		MissingFaces:      derived.faces,
		MissingPhashes:    derived.phashes,
	}, nil
}

// scanFiles iterates every photo's primary original, checking the original's
// presence on disk and its representative thumbnail in the cache, returning the
// missing-original and missing-thumbnail findings.
func (s *Service) scanFiles(ctx context.Context) (missingOriginals, missingThumbs Finding, err error) {
	primary, err := s.photos.ListPrimaryFiles(ctx)
	if err != nil {
		return Finding{}, Finding{}, fmt.Errorf("maintenance: listing primary files: %w", err)
	}
	origs := newFindingCollector(s.sampleLimit)
	thumbs := newFindingCollector(s.sampleLimit)
	for _, pf := range primary {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Finding{}, Finding{}, fmt.Errorf("maintenance: scan interrupted: %w", ctxErr)
		}
		present, statErr := s.originalPresent(ctx, pf.FilePath)
		if statErr != nil {
			return Finding{}, Finding{}, statErr
		}
		if !present {
			origs.add(pf.PhotoUID)
		}
		cached, thumbErr := s.thumbs.HasThumbnail(pf.FileHash)
		if thumbErr != nil {
			return Finding{}, Finding{}, fmt.Errorf("maintenance: checking thumbnail for %s: %w", pf.PhotoUID, thumbErr)
		}
		if !cached {
			thumbs.add(pf.PhotoUID)
		}
	}
	return origs.finding(), thumbs.finding(), nil
}

// originalPresent reports whether the original at relPath exists on disk,
// treating an os.ErrNotExist Stat as absent rather than an error.
func (s *Service) originalPresent(ctx context.Context, relPath string) (bool, error) {
	if _, err := s.originals.Stat(ctx, relPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("maintenance: statting original %s: %w", relPath, err)
	}
	return true, nil
}

// scanOrphans lists the catalogued file keys and the files on disk and returns
// the orphan keys (on disk, not catalogued) plus the two totals.
func (s *Service) scanOrphans(ctx context.Context) (orphans []string, filesInDB, originalsOnDisk int, err error) {
	dbPaths, err := s.photos.ListFilePaths(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("maintenance: listing catalogued files: %w", err)
	}
	diskFiles, err := s.disk.List(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("maintenance: listing originals on disk: %w", err)
	}
	diskKeys := make([]string, len(diskFiles))
	for i, f := range diskFiles {
		diskKeys[i] = f.Key
	}
	return orphanKeys(dbPaths, diskKeys), len(dbPaths), len(diskKeys), nil
}

// derivedFindings groups the three derived-data findings produced from list
// queries.
type derivedFindings struct {
	embeddings Finding
	faces      Finding
	phashes    Finding
}

// scanDerived lists the photos missing each kind of derived data (embeddings,
// faces, perceptual hashes) and turns each list into a Finding.
func (s *Service) scanDerived(ctx context.Context) (derivedFindings, error) {
	embUIDs, err := s.vectors.ListPhotosMissingEmbedding(ctx, 0)
	if err != nil {
		return derivedFindings{}, fmt.Errorf("maintenance: listing photos missing embedding: %w", err)
	}
	faceUIDs, err := s.vectors.ListPhotosMissingFaces(ctx, 0)
	if err != nil {
		return derivedFindings{}, fmt.Errorf("maintenance: listing photos missing faces: %w", err)
	}
	phashUIDs, err := s.photos.ListPhotosMissingPhash(ctx, 0)
	if err != nil {
		return derivedFindings{}, fmt.Errorf("maintenance: listing photos missing phash: %w", err)
	}
	return derivedFindings{
		embeddings: findingFrom(embUIDs, s.sampleLimit),
		faces:      findingFrom(faceUIDs, s.sampleLimit),
		phashes:    findingFrom(phashUIDs, s.sampleLimit),
	}, nil
}
