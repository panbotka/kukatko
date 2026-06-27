package ppimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// defaultProber is the production VideoProber backed by the video package's
// ffprobe/exiftool probe.
type defaultProber struct{}

// Probe delegates to video.Probe, wrapping its error for context.
func (defaultProber) Probe(ctx context.Context, filePath string) (video.Metadata, error) {
	meta, err := video.Probe(ctx, filePath)
	if err != nil {
		return video.Metadata{}, fmt.Errorf("ppimport: probing video: %w", err)
	}
	return meta, nil
}

// mediaSelection describes how a PhotoPrism photo maps onto Kukátko storage: the
// file downloaded and stored as the primary original (and used for content dedup
// and the photoprism_file_hash reference), the resolved media kind, and — for a
// live photo — the companion motion clip stored as a sidecar file.
type mediaSelection struct {
	// original is the file stored as the photo's primary original.
	original photoprism.File
	// motion is the live-photo motion clip stored as a sidecar, or nil.
	motion *photoprism.File
	// kind is the resolved Kukátko media type (image, video or live).
	kind photos.MediaType
}

// selectMedia resolves which file(s) to import for a PhotoPrism photo and the
// media kind to catalogue it as. A video/live/animated photo stores its actual
// motion file (live photos additionally store the still as the primary); a still
// stores its primary file. It returns false when the photo exposes no downloadable
// file.
func selectMedia(pp photoprism.Photo) (mediaSelection, bool) {
	switch mapMediaType(pp.Type) {
	case photos.MediaLive:
		return selectLive(pp)
	case photos.MediaVideo:
		return selectVideo(pp)
	default:
		primary, ok := pp.PrimaryFile()
		return mediaSelection{original: primary, kind: photos.MediaImage}, ok
	}
}

// selectVideo selects the video file as the stored original, falling back to the
// primary file (catalogued as an image) when PhotoPrism exposes no video stream.
func selectVideo(pp photoprism.Photo) (mediaSelection, bool) {
	if vf, ok := pp.VideoFile(); ok {
		return mediaSelection{original: vf, kind: photos.MediaVideo}, true
	}
	primary, ok := pp.PrimaryFile()
	return mediaSelection{original: primary, kind: photos.MediaImage}, ok
}

// selectLive selects the still image as the primary original and the motion clip
// as a sidecar companion. A live photo with no still degrades to its motion file
// catalogued as a plain video.
func selectLive(pp photoprism.Photo) (mediaSelection, bool) {
	still, ok := pp.StillFile()
	if !ok {
		return selectVideo(pp)
	}
	sel := mediaSelection{original: still, kind: photos.MediaLive}
	if motion, hasMotion := pp.VideoFile(); hasMotion {
		sel.motion = &motion
	}
	return sel, true
}

// videoFields holds the probed video-only metadata applied to a photo row.
type videoFields struct {
	durationMs *int
	videoCodec string
	audioCodec string
	hasAudio   bool
	fps        *float64
}

// apply copies the probed video fields onto p; a zero videoFields leaves the
// (already-zero) video columns untouched for stills.
func (v videoFields) apply(p *photos.Photo) {
	p.DurationMs = v.durationMs
	p.VideoCodec = v.videoCodec
	p.AudioCodec = v.audioCodec
	p.HasAudio = v.hasAudio
	p.FPS = v.fps
}

// videoFieldsFor probes the video metadata for a selection: a plain video is
// probed from the stored original, a live photo from its (already-staged) motion
// clip, and a still yields zero fields. The probe is best-effort — a failure
// degrades to empty fields and never blocks the import.
func (s *Service) videoFieldsFor(ctx context.Context, sel mediaSelection, original, motion *stagedFile) videoFields {
	switch {
	case sel.kind == photos.MediaVideo:
		return s.probeVideo(ctx, original.path)
	case sel.kind == photos.MediaLive && motion != nil:
		return s.probeVideo(ctx, motion.path)
	default:
		return videoFields{}
	}
}

// probeVideo probes a staged video file, degrading to zero fields (logged) on any
// probe error so a video missing ffprobe/ffmpeg is still catalogued.
func (s *Service) probeVideo(ctx context.Context, filePath string) videoFields {
	meta, err := s.prober.Probe(ctx, filePath)
	if err != nil {
		s.log.Warn("ppimport: video probe failed", "path", filePath, "err", err)
		return videoFields{}
	}
	return videoFields{
		durationMs: meta.DurationMs,
		videoCodec: meta.VideoCodec,
		audioCodec: meta.AudioCodec,
		hasAudio:   meta.HasAudio,
		fps:        meta.FPS,
	}
}

// stageMotion downloads a live photo's motion clip into a temp file for probing
// and storage. It returns nil for a non-live selection or when the download
// fails: a missing motion clip degrades the live photo to its still rather than
// failing the import.
func (s *Service) stageMotion(ctx context.Context, sel mediaSelection) *stagedFile {
	if sel.motion == nil {
		return nil
	}
	staged, err := s.download(ctx, sel.motion.Hash)
	if err != nil {
		s.log.Warn("ppimport: downloading motion clip", "hash", sel.motion.Hash, "err", err)
		return nil
	}
	return staged
}

// linkMotion publishes a live photo's staged motion clip and links it as a
// sidecar file on the already-catalogued photo. Every failure is logged and
// skipped: the still is already imported, so a missing motion clip is a degraded
// (repairable) state, never a reason to fail the import.
func (s *Service) linkMotion(ctx context.Context, photo photos.Photo, motion photoprism.File, staged *stagedFile) {
	file, err := os.Open(staged.path)
	if err != nil {
		s.log.Warn("ppimport: reopening motion clip", "photo", photo.UID, "err", err)
		return
	}
	defer func() { _ = file.Close() }()

	stored, err := s.storage.Store(ctx, file, derefTime(photo.TakenAt), companionName(motion))
	if err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		s.log.Warn("ppimport: storing motion clip", "photo", photo.UID, "err", err)
		return
	}
	_, err = s.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID,
		FilePath: stored.RelPath,
		FileHash: stored.Hash,
		FileSize: stored.Size,
		FileMime: firstNonEmpty(motion.Mime, stored.MIME),
		Role:     photos.RoleSidecar,
	})
	if err != nil && !errors.Is(err, photos.ErrFilePathTaken) {
		s.log.Warn("ppimport: linking motion clip", "photo", photo.UID, "err", err)
	}
}

// companionName resolves the stored file name for a motion clip: the base of its
// PhotoPrism name, falling back to its hash with a generic video extension.
func companionName(f photoprism.File) string {
	if name := strings.TrimSpace(f.Name); name != "" {
		return path.Base(name)
	}
	return f.Hash + ".mov"
}

// derefTime returns the pointed-to time, or the zero time when t is nil.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
