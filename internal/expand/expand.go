// Package expand finds the photos most like the ones already in a collection — an
// album or a label — so a half-tagged library can be finished by adding the ones
// that were missed ("show me photos that look like the ones already on the label
// Ostatky"). It is the collection-level counterpart of the per-photo similarity
// endpoint GET /photos/{uid}/similar: instead of one query vector it votes over
// every member's CLIP image embedding, unions the neighbours, and returns the
// photos several members agree on that are not in the collection yet.
//
// The search is per-photo kNN unioned with voting, deliberately NOT a mean-of-the-
// collection vector: a collection like "Ostatky" is not one visual concept, and
// averaging its embeddings yields a centroid that resembles nothing in it.
//
// It is read-only. It never adds a photo to a collection — that goes through the
// existing POST /photos/bulk path — and it never auto-decides: the vote rule only
// narrows the firehose a raw per-member kNN would otherwise be.
//
// Album and label share one pipeline; only the source-set resolution differs. An
// album has no rejection model (internal/feedback models label and face rejections,
// not album ones), so the rejection and negative-exemplar filters apply to labels
// only. The asymmetry is intentional — it is not papered over with a new table here.
package expand

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// Defaults tune the search when a config value is non-positive. They mirror what
// works in photo-sorter: a 0.30 cosine distance (shown as 70 % similarity), 50
// results capped at 200, a generous per-source over-fetch, and a source-set cap so
// a huge album does not run thousands of kNN queries.
const (
	// DefaultMaxDistance is the fallback maximum cosine distance a candidate may sit
	// from a source photo, and the baseline the vote rule scales against.
	DefaultMaxDistance = 0.30
	// DefaultLimit is the fallback number of candidates returned.
	DefaultLimit = 50
	// DefaultMaxLimit caps a request's own limit so one call cannot ask for an
	// unbounded neighbourhood.
	DefaultMaxLimit = 200
	// DefaultSearchLimit is how many nearest photos each source photo's kNN returns
	// before voting merges them, over-fetching so the later filters do not starve.
	DefaultSearchLimit = 200
	// DefaultSourceCap bounds how many member photos are used as query vectors, so a
	// thousands-strong album is sampled rather than queried in full.
	DefaultSourceCap = 500
	// DefaultConcurrency bounds how many per-source kNN searches run at once.
	DefaultConcurrency = 8
	// minMatchDivisor scales the square-root-of-source-count vote floor down to the
	// 1..5 band; a larger divisor makes the vote rule more permissive.
	minMatchDivisor = 2.0
)

// Kind identifies which sort of collection a result expanded, echoed back so the UI
// can label it.
const (
	// KindAlbum marks a result produced from an album's members.
	KindAlbum = "album"
	// KindLabel marks a result produced from a label's members.
	KindLabel = "label"
)

// Reason explains an empty result so the UI can say why it is empty rather than
// showing a bare "no matches".
const (
	// ReasonEmpty means the collection has no photos at all.
	ReasonEmpty = "empty_collection"
	// ReasonNoEmbeddings means the collection has photos but none of the sampled
	// source photos has a CLIP embedding yet (the sidecar is often offline).
	ReasonNoEmbeddings = "no_source_embeddings"
)

// VectorStore loads a photo's image embedding and runs image kNN over the catalog.
// It is an interface so expand depends on the vectors layer's behaviour, not its
// construction; *vectors.Store satisfies it.
type VectorStore interface {
	// GetEmbedding returns a photo's image embedding, or vectors.ErrEmbeddingNotFound
	// when it has not been embedded yet.
	GetEmbedding(ctx context.Context, photoUID string) (vectors.Embedding, error)
	// FindSimilar returns the photos nearest to vec by cosine distance, nearest
	// first; maxDistance <= 0 disables the distance filter.
	FindSimilar(ctx context.Context, vec []float32, limit int, maxDistance float64) ([]vectors.Match, error)
}

// OrganizeStore resolves collection membership natively and validates a collection
// UID. Kukátko owns its own albums and labels, so this never calls PhotoPrism.
type OrganizeStore interface {
	// GetAlbumByUID returns the album, or organize.ErrAlbumNotFound.
	GetAlbumByUID(ctx context.Context, uid string) (organize.Album, error)
	// ListPhotoUIDs returns an album's member photo UIDs in display order.
	ListPhotoUIDs(ctx context.Context, albumUID string) ([]string, error)
	// GetLabelByUID returns the label, or organize.ErrLabelNotFound.
	GetLabelByUID(ctx context.Context, uid string) (organize.Label, error)
	// ListPhotoUIDsByLabel returns the UIDs of photos carrying the label.
	ListPhotoUIDsByLabel(ctx context.Context, labelUID string) ([]string, error)
}

// FeedbackStore lists the photos a user has rejected for a label. Albums have no
// rejection model, so this is consulted for labels only.
type FeedbackStore interface {
	// LabelRejectionsForLabel returns the UIDs of photos rejected for the label.
	LabelRejectionsForLabel(ctx context.Context, labelUID string) ([]string, error)
}

// PhotoStore hydrates candidate photo UIDs into full catalog records.
type PhotoStore interface {
	// ListByUIDs returns the photo records for uids, in unspecified order, silently
	// omitting UIDs with no row.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// Config bundles the dependencies and tunables of a Service. The four stores are
// required; Media may be nil (a nil *mediaurl.Builder yields the application's own
// media routes).
type Config struct {
	// Vectors loads embeddings and runs the per-source kNN.
	Vectors VectorStore
	// Organize resolves album/label membership and validates the collection UID.
	Organize OrganizeStore
	// Feedback lists label rejections for the rejection and negative-exemplar filters.
	Feedback FeedbackStore
	// Photos hydrates surviving candidate UIDs into records.
	Photos PhotoStore
	// Media stamps thumb_url/download_url onto each candidate photo.
	Media *mediaurl.Builder

	// MaxDistance is the fallback threshold and the vote-rule baseline.
	MaxDistance float64
	// Limit is the fallback result count.
	Limit int
	// MaxLimit caps a request's own limit.
	MaxLimit int
	// SearchLimit is the per-source kNN over-fetch.
	SearchLimit int
	// SourceCap bounds how many members are used as query vectors.
	SourceCap int
	// Concurrency bounds how many per-source searches run at once.
	Concurrency int
}

// Service runs the collection-expansion search. Construct it with New.
type Service struct {
	vectors     VectorStore
	organize    OrganizeStore
	feedback    FeedbackStore
	photos      PhotoStore
	media       *mediaurl.Builder
	maxDistance float64
	limit       int
	maxLimit    int
	searchLimit int
	sourceCap   int
	concurrency int
}

// New returns a Service from cfg, substituting the package defaults for any
// non-positive tunable. It panics when a required store is nil, treating that as a
// wiring bug rather than a runtime condition.
func New(cfg Config) *Service {
	if cfg.Vectors == nil || cfg.Organize == nil || cfg.Feedback == nil || cfg.Photos == nil {
		panic("expand: New requires non-nil Vectors, Organize, Feedback and Photos")
	}
	return &Service{
		vectors:     cfg.Vectors,
		organize:    cfg.Organize,
		feedback:    cfg.Feedback,
		photos:      cfg.Photos,
		media:       cfg.Media,
		maxDistance: orDefaultFloat(cfg.MaxDistance, DefaultMaxDistance),
		limit:       orDefaultInt(cfg.Limit, DefaultLimit),
		maxLimit:    orDefaultInt(cfg.MaxLimit, DefaultMaxLimit),
		searchLimit: orDefaultInt(cfg.SearchLimit, DefaultSearchLimit),
		sourceCap:   orDefaultInt(cfg.SourceCap, DefaultSourceCap),
		concurrency: orDefaultInt(cfg.Concurrency, DefaultConcurrency),
	}
}

// Request is the per-call search input. A zero value uses all configured defaults.
type Request struct {
	// Threshold overrides the maximum cosine distance; non-positive uses the default.
	Threshold float64
	// Limit overrides the result count; non-positive uses the default, and any value
	// is capped at MaxLimit.
	Limit int
}

// Candidate is one expansion result: the photo in the shape the grid consumes (with
// thumb_url/download_url stamped), its distance to the nearest agreeing source
// photo, the similarity (1 - distance), and how many source photos voted for it.
type Candidate struct {
	// Photo is the full catalog record with media URLs stamped.
	Photo photos.Photo `json:"photo"`
	// Distance is the minimum cosine distance to any voting source photo.
	Distance float64 `json:"distance"`
	// Similarity is 1 - Distance, the value the UI shows as a percentage.
	Similarity float64 `json:"similarity"`
	// MatchCount is how many source photos returned this candidate in their kNN.
	MatchCount int `json:"match_count"`
}

// Result is the full response: the ranked candidates plus a summary the UI uses to
// explain thin or empty results (a half-embedded collection, an applied source cap).
type Result struct {
	// Kind is KindAlbum or KindLabel.
	Kind string `json:"kind"`
	// CollectionUID is the album or label the result expanded.
	CollectionUID string `json:"collection_uid"`
	// SourcePhotoCount is the total number of members in the collection.
	SourcePhotoCount int `json:"source_photo_count"`
	// SourcePhotosSampled is how many members were used as query vectors after the cap.
	SourcePhotosSampled int `json:"source_photos_sampled"`
	// SourcePhotosWithEmbedding is how many of the sampled members had an embedding
	// (and so actually contributed a query vector).
	SourcePhotosWithEmbedding int `json:"source_photos_with_embedding"`
	// SourceCapped reports whether the source set was sampled down to the cap.
	SourceCapped bool `json:"source_capped"`
	// SourceCap is the cap that was in force.
	SourceCap int `json:"source_cap"`
	// MinMatchCount is the vote floor a candidate had to clear.
	MinMatchCount int `json:"min_match_count"`
	// Threshold is the maximum cosine distance that was applied.
	Threshold float64 `json:"threshold"`
	// Limit is the effective result cap after defaulting and clamping.
	Limit int `json:"limit"`
	// ResultCount is len(Candidates).
	ResultCount int `json:"result_count"`
	// Reason names why the result is empty (ReasonEmpty/ReasonNoEmbeddings), if it is.
	Reason string `json:"reason,omitempty"`
	// Candidates are the ranked expansion results, never nil (an empty slice encodes
	// as a JSON []).
	Candidates []Candidate `json:"candidates"`
}

// collection is a resolved source set: the members to vote over and exclude, plus
// any label rejections. Albums leave rejected nil.
type collection struct {
	kind     string
	uid      string
	members  []string
	rejected []string
}

// Album expands an album: it validates the album exists (returning
// organize.ErrAlbumNotFound otherwise), resolves its members natively, and votes
// over them. Albums carry no rejections.
func (s *Service) Album(ctx context.Context, albumUID string, req Request) (Result, error) {
	if _, err := s.organize.GetAlbumByUID(ctx, albumUID); err != nil {
		return Result{}, fmt.Errorf("expand: loading album %s: %w", albumUID, err)
	}
	members, err := s.organize.ListPhotoUIDs(ctx, albumUID)
	if err != nil {
		return Result{}, fmt.Errorf("expand: listing album %s members: %w", albumUID, err)
	}
	return s.find(ctx, collection{kind: KindAlbum, uid: albumUID, members: members}, req)
}

// Label expands a label: it validates the label exists (returning
// organize.ErrLabelNotFound otherwise), resolves the photos carrying it, loads its
// rejections, and votes over the members with the rejection and negative-exemplar
// filters applied.
func (s *Service) Label(ctx context.Context, labelUID string, req Request) (Result, error) {
	if _, err := s.organize.GetLabelByUID(ctx, labelUID); err != nil {
		return Result{}, fmt.Errorf("expand: loading label %s: %w", labelUID, err)
	}
	members, err := s.organize.ListPhotoUIDsByLabel(ctx, labelUID)
	if err != nil {
		return Result{}, fmt.Errorf("expand: listing label %s members: %w", labelUID, err)
	}
	rejected, err := s.feedback.LabelRejectionsForLabel(ctx, labelUID)
	if err != nil {
		return Result{}, fmt.Errorf("expand: loading label %s rejections: %w", labelUID, err)
	}
	return s.find(ctx, collection{kind: KindLabel, uid: labelUID, members: members, rejected: rejected}, req)
}

// orDefaultFloat returns value when positive, otherwise fallback.
func orDefaultFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

// orDefaultInt returns value when positive, otherwise fallback.
func orDefaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
