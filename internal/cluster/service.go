package cluster

import (
	"context"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/vectors"
)

// Default tunables, applied when the corresponding Config field is left zero.
const (
	// DefaultThreshold is the maximum cosine distance between two faces for them to
	// be linked as the same person during clustering. ArcFace embeddings of the
	// same identity sit well below this; different identities sit above it.
	DefaultThreshold = 0.4
	// DefaultMinSize is the smallest connected component that becomes a cluster;
	// smaller groups stay unclustered so single stray faces are not surfaced.
	DefaultMinSize = 2
	// DefaultSuggestionMaxDistance is the cosine-distance cutoff for the
	// nearest-named-subject suggestion: named neighbours farther than this from a
	// cluster's centroid do not produce a suggestion.
	DefaultSuggestionMaxDistance = 0.5
)

const (
	// neighborSearchLimit caps how many nearest faces the HNSW search returns per
	// clusterable face when building the similarity graph.
	neighborSearchLimit = 100
	// maxExamples caps how many example faces a cluster listing carries per cluster.
	maxExamples = 4
	// suggestionSearchLimit caps how many nearest faces the suggestion query scans
	// before aggregating the named ones by subject.
	suggestionSearchLimit = 100
)

// FaceSearcher is the subset of vectors.Store the service uses to find a face's
// nearest neighbours (for the clustering graph) and a centroid's nearest named
// candidates (for suggestions).
type FaceSearcher interface {
	// FindSimilarFaces returns the faces closest to vec by cosine distance.
	FindSimilarFaces(
		ctx context.Context, vec []float32, limit int, maxDistance float64,
	) ([]vectors.FaceMatch, error)
	// FindSimilarFaceCandidates returns the nearest faces with their cached subject
	// assignment, used to find the nearest already-named subject.
	FindSimilarFaceCandidates(
		ctx context.Context, vec []float32, limit int, maxDistance float64,
	) ([]vectors.FaceCandidate, error)
}

// FaceAssigner is the subset of facematch.Service the service uses to turn each
// cluster member into a named marker, reusing the single assignment state machine
// rather than duplicating marker creation here.
type FaceAssigner interface {
	// Apply runs one assignment-state transition (here always create_marker),
	// auditing the change with the supplied meta.
	Apply(ctx context.Context, req facematch.AssignRequest, meta audit.Meta) (facematch.AssignResult, error)
}

// Config bundles the Service's collaborators and tunables. Store, Faces and
// Assigner are required; the remaining fields fall back to package defaults when
// left zero.
type Config struct {
	Store                 *Store
	Faces                 FaceSearcher
	Assigner              FaceAssigner
	Threshold             float64
	MinSize               int
	SuggestionMaxDistance float64
}

// Service runs face auto-clustering: building clusters from unassigned faces,
// listing them with representatives and suggestions, assigning a whole cluster to
// a subject and removing a stray face from a cluster.
type Service struct {
	store                 *Store
	faces                 FaceSearcher
	assigner              FaceAssigner
	threshold             float64
	minSize               int
	suggestionMaxDistance float64
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since a missing one is a wiring bug
// that should surface at startup rather than as a nil dereference per request.
func New(cfg Config) *Service {
	if cfg.Store == nil || cfg.Faces == nil || cfg.Assigner == nil {
		panic("cluster: New requires Store, Faces and Assigner")
	}
	return &Service{
		store:                 cfg.Store,
		faces:                 cfg.Faces,
		assigner:              cfg.Assigner,
		threshold:             orDefaultFloat(cfg.Threshold, DefaultThreshold),
		minSize:               orDefaultInt(cfg.MinSize, DefaultMinSize),
		suggestionMaxDistance: orDefaultFloat(cfg.SuggestionMaxDistance, DefaultSuggestionMaxDistance),
	}
}

// orDefaultFloat returns v when positive, else fallback.
func orDefaultFloat(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

// orDefaultInt returns v when positive, else fallback.
func orDefaultInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
