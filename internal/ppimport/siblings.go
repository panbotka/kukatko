package ppimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// importSiblings brings across the files of a PhotoPrism photo that the main
// import path leaves behind. PhotoPrism keeps one photo per SHOT and hangs every
// file of it off that one row: a RAW next to the JPEG rendered from it, an
// alternative encoding, a generated still beside a clip. Kukátko stores exactly
// one original per photo row, so an import that only ever downloaded the primary
// file dropped the rest — on the production library that is the RAW of every
// `type:raw` photo, the one file no rendering can reconstruct.
//
// Each sibling therefore becomes its own catalogue row (its own original, its own
// photo_files row) and the whole set is grouped into ONE stack whose primary is
// the displayable original the source itself marks primary — the shape
// internal/stacks exists for ("a RAW next to its JPEG"). The library still shows
// one tile per shot, because the default visibility gate is
// (stack_uid IS NULL OR stack_primary), and the RAW is reachable, previewable and
// downloadable as a variant of it instead of being lost.
//
// It runs for every photo the listing serves, not only for a fresh import: a
// library imported before this existed is missing exactly these files, and a
// scoped re-run is how it is backfilled. That costs nothing for the overwhelming
// majority of photos — a single-file photo returns before it touches the database
// — and one hash lookup per extra file for the rest.
//
// It returns true when it brought something new across (a file imported, or a
// sibling grouped with its photo for the first time), so a photo the listing pass
// counted as skipped is reported as updated instead.
//
// Everything here is best effort: a sibling that cannot be downloaded, catalogued
// or grouped is logged, recorded as a per-file failure (which reports the run
// 'partial') and skipped. The photo itself is already imported, and a re-run
// repairs the rest.
func (s *Service) importSiblings(ctx context.Context, pp photoprism.Photo, state *runState) bool {
	sel, ok := selectMedia(pp)
	if !ok {
		return false
	}
	files := siblingFiles(pp, sel)
	if len(files) == 0 {
		return false
	}
	photo, ok := s.lookupImported(ctx, pp.UID)
	if !ok || photo.ArchivedAt != nil {
		return false
	}
	ungrouped, imported := s.importSiblingFiles(ctx, pp, photo, files, state)
	if len(ungrouped) == 0 {
		return imported
	}
	if err := s.stackSiblings(ctx, photo, ungrouped); err != nil {
		s.log.Warn("ppimport: grouping siblings", "photo", photo.UID, "err", err)
		return imported
	}
	return true
}

// siblingFiles returns the source files of a photo that the main import path does
// not store: everything except the file selected as the photo's own original and,
// for a live photo, the motion clip already linked as its sidecar. Files are
// deduplicated by hash (PhotoPrism can list the same file twice on a merged
// result) and one with no hash is dropped — the hash IS the download key, so such
// a file cannot be fetched at all.
//
// It is deliberately blind to the file's type: the RAW is what the production
// library actually loses today, but an alternative encoding or a generated still
// is just as much a file of the shot, and a rule that named the types it knows
// would silently drop the next one PhotoPrism invents.
func siblingFiles(pp photoprism.Photo, sel mediaSelection) []photoprism.File {
	seen := map[string]struct{}{sel.original.Hash: {}}
	if sel.motion != nil {
		seen[sel.motion.Hash] = struct{}{}
	}
	out := make([]photoprism.File, 0, len(pp.Files))
	for _, f := range pp.Files {
		if f.Hash == "" {
			continue
		}
		if _, dup := seen[f.Hash]; dup {
			continue
		}
		seen[f.Hash] = struct{}{}
		out = append(out, f)
	}
	return out
}

// importSiblingFiles resolves every sibling file to the catalogue row holding it,
// importing the ones that are not there yet. It returns the uids of the siblings
// that are not yet grouped with photo (the ones the caller has to stack) and
// whether any file was newly imported. A per-file failure is recorded and skipped:
// one unreadable RAW must not cost the photo its other files.
func (s *Service) importSiblingFiles(
	ctx context.Context, pp photoprism.Photo, photo photos.Photo, files []photoprism.File, state *runState,
) (ungrouped []string, imported bool) {
	for _, f := range files {
		sibling, fresh, err := s.resolveSibling(ctx, pp, f)
		if err != nil {
			s.log.Warn("ppimport: sibling file failed", "pp_uid", pp.UID, "hash", f.Hash, "err", err)
			state.recordItemFailure(importer.StageFile, photo.UID, f.Hash, siblingName(f), err)
			// The photo is imported, so it is not a failed photo — but the watermark must
			// not move past it, or the file it just lost would never be offered again.
			state.holdWatermark(pp.UpdatedAt)
			continue
		}
		imported = imported || fresh
		// A file whose bytes are the photo's own resolves back to it; nothing to group
		// (and a stack of one photo with itself is not a stack). An archived sibling is
		// deliberately left out too: the user took it out of circulation — which took it
		// out of its stack — and a stack only ever holds live photos anyway.
		if sibling.UID != photo.UID && sibling.ArchivedAt == nil && !sameStack(photo, sibling) {
			ungrouped = append(ungrouped, sibling.UID)
		}
	}
	return ungrouped, imported
}

// resolveSibling returns the catalogue row holding one source file, importing it
// when it is not catalogued yet, and reports whether this call created it. An
// already-imported sibling is recognised by its PhotoPrism file hash alone, which
// is what keeps a re-run from downloading it a second time.
func (s *Service) resolveSibling(
	ctx context.Context, pp photoprism.Photo, f photoprism.File,
) (photos.Photo, bool, error) {
	existing, err := s.photos.GetByPhotoprismFileHash(ctx, f.Hash)
	switch {
	case err == nil:
		return existing, false, nil
	case errors.Is(err, photos.ErrPhotoNotFound):
		return s.importSiblingFile(ctx, pp, f)
	default:
		return photos.Photo{}, false, fmt.Errorf("ppimport: looking up sibling %s: %w", f.Hash, err)
	}
}

// importSiblingFile downloads, dedups, stores and catalogues one non-primary
// source file as its own photo. Content that is already catalogued (the same RAW
// uploaded directly, or migrated from photo-sorter) is not duplicated: the
// existing row is reused and stamped with the source file hash, so the next run
// resolves it without downloading again.
func (s *Service) importSiblingFile(
	ctx context.Context, pp photoprism.Photo, f photoprism.File,
) (photos.Photo, bool, error) {
	staged, err := s.download(ctx, f.Hash)
	if err != nil {
		return photos.Photo{}, false, err
	}
	defer staged.cleanup()

	dup, found, err := s.dedupSiblingByContent(ctx, staged.hash, f)
	if err != nil || found {
		return dup, false, err
	}
	stored, err := s.storeStaged(ctx, staged, pp.TakenAt, siblingName(f))
	if err != nil {
		return photos.Photo{}, false, err
	}
	sibling := buildSiblingPhoto(pp, f, stored, extractFileMeta(ctx, staged.path))
	if sibling.MediaType == photos.MediaVideo {
		s.probeVideo(ctx, staged.path).apply(&sibling)
	}
	created, err := s.createSibling(ctx, sibling, stored)
	if err != nil {
		return photos.Photo{}, false, err
	}
	// Thumbnails only: no image_embed and no face_detect. A sibling is the SAME
	// shot as the stack's primary, so its embedding and its faces would be a
	// near-exact twin of the primary's — a permanent self-duplicate in every
	// similarity search and a second copy of every face — for a row the library
	// never shows on its own. The primary carries the shot's identity.
	s.enqueueThumbnail(ctx, created.UID)
	return created, true, nil
}

// dedupSiblingByContent reports whether the staged content is already catalogued
// and returns that photo, backfilling the source file hash onto it when it does
// not carry one. It leaves an existing hash alone: that row is another source
// file's own row and overwriting its reference would corrupt the mapping.
func (s *Service) dedupSiblingByContent(
	ctx context.Context, contentHash string, f photoprism.File,
) (photos.Photo, bool, error) {
	existing, err := s.photos.GetByFileHash(ctx, contentHash)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return photos.Photo{}, false, nil
	}
	if err != nil {
		return photos.Photo{}, false, fmt.Errorf("ppimport: content dedup for sibling %s: %w", f.Hash, err)
	}
	if existing.PhotoprismFileHash != nil {
		return existing, true, nil
	}
	stamped, err := s.photos.SetPhotoprismFileHash(ctx, existing.UID, f.Hash)
	if err != nil {
		return photos.Photo{}, false, fmt.Errorf("ppimport: backfilling file hash onto %s: %w", existing.UID, err)
	}
	return stamped, true, nil
}

// createSibling inserts the sibling photo together with its primary file row,
// rolling the photo back when the file row cannot be written so a half-created
// record is never left behind. A unique-content race (the same bytes catalogued
// concurrently) resolves to the winning row rather than an error.
func (s *Service) createSibling(
	ctx context.Context, sibling photos.Photo, stored storage.StoredFile,
) (photos.Photo, error) {
	created, err := s.photos.Create(ctx, sibling)
	if errors.Is(err, photos.ErrFileHashTaken) {
		winner, getErr := s.photos.GetByFileHash(ctx, sibling.FileHash)
		if getErr != nil {
			return photos.Photo{}, fmt.Errorf("ppimport: resolving raced sibling: %w", getErr)
		}
		return winner, nil
	}
	if err != nil {
		return photos.Photo{}, fmt.Errorf("ppimport: cataloguing sibling %s: %w", sibling.FileName, err)
	}
	if err := s.createPrimaryFile(ctx, created, stored); err != nil {
		_ = s.photos.Delete(ctx, created.UID)
		return photos.Photo{}, err
	}
	return created, nil
}

// buildSiblingPhoto maps a non-primary source file onto its catalogue row: the
// source photo's curated metadata (it is the same shot, so capture time, GPS,
// camera and title all hold) over the sibling file's own geometry, MIME and EXIF.
//
// It deliberately leaves photoprism_uid NULL. That column is the 1:1 key of the
// source photo — internal/ppimport dedups incremental runs on it and
// internal/psfeedsimport joins photo-sorter's embeddings and faces onto it — and a
// second row wearing it would make both ambiguous, which is a far worse failure
// than the missing provenance. The sibling's own photoprism_file_hash identifies
// it instead (see resolveSibling).
func buildSiblingPhoto(
	pp photoprism.Photo, f photoprism.File, stored storage.StoredFile, meta exif.Metadata,
) photos.Photo {
	p := buildPhoto(pp, f, stored, meta)
	p.PhotoprismUID = nil
	p.FileWidth = firstPositive(f.Width, meta.Width, pp.Width)
	p.FileHeight = firstPositive(f.Height, meta.Height, pp.Height)
	// The photo's type describes the shot, the file decides what this row actually
	// holds: the generated still beside a clip is an image, an extra encoding of it
	// is a video.
	p.MediaType = photos.MediaImage
	if f.IsVideo() {
		p.MediaType = photos.MediaVideo
	}
	return p
}

// stackSiblings groups the ungrouped siblings with the photo they belong to into
// one stack. The primary is the photo itself — PhotoPrism already decided which
// file of the shot is the displayable one, so the rule-based election
// (stacks.PickPrimary) has nothing to add here — unless the photo already belongs
// to a stack, in which case that stack's members ride along and its own primary is
// kept: a stack a user curated must not be dissolved or re-pointed by an import.
func (s *Service) stackSiblings(ctx context.Context, photo photos.Photo, siblingUIDs []string) error {
	primary := photo.UID
	members := append([]string{photo.UID}, siblingUIDs...)
	if photo.StackUID != nil {
		existing, err := s.photos.ListStackMembers(ctx, *photo.StackUID)
		if err != nil {
			return fmt.Errorf("ppimport: reading stack of %s: %w", photo.UID, err)
		}
		for _, member := range existing {
			members = append(members, member.UID)
			if member.StackPrimary {
				primary = member.UID
			}
		}
	}
	if _, err := s.photos.CreateStack(ctx, primary, members); err != nil {
		return fmt.Errorf("ppimport: stacking siblings of %s: %w", photo.UID, err)
	}
	return nil
}

// sameStack reports whether two photos already belong to the same stack.
func sameStack(a, b photos.Photo) bool {
	return a.StackUID != nil && b.StackUID != nil && *a.StackUID == *b.StackUID
}

// siblingName resolves the stored file name of a sibling file: the base of its
// PhotoPrism name, which keeps the source's own extension so a RAW stays a RAW
// (and the base-name stack rules keep recognising it), falling back to the file
// hash when the source reports no name.
func siblingName(f photoprism.File) string {
	if name := strings.TrimSpace(f.Name); name != "" {
		return path.Base(name)
	}
	return f.Hash
}
