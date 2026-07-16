// Package ingest implements Kukátko's upload pipeline: it turns an uploaded
// byte stream into a catalogued photo with an on-disk original, perceptual
// hashes, generated thumbnails and (eventually) queued background work.
//
// The pipeline per file is: stream to a temp file while computing the SHA256
// content hash; short-circuit known exact duplicates; extract EXIF/GPS
// metadata; publish the original into the storage layout (YYYY/MM); insert the
// photos + primary photo_files rows; compute and store the pHash/dHash; render
// thumbnails; and enqueue embedding/face jobs. Identity is the SHA256 hash, so
// concurrent uploads of identical bytes converge on a single photo: the storage
// layer hard-links to one file and the photos.file_hash unique constraint lets
// exactly one INSERT win, the loser reporting a clean duplicate.
//
// Everything streams — files are never buffered whole in memory — and every
// per-file failure is captured in a FileResult rather than aborting the batch,
// so a multi-file upload reports mixed created/duplicate/error outcomes cleanly.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	// Register the pure-Go image decoders so image.Decode handles the formats
	// the pipeline hashes directly; HEIC/RAW are pre-converted by imgconvert.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/phash"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sidecar"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/video"
)

// ErrFileTooLarge indicates an uploaded file exceeded the configured maximum
// size and was rejected without being catalogued.
var ErrFileTooLarge = errors.New("ingest: file exceeds maximum upload size")

// Warning codes attached to a per-file result. They flag non-fatal conditions
// (the photo was still created) such as a near-duplicate match or a failed but
// regenerable side effect.
const (
	// warnNearDuplicate marks a photo whose pHash is within the configured
	// distance of an existing photo.
	warnNearDuplicate = "near_duplicate"
	// warnPhashFailed marks a photo whose perceptual hashes could not be
	// computed or stored (it has no near-duplicate protection until reprocessed).
	warnPhashFailed = "phash_failed"
	// warnThumbnailFailed marks a photo whose thumbnails could not be generated;
	// the cache is regenerable so the original and record are intact.
	warnThumbnailFailed = "thumbnail_failed"
	// warnEnqueueFailed marks a photo whose background jobs could not be queued.
	warnEnqueueFailed = "enqueue_failed"
)

// Outcome classifies what happened to one uploaded file.
type Outcome string

const (
	// OutcomeCreated means a new photo was catalogued.
	OutcomeCreated Outcome = "created"
	// OutcomeDuplicate means the file's content was already catalogued; nothing
	// new was created.
	OutcomeDuplicate Outcome = "duplicate"
	// OutcomeError means the file could not be ingested.
	OutcomeError Outcome = "error"
)

// Warning is a non-fatal condition reported alongside a created photo.
type Warning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	PhotoUID string `json:"photo_uid,omitempty"`
}

// FileResult is the outcome of ingesting one uploaded file. Status carries
// HTTP-style per-file semantics (201 created, 409 duplicate, 413/500 error) so
// a batch upload reports each file's fate without failing the whole request.
type FileResult struct {
	Filename string    `json:"filename"`
	Status   int       `json:"status"`
	Outcome  Outcome   `json:"outcome"`
	PhotoUID string    `json:"photo_uid,omitempty"`
	Error    string    `json:"error,omitempty"`
	Warnings []Warning `json:"warnings,omitempty"`
}

// Config bundles the collaborators and tunables a Service needs.
type Config struct {
	// Storage publishes originals and resolves their absolute paths (required).
	Storage storage.Storage
	// Photos is the catalogue repository (required).
	Photos *photos.Store
	// Thumbnailer renders derived images (required).
	Thumbnailer *thumb.Thumbnailer
	// Enqueuer schedules background jobs; nil means NopEnqueuer (no queue yet).
	Enqueuer JobEnqueuer
	// Sidecar schedules the freshly catalogued photo's metadata sidecar. A nil
	// Sidecar — the export switched off — schedules none, and the upload still
	// succeeds.
	Sidecar SidecarEnqueuer
	// Duplicate gates and tunes near-duplicate warnings.
	Duplicate config.DuplicateConfig
	// MaxFileSize caps a single uploaded file in bytes; 0 means unlimited.
	MaxFileSize int64
	// TempDir is where uploads are streamed before publishing; "" uses the OS
	// temp directory.
	TempDir string
}

// Service runs the ingest pipeline. It is safe for concurrent use: each Ingest
// call owns its own temp file and the storage and catalogue layers handle
// concurrent identical content race-free.
type Service struct {
	storage     storage.Storage
	photos      *photos.Store
	thumbs      *thumb.Thumbnailer
	enqueuer    JobEnqueuer
	sidecar     SidecarEnqueuer
	dup         config.DuplicateConfig
	maxFileSize int64
	tempDir     string
}

// New returns a Service from cfg, defaulting the enqueuer to NopEnqueuer.
func New(cfg Config) *Service {
	enq := cfg.Enqueuer
	if enq == nil {
		enq = NopEnqueuer{}
	}
	return &Service{
		storage:     cfg.Storage,
		photos:      cfg.Photos,
		thumbs:      cfg.Thumbnailer,
		enqueuer:    enq,
		sidecar:     cfg.Sidecar,
		dup:         cfg.Duplicate,
		maxFileSize: cfg.MaxFileSize,
		tempDir:     cfg.TempDir,
	}
}

// Request describes one file handed to the pipeline.
type Request struct {
	// Filename is the name the file arrived under; it seeds the stored file name
	// and, for a file with no EXIF date, the capture-time guess.
	Filename string
	// UploadedBy is the UID of the authenticated uploader (empty for
	// anonymous/system imports).
	UploadedBy string
	// Sidecar is the metadata an export wrote *beside* the file (a Google Takeout
	// JSON, an XMP), already read by the caller; nil when the file has none. It
	// fills what the file's own EXIF does not carry — which, for a Takeout export
	// whose EXIF was stripped in re-encoding, is the capture date, the caption and
	// the GPS fix. See internal/sidecar for the precedence rules.
	Sidecar *sidecar.Metadata
}

// Ingest runs the pipeline for one uploaded file with no sidecar — the plain
// upload. See IngestFile for the full form.
func (s *Service) Ingest(ctx context.Context, src io.Reader, filename, uploadedBy string) FileResult {
	return s.IngestFile(ctx, src, Request{Filename: filename, UploadedBy: uploadedBy})
}

// IngestFile runs the full pipeline for one file read from src and returns a
// per-file result. It never returns an error: every failure is captured in the
// FileResult so batch callers can report mixed outcomes.
func (s *Service) IngestFile(ctx context.Context, src io.Reader, req Request) FileResult {
	staged, err := s.stage(ctx, src)
	if err != nil {
		return errorResult(req.Filename, err)
	}
	defer staged.cleanup()

	if dup, ok := s.existingDuplicate(ctx, req.Filename, staged.hash); ok {
		return dup
	}

	media, err := extractMedia(ctx, staged.path, req.Filename)
	if err != nil {
		return errorResult(req.Filename, err)
	}
	applySidecar(&media, req.Sidecar)

	stored, err := s.storeOriginal(ctx, staged, req.Filename, media.shared.TakenAt)
	if err != nil {
		return errorResult(req.Filename, err)
	}

	photo, dup, err := s.catalogue(ctx, req.Filename, stored, media, req.UploadedBy)
	if err != nil {
		return errorResult(req.Filename, err)
	}
	if dup != nil {
		return *dup
	}

	return createdResult(req.Filename, photo.UID, s.postProcess(ctx, photo))
}

// applySidecar folds the file's sidecar (when it has one) into the metadata read
// from the file itself, before anything is stored: the merged capture time is
// what decides the YYYY/MM the original is published under, so a Takeout photo
// with stripped EXIF lands in the month it was taken rather than the month it
// was imported.
func applySidecar(media *mediaMeta, sc *sidecar.Metadata) {
	if sc == nil {
		return
	}
	media.sidecar = sc
	sidecar.Apply(&media.shared, *sc)
}

// stagedFile is an upload streamed to a temp file, with its content hash and
// size computed during the copy.
type stagedFile struct {
	path string
	hash string
	size int64
}

// cleanup removes the temp file; it is safe to defer immediately after staging.
func (f *stagedFile) cleanup() {
	if f != nil && f.path != "" {
		//nolint:gosec // G703: f.path is our own os.CreateTemp file, not user-controlled input.
		_ = os.Remove(f.path)
	}
}

// stage streams src into a temp file under the service temp directory while
// computing its SHA256 hash and byte count, never buffering the whole file in
// memory. It enforces MaxFileSize (when set) and returns ErrFileTooLarge for an
// oversized upload, removing the partial temp file.
func (s *Service) stage(ctx context.Context, src io.Reader) (*stagedFile, error) {
	tmp, err := os.CreateTemp(s.tempDir, "kukatko-ingest-*")
	if err != nil {
		return nil, fmt.Errorf("ingest: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	reader := ctxReader(ctx, src)
	if s.maxFileSize > 0 {
		reader = io.LimitReader(reader, s.maxFileSize+1)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), reader)
	closeErr := tmp.Close()
	if err := firstErr(copyErr, closeErr); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("ingest: streaming upload: %w", err)
	}
	if s.maxFileSize > 0 && size > s.maxFileSize {
		_ = os.Remove(tmpPath)
		return nil, ErrFileTooLarge
	}
	return &stagedFile{path: tmpPath, hash: hex.EncodeToString(hasher.Sum(nil)), size: size}, nil
}

// existingDuplicate reports whether a photo with this content hash is already
// catalogued, returning a ready duplicate result if so. It is an optimisation:
// the authoritative dedup is the file_hash unique constraint at insert time,
// which also covers the concurrent-upload race this check cannot see.
func (s *Service) existingDuplicate(ctx context.Context, filename, hash string) (FileResult, bool) {
	existing, err := s.photos.GetByFileHash(ctx, hash)
	if err != nil {
		return FileResult{}, false
	}
	return duplicateResult(filename, existing.UID), true
}

// storeOriginal publishes the staged temp file into the storage layout under
// the photo's capture month (or the import month when unknown). A storage
// ErrAlreadyExists is treated as success: the byte-identical file is already in
// place and the catalogue step decides whether it is a duplicate.
func (s *Service) storeOriginal(
	ctx context.Context, staged *stagedFile, filename string, takenAt *time.Time,
) (storage.StoredFile, error) {
	file, err := os.Open(staged.path) //nolint:gosec // G304: staged.path is our own freshly created temp file.
	if err != nil {
		return storage.StoredFile{}, fmt.Errorf("ingest: reopening staged file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var when time.Time
	if takenAt != nil {
		when = *takenAt
	}
	stored, err := s.storage.Store(ctx, file, when, filename)
	if err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		return storage.StoredFile{}, fmt.Errorf("ingest: storing original: %w", err)
	}
	return stored, nil
}

// catalogue inserts the photos row and its primary photo_files row. When the
// file_hash unique constraint rejects the insert (a pre-existing or concurrently
// inserted duplicate), it returns a duplicate FileResult via the second return
// value instead of an error; the photo is returned only on a genuine create.
func (s *Service) catalogue(
	ctx context.Context, filename string, stored storage.StoredFile, media mediaMeta, uploadedBy string,
) (photos.Photo, *FileResult, error) {
	created, err := s.photos.Create(ctx, buildPhoto(stored, media, filename, uploadedBy))
	if errors.Is(err, photos.ErrFileHashTaken) {
		dup := s.resolveRace(ctx, filename, stored)
		return photos.Photo{}, &dup, nil
	}
	if err != nil {
		return photos.Photo{}, nil, fmt.Errorf("ingest: cataloguing photo: %w", err)
	}

	if err := s.createPrimaryFile(ctx, created, stored); err != nil {
		// Roll back the orphaned photo row; the on-disk original is a harmless
		// content-addressed file a later sweep can reclaim.
		_ = s.photos.Delete(ctx, created.UID)
		return photos.Photo{}, nil, err
	}
	return created, nil, nil
}

// resolveRace turns a lost insert race (or a late-discovered duplicate) into a
// clean duplicate result. If this upload published its own distinct hard link
// (a different filename than the winning photo's), that now-unreferenced link
// is removed; a shared path is left untouched so the winner's file survives.
func (s *Service) resolveRace(ctx context.Context, filename string, stored storage.StoredFile) FileResult {
	existing, err := s.photos.GetByFileHash(ctx, stored.Hash)
	if err != nil {
		return duplicateResult(filename, "")
	}
	if existing.FilePath != stored.RelPath {
		_ = s.storage.Delete(ctx, stored.RelPath)
	}
	return duplicateResult(filename, existing.UID)
}

// createPrimaryFile inserts the original file as the photo's primary
// photo_files row.
func (s *Service) createPrimaryFile(ctx context.Context, photo photos.Photo, stored storage.StoredFile) error {
	_, err := s.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID:  photo.UID,
		FilePath:  stored.RelPath,
		FileHash:  stored.Hash,
		FileSize:  stored.Size,
		FileMime:  photo.FileMime,
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	})
	if err != nil {
		return fmt.Errorf("ingest: creating primary file: %w", err)
	}
	return nil
}

// postProcess runs the regenerable side effects for a freshly created photo —
// perceptual hashing (with near-duplicate detection), thumbnail generation and
// job enqueue — collecting any non-fatal failures as warnings. None of these
// undo the create: a photo with a missing thumbnail or unqueued job is a
// degraded but valid, repairable state.
func (s *Service) postProcess(ctx context.Context, photo photos.Photo) []Warning {
	warnings := slices.Concat(
		s.computePhash(ctx, photo),
		s.generateThumbnails(ctx, photo),
		s.enqueueJobs(ctx, photo.UID),
	)
	if len(warnings) == 0 {
		return nil
	}
	return warnings
}

// computePhash decodes the stored original, checks it against existing photos
// for a near-duplicate, and stores its pHash/dHash. A decode or store failure
// is reported as a warning, not an error.
func (s *Service) computePhash(ctx context.Context, photo photos.Photo) []Warning {
	img, cleanup, err := s.decodeOriginal(ctx, photo)
	if err != nil {
		return []Warning{{Code: warnPhashFailed, Message: err.Error()}}
	}
	defer cleanup()

	hashes := phash.Compute(img)
	warnings := s.nearDuplicateWarning(ctx, hashes.Phash)
	if err := s.photos.SetPhash(ctx, photos.Phash{
		PhotoUID: photo.UID, Phash: hashes.Phash, Dhash: hashes.Dhash,
	}); err != nil {
		warnings = append(warnings, Warning{Code: warnPhashFailed, Message: err.Error()})
	}
	return warnings
}

// decodeOriginal resolves the photo's stored original to a decodable image,
// shelling out via imgconvert for HEIC/RAW and decoding pure-Go formats
// directly. The returned cleanup releases the materialized original along with
// any intermediate file, and must be deferred by the caller.
func (s *Service) decodeOriginal(ctx context.Context, photo photos.Photo) (image.Image, func(), error) {
	abs, releaseOriginal, err := s.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest: materializing original: %w", err)
	}
	decPath, releaseDecoded, err := imgconvert.EnsureDecodable(ctx, abs)
	if err != nil {
		releaseOriginal()
		return nil, nil, fmt.Errorf("ingest: ensuring decodable: %w", err)
	}
	// The decoded file may be derived from the original, so drop it first.
	cleanup := func() { releaseDecoded(); releaseOriginal() }

	file, err := os.Open(decPath) //nolint:gosec // G304: decPath comes from storage/imgconvert, not user input.
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("ingest: opening original: %w", err)
	}
	img, _, err := image.Decode(file)
	_ = file.Close()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("ingest: decoding original: %w", err)
	}
	return img, cleanup, nil
}

// nearDuplicateWarning returns a near-duplicate warning when detection is
// enabled and an existing photo's pHash lies within the configured distance.
// It is called before the new photo's own pHash is stored, so it never matches
// the photo against itself.
func (s *Service) nearDuplicateWarning(ctx context.Context, ph int64) []Warning {
	if !s.dup.Enabled {
		return nil
	}
	uid, distance, err := s.photos.NearestPhash(ctx, ph)
	if err != nil || distance > s.dup.PhashMaxDiff {
		return nil
	}
	return []Warning{{
		Code:     warnNearDuplicate,
		PhotoUID: uid,
		Message:  fmt.Sprintf("possible near-duplicate of %s (pHash distance %d)", uid, distance),
	}}
}

// generateThumbnails renders every registered thumbnail size for the photo,
// reporting a single warning if generation fails.
func (s *Service) generateThumbnails(ctx context.Context, photo photos.Photo) []Warning {
	if _, err := s.thumbs.GenerateAll(ctx, photo); err != nil {
		return []Warning{{Code: warnThumbnailFailed, Message: err.Error()}}
	}
	return nil
}

// enqueueJobs schedules the image-embedding, face-detection and metadata-sidecar
// jobs, reporting a warning per failed enqueue. With the default NopEnqueuer the
// first two always succeed; a nil Sidecar skips the third.
//
// The sidecar is scheduled for a photo that has no curation yet on purpose: it
// gives the photo a file from the moment it is catalogued, so a library that is
// imported and never touched again is still described on disk rather than only in
// a database. The alternative — wait for the first edit — leaves exactly the
// never-edited photos, the ones nobody would notice were undescribed, with
// nothing.
func (s *Service) enqueueJobs(ctx context.Context, photoUID string) []Warning {
	var warnings []Warning
	if err := s.enqueuer.EnqueueImageEmbed(ctx, photoUID); err != nil {
		warnings = append(warnings, Warning{Code: warnEnqueueFailed, Message: err.Error()})
	}
	if err := s.enqueuer.EnqueueFaceDetect(ctx, photoUID); err != nil {
		warnings = append(warnings, Warning{Code: warnEnqueueFailed, Message: err.Error()})
	}
	if s.sidecar != nil {
		if err := s.sidecar.EnqueueSidecar(ctx, photoUID); err != nil {
			warnings = append(warnings, Warning{Code: warnEnqueueFailed, Message: err.Error()})
		}
	}
	return warnings
}

// mediaMeta is the unified metadata an uploaded file contributes to its
// photos.Photo: the shared (image-shaped) fields plus, for videos, the
// video-only extras. It lets a single buildPhoto handle both kinds.
type mediaMeta struct {
	// kind is the media type written to photos.media_type.
	kind photos.MediaType
	// shared carries the fields common to images and videos (capture time, GPS,
	// dimensions, MIME, raw metadata document). A sidecar has already been folded
	// into it by applySidecar.
	shared exif.Metadata
	// video is non-nil only for videos and carries the video-only fields.
	video *videoFields
	// sidecar is the export metadata found beside the file, nil when there is
	// none. It carries the caption fields, which have no EXIF counterpart here.
	sidecar *sidecar.Metadata
}

// videoFields holds the video-only metadata probed from the container.
type videoFields struct {
	durationMs *int
	videoCodec string
	audioCodec string
	hasAudio   bool
	fps        *float64
}

// extractMedia reads the metadata for an uploaded file, dispatching on whether
// filename names a video. Images take the EXIF path; videos are probed via
// ffprobe/exiftool and require ffmpeg for poster extraction, so a missing ffmpeg
// is reported as an error (the only fatal case — see extractVideoMedia).
func extractMedia(ctx context.Context, path, filename string) (mediaMeta, error) {
	if !video.IsVideoPath(filename) {
		return mediaMeta{kind: photos.MediaImage, shared: extractMeta(ctx, path)}, nil
	}
	return extractVideoMedia(ctx, path, filename)
}

// extractMeta reads EXIF/GPS metadata from the staged file, degrading to an
// empty (unknown-source) Metadata when extraction fails so a file with no EXIF
// is never a reason to reject an upload.
func extractMeta(ctx context.Context, path string) exif.Metadata {
	meta, err := exif.Extract(ctx, path)
	if err != nil {
		return exif.Metadata{TakenAtSource: exif.SourceUnknown}
	}
	return meta
}

// extractVideoMedia probes a video's metadata. It requires ffmpeg (the poster
// frame, generated downstream by the thumbnailer/hasher via imgconvert, has no
// fallback) and returns a clear ErrFFmpegMissing-wrapped error when it is
// absent. A metadata probe failure is non-fatal: the video is still catalogued
// with whatever could be resolved (capture time falls back to the filename).
func extractVideoMedia(ctx context.Context, path, filename string) (mediaMeta, error) {
	if !video.FFmpegAvailable() {
		return mediaMeta{}, fmt.Errorf("ingest: video %q: %w", filename, video.ErrFFmpegMissing)
	}
	vm, err := video.Probe(ctx, path)
	if err != nil {
		vm = video.Metadata{}
	}
	return mediaMeta{
		kind:   photos.MediaVideo,
		shared: sharedFromVideo(vm, filename),
		video: &videoFields{
			durationMs: vm.DurationMs,
			videoCodec: vm.VideoCodec,
			audioCodec: vm.AudioCodec,
			hasAudio:   vm.HasAudio,
			fps:        vm.FPS,
		},
	}, nil
}

// sharedFromVideo maps a video.Metadata onto the shared exif.Metadata fields,
// resolving the capture time from the container creation time and falling back
// to the original upload filename (the staged temp file has no usable name).
func sharedFromVideo(vm video.Metadata, filename string) exif.Metadata {
	meta := exif.Metadata{
		TakenAt:  vm.TakenAt,
		Lat:      vm.Lat,
		Lng:      vm.Lng,
		Altitude: vm.Altitude,
		Width:    vm.Width,
		Height:   vm.Height,
		Mime:     vm.Mime,
		Exif:     vm.Raw,
	}
	switch {
	case meta.TakenAt != nil:
		meta.TakenAtSource = exif.SourceExif
	default:
		if when, ok := exif.FilenameTakenAt(filename); ok {
			meta.TakenAt = when
			meta.TakenAtSource = exif.SourceFilename
		} else {
			meta.TakenAtSource = exif.SourceUnknown
		}
	}
	return meta
}

// buildPhoto maps a stored file and its extracted metadata onto a photos.Photo
// ready for insertion. filename is the name the upload arrived under, kept as the
// photo's original_name: the storage layout renames the file, and that name is the
// only trace of what the user (or the exporting app) called it. The UID and
// timestamps are assigned by the database.
func buildPhoto(stored storage.StoredFile, media mediaMeta, filename, uploadedBy string) photos.Photo {
	meta := media.shared
	kind := media.kind
	if kind == "" {
		kind = photos.MediaImage
	}
	mime := chooseMIME(meta.Mime, stored.MIME)
	extracted := time.Now().UTC()
	p := photos.Photo{
		FileHash:        stored.Hash,
		FilePath:        stored.RelPath,
		FileName:        path.Base(stored.RelPath),
		FileSize:        stored.Size,
		FileMime:        mime,
		FileWidth:       meta.Width,
		FileHeight:      meta.Height,
		FileOrientation: orientationOrDefault(meta.Orientation),
		MediaType:       kind,
		TakenAt:         meta.TakenAt,
		TakenAtSource:   takenAtSource(meta.TakenAtSource),
		Lat:             meta.Lat,
		Lng:             meta.Lng,
		Altitude:        meta.Altitude,
		LocationSource:  locationSource(meta.Lat, meta.Lng),
		CameraMake:      meta.CameraMake,
		CameraModel:     meta.CameraModel,
		LensModel:       meta.LensModel,
		ISO:             meta.ISO,
		Aperture:        meta.Aperture,
		Exposure:        meta.Exposure,
		FocalLength:     meta.FocalLength,
		OriginalName:    originalName(filename),
		Exif:            marshalExif(meta.Exif),
		// The file's own metadata is read here and nowhere else in the pipeline, so
		// this photo is done: the metadata backfill has nothing left to do for it.
		MetadataExtractedAt: &extracted,
	}
	applyFileMetadata(&p, meta, kind, mime)
	if media.video != nil {
		p.DurationMs = media.video.durationMs
		p.VideoCodec = media.video.videoCodec
		p.AudioCodec = media.video.audioCodec
		p.HasAudio = media.video.hasAudio
		p.FPS = media.video.fps
	}
	if media.sidecar != nil {
		// The caption fields exist nowhere else: an export's title and description
		// are precisely what the media file itself no longer carries.
		p.Title = media.sidecar.Title
		p.Description = media.sidecar.Description
	}
	if uploadedBy != "" {
		p.UploadedBy = &uploadedBy
	}
	return p
}

// applyFileMetadata stamps the IPTC/XMP credit fields and the file-technical
// fields the extractor read out of the file onto p. They describe the file rather
// than the user's view of it, so ingest is where they are written: nothing in the
// upload flow can contradict what the file itself says.
func applyFileMetadata(p *photos.Photo, meta exif.Metadata, kind photos.MediaType, mime string) {
	p.Subject = meta.Subject
	p.Keywords = meta.Keywords
	p.Artist = meta.Artist
	p.Copyright = meta.Copyright
	p.License = meta.License
	p.Software = meta.Software
	p.CameraSerial = meta.CameraSerial
	p.ColorProfile = meta.ColorProfile
	p.Projection = meta.Projection
	p.ImageCodec = imageCodec(kind, meta.ImageCodec, mime)
}

// chooseMIME prefers the EXIF-derived media type (often more specific, e.g.
// image/heic) and falls back to the storage layer's content-sniffed type.
func chooseMIME(exifMime, storedMime string) string {
	if exifMime != "" {
		return exifMime
	}
	return storedMime
}

// originalName reduces the name an upload arrived under to its base name: a
// folder import hands over a path, and only the file's own name belongs in
// original_name. An unnamed upload (a bare stream) keeps the empty string rather
// than path.Base's ".".
func originalName(filename string) string {
	if filename == "" {
		return ""
	}
	return path.Base(filename)
}

// imageCodec settles the still image's compression: the token the extractor read
// out of the file (already normalised — "jpeg", "heic", "raw"), falling back to
// the MIME subtype for a format it did not recognise. A video's compression lives
// in video_codec, so it yields nothing here; a live photo does, since its primary
// file is a still image.
func imageCodec(kind photos.MediaType, extracted, mime string) string {
	if kind == photos.MediaVideo {
		return ""
	}
	if extracted != "" {
		return extracted
	}
	subtype, ok := strings.CutPrefix(mime, "image/")
	if !ok {
		return ""
	}
	return subtype
}

// orientationOrDefault normalises an EXIF orientation to the valid 1..8 range,
// returning 1 (no transform) for the 0/absent or out-of-range case.
func orientationOrDefault(orientation int) int {
	if orientation < 1 || orientation > 8 {
		return 1
	}
	return orientation
}

// takenAtSource returns the EXIF source as a string, substituting "unknown"
// for an empty source so the column is never blank.
func takenAtSource(src exif.Source) string {
	if src == "" {
		return string(exif.SourceUnknown)
	}
	return string(src)
}

// locationSource returns the provenance of a freshly extracted GPS fix: "exif"
// when the file carried one, empty when it did not.
//
// Both halves must be present. Half a fix is not a location, and stamping "exif"
// on a photo that has only a latitude would claim provenance for a coordinate
// that does not exist — and, worse, would take that photo out of the estimator's
// reach for a location it never actually got.
func locationSource(lat, lng *float64) string {
	if lat == nil || lng == nil {
		return ""
	}
	return photos.LocationSourceExif
}

// marshalExif serialises the EXIF document to JSON for the jsonb column,
// returning nil (SQL NULL) when there is no EXIF or it cannot be marshalled.
func marshalExif(doc map[string]any) json.RawMessage {
	if len(doc) == 0 {
		return nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return raw
}

// createdResult builds the result for a newly catalogued photo.
func createdResult(filename, uid string, warnings []Warning) FileResult {
	return FileResult{
		Filename: filename,
		Status:   http.StatusCreated,
		Outcome:  OutcomeCreated,
		PhotoUID: uid,
		Warnings: warnings,
	}
}

// duplicateResult builds the result for content that was already catalogued.
func duplicateResult(filename, uid string) FileResult {
	return FileResult{
		Filename: filename,
		Status:   http.StatusConflict,
		Outcome:  OutcomeDuplicate,
		PhotoUID: uid,
	}
}

// errorResult builds the result for a file that could not be ingested, mapping
// the oversize case to 413 and everything else to 500.
func errorResult(filename string, err error) FileResult {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrFileTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	return FileResult{
		Filename: filename,
		Status:   status,
		Outcome:  OutcomeError,
		Error:    err.Error(),
	}
}

// firstErr returns the first non-nil error among its arguments, or nil.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// readerFunc adapts a function to io.Reader.
type readerFunc func(p []byte) (int, error)

// Read calls the underlying function.
func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// ctxReader wraps reader so a read aborts once ctx is cancelled, letting a slow
// or oversized upload be cut off mid-stream. io.EOF passes through unwrapped so
// io.Copy terminates normally.
func ctxReader(ctx context.Context, reader io.Reader) io.Reader {
	return readerFunc(func(p []byte) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("ingest: upload cancelled: %w", err)
		}
		// Pass the source reader's error (including io.EOF) through verbatim.
		return reader.Read(p)
	})
}
