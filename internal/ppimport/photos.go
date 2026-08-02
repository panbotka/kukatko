package ppimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// outcome classifies how one PhotoPrism photo was handled.
type outcome int

const (
	// outcomeImported means a new photo was downloaded and catalogued.
	outcomeImported outcome = iota
	// outcomeUpdated means an already-imported photo changed: its metadata was
	// re-applied, or a row that held its content without knowing where it came from
	// was stamped with this photo's PhotoPrism references.
	outcomeUpdated
	// outcomeSkipped means nothing changed: the metadata was already up to date.
	outcomeSkipped
	// outcomeDeduplicated means the source photo has no row of its own because the
	// catalogue already holds its exact content under ANOTHER source photo. The
	// collapse is recorded as an alias, so the source uid still resolves and the
	// photo's albums, labels and markers land on the surviving row.
	outcomeDeduplicated
)

// importPhotos walks every page of the photo listing, importing each photo and
// checkpointing the run's counts after every page. A listing error is an
// infrastructure failure (returned to fail the run); a per-photo failure is
// recorded in the run state and never aborts the walk.
//
// The listing is incremental (resuming from state.since) for a full run, and
// narrowed by state.scope for a scoped run — the album filter and the search
// expression the other filters render to. A scoped listing deliberately ignores
// the watermark, so its slice of the library is imported whole however old its
// photos are (the source's q= expression takes precedence over the watermark
// filter, and an album-only scope carries no q= at all, so the zero state.since
// of a scoped run keeps the listing unfiltered by time).
func (s *Service) importPhotos(ctx context.Context, runID int64, state *runState) error {
	// Read the source's subjects once so the people seeded from this run's face
	// markers carry their type and favorite/private flags. Best effort: a failure
	// leaves the index empty and subjects fall back to a plain public person.
	state.subjects = s.loadSubjectIndex(ctx)
	query := state.scope.Query()
	lastPage := ""
	for offset := 0; ; {
		page, err := s.client.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:        s.pageSize,
			Offset:       offset,
			UpdatedSince: state.since,
			AlbumUID:     state.scope.AlbumUID,
			Query:        query,
		})
		if err != nil {
			return fmt.Errorf("ppimport: listing photos at offset %d: %w", offset, err)
		}
		for i := range page {
			s.importOnePhoto(ctx, page[i], state)
		}
		if err := s.runs.UpdateCounts(ctx, runID, state.counts); err != nil {
			return fmt.Errorf("ppimport: checkpointing counts: %w", err)
		}
		c := state.counts
		s.metrics.SetImportProgress("photoprism", c.Imported, c.Updated, c.Skipped, c.Failed)
		// Only an EMPTY page ends the library. A photo listing is served merged, so
		// the source collapses a photo's file rows into one entry and a page comes
		// back shorter than the requested count whenever the window holds a
		// multi-file photo — a short page is routine, not exhaustion. Reading it as
		// the end imported the first page of the library and reported done.
		if len(page) == 0 {
			return nil
		}
		// A source that ignores the offset would serve the same window forever and
		// the walk above would never reach an empty page — a hang, not an error, and
		// one that only shows up against a full library. Two identical consecutive
		// windows mean no progress: stop and say so rather than spin.
		fingerprint := pageFingerprint(page)
		if fingerprint == lastPage {
			return fmt.Errorf(
				"ppimport: source served the same %d-photo window twice at offset %d: it is ignoring the offset",
				len(page), offset,
			)
		}
		lastPage = fingerprint
		// Advancing by the page length under-advances against the source's file-row
		// offset, which never skips a row (the overlap is re-listed and re-imports
		// idempotently) and always moves while the page is non-empty, so the walk
		// terminates.
		offset += len(page)
	}
}

// importOnePhoto processes a single photo, translating its outcome (or failure)
// into the run state. A failure is logged and tallied; it never propagates.
//
// A photo that made it into the catalogue then reads its detail — the only payload
// carrying its credits, its face markers and (for a scoped run) the albums and
// labels the source has it in — and brings all of that across too
// (importPhotoDetail). That runs for every outcome, not only for a fresh import: a
// photo the run skipped as unchanged, or deduped by content onto an existing row,
// still carries the source's subject and copyright and still belongs in its albums,
// and those may be the very thing this run is meant to bring over. Which is also why
// the outcome is counted only afterwards — a photo the listing pass had nothing new
// for, but whose detail did, is an update, not a skip.
//
// The photo's other FILES follow the same rule (importSiblings): a shot the source
// keeps as a RAW next to its JPEG has one file the main path never stores, and a
// library imported before that was mapped is missing exactly those — so they are
// resolved for every listed photo, not only for a fresh import, and a re-run
// backfills them.
func (s *Service) importOnePhoto(ctx context.Context, pp photoprism.Photo, state *runState) {
	result, err := s.processPhoto(ctx, pp)
	if err != nil {
		s.log.Warn("ppimport: photo failed", "pp_uid", pp.UID, "err", err)
		state.recordFailure(pp.UpdatedAt)
		state.recordItemFailure(importer.StagePhoto, "", pp.UID, pp.Title, err)
		return
	}
	state.recordSuccess(pp.UpdatedAt)
	result = s.importPhotoDetail(ctx, pp, state, result)
	if s.importSiblings(ctx, pp, state) && result == outcomeSkipped {
		result = outcomeUpdated
	}
	switch result {
	case outcomeImported:
		state.counts.Imported++
	case outcomeUpdated:
		state.counts.Updated++
	case outcomeSkipped:
		state.counts.Skipped++
	case outcomeDeduplicated:
		state.counts.Deduplicated++
	}
}

// processPhoto dedups a photo by its PhotoPrism UID — updating an already-imported
// photo's metadata when it changed — and otherwise imports it as new. A photo with
// no importable file (no still and no video) cannot be downloaded and is a
// per-photo failure.
//
// A source photo with no row of its own may still be accounted for: an earlier run
// may have found the catalogue already holding its exact content under another
// source photo and recorded that collapse as an alias. Resolving the alias BEFORE
// importing it as new is what keeps a re-run from downloading those originals over
// and over only to discard them again at the dedup.
func (s *Service) processPhoto(ctx context.Context, pp photoprism.Photo) (outcome, error) {
	sel, ok := selectMedia(pp)
	if !ok {
		return outcomeSkipped, fmt.Errorf("ppimport: photo %s has no importable file", pp.UID)
	}
	existing, err := s.photos.GetByPhotoprismUID(ctx, pp.UID)
	switch {
	case err == nil:
		return s.updateExisting(ctx, existing, pp)
	case errors.Is(err, photos.ErrPhotoNotFound):
		return s.importUnknown(ctx, pp, sel)
	default:
		return outcomeSkipped, fmt.Errorf("ppimport: looking up %s: %w", pp.UID, err)
	}
}

// importUnknown handles a source photo the catalogue has no row for: it reports it
// as deduplicated when a previous run already collapsed it onto the row holding its
// content, and imports it as new otherwise.
func (s *Service) importUnknown(ctx context.Context, pp photoprism.Photo, sel mediaSelection) (outcome, error) {
	_, err := s.photos.GetByPhotoprismAlias(ctx, pp.UID)
	switch {
	case err == nil:
		return outcomeDeduplicated, nil
	case errors.Is(err, photos.ErrPhotoNotFound):
		return s.importNew(ctx, pp, sel)
	default:
		return outcomeSkipped, fmt.Errorf("ppimport: resolving alias of %s: %w", pp.UID, err)
	}
}

// updateExisting applies PhotoPrism's current metadata to an already-imported
// photo, returning outcomeSkipped when nothing changed (the common case on a
// re-run) so the import stays idempotent. Markers are seeded only on first import,
// so a metadata update does not re-create them.
func (s *Service) updateExisting(ctx context.Context, existing photos.Photo, pp photoprism.Photo) (outcome, error) {
	update := metadataUpdate(existing, pp)
	if metadataUnchanged(existing, update) {
		return outcomeSkipped, nil
	}
	if _, err := s.photos.UpdateMetadata(ctx, existing.UID, update); err != nil {
		return outcomeSkipped, fmt.Errorf("ppimport: updating metadata for %s: %w", existing.UID, err)
	}
	return outcomeUpdated, nil
}

// importNew downloads, dedups, stores and catalogues a not-yet-imported photo,
// reusing the video ingest path for videos and live photos: the original (a video
// for clips, the still for live photos) is staged and probed, a live photo's
// motion clip is staged alongside it, and the catalogued row carries the resolved
// media type plus any probed video metadata. A content hash that already exists —
// an identical file uploaded directly, migrated from photo-sorter, or the source
// keeping the same bytes as a second photo — does not create a row: the source
// photo's identity is recorded on the row that already holds its content instead
// (adoptExisting), and that is what the outcome then reports.
func (s *Service) importNew(ctx context.Context, pp photoprism.Photo, sel mediaSelection) (outcome, error) {
	staged, err := s.download(ctx, sel.original.Hash)
	if err != nil {
		return outcomeSkipped, err
	}
	defer staged.cleanup()

	if dup, handled, err := s.dedupByContent(ctx, staged.hash, pp, sel.original); err != nil {
		return outcomeSkipped, err
	} else if handled {
		return dup, nil
	}

	motion := s.stageMotion(ctx, sel)
	if motion != nil {
		defer motion.cleanup()
	}
	vfields := s.videoFieldsFor(ctx, sel, staged, motion)

	photo, result, err := s.catalogue(ctx, pp, sel, staged, vfields)
	if err != nil {
		return outcomeSkipped, err
	}
	if result != outcomeImported {
		return result, nil
	}
	if motion != nil {
		s.linkMotion(ctx, photo, *sel.motion, motion)
	}
	s.postProcess(ctx, photo)
	return outcomeImported, nil
}

// dedupByContent reports whether the staged content is already catalogued and,
// when it is, records the source photo's identity onto the row holding it
// (adoptExisting). handled=false means the content is new and the caller must
// catalogue it.
func (s *Service) dedupByContent(
	ctx context.Context, hash string, pp photoprism.Photo, primary photoprism.File,
) (result outcome, handled bool, err error) {
	existing, err := s.photos.GetByFileHash(ctx, hash)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return outcomeSkipped, false, nil
	}
	if err != nil {
		return outcomeSkipped, false, fmt.Errorf("ppimport: content dedup for %s: %w", pp.UID, err)
	}
	result, err = s.adoptExisting(ctx, existing, pp, primary)
	if err != nil {
		return outcomeSkipped, false, err
	}
	return result, true, nil
}

// adoptExisting records a source photo's identity onto the catalogue row that
// already holds its exact content, and reports how to count it.
//
// Two rows can hold one photo's bytes for two different reasons, and they need
// different answers:
//
//   - The row carries NO PhotoPrism uid — it came from somewhere else (a direct
//     upload, the photo-sorter migration) and nothing yet says which source photo
//     it is. Stamping the references on it makes the source uid resolve to it 1:1,
//     which is a real change to the row, hence outcomeUpdated.
//   - The row already answers to ANOTHER source photo, because the source keeps the
//     same bytes twice. Its photoprism_uid must not be overwritten (that would
//     merely move the loss to the other photo), so the collapse is recorded as an
//     alias: the uid still resolves — to the surviving row — and everything hanging
//     off the identity (albums, labels, face markers) is attached to it by the
//     detail pass that follows.
//
// An alias that cannot be written is an ERROR, not a warning: an unrecorded
// collapse is a source photo dropped in silence, which is precisely the defect
// this path exists to prevent. The caller turns it into a per-photo failure, so
// the run reports it and ends 'partial'.
func (s *Service) adoptExisting(
	ctx context.Context, existing photos.Photo, pp photoprism.Photo, primary photoprism.File,
) (outcome, error) {
	if existing.PhotoprismUID == nil {
		if _, err := s.photos.SetPhotoprismRef(ctx, existing.UID, pp.UID, primary.Hash); err != nil {
			return outcomeSkipped, fmt.Errorf("ppimport: backfilling refs onto %s: %w", existing.UID, err)
		}
		return outcomeUpdated, nil
	}
	if *existing.PhotoprismUID == pp.UID {
		return outcomeSkipped, nil
	}
	if err := s.photos.AddPhotoprismAlias(ctx, pp.UID, existing.UID, primary.Hash); err != nil {
		return outcomeSkipped, fmt.Errorf("ppimport: aliasing %s onto %s: %w", pp.UID, existing.UID, err)
	}
	s.log.Info("ppimport: source photo collapsed onto identical content",
		"pp_uid", pp.UID, "photo", existing.UID, "held_by_pp_uid", *existing.PhotoprismUID,
		"pp_file_hash", primary.Hash)
	return outcomeDeduplicated, nil
}

// catalogue stores the original and inserts the photos + primary photo_files
// rows. The photo carries the selection's resolved media type (authoritative over
// PhotoPrism's, so a video with no detectable stream degrades to an image) and any
// probed video metadata. The stored original is published before the row so a
// failed insert leaves only a reclaimable content-addressed file behind.
//
// It returns the outcome to report: outcomeImported for the row it created, or
// whatever adoptExisting decides when the insert loses to a unique-content clash.
// That clash means something published these exact bytes between the pre-check and
// the insert — this run's own sibling pass, a concurrent upload — and it gets the
// same treatment as the pre-check would have given it, never a silent skip.
func (s *Service) catalogue(
	ctx context.Context, pp photoprism.Photo, sel mediaSelection, staged *stagedFile, vfields videoFields,
) (photos.Photo, outcome, error) {
	stored, err := s.storeOriginal(ctx, pp, sel.original, staged)
	if err != nil {
		return photos.Photo{}, outcomeSkipped, err
	}
	meta := extractFileMeta(ctx, staged.path)
	photo := buildPhoto(pp, sel.original, stored, meta)
	photo.MediaType = sel.kind
	vfields.apply(&photo)
	created, err := s.photos.Create(ctx, photo)
	if errors.Is(err, photos.ErrFileHashTaken) {
		result, handled, dupErr := s.dedupByContent(ctx, photo.FileHash, pp, sel.original)
		if dupErr != nil {
			return photos.Photo{}, outcomeSkipped, dupErr
		}
		if !handled {
			return photos.Photo{}, outcomeSkipped, fmt.Errorf(
				"ppimport: content of %s is taken but its holder cannot be found", pp.UID)
		}
		return photos.Photo{}, result, nil
	}
	if err != nil {
		return photos.Photo{}, outcomeSkipped, fmt.Errorf("ppimport: cataloguing %s: %w", pp.UID, err)
	}
	if err := s.createPrimaryFile(ctx, created, stored); err != nil {
		_ = s.photos.Delete(ctx, created.UID)
		return photos.Photo{}, outcomeSkipped, err
	}
	return created, outcomeImported, nil
}

// createPrimaryFile inserts the stored original as the photo's primary file row.
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
		return fmt.Errorf("ppimport: creating primary file for %s: %w", photo.UID, err)
	}
	return nil
}

// postProcess runs the regenerable side effects of a freshly imported photo —
// thumbnails and background jobs — collecting failures as logged warnings. Neither
// undoes the import: a missing thumbnail or unqueued job is a degraded but
// repairable state.
//
// Neither the credits nor the people are seeded here. Both live only on the photo
// detail, which this path does not have (the listing carries no Details object and
// its files' marker arrays are always empty); they are brought over from the detail
// the caller reads afterwards instead (importPhotoDetail).
func (s *Service) postProcess(ctx context.Context, photo photos.Photo) {
	s.generateThumbs(ctx, photo)
	s.enqueueJobs(ctx, photo.UID)
}

// generateThumbs renders the derived images of a catalogued photo, logging a
// failure rather than undoing the import: a missing thumbnail is a degraded state
// the thumbnail job repairs, not a reason to lose a downloaded original.
func (s *Service) generateThumbs(ctx context.Context, photo photos.Photo) {
	if _, err := s.thumbs.GenerateAll(ctx, photo); err != nil {
		s.log.Warn("ppimport: thumbnails failed", "photo", photo.UID, "err", err)
	}
}

// enqueueJobs schedules the image_embed and face_detect jobs for a new photo so
// embeddings and faces are computed once the box is reachable. A duplicate active
// job is a no-op the enqueuer swallows; any other error is logged.
func (s *Service) enqueueJobs(ctx context.Context, photoUID string) {
	if err := s.enqueuer.EnqueueImageEmbed(ctx, photoUID); err != nil {
		s.log.Warn("ppimport: enqueue image_embed failed", "photo", photoUID, "err", err)
	}
	if err := s.enqueuer.EnqueueFaceDetect(ctx, photoUID); err != nil {
		s.log.Warn("ppimport: enqueue face_detect failed", "photo", photoUID, "err", err)
	}
}

// storeOriginal reopens the staged temp file and publishes it into the storage
// layout under the photo's capture month (or the import month when the capture
// time is unknown). A storage ErrAlreadyExists is treated as success: the
// byte-identical original is already in place.
func (s *Service) storeOriginal(
	ctx context.Context, pp photoprism.Photo, primary photoprism.File, staged *stagedFile,
) (storage.StoredFile, error) {
	return s.storeStaged(ctx, staged, pp.TakenAt, originalName(pp, primary))
}

// storeStaged reopens a staged temp file and publishes it into the storage layout
// under takenAt's month (or the import month when the capture time is unknown),
// named name. A storage ErrAlreadyExists is treated as success: the byte-identical
// file is already in place. It is shared by every path that publishes a download —
// the photo's own original, a live photo's motion clip and a non-primary sibling
// file — which differ only in the name they file it under.
func (s *Service) storeStaged(
	ctx context.Context, staged *stagedFile, takenAt time.Time, name string,
) (storage.StoredFile, error) {
	file, err := os.Open(staged.path)
	if err != nil {
		return storage.StoredFile{}, fmt.Errorf("ppimport: reopening staged file: %w", err)
	}
	defer func() { _ = file.Close() }()

	out, err := s.storage.Store(ctx, file, takenAt, name)
	if err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		return storage.StoredFile{}, fmt.Errorf("ppimport: storing %s: %w", name, err)
	}
	return out, nil
}

// stagedFile is a downloaded original streamed to a temp file, with its SHA256
// content hash and byte size computed during the copy.
type stagedFile struct {
	path string
	hash string
	size int64
}

// cleanup removes the temp file; it is safe to defer immediately after staging.
func (f *stagedFile) cleanup() {
	if f != nil && f.path != "" {
		_ = os.Remove(f.path)
	}
}

// download streams a PhotoPrism original (by its SHA1 file hash) into a temp file
// while computing its SHA256 hash and size, never buffering the file whole in
// memory. An oversized download (past MaxFileSize) is rejected.
func (s *Service) download(ctx context.Context, fileHash string) (*stagedFile, error) {
	dl, err := s.client.DownloadOriginal(ctx, fileHash)
	if err != nil {
		return nil, fmt.Errorf("ppimport: downloading %s: %w", fileHash, err)
	}
	defer func() { _ = dl.Body.Close() }()

	tmp, err := os.CreateTemp(s.tempDir, "kukatko-ppimport-*")
	if err != nil {
		return nil, fmt.Errorf("ppimport: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	var reader io.Reader = dl.Body
	if s.maxFileSize > 0 {
		reader = io.LimitReader(reader, s.maxFileSize+1)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), reader)
	closeErr := tmp.Close()
	if err := firstErr(copyErr, closeErr); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("ppimport: streaming download %s: %w", fileHash, err)
	}
	if s.maxFileSize > 0 && size > s.maxFileSize {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("ppimport: original %s exceeds max size %d", fileHash, s.maxFileSize)
	}
	return &stagedFile{path: tmpPath, hash: hex.EncodeToString(hasher.Sum(nil)), size: size}, nil
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

// pageFingerprint identifies a listing window by its first and last photo uid and
// its length — enough to tell "the source moved on" from "the source served the
// same window again", without holding the page itself.
func pageFingerprint(page []photoprism.Photo) string {
	if len(page) == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%s:%s", len(page), page[0].UID, page[len(page)-1].UID)
}
