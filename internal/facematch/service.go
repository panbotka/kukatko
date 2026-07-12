package facematch

import (
	"context"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// Default tunables, mirroring photo-sorter where applicable. They apply when the
// corresponding Config field is left zero.
const (
	// DefaultIoUThreshold is the minimum overlap for a face↔marker match.
	DefaultIoUThreshold = 0.1
	// DefaultSuggestionLimit caps suggested subjects per unnamed face.
	DefaultSuggestionLimit = 5
	// DefaultSuggestionMaxDistance is the primary cosine-distance cutoff before the
	// threshold fallback widens the suggestion search.
	DefaultSuggestionMaxDistance = 0.5
	// DefaultMinFaceSize is the minimum normalised width a neighbouring face needs
	// to contribute a suggestion.
	DefaultMinFaceSize = 0.02
	// suggestionSearchLimit is how many nearest faces each suggestion query scans
	// before they are aggregated by subject.
	suggestionSearchLimit = 200
)

// PhotoStore resolves a photo to its stored display dimensions and orientation.
type PhotoStore interface {
	// GetByUID returns the photo with the given uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// FaceStore is the subset of vectors.Store the service uses to read a photo's
// faces, search neighbouring faces, and cache the matched marker on a face.
type FaceStore interface {
	// ListFaces returns every stored face of the photo, ordered by face index.
	ListFaces(ctx context.Context, photoUID string) ([]vectors.Face, error)
	// FindSimilarFaceCandidates returns nearest faces with their cached assignment.
	FindSimilarFaceCandidates(
		ctx context.Context, vec []float32, limit int, maxDistance float64,
	) ([]vectors.FaceCandidate, error)
	// UpdateFaceMarker caches the marker/subject assignment on one face.
	UpdateFaceMarker(
		ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string,
	) error
}

// PeopleStore is the subset of people.Store the service uses to read markers and to
// drive the subject/marker assignment state machine. The marker-mutating methods
// take an audit.Entry the store writes in the same transaction as the change, so a
// face assignment and the record of who made it commit atomically.
type PeopleStore interface {
	// ListMarkersByPhoto returns every marker on the photo, oldest first.
	ListMarkersByPhoto(ctx context.Context, photoUID string) ([]people.Marker, error)
	// CreateMarkerAudited inserts a marker (optionally already naming a subject),
	// auditing the change.
	CreateMarkerAudited(ctx context.Context, m people.Marker, entry audit.Entry) (people.Marker, error)
	// AssignSubjectAudited points a marker at a subject, refreshes the faces cache
	// and audits the change.
	AssignSubjectAudited(
		ctx context.Context, markerUID, subjectUID string, entry audit.Entry,
	) (people.Marker, error)
	// UnassignSubjectAudited clears a marker's subject and the faces cache, auditing
	// the change.
	UnassignSubjectAudited(ctx context.Context, markerUID string, entry audit.Entry) (people.Marker, error)
	// SetMarkerReviewed sets or clears the reviewed flag on a marker.
	SetMarkerReviewed(ctx context.Context, uid string, reviewed bool) (people.Marker, error)
	// GetSubjectByUID returns a subject by uid, or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
	// GetSubjectBySlug returns a subject by slug, or people.ErrSubjectNotFound.
	GetSubjectBySlug(ctx context.Context, slug string) (people.Subject, error)
	// CreateSubject inserts a subject, generating a unique slug from its name. The
	// auto-create-by-name path is incidental to a face assignment (which carries its
	// own audit entry naming the subject), so it is not separately audited.
	CreateSubject(ctx context.Context, s people.Subject) (people.Subject, error)
}

// Config bundles the Service's collaborators and tunables. The three stores are
// required; the remaining fields fall back to package defaults when left zero.
type Config struct {
	Photos                PhotoStore
	Faces                 FaceStore
	People                PeopleStore
	IoUThreshold          float64
	SuggestionLimit       int
	SuggestionMaxDistance float64
	MinFaceSize           float64
}

// Service matches faces to markers, drives the assignment state machine and builds
// identity suggestions.
type Service struct {
	photos          PhotoStore
	faces           FaceStore
	people          PeopleStore
	iouThreshold    float64
	suggestionLimit int
	maxDistance     float64
	minFaceSize     float64
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required store is nil, since a missing one is a wiring bug that
// should surface at startup rather than as a nil dereference per request.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Faces == nil || cfg.People == nil {
		panic("facematch: New requires Photos, Faces and People stores")
	}
	return &Service{
		photos:          cfg.Photos,
		faces:           cfg.Faces,
		people:          cfg.People,
		iouThreshold:    orDefaultFloat(cfg.IoUThreshold, DefaultIoUThreshold),
		suggestionLimit: orDefaultInt(cfg.SuggestionLimit, DefaultSuggestionLimit),
		maxDistance:     orDefaultFloat(cfg.SuggestionMaxDistance, DefaultSuggestionMaxDistance),
		minFaceSize:     orDefaultFloat(cfg.MinFaceSize, DefaultMinFaceSize),
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

// markerBox returns a marker's normalised box in [x, y, w, h] form for IoU.
func markerBox(m people.Marker) [4]float64 {
	return [4]float64{m.X, m.Y, m.W, m.H}
}

// findBestMarker returns the face-type, non-invalid marker overlapping faceBBox the
// most, together with its IoU, or nil when none reaches the matching threshold.
func (s *Service) findBestMarker(faceBBox [4]float64, markers []people.Marker) (*people.Marker, float64) {
	var best *people.Marker
	bestIoU := 0.0
	for i := range markers {
		if markers[i].Type != people.MarkerFace || markers[i].Invalid {
			continue
		}
		score := IoU(faceBBox, markerBox(markers[i]))
		if score > bestIoU {
			bestIoU = score
			best = &markers[i]
		}
	}
	if best == nil || bestIoU < s.iouThreshold {
		return nil, 0
	}
	return best, bestIoU
}
