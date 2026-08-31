// Package embedjob wires image embedding into Kukátko's background job
// system and exposes the embedding-backed queries built on top of it.
//
// Its centrepiece is the image_embed job handler: given a photo uid it loads a
// decodable preview, asks the embeddings sidecar for the image vector and
// stores it in the vectors layer. The handler is idempotent (a photo that
// already has an embedding is skipped) and offline-aware: when the sidecar is
// unreachable the job is deferred — requeued without burning a retry attempt —
// so it completes once the box comes back rather than dead-lettering while it
// sleeps.
//
// The same Service also drives the embedding backfill (enqueue an image_embed
// job for every photo missing one) and the embedding-distance duplicate check
// used to flag near-duplicates, complementing the pHash check the upload
// pipeline applies at ingest time.
//
// Every collaborator is an interface so the Service unit-tests with fakes and no
// network, database or filesystem.
package embedjob

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
	// DefaultPreviewSize is the thumbnail rendered (if missing) and sent to the
	// sidecar for embedding. A medium fit thumbnail decodes uniformly for HEIC,
	// RAW and video posters and is far cheaper to ship to the box than the
	// original; the image tower downsamples to a small square regardless.
	DefaultPreviewSize = "fit_720"
	// DefaultOfflineRetryDelay is how long an image_embed job waits before
	// becoming runnable again after the sidecar was found offline.
	DefaultOfflineRetryDelay = 5 * time.Minute
	// dupCandidateLimit bounds how many neighbours the duplicate check fetches;
	// near-duplicates cluster tightly so a small window suffices.
	dupCandidateLimit = 20
)

// ErrMissingPhotoUID indicates an image_embed job payload had no photo_uid.
var ErrMissingPhotoUID = errors.New("embedjob: payload missing photo_uid")

// PhotoStore is the subset of photos.Store the service reads.
type PhotoStore interface {
	// GetByUID returns the photo with the given uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// VectorStore is the subset of vectors.Store the service uses to read, write and
// search embeddings and to enumerate photos still missing one.
type VectorStore interface {
	// GetEmbedding returns a photo's image embedding, or
	// vectors.ErrEmbeddingNotFound.
	GetEmbedding(ctx context.Context, photoUID string) (vectors.Embedding, error)
	// SaveEmbedding stores (or overwrites) a photo's image embedding.
	SaveEmbedding(ctx context.Context, emb vectors.Embedding) (vectors.Embedding, error)
	// FindSimilar returns embeddings nearest to vec within maxDistance.
	FindSimilar(ctx context.Context, vec []float32, limit int, maxDistance float64) ([]vectors.Match, error)
	// ListPhotosMissingEmbedding returns uids of non-archived photos with no
	// embedding yet (limit <= 0 returns all).
	ListPhotosMissingEmbedding(ctx context.Context, limit int) ([]string, error)
}

// Previewer opens a decodable preview image for a photo, producing it if it does
// not exist yet. It is satisfied by thumb.Thumbnailer.
type Previewer interface {
	// OpenOrGenerate returns a reader over the photo's preview at size, from
	// wherever it is available (local cache or object store) and generating it
	// when it is available nowhere.
	OpenOrGenerate(ctx context.Context, photo photos.Photo, size string) (io.ReadCloser, error)
}

// Enqueuer schedules image_embed jobs for the backfill. It is satisfied by
// jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueImageEmbed schedules embedding for photoUID, treating an existing
	// active job as a no-op.
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
}

// Config bundles the Service's collaborators and tunables. The four
// collaborators and Client are required; the remaining fields fall back to
// package defaults when left zero.
type Config struct {
	// Photos resolves a photo uid to its catalogue record.
	Photos PhotoStore
	// Vectors reads, writes and searches embeddings.
	Vectors VectorStore
	// Client is the embeddings sidecar client.
	Client embedding.Client
	// Previewer renders/opens the preview image sent to the sidecar.
	Previewer Previewer
	// Enqueuer schedules backfill jobs.
	Enqueuer Enqueuer
	// PreviewSize is the thumbnail size embedded (default DefaultPreviewSize).
	PreviewSize string
	// OfflineRetryDelay is the deferral applied when the box is offline (default
	// DefaultOfflineRetryDelay).
	OfflineRetryDelay time.Duration
	// DuplicateMaxDist is the maximum cosine distance Duplicates treats as a
	// near-duplicate (config duplicate.embedding_max_dist). <= 0 disables it.
	DuplicateMaxDist float64
}

// Service computes and stores image embeddings, backfills them, and answers
// embedding-distance duplicate queries.
type Service struct {
	photos      PhotoStore
	vectors     VectorStore
	client      embedding.Client
	preview     Previewer
	enqueuer    Enqueuer
	previewSize string
	retryDelay  time.Duration
	dupMaxDist  float64
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since none has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Vectors == nil || cfg.Client == nil ||
		cfg.Previewer == nil || cfg.Enqueuer == nil {
		panic("embedjob: New requires Photos, Vectors, Client, Previewer and Enqueuer")
	}
	previewSize := cfg.PreviewSize
	if previewSize == "" {
		previewSize = DefaultPreviewSize
	}
	retryDelay := cfg.OfflineRetryDelay
	if retryDelay <= 0 {
		retryDelay = DefaultOfflineRetryDelay
	}
	return &Service{
		photos:      cfg.Photos,
		vectors:     cfg.Vectors,
		client:      cfg.Client,
		preview:     cfg.Previewer,
		enqueuer:    cfg.Enqueuer,
		previewSize: previewSize,
		retryDelay:  retryDelay,
		dupMaxDist:  cfg.DuplicateMaxDist,
	}
}

// jobPayload is the JSON shape of an image_embed job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
	// Force asks for the rebuild rather than the repair: the vector is recomputed
	// and overwritten even though the photo already has one. The repair path skips
	// such a photo, which is right when the embedding is merely missing and wrong
	// when the stored one describes the wrong picture — a preview that has since
	// been corrected. See jobs.Enqueuer.EnqueueImageEmbedRebuild.
	Force bool `json:"force"`
}

// Handle is the worker.HandlerFunc for image_embed jobs: it decodes the photo
// uid from the job payload and embeds it. A malformed or empty payload is a
// permanent error (the job will eventually dead-letter rather than retry a
// payload that can never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("embedjob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	if p.Force {
		return s.ForceEmbed(ctx, p.PhotoUID)
	}
	return s.Embed(ctx, p.PhotoUID)
}

// Embed computes and stores the image embedding for photoUID. It is
// idempotent: a photo that already has an embedding returns nil without calling
// the sidecar. When the sidecar is offline it returns a worker.RetryAfter error
// so the job is requeued without consuming a retry attempt; any other sidecar or
// storage failure is returned as an ordinary (retryable) error. A missing photo
// is returned as an error so the job fails and dead-letters rather than looping.
func (s *Service) Embed(ctx context.Context, photoUID string) error {
	return s.embed(ctx, photoUID, false)
}

// ForceEmbed recomputes the image embedding for photoUID and replaces the stored
// one, where Embed would skip a photo that already has an embedding. It is the
// counterpart to Embed in the sense thumbjob.ForceRegenerate is to
// thumbjob.Regenerate: the repair fills a gap, the rebuild corrects a wrong
// answer — an embedding computed from a preview that has since been fixed, or by
// a model that has since changed.
//
// Everything else is Embed's behaviour, deliberately: the sidecar being offline
// still yields a worker.RetryAfter (so a forced job waits for the box rather than
// dead-lettering, and an on-demand caller can queue it instead of failing), and a
// missing photo is still an error. There is exactly one embedding row per photo,
// so the recomputed vector replaces the old one; nothing is appended.
func (s *Service) ForceEmbed(ctx context.Context, photoUID string) error {
	return s.embed(ctx, photoUID, true)
}

// embed computes and stores the photo's embedding, skipping a photo that already
// has one unless force is set. It is the shared body of Embed and ForceEmbed, so
// the two differ in exactly one thing — whether the existing evidence is a reason
// to stop.
func (s *Service) embed(ctx context.Context, photoUID string, force bool) error {
	if !force {
		switch _, err := s.vectors.GetEmbedding(ctx, photoUID); {
		case err == nil:
			return nil // already embedded — idempotent skip
		case errors.Is(err, vectors.ErrEmbeddingNotFound):
			// fall through and compute it
		default:
			return fmt.Errorf("embedjob: checking existing embedding for %s: %w", photoUID, err)
		}
	}

	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("embedjob: loading photo %s: %w", photoUID, err)
	}

	vec, model, pretrained, err := s.computeEmbedding(ctx, photo)
	if err != nil {
		if embedding.IsUnavailable(err) {
			// RetryAfter is our own worker control-flow signal, not a foreign
			// error to annotate; wrapping it would obscure the type the worker
			// matches with errors.As.
			return worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
		}
		return err
	}

	if _, err := s.vectors.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID:   photoUID,
		Vector:     vec,
		Model:      model,
		Pretrained: pretrained,
	}); err != nil {
		return fmt.Errorf("embedjob: saving embedding for %s: %w", photoUID, err)
	}
	return nil
}

// computeEmbedding opens the photo's preview thumbnail — rendering or fetching
// it as needed — and streams it to the sidecar, returning the embedding vector
// and the sidecar's model tags. The sidecar error (including the offline
// ErrUnavailable) is returned wrapped so callers can classify it with
// embedding.IsUnavailable.
//
// The preview is asked for by photo rather than opened from the thumbnail cache
// by hand: on an object-store backend the preview usually has no local cache file
// at all (the thumbnail job published it and the cache is pruned), and reading
// the cache directly is what left every image_embed job on such a backend
// dead-lettering with "thumbnail not cached".
func (s *Service) computeEmbedding(
	ctx context.Context, photo photos.Photo,
) (vec []float32, model, pretrained string, err error) {
	reader, err := s.preview.OpenOrGenerate(ctx, photo, s.previewSize)
	if err != nil {
		return nil, "", "", fmt.Errorf("embedjob: opening preview for %s: %w", photo.UID, err)
	}
	defer func() { _ = reader.Close() }()

	vec, model, pretrained, err = s.client.ImageEmbedding(ctx, reader)
	if err != nil {
		return nil, "", "", fmt.Errorf("embedjob: embedding %s: %w", photo.UID, err)
	}
	return vec, model, pretrained, nil
}

// BackfillEmbeddings enqueues an image_embed job for every non-archived photo
// that has no embedding yet, returning how many uids it scheduled. Photos that
// already have an embedding are never touched, and a photo whose job is already
// queued is a harmless no-op (the enqueuer dedupes), so the backfill is safe to
// run repeatedly.
func (s *Service) BackfillEmbeddings(ctx context.Context) (int, error) {
	uids, err := s.vectors.ListPhotosMissingEmbedding(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("embedjob: listing photos missing embedding: %w", err)
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueImageEmbed(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("embedjob: enqueuing image_embed for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// Duplicates returns the photos whose embedding is within the configured maximum
// cosine distance of photoUID's embedding, nearest first, excluding photoUID
// itself. It returns nil (no duplicates) when photoUID has no embedding yet or
// when the duplicate distance is disabled (<= 0). This is the embedding-based
// near-duplicate check; the upload pipeline applies the pHash check at ingest
// time, before any embedding exists.
func (s *Service) Duplicates(ctx context.Context, photoUID string) ([]vectors.Match, error) {
	if s.dupMaxDist <= 0 {
		return nil, nil
	}
	emb, err := s.vectors.GetEmbedding(ctx, photoUID)
	if errors.Is(err, vectors.ErrEmbeddingNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("embedjob: loading embedding for %s: %w", photoUID, err)
	}
	matches, err := s.vectors.FindSimilar(ctx, emb.Vector, dupCandidateLimit, s.dupMaxDist)
	if err != nil {
		return nil, fmt.Errorf("embedjob: finding duplicates for %s: %w", photoUID, err)
	}
	return excludeUID(matches, photoUID), nil
}

// excludeUID returns matches with any entry for uid removed, preserving order.
// A photo is always its own nearest neighbour (distance 0), so self-exclusion is
// needed whenever a photo's own vector is used as the query.
func excludeUID(matches []vectors.Match, uid string) []vectors.Match {
	out := matches[:0]
	for _, m := range matches {
		if m.PhotoUID != uid {
			out = append(out, m)
		}
	}
	return out
}
