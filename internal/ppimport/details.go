package ppimport

import (
	"context"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
)

// importPhotoDetail brings across everything PhotoPrism serves on the photo DETAIL
// endpoint and nowhere else, off a single request: the Details block (subject,
// artist, copyright, licence, keywords, notes, software), the file-technical fields
// (scan, camera serial, colour profile, still codec, projection, original name),
// the face markers, and — for a scoped run — every album the photo belongs to and
// every label it carries. The listing payload has none of it: it is a flattened
// search struct with no Details object at all, files with an empty marker array and
// no per-file codec or colour profile.
//
// It returns the photo's outcome, promoting a skip to an update when the detail
// turned out to carry something the catalogue did not have. A photo the listing
// pass skipped as unchanged may still have credits to bring over — that is exactly
// how a library imported before this mapping existed reaches parity, without
// re-downloading a byte.
//
// Who gets a detail read is the cost boundary of the import. A scoped run reads one
// per photo — a slice of the library, 17 photos = 17 requests — because it maps each
// photo's whole context off it. A full run reads one for a photo it has just written
// (noise beside downloading the original) and for a photo the source has genuinely
// touched since the last run, whose details may be the only thing that changed: an
// edited copyright bumps the photo's UpdatedAt while leaving every listed field
// identical, so a run that only read the detail of photos the LISTING pass found
// changed would never see it. What it must not do is read one per *listed* photo:
// the incremental listing re-serves the watermark's own photos on every run, and on
// a first pass it serves the whole 20k-photo library.
//
// Everything here is best effort: an unreadable detail is logged and skipped, never
// a reason to fail (and lose) an already-downloaded photo, and a re-run repairs it.
func (s *Service) importPhotoDetail(
	ctx context.Context, pp photoprism.Photo, state *runState, result outcome,
) outcome {
	if !s.wantsDetail(pp, state, result) {
		return result
	}
	photo, ok := s.lookupImported(ctx, pp.UID)
	if !ok {
		return result
	}
	detail, err := s.client.GetPhoto(ctx, pp.UID)
	if err != nil {
		s.log.Warn("ppimport: reading photo detail", "pp_uid", pp.UID, "err", err)
		return result
	}
	if s.applyDetailMetadata(ctx, photo.UID, detail) && result == outcomeSkipped {
		result = outcomeUpdated
	}
	s.mapPhotoContext(ctx, photo.UID, detail, state)
	// The people ride on this same detail — the listing's markers are always empty —
	// and importing them on every scoped run (rather than on first import only) is
	// what lets a re-run backfill the photos an earlier, marker-blind run brought over.
	s.importMarkers(ctx, photo.UID, detail.Photo, state.subjects)
	return result
}

// wantsDetail reports whether this run should read the photo's detail. A scoped run
// always does (it maps the photo's whole context off it). A full run does when the
// photo was written by this run, or when the source touched it after the watermark
// the run resumed from — the two cases where the detail can hold something the
// catalogue does not. A photo the run merely re-listed at the watermark itself,
// unchanged upstream, is skipped: that is the difference between one request per
// changed photo and one per photo in the library.
func (s *Service) wantsDetail(pp photoprism.Photo, state *runState, result outcome) bool {
	return state.photoCtx != nil || result != outcomeSkipped || pp.UpdatedAt.After(state.since)
}

// applyDetailMetadata writes the photo's PhotoPrism-owned metadata into the
// catalogue and reports whether anything changed. A failure is logged and reported
// as unchanged: the photo itself is already catalogued, and its credits are
// repairable by re-running.
func (s *Service) applyDetailMetadata(
	ctx context.Context, photoUID string, detail photoprism.PhotoDetail,
) bool {
	changed, err := s.photos.ApplyImportMetadata(ctx, photoUID, importMetadata(detail))
	if err != nil {
		s.log.Warn("ppimport: applying photo details", "photo", photoUID, "err", err)
		return false
	}
	return changed
}

// importMetadata maps a PhotoPrism photo detail onto the metadata the catalogue
// takes from an import. Every text field is trimmed, the keywords are re-rendered
// in the form Kukátko's own extraction stores them in, and the codec is normalised
// onto the same token vocabulary — an imported photo's columns must read like an
// extracted photo's, or they are two columns wearing one name.
//
// A photo indexed by an older PhotoPrism has no details row at all, so the Details
// block arrives null and this yields the zero value: an inert patch that writes
// nothing (see photos.Store.ApplyImportMetadata).
func importMetadata(detail photoprism.PhotoDetail) photos.ImportMetadata {
	m := photos.ImportMetadata{
		Subject:      strings.TrimSpace(detail.Details.Subject),
		Keywords:     exif.NormalizeKeywords(detail.Details.Keywords),
		Artist:       strings.TrimSpace(detail.Details.Artist),
		Copyright:    strings.TrimSpace(detail.Details.Copyright),
		License:      strings.TrimSpace(detail.Details.License),
		Notes:        strings.TrimSpace(detail.Details.Notes),
		Software:     strings.TrimSpace(detail.Details.Software),
		Scan:         detail.Scan,
		CameraSerial: strings.TrimSpace(detail.CameraSerial),
		OriginalName: strings.TrimSpace(detail.OriginalName),
	}
	primary, ok := detail.PrimaryFile()
	if !ok {
		return m
	}
	m.ColorProfile = strings.TrimSpace(primary.ColorProfile)
	m.Projection = strings.TrimSpace(primary.Projection)
	if !primary.IsVideo() {
		// image_codec is the STILL's compression. A video's codec ("avc1", "hvc1")
		// belongs in video_codec, which ffprobe owns and this import must not touch.
		m.ImageCodec = exif.CodecToken(primary.Codec)
	}
	return m
}
