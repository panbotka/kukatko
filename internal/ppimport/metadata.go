package ppimport

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// extractFileMeta reads EXIF metadata from the downloaded original, degrading to
// an empty (unknown-source) document when extraction fails. PhotoPrism does not
// expose the EXIF orientation, so the importer reads the file-shaped fields
// (orientation, pixel geometry, MIME, raw EXIF blob) from the original itself and
// overlays PhotoPrism's curated fields on top (see buildPhoto).
func extractFileMeta(ctx context.Context, filePath string) exif.Metadata {
	meta, err := exif.Extract(ctx, filePath)
	if err != nil {
		return exif.Metadata{TakenAtSource: exif.SourceUnknown}
	}
	return meta
}

// buildPhoto maps a PhotoPrism photo, its primary file, the stored original and
// the original's own EXIF onto a photos.Photo ready for insertion. PhotoPrism's
// curated metadata (title, description, capture time, GPS, camera/lens, privacy)
// takes precedence; the file's EXIF supplies the geometry and orientation
// PhotoPrism does not return. The UID and timestamps are assigned by the database.
func buildPhoto(
	pp photoprism.Photo, primary photoprism.File, stored storage.StoredFile, meta exif.Metadata,
) photos.Photo {
	ppUID := pp.UID
	ppHash := primary.Hash
	p := photos.Photo{
		FileHash:           stored.Hash,
		FilePath:           stored.RelPath,
		FileName:           path.Base(stored.RelPath),
		FileSize:           stored.Size,
		FileMime:           firstNonEmpty(primary.Mime, meta.Mime, stored.MIME),
		FileWidth:          firstPositive(pp.Width, meta.Width),
		FileHeight:         firstPositive(pp.Height, meta.Height),
		FileOrientation:    orientationOrDefault(meta.Orientation),
		MediaType:          mapMediaType(pp.Type),
		Title:              pp.Title,
		Description:        caption(pp),
		Private:            pp.Private,
		Exif:               marshalExif(meta.Exif),
		PhotoprismUID:      &ppUID,
		PhotoprismFileHash: &ppHash,
	}
	applyCaptureMeta(&p, pp, meta)
	applyCameraMeta(&p, pp, meta)
	return p
}

// caption returns the photo's long description. PhotoPrism renamed its
// photo_description column to photo_caption (and description_src to caption_src);
// the Description field it still serialises is backed by a Go field marked
// gorm:"-", i.e. it is never loaded from the database and always arrives empty. So
// Caption is the live value and Description only what an old instance answers with
// — read Description alone and every caption in the library is silently dropped on
// import.
func caption(pp photoprism.Photo) string {
	return firstNonEmpty(pp.Caption, pp.Description)
}

// applyCaptureMeta fills the capture time and GPS fields, preferring PhotoPrism's
// values (which may carry user corrections) and falling back to the file's EXIF.
func applyCaptureMeta(p *photos.Photo, pp photoprism.Photo, meta exif.Metadata) {
	if !pp.TakenAt.IsZero() {
		taken := pp.TakenAt
		p.TakenAt = &taken
		p.TakenAtSource = string(exif.SourceExif)
	} else {
		p.TakenAt = meta.TakenAt
		p.TakenAtSource = takenAtSource(meta.TakenAtSource)
	}
	if pp.Lat != 0 || pp.Lng != 0 {
		lat, lng := pp.Lat, pp.Lng
		p.Lat, p.Lng = &lat, &lng
	} else {
		p.Lat, p.Lng = meta.Lat, meta.Lng
	}
	if pp.Altitude != 0 {
		alt := float64(pp.Altitude)
		p.Altitude = &alt
	} else {
		p.Altitude = meta.Altitude
	}
}

// applyCameraMeta fills the camera, lens and exposure fields, preferring
// PhotoPrism's values and falling back to the file's EXIF.
func applyCameraMeta(p *photos.Photo, pp photoprism.Photo, meta exif.Metadata) {
	p.CameraMake = firstNonEmpty(pp.CameraMake, meta.CameraMake)
	p.CameraModel = firstNonEmpty(pp.CameraModel, meta.CameraModel)
	p.LensModel = firstNonEmpty(pp.LensModel, meta.LensModel)
	p.Exposure = firstNonEmpty(pp.Exposure, meta.Exposure)
	p.ISO = firstIntPtr(pp.Iso, meta.ISO)
	p.Aperture = firstFloatPtr(pp.FNumber, meta.Aperture)
	p.FocalLength = firstFloatPtr(float64(pp.FocalLength), meta.FocalLength)
}

// metadataUpdate builds the metadata patch applied to an already-imported photo
// from PhotoPrism's current values, and the capture-time source mirrors
// buildPhoto.
//
// Store.UpdateMetadata overwrites the whole row, so every editable field this patch
// does not carry has to be taken from the photo as it stands, or an incremental run
// would silently blank it. That is every Kukátko-only field (notes, ai_note) plus
// the IPTC/XMP credits — which the import DOES map, but off the photo detail rather
// than the listing this patch is built from (see importPhotoDetail), so here they
// are simply carried through untouched.
//
// The two fields it does map obey the import's precedence rule: PhotoPrism wins
// when it has a value, but an empty PhotoPrism value never erases a non-empty
// Kukátko one. A title cleared upstream therefore survives here — the alternative,
// silently destroying a caption the user typed because the source has none, is the
// far worse failure.
func metadataUpdate(existing photos.Photo, pp photoprism.Photo) photos.MetadataUpdate {
	update := photos.MetadataUpdate{
		Title:         firstNonEmpty(pp.Title, existing.Title),
		Description:   firstNonEmpty(caption(pp), existing.Description),
		Notes:         existing.Notes,
		AiNote:        existing.AiNote,
		Subject:       existing.Subject,
		Keywords:      existing.Keywords,
		Artist:        existing.Artist,
		Copyright:     existing.Copyright,
		License:       existing.License,
		Scan:          existing.Scan,
		Private:       pp.Private,
		TakenAt:       existing.TakenAt,
		TakenAtSource: existing.TakenAtSource,
		Lat:           existing.Lat,
		Lng:           existing.Lng,
		Altitude:      existing.Altitude,
	}
	if !pp.TakenAt.IsZero() {
		taken := pp.TakenAt
		update.TakenAt = &taken
		update.TakenAtSource = string(exif.SourceExif)
	}
	if pp.Lat != 0 || pp.Lng != 0 {
		lat, lng := pp.Lat, pp.Lng
		update.Lat, update.Lng = &lat, &lng
	}
	if pp.Altitude != 0 {
		alt := float64(pp.Altitude)
		update.Altitude = &alt
	}
	return update
}

// metadataUnchanged reports whether applying update to existing would be a no-op,
// so a re-imported photo can be counted as skipped rather than updated. It
// compares every field UpdateMetadata writes, the carried-over ones included: a
// field that drops out of the comparison would make an update look like a no-op
// even while it rewrites the row.
func metadataUnchanged(existing photos.Photo, update photos.MetadataUpdate) bool {
	return captionsUnchanged(existing, update) &&
		creditsUnchanged(existing, update) &&
		placementUnchanged(existing, update)
}

// captionsUnchanged compares the caption-like text fields.
func captionsUnchanged(existing photos.Photo, update photos.MetadataUpdate) bool {
	return existing.Title == update.Title &&
		existing.Description == update.Description &&
		existing.Notes == update.Notes &&
		existing.AiNote == update.AiNote
}

// creditsUnchanged compares the IPTC/XMP credit fields.
func creditsUnchanged(existing photos.Photo, update photos.MetadataUpdate) bool {
	return existing.Subject == update.Subject &&
		existing.Keywords == update.Keywords &&
		existing.Artist == update.Artist &&
		existing.Copyright == update.Copyright &&
		existing.License == update.License &&
		existing.Scan == update.Scan
}

// placementUnchanged compares the capture time, the GPS fix and the private flag.
func placementUnchanged(existing photos.Photo, update photos.MetadataUpdate) bool {
	return existing.Private == update.Private &&
		existing.TakenAtSource == update.TakenAtSource &&
		timeEqual(existing.TakenAt, update.TakenAt) &&
		floatEqual(existing.Lat, update.Lat) &&
		floatEqual(existing.Lng, update.Lng) &&
		floatEqual(existing.Altitude, update.Altitude)
}

// originalName resolves the best original file name for the stored layout: the
// PhotoPrism OriginalName, falling back to the primary file's base name, then to
// the photo UID.
func originalName(pp photoprism.Photo, primary photoprism.File) string {
	if name := strings.TrimSpace(pp.OriginalName); name != "" {
		return path.Base(name)
	}
	if name := strings.TrimSpace(primary.Name); name != "" {
		return path.Base(name)
	}
	return pp.UID
}

// mapMediaType maps a PhotoPrism photo type onto Kukátko's media-type
// discriminator. Videos and animated photos (which PhotoPrism backs with a
// transcoded clip) are videos; live photos keep their own kind; everything else
// (raw, vector, …) is catalogued as an image. The actual stored file still decides
// the final kind (see selectMedia): a video type with no detectable stream
// degrades to an image.
func mapMediaType(ppType string) photos.MediaType {
	switch strings.ToLower(ppType) {
	case "video", "animated":
		return photos.MediaVideo
	case "live":
		return photos.MediaLive
	default:
		return photos.MediaImage
	}
}

// marshalExif serialises the EXIF document to JSON for the jsonb column, returning
// nil (SQL NULL) when there is no EXIF or it cannot be marshalled.
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

// orientationOrDefault normalises an EXIF orientation to the valid 1..8 range,
// returning 1 (no transform) for the 0/absent or out-of-range case.
func orientationOrDefault(orientation int) int {
	if orientation < 1 || orientation > 8 {
		return 1
	}
	return orientation
}

// takenAtSource returns the EXIF source as a string, substituting "unknown" for an
// empty source so the column is never blank.
func takenAtSource(src exif.Source) string {
	if src == "" {
		return string(exif.SourceUnknown)
	}
	return string(src)
}
