// Package ocrjob is the worker handler for `ocr` jobs: it reads the text
// printed in a photo — a street sign, a shop front, a scanned page, a banner
// over a stage — and stores it so the library's search can find the photo by
// what it says.
//
// The work is a single call to the embeddings sidecar's /ocr/image endpoint over
// the photo's fit_1920 preview. That preview size is a deliberate departure from
// the fit_720 image embedding uses: small print on a sign or in a newspaper
// simply is not there at 720 px, and 1920 was the size that read it back on real
// photos from this library.
//
// Two behaviours matter more than the recognition itself:
//
//   - An empty result is a success. Most photographs have no writing in them, and
//     recording "the recogniser looked and found nothing" — rather than leaving
//     the row untouched — is what stops every backfill from re-scheduling the
//     whole library forever. The timestamp is the marker; see migration 0058.
//
//   - The box is usually offline. Exactly like image_embed, an unreachable
//     sidecar defers the job (requeued without burning a retry attempt) so it
//     completes when the box comes back, and uploading and browsing are entirely
//     unaffected in the meantime.
//
// Videos are out of scope: OCR runs on stills only, with no poster-frame
// recognition. Every collaborator is an interface, so the Service unit-tests with
// fakes and no network, database or filesystem.
package ocrjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

const (
	// DefaultPreviewSize is the thumbnail rendered (if missing) and sent to the
	// recogniser. It is larger than the fit_720 used for embedding on purpose:
	// the image tower downsamples to a small square anyway, while OCR needs the
	// pixels — small print on signs and newspapers is unreadable at 720 px.
	DefaultPreviewSize = "fit_1920"
	// DefaultOfflineRetryDelay is how long an `ocr` job waits before becoming
	// runnable again after the sidecar was found offline. It matches embedjob's,
	// because it is the same box being waited for.
	DefaultOfflineRetryDelay = 5 * time.Minute
)

// ErrMissingPhotoUID indicates an `ocr` job payload carried no photo_uid, a
// permanent error so the job dead-letters rather than retrying a payload that can
// never succeed.
var ErrMissingPhotoUID = errors.New("ocrjob: payload missing photo_uid")

// ErrBackfillUnavailable indicates BackfillOCR was called on a Service built
// without the backfill collaborators (a Lister and an Enqueuer). A Service that
// only runs the worker handler omits them; the one behind POST /process/ocr is
// wired with both.
var ErrBackfillUnavailable = errors.New("ocrjob: OCR backfill not configured")

// PhotoStore is the subset of the photo catalogue the handler needs: loading the
// photo and storing what was read from it. It is satisfied by *photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo identified by uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// SaveOCR stores the recognised text and stamps the photo as OCR'd.
	SaveOCR(ctx context.Context, uid string, result photos.OCR) error
}

// Recognizer reads the text out of an image. It is the narrow slice of
// embedding.Client this package uses, so tests substitute a fake with no box.
type Recognizer interface {
	// ImageOCR reads the text printed in the image streamed from img. A
	// non-positive minConfidence leaves the service's own default in place.
	ImageOCR(ctx context.Context, img io.Reader, minConfidence float64) (embedding.OCRResult, error)
}

// Previewer opens a decodable preview image for a photo, producing it if it does
// not exist yet. It is satisfied by thumb.Thumbnailer.
type Previewer interface {
	// OpenOrGenerate returns a reader over the photo's preview at size, from
	// wherever it is available (local cache or object store) and generating it
	// when it is available nowhere.
	OpenOrGenerate(ctx context.Context, photo photos.Photo, size string) (io.ReadCloser, error)
}

// PhotoLister enumerates the photos an OCR backfill should schedule. It is
// satisfied by *photos.Store and is optional (only the Service behind
// POST /process/ocr needs it).
type PhotoLister interface {
	// ListPhotosMissingOCR returns the uids of non-archived, non-video photos that
	// have never been through the recogniser (limit <= 0 returns all).
	ListPhotosMissingOCR(ctx context.Context, limit int) ([]string, error)
	// ListActiveImageUIDs returns the uids of every non-archived, non-video photo,
	// for a forced full re-run.
	ListActiveImageUIDs(ctx context.Context) ([]string, error)
}

// Enqueuer schedules `ocr` jobs for the backfill. It is satisfied by
// jobs.Enqueuer and is optional (only the Service behind POST /process/ocr needs
// it).
type Enqueuer interface {
	// EnqueueOCR schedules text recognition for photoUID, treating an existing
	// active job as a no-op so repeated backfills do not pile up.
	EnqueueOCR(ctx context.Context, photoUID string) error
}

// Config bundles the Service's collaborators and tunables. Photos, Client and
// Previewer are required; Lister and Enqueuer are optional and enable the
// backfill (BackfillOCR) when both are supplied. The remaining fields fall back
// to the package defaults when left zero.
type Config struct {
	// Photos resolves a photo uid and stores the recognised text.
	Photos PhotoStore
	// Client is the text recogniser (the embeddings sidecar).
	Client Recognizer
	// Previewer renders/opens the preview image sent to the recogniser.
	Previewer Previewer
	// Lister enumerates photos for the OCR backfill (optional).
	Lister PhotoLister
	// Enqueuer schedules OCR backfill jobs (optional).
	Enqueuer Enqueuer
	// PreviewSize is the thumbnail size recognised (default DefaultPreviewSize).
	PreviewSize string
	// MinConfidence is the per-block confidence floor passed to the service; a
	// non-positive value leaves the service's own default in place.
	MinConfidence float64
	// OfflineRetryDelay is the deferral applied when the box is offline (default
	// DefaultOfflineRetryDelay).
	OfflineRetryDelay time.Duration
	// Logger records skipped photos; nil uses slog.Default().
	Logger *slog.Logger
}

// Service recognises the text in a photo, stores it, and backfills the library.
type Service struct {
	photos        PhotoStore
	client        Recognizer
	preview       Previewer
	lister        PhotoLister
	enqueuer      Enqueuer
	previewSize   string
	minConfidence float64
	retryDelay    time.Duration
	log           *slog.Logger
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if Photos, Client or Previewer is nil, since an `ocr` job cannot run
// without them and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Client == nil || cfg.Previewer == nil {
		panic("ocrjob: New requires Photos, Client and Previewer")
	}
	previewSize := cfg.PreviewSize
	if previewSize == "" {
		previewSize = DefaultPreviewSize
	}
	retryDelay := cfg.OfflineRetryDelay
	if retryDelay <= 0 {
		retryDelay = DefaultOfflineRetryDelay
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		photos:        cfg.Photos,
		client:        cfg.Client,
		preview:       cfg.Previewer,
		lister:        cfg.Lister,
		enqueuer:      cfg.Enqueuer,
		previewSize:   previewSize,
		minConfidence: cfg.MinConfidence,
		retryDelay:    retryDelay,
		log:           logger,
	}
}

// jobPayload is the JSON shape of an `ocr` job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle is the worker.HandlerFunc for `ocr` jobs: it decodes the photo uid from
// the job payload and recognises that photo's text. A malformed or empty payload
// is a permanent error (the job dead-letters rather than retrying a payload that
// can never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("ocrjob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Recognize(ctx, p.PhotoUID)
}

// Recognize reads the text printed in the photo identified by photoUID and stores
// it, stamping the photo as OCR'd whatever the outcome — an empty reading is a
// finished photo, not a pending one.
//
// It is not idempotent in the "skip if already done" sense and must not be: a
// forced backfill (?all=true) exists precisely to re-read photos with a better
// model, and a handler that skipped anything already stamped would make that
// impossible. Running it twice over an unchanged photo and an unchanged model
// simply writes the same text again.
//
// A video is skipped (nil error, nothing written): OCR runs on stills only, and
// failing the job would dead-letter work that was never meant to happen. When the
// sidecar is offline it returns a worker.RetryAfter error so the job is requeued
// without consuming a retry attempt; any other sidecar or storage failure is
// returned as an ordinary (retryable) error.
func (s *Service) Recognize(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("ocrjob: loading photo %s: %w", photoUID, err)
	}
	if photo.MediaType == photos.MediaVideo {
		s.log.DebugContext(ctx, "ocr skipped: video", slog.String("photo_uid", photo.UID))
		return nil
	}

	result, err := s.recognize(ctx, photo)
	if err != nil {
		if embedding.IsUnavailable(err) {
			// RetryAfter is our own worker control-flow signal, not a foreign error
			// to annotate; wrapping it would obscure the type the worker matches
			// with errors.As.
			return worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
		}
		return err
	}

	if err := s.photos.SaveOCR(ctx, photo.UID, photos.OCR{Text: result.Text, Model: result.Model}); err != nil {
		return fmt.Errorf("ocrjob: storing OCR of %s: %w", photoUID, err)
	}
	return nil
}

// recognize opens the photo's preview thumbnail — rendering or fetching it as
// needed — and streams it to the recogniser. The sidecar error (including the
// offline ErrUnavailable) is returned wrapped so callers can classify it with
// embedding.IsUnavailable.
//
// The preview is asked for by photo rather than opened from the thumbnail cache
// by hand, for the same reason embedjob does it: on an object-store backend the
// preview usually has no local cache file at all, and reading the cache directly
// would leave every job on such a backend dead-lettering with "thumbnail not
// cached".
func (s *Service) recognize(ctx context.Context, photo photos.Photo) (embedding.OCRResult, error) {
	reader, err := s.preview.OpenOrGenerate(ctx, photo, s.previewSize)
	if err != nil {
		return embedding.OCRResult{}, fmt.Errorf("ocrjob: opening preview for %s: %w", photo.UID, err)
	}
	defer func() { _ = reader.Close() }()

	result, err := s.client.ImageOCR(ctx, reader, s.minConfidence)
	if err != nil {
		return embedding.OCRResult{}, fmt.Errorf("ocrjob: recognising %s: %w", photo.UID, err)
	}
	return result, nil
}

// BackfillOCR enqueues an `ocr` job for every photo the recogniser has never
// seen, returning how many uids it scheduled. When all is true it instead
// schedules every non-archived still — a forced full re-run, which is how a
// library picks up a better recognition model.
//
// It only enqueues jobs; the recognition happens later in the worker, so the call
// returns immediately even though draining the queue against the box takes hours
// for a whole library. It is idempotent and resumable: the queue dedupes an
// already-active job per photo, and each photo is stamped the moment its job
// completes, so a run interrupted halfway picks up exactly where it stopped and a
// second run over a drained library enqueues nothing. It returns
// ErrBackfillUnavailable when the Service was built without the Lister and
// Enqueuer collaborators.
func (s *Service) BackfillOCR(ctx context.Context, all bool) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.backfillCandidates(ctx, all)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueOCR(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("ocrjob: enqueuing ocr for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// backfillCandidates returns the uids the backfill should schedule: every
// non-archived still when all is set, otherwise only those never recognised.
func (s *Service) backfillCandidates(ctx context.Context, all bool) ([]string, error) {
	if all {
		uids, err := s.lister.ListActiveImageUIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("ocrjob: listing active photos: %w", err)
		}
		return uids, nil
	}
	uids, err := s.lister.ListPhotosMissingOCR(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("ocrjob: listing photos missing OCR: %w", err)
	}
	return uids, nil
}
