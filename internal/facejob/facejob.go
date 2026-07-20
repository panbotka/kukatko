// Package facejob wires face detection into Kukátko's background job system.
//
// Its centrepiece is the face_detect job handler: given a photo uid it opens a
// decodable copy of the original image, asks the embeddings sidecar to detect and
// embed faces, converts each pixel bounding box to a normalized display-space box
// and stores the faces. The full-resolution original is sent (not a downscaled
// preview) because the sidecar auto-rotates by EXIF and reports pixel boxes in
// display space; normalizing those against the photo's stored dimensions only
// lines up when the detected image and the stored dimensions share a scale.
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
	"io"
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
	// in one transaction.
	RecordFaceDetection(ctx context.Context, photoUID string, faces []vectors.Face, model string) error
	// ListPhotosMissingFaces returns uids of non-archived photos that have never
	// had face detection run (limit <= 0 returns all).
	ListPhotosMissingFaces(ctx context.Context, limit int) ([]string, error)
}

// ImageSource opens a decodable copy of a photo's original image. It is satisfied
// by StorageSource, which converts HEIC/RAW/video as needed.
type ImageSource interface {
	// OpenDecodable opens a JPEG/PNG/WebP stream for photo. The caller owns the
	// returned reader and must close it.
	OpenDecodable(ctx context.Context, photo photos.Photo) (io.ReadCloser, error)
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
	// Source opens the decodable original sent to the sidecar.
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
	detected, err := s.vectors.FacesDetected(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("facejob: checking face detection for %s: %w", photoUID, err)
	}
	if detected {
		return nil // already processed — idempotent skip
	}

	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("facejob: loading photo %s: %w", photoUID, err)
	}

	faces, model, err := s.detectFaces(ctx, photo)
	if err != nil {
		if embedding.IsUnavailable(err) {
			// RetryAfter is our own worker control-flow signal, not a foreign error
			// to annotate; wrapping it would obscure the type the worker matches.
			return worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
		}
		return err
	}

	stored := s.buildFaces(photo, faces, model)
	if err := s.vectors.RecordFaceDetection(ctx, photoUID, stored, model); err != nil {
		return fmt.Errorf("facejob: recording faces for %s: %w", photoUID, err)
	}
	return nil
}

// detectFaces opens the photo's decodable original and streams it to the sidecar,
// returning the detected faces and the sidecar's model tag. The sidecar error
// (including the offline ErrUnavailable) is returned wrapped so callers can
// classify it with embedding.IsUnavailable.
func (s *Service) detectFaces(
	ctx context.Context, photo photos.Photo,
) (faces []embedding.Face, model string, err error) {
	reader, err := s.source.OpenDecodable(ctx, photo)
	if err != nil {
		return nil, "", fmt.Errorf("facejob: opening image for %s: %w", photo.UID, err)
	}
	defer func() { _ = reader.Close() }()

	faces, model, err = s.client.FaceEmbeddings(ctx, reader)
	if err != nil {
		return nil, "", fmt.Errorf("facejob: detecting faces in %s: %w", photo.UID, err)
	}
	return faces, model, nil
}

// buildFaces converts the sidecar's detections into vectors.Face rows: it drops
// faces below the configured det_score floor, normalizes each pixel bounding box
// to display space using the photo's dimensions and EXIF orientation, and assigns
// contiguous face indexes so filtered gaps never collide on the UNIQUE
// (photo_uid, face_index) constraint.
func (s *Service) buildFaces(photo photos.Photo, detected []embedding.Face, model string) []vectors.Face {
	out := make([]vectors.Face, 0, len(detected))
	for _, f := range detected {
		if f.DetScore < s.minDetScore {
			continue
		}
		out = append(out, vectors.Face{
			PhotoUID:    photo.UID,
			FaceIndex:   len(out),
			Vector:      f.Embedding,
			BBox:        NormalizeBBox(f.BBox, photo.FileWidth, photo.FileHeight, photo.FileOrientation),
			DetScore:    f.DetScore,
			Model:       model,
			PhotoWidth:  photo.FileWidth,
			PhotoHeight: photo.FileHeight,
			Orientation: photo.FileOrientation,
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
