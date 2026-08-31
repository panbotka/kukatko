// Package facejob wires face detection into Kukátko's background job system.
//
// Its centrepiece is the face_detect job handler: given a photo uid it opens an
// UPRIGHT copy of the original image, asks the embeddings sidecar to detect and
// embed faces, normalizes each returned pixel box against the frame of the very
// image it sent, and stores the faces together with that frame.
//
// The upright part is the invariant of this package, not a detail: the sidecar
// does not read EXIF, so an image sent with its orientation tag unapplied is
// detected sideways — faces are missed outright and the boxes that do come back
// are in a frame nobody displays. This package therefore owns the rotation
// (StorageSource.OpenUpright) and normalizes against the frame it measured on the
// bytes it sent (UprightImage), which leaves no orientation reasoning anywhere in
// the conversion. The full-resolution image is sent, not a downscaled preview.
//
// The handler is idempotent — a photo whose detection has already been recorded
// is skipped without calling the sidecar — and offline-aware: when the box is
// unreachable the job is deferred (requeued without burning a retry attempt) so
// it completes once the box comes back rather than dead-lettering while it sleeps.
// The same Service drives the face-detection backfill (enqueue a face_detect job
// for every photo that has never been processed).
//
// Every collaborator is an interface so the Service unit-tests with fakes and no
// network, database or filesystem.
package facejob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

const (
	// DefaultOfflineRetryDelay is how long a face_detect job waits before becoming
	// runnable again after the sidecar was found offline.
	DefaultOfflineRetryDelay = 5 * time.Minute
	// DefaultMinDetScore is the minimum detector confidence a face must have to be
	// stored. The sidecar already applies its own detection threshold; this is a
	// second, configurable floor that drops very low-confidence detections.
	DefaultMinDetScore = 0.5
)

// ErrMissingPhotoUID indicates a face_detect job payload had no photo_uid.
var ErrMissingPhotoUID = errors.New("facejob: payload missing photo_uid")

// PhotoStore is the subset of photos.Store the service reads.
type PhotoStore interface {
	// GetByUID returns the photo with the given uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// VectorStore is the subset of vectors.Store the service uses to record faces and
// to enumerate photos still missing face detection.
type VectorStore interface {
	// FacesDetected reports whether face detection has already been recorded for
	// the photo (used for the idempotent skip).
	FacesDetected(ctx context.Context, photoUID string) (bool, error)
	// RecordFaceDetection stores the detected faces and marks the photo processed
	// in one transaction, recording the frame the detector saw along with them.
	RecordFaceDetection(ctx context.Context, photoUID string, det vectors.Detection) error
	// ListPhotosMissingFaces returns uids of non-archived photos that have never
	// had face detection run (limit <= 0 returns all).
	ListPhotosMissingFaces(ctx context.Context, limit int) ([]string, error)
}

// ImageSource opens an upright copy of a photo's original image. It is satisfied
// by StorageSource, which converts HEIC/RAW/video and rotates as needed.
type ImageSource interface {
	// OpenUpright opens a JPEG/PNG/WebP stream for photo whose pixels are already in
	// display orientation, together with the frame those pixels measure. The caller
	// owns the returned reader and must close it.
	OpenUpright(ctx context.Context, photo photos.Photo) (UprightImage, error)
}

// Enqueuer schedules face_detect jobs for the backfill. It is satisfied by
// jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueFaceDetect schedules face detection for photoUID, treating an existing
	// active job as a no-op.
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
}

// Config bundles the Service's collaborators and tunables. The five collaborators
// are required; the remaining fields fall back to package defaults when left zero.
type Config struct {
	// Photos resolves a photo uid to its catalogue record.
	Photos PhotoStore
	// Vectors records faces and enumerates unprocessed photos.
	Vectors VectorStore
	// Client is the embeddings sidecar client.
	Client embedding.Client
	// Source opens the upright original sent to the sidecar.
	Source ImageSource
	// Enqueuer schedules backfill jobs.
	Enqueuer Enqueuer
	// OfflineRetryDelay is the deferral applied when the box is offline (default
	// DefaultOfflineRetryDelay).
	OfflineRetryDelay time.Duration
	// MinDetScore is the minimum det_score a face must have to be stored (default
	// DefaultMinDetScore; a non-positive value disables the filter).
	MinDetScore float64
}

// Service detects, converts and stores faces, and backfills face detection.
type Service struct {
	photos      PhotoStore
	vectors     VectorStore
	client      embedding.Client
	source      ImageSource
	enqueuer    Enqueuer
	retryDelay  time.Duration
	minDetScore float64
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since none has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Vectors == nil || cfg.Client == nil ||
		cfg.Source == nil || cfg.Enqueuer == nil {
		panic("facejob: New requires Photos, Vectors, Client, Source and Enqueuer")
	}
	retryDelay := cfg.OfflineRetryDelay
	if retryDelay <= 0 {
		retryDelay = DefaultOfflineRetryDelay
	}
	minDetScore := cfg.MinDetScore
	if minDetScore == 0 {
		minDetScore = DefaultMinDetScore
	}
	return &Service{
		photos:      cfg.Photos,
		vectors:     cfg.Vectors,
		client:      cfg.Client,
		source:      cfg.Source,
		enqueuer:    cfg.Enqueuer,
		retryDelay:  retryDelay,
		minDetScore: minDetScore,
	}
}

// jobPayload is the JSON shape of a face_detect job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
	// Force asks for the rebuild rather than the repair: the detector runs again
	// even though a detection is already recorded, and what it finds replaces the
	// stored faces. The repair path skips such a photo, which is right when the
	// detection is merely missing and wrong when it was made on the wrong picture —
	// a RAW preview that has since been corrected, or an image sent sideways. See
	// jobs.Enqueuer.EnqueueFaceDetectRebuild.
	Force bool `json:"force"`
}

// Handle is the worker.HandlerFunc for face_detect jobs: it decodes the photo uid
// from the job payload and runs detection. A malformed or empty payload is a
// permanent error (the job dead-letters rather than retrying a payload that can
// never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("facejob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	if p.Force {
		_, err := s.ForceDetect(ctx, p.PhotoUID)
		return err
	}
	return s.Detect(ctx, p.PhotoUID)
}

// Detect runs face detection for photoUID and stores the results. It is
// idempotent: a photo whose detection has already been recorded returns nil
// without calling the sidecar. When the sidecar is offline it returns a
// worker.RetryAfter error so the job is requeued without consuming a retry
// attempt; any other sidecar or storage failure is returned as an ordinary
// (retryable) error. A missing photo is returned as an error so the job fails and
// dead-letters rather than looping.
func (s *Service) Detect(ctx context.Context, photoUID string) error {
	_, err := s.detect(ctx, photoUID, false)
	return err
}

// ForceDetect runs face detection for photoUID even though a detection is
// already recorded, replaces the stored faces with what it finds, and returns how
// many faces the photo has afterwards. It is to Detect what
// thumbjob.ForceRegenerate is to thumbjob.Regenerate: the repair fills a gap, the
// rebuild corrects a wrong answer — a detection made on a preview that has since
// been fixed, or on an image the detector saw sideways.
//
// Nothing is appended: the store replaces the photo's faces in one transaction,
// carrying the marker and subject a re-detected face already had onto the face
// that takes its place (see vectors.RecordFaceDetection), so naming the people on
// a photo survives redoing the detection. Everything else is Detect's behaviour —
// an offline sidecar still yields a worker.RetryAfter, a missing photo is still an
// error.
//
// The returned count is the number of faces stored, which is zero when the photo
// genuinely holds none: a photo looked at and found empty is a result, not a
// failure.
func (s *Service) ForceDetect(ctx context.Context, photoUID string) (int, error) {
	return s.detect(ctx, photoUID, true)
}

// detect runs the detection for photoUID and returns how many faces were stored,
// skipping a photo whose detection is already recorded unless force is set. It is
// the shared body of Detect and ForceDetect, so the two differ in exactly one
// thing — whether the existing record is a reason to stop. A skipped photo
// reports zero faces, which the caller (Detect) discards.
func (s *Service) detect(ctx context.Context, photoUID string, force bool) (int, error) {
	if !force {
		detected, err := s.vectors.FacesDetected(ctx, photoUID)
		if err != nil {
			return 0, fmt.Errorf("facejob: checking face detection for %s: %w", photoUID, err)
		}
		if detected {
			return 0, nil // already processed — idempotent skip
		}
	}

	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return 0, fmt.Errorf("facejob: loading photo %s: %w", photoUID, err)
	}

	detection, err := s.detectFaces(ctx, photo)
	if err != nil {
		if embedding.IsUnavailable(err) {
			// RetryAfter is our own worker control-flow signal, not a foreign error
			// to annotate; wrapping it would obscure the type the worker matches.
			return 0, worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
		}
		return 0, err
	}

	faces := s.buildFaces(photo, detection)
	if err := s.vectors.RecordFaceDetection(ctx, photoUID, vectors.Detection{
		Faces:       faces,
		Model:       detection.model,
		FrameWidth:  detection.frameWidth,
		FrameHeight: detection.frameHeight,
	}); err != nil {
		return 0, fmt.Errorf("facejob: recording faces for %s: %w", photoUID, err)
	}
	return len(faces), nil
}

// detectionResult is one completed sidecar call: the faces it returned, its model
// tag, and the frame of the image it was given together with the orientation that
// frame is in. The frame travels with the faces because every box in them is in it
// — it is what the boxes are normalized against and what is recorded alongside
// them, and separating the two is what made the boxes wrong in the first place.
// The orientation travels with it for the same reason one step further out: it is
// the render hint cached on the face row, and reading it off the catalogue while
// the frame came from the image source let one row describe two pictures.
type detectionResult struct {
	faces                   []embedding.Face
	model                   string
	frameWidth, frameHeight int
	orientation             int
}

// detectFaces opens the photo's upright original and streams it to the sidecar,
// returning the detections together with the frame that was sent. The sidecar
// error (including the offline ErrUnavailable) is returned wrapped so callers can
// classify it with embedding.IsUnavailable.
func (s *Service) detectFaces(ctx context.Context, photo photos.Photo) (detectionResult, error) {
	upright, err := s.source.OpenUpright(ctx, photo)
	if err != nil {
		return detectionResult{}, fmt.Errorf("facejob: opening image for %s: %w", photo.UID, err)
	}
	defer func() { _ = upright.Reader.Close() }()

	faces, model, err := s.client.FaceEmbeddings(ctx, upright.Reader)
	if err != nil {
		return detectionResult{}, fmt.Errorf("facejob: detecting faces in %s: %w", photo.UID, err)
	}
	return detectionResult{
		faces:       faces,
		model:       model,
		frameWidth:  upright.Width,
		frameHeight: upright.Height,
		orientation: upright.Orientation,
	}, nil
}

// buildFaces converts the sidecar's detections into vectors.Face rows: it drops
// faces below the configured det_score floor, normalizes each pixel bounding box
// against the frame of the image that was sent, and assigns contiguous face
// indexes so filtered gaps never collide on the UNIQUE (photo_uid, face_index)
// constraint.
//
// The box is divided by the detection's own frame; the photo's stored pair is
// only copied onto the row as the render hint it has always been (raw dimensions
// — see vectors.Face). Keeping the two roles apart is what this signature exists
// for: mixing them is what moved the boxes. The orientation comes from the
// detection rather than from photo.FileOrientation so it describes the frame the
// box was measured in; the two are the same value whenever the readers agree, and
// where they do not it is the image source — the one that actually turned the
// pixels — that gets the last word.
func (s *Service) buildFaces(photo photos.Photo, det detectionResult) []vectors.Face {
	out := make([]vectors.Face, 0, len(det.faces))
	for _, f := range det.faces {
		if f.DetScore < s.minDetScore {
			continue
		}
		out = append(out, vectors.Face{
			PhotoUID:    photo.UID,
			FaceIndex:   len(out),
			Vector:      f.Embedding,
			BBox:        NormalizeBBox(f.BBox, det.frameWidth, det.frameHeight),
			DetScore:    f.DetScore,
			Model:       det.model,
			PhotoWidth:  photo.FileWidth,
			PhotoHeight: photo.FileHeight,
			Orientation: det.orientation,
		})
	}
	return out
}

// BackfillFaces enqueues a face_detect job for every non-archived photo that has
// never had face detection run, returning how many uids it scheduled. Photos that
// were already processed are never touched, and a photo whose job is already
// queued is a harmless no-op (the enqueuer dedupes), so the backfill is safe to
// run repeatedly.
func (s *Service) BackfillFaces(ctx context.Context) (int, error) {
	uids, err := s.vectors.ListPhotosMissingFaces(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("facejob: listing photos missing faces: %w", err)
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueFaceDetect(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("facejob: enqueuing face_detect for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}
