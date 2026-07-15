// Package candidates answers "where else does this person appear, that nobody has
// named yet?". Given a named subject it loads the subject's own tagged faces as
// exemplars, runs a kNN over the unassigned faces (subject_uid IS NULL) from each
// exemplar, and merges the neighbours with a per-candidate vote count. A candidate
// must be voted for by enough exemplars, must not be a face the user already
// rejected for this subject (nor trip the negative-exemplar margin rule), and must
// be large enough to review. Survivors are returned nearest-first, each tagged with
// the action that confirming it would take.
//
// The package is read-only: it computes candidates and classifies them, but every
// mutation still goes through the existing face-assignment path
// (POST /photos/{uid}/faces/assign in internal/facematch). It reads vectors already
// in Postgres, so the embeddings sidecar being offline does not matter here. The
// all-people sweep can call Find per subject without re-implementing any of this.
package candidates

import (
	"context"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// Default tunables, applied when a Config field is left non-positive. They mirror
// the values that work in photo-sorter.
const (
	// DefaultMaxDistance is the fallback maximum cosine distance a candidate may sit
	// from an exemplar, and the baseline the vote rule scales against.
	DefaultMaxDistance = 0.5
	// DefaultSearchLimit is the fallback per-exemplar kNN result cap.
	DefaultSearchLimit = 1000
	// DefaultConcurrency is the fallback bound on concurrent exemplar searches.
	DefaultConcurrency = 8
	// DefaultMinFacePx is the fallback minimum reviewable face width in pixels.
	DefaultMinFacePx = 32
)

// minMatchDivisor scales the vote rule: min_match_count grows with the square root
// of the exemplar count over this divisor, so small source sets ask for one vote
// while large ones ask for several. See computeMinMatchCount.
const minMatchDivisor = 2.0

// Reasons a search returns no candidates for a structural (non-error) cause. They
// let the UI explain the emptiness instead of showing a blank result.
const (
	// ReasonNoFaces means the subject has no tagged faces at all yet.
	ReasonNoFaces = "no_faces"
	// ReasonNoEmbeddings means the subject is tagged on photos but none of those
	// faces carry an embedding (the sidecar was offline when they were detected), so
	// there is nothing to search from.
	ReasonNoEmbeddings = "no_embeddings"
)

// Action classifies what confirming a candidate would do, so the UI can label the
// button and the caller can route the follow-up assign call.
type Action string

const (
	// ActionCreateMarker means the face has no marker yet: confirming creates one.
	ActionCreateMarker Action = "create_marker"
	// ActionAssignPerson means the face has a marker but no (matching) subject:
	// confirming assigns the person.
	ActionAssignPerson Action = "assign_person"
	// ActionAlreadyDone means the face already belongs to this subject — a rare
	// stale-cache case where confirming is a no-op.
	ActionAlreadyDone Action = "already_done"
)

// FaceStore is the subset of vectors.Store the service reads. It is an interface so
// the service is unit-testable with a fake; *vectors.Store satisfies it.
type FaceStore interface {
	// ListFacesBySubject returns every face cached as assigned to subjectUID.
	ListFacesBySubject(ctx context.Context, subjectUID string) ([]vectors.Face, error)
	// FindSimilarUnassignedFaceCandidates returns the nearest unassigned faces to
	// vec, excluding the given keys, within maxDistance.
	FindSimilarUnassignedFaceCandidates(
		ctx context.Context, vec []float32, limit int, maxDistance float64, exclude []vectors.FaceKey,
	) ([]vectors.FaceCandidate, error)
	// FacesByKeys returns the face rows for the given keys (embeddings included).
	FacesByKeys(ctx context.Context, keys []vectors.FaceKey) ([]vectors.Face, error)
}

// PeopleStore is the subset of people.Store the service reads: it validates the
// subject, counts its marked photos, and resolves a candidate's marker to classify
// the action. *people.Store satisfies it.
type PeopleStore interface {
	// GetSubjectByUID returns the subject, or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
	// GetMarkerByUID returns the marker, or people.ErrMarkerNotFound.
	GetMarkerByUID(ctx context.Context, uid string) (people.Marker, error)
	// ListPhotoUIDsBySubject returns the distinct photos carrying a non-invalid
	// marker for the subject; the gap versus embedded faces is what "no embeddings"
	// reports.
	ListPhotoUIDsBySubject(ctx context.Context, subjectUID string) ([]string, error)
}

// FeedbackStore is the subset of feedback.Store the service reads: the faces a user
// has rejected for a subject, used both as a coarse SQL exclusion and to feed the
// negative-exemplar margin rule. *feedback.Store satisfies it.
type FeedbackStore interface {
	// FaceRejectionsForSubject returns the faces rejected as "not this person".
	FaceRejectionsForSubject(ctx context.Context, subjectUID string) ([]feedback.FaceRef, error)
}

// PhotoStore is the subset of photos.Store the service reads: the photo records for
// the surviving candidates, so the response can carry stamped media URLs and the
// pixel dimensions the bounding box is projected into. *photos.Store satisfies it.
type PhotoStore interface {
	// ListByUIDs returns the photos with the given uids, in an unspecified order.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// Config bundles the Service's collaborators and tunables. The four stores and the
// media builder are required; the numeric tunables fall back to their Default* when
// non-positive.
type Config struct {
	// Faces, People, Feedback and Photos are the data sources.
	Faces    FaceStore
	People   PeopleStore
	Feedback FeedbackStore
	Photos   PhotoStore
	// Media stamps thumb/download URLs onto candidate photos. A nil-store builder is
	// valid (it stamps the app's own routes); a nil *Builder is not.
	Media *mediaurl.Builder
	// MaxDistance, SearchLimit, MinFacePx and Concurrency are the tunables; see the
	// Default* constants.
	MaxDistance float64
	SearchLimit int
	MinFacePx   int
	Concurrency int
	// MinFaceRel is the minimum normalised face width (0..1), reused from
	// faces.min_face_size. A non-positive value disables the relative floor.
	MinFaceRel float64
}

// Service computes untagged-face candidates for a subject.
type Service struct {
	faces       FaceStore
	people      PeopleStore
	feedback    FeedbackStore
	photos      PhotoStore
	media       *mediaurl.Builder
	maxDistance float64
	searchLimit int
	minFacePx   int
	concurrency int
	minFaceRel  float64
}

// New returns a Service from cfg, applying the Default* tunables where cfg leaves a
// value non-positive. It panics if any required store or the media builder is nil,
// treating that as a startup wiring bug rather than a per-request error.
func New(cfg Config) *Service {
	if cfg.Faces == nil || cfg.People == nil || cfg.Feedback == nil || cfg.Photos == nil || cfg.Media == nil {
		panic("candidates: New requires non-nil Faces, People, Feedback, Photos and Media")
	}
	return &Service{
		faces:       cfg.Faces,
		people:      cfg.People,
		feedback:    cfg.Feedback,
		photos:      cfg.Photos,
		media:       cfg.Media,
		maxDistance: orDefaultFloat(cfg.MaxDistance, DefaultMaxDistance),
		searchLimit: orDefaultInt(cfg.SearchLimit, DefaultSearchLimit),
		minFacePx:   orDefaultInt(cfg.MinFacePx, DefaultMinFacePx),
		concurrency: orDefaultInt(cfg.Concurrency, DefaultConcurrency),
		minFaceRel:  cfg.MinFaceRel,
	}
}

// Request are the per-call search parameters.
type Request struct {
	// Threshold is the maximum cosine distance a candidate may sit from an exemplar.
	// A non-positive value uses the configured default.
	Threshold float64 `json:"threshold"`
	// Limit caps how many candidates are returned; 0 means all.
	Limit int `json:"limit"`
}

// FaceBox is a candidate face's bounding box in both spaces the UI needs:
// display-relative (0..1, already EXIF-oriented) and display pixels.
type FaceBox struct {
	// Relative is [x, y, w, h] in 0..1.
	Relative [4]float64 `json:"relative"`
	// Pixel is [x, y, w, h] in display pixels (EXIF-oriented).
	Pixel [4]int `json:"pixel"`
}

// Candidate is one untagged face that resembles the subject.
type Candidate struct {
	// Photo is the owning photo, with media URLs stamped.
	Photo photos.Photo `json:"photo"`
	// FaceIndex identifies the face within its photo.
	FaceIndex int `json:"face_index"`
	// BBox is the face box in relative and pixel space.
	BBox FaceBox `json:"bbox"`
	// Distance is the minimum cosine distance to any voting exemplar (nearest wins).
	Distance float64 `json:"distance"`
	// MatchCount is how many distinct exemplars returned this face.
	MatchCount int `json:"match_count"`
	// Action is what confirming this candidate would do.
	Action Action `json:"action"`
}

// Counts is the number of candidates per action, for a summary the UI can show
// without walking the list.
type Counts struct {
	CreateMarker int `json:"create_marker"`
	AssignPerson int `json:"assign_person"`
	AlreadyDone  int `json:"already_done"`
}

// Result is the search outcome for one subject.
type Result struct {
	// SubjectUID echoes the searched subject.
	SubjectUID string `json:"subject_uid"`
	// SourcePhotoCount is how many distinct photos contributed an exemplar (one per
	// photo), and SourceFaceCount how many embedded faces the subject has.
	SourcePhotoCount int `json:"source_photo_count"`
	SourceFaceCount  int `json:"source_face_count"`
	// FacesWithoutEmbedding is how many of the subject's marked photos have no
	// embedded face to search from (the sidecar was offline). Surfaced, not hidden.
	FacesWithoutEmbedding int `json:"faces_without_embedding"`
	// MinMatchCount is the computed vote threshold that was applied.
	MinMatchCount int `json:"min_match_count"`
	// Threshold is the maximum cosine distance actually used (after defaulting).
	Threshold float64 `json:"threshold"`
	// Reason is set when the result is empty for a structural cause (ReasonNoFaces
	// or ReasonNoEmbeddings); empty otherwise.
	Reason string `json:"reason,omitempty"`
	// Counts summarises the candidates per action.
	Counts Counts `json:"counts"`
	// Candidates are the surviving untagged faces, nearest first.
	Candidates []Candidate `json:"candidates"`
}

// Find computes the untagged-face candidates for subjectUID under req. It returns
// people.ErrSubjectNotFound when no such subject exists. A subject with no exemplars
// yields a non-error empty Result carrying a Reason. The work stays in SQL and is
// bounded: exemplar searches run with a concurrency cap and only the filtered
// survivors are hydrated into memory.
func (s *Service) Find(ctx context.Context, subjectUID string, req Request) (Result, error) {
	if _, err := s.people.GetSubjectByUID(ctx, subjectUID); err != nil {
		return Result{}, fmt.Errorf("loading subject %s: %w", subjectUID, err)
	}
	src, err := s.loadSource(ctx, subjectUID)
	if err != nil {
		return Result{}, err
	}
	threshold := orDefaultFloat(req.Threshold, s.maxDistance)
	result := s.baseResult(subjectUID, src, threshold)
	if len(src.exemplars) == 0 {
		result.Reason = src.emptyReason
		return result, nil
	}

	minMatch := computeMinMatchCount(len(src.exemplars), threshold, s.maxDistance)
	result.MinMatchCount = minMatch

	rejected, err := s.feedback.FaceRejectionsForSubject(ctx, subjectUID)
	if err != nil {
		return Result{}, fmt.Errorf("loading rejections for subject %s: %w", subjectUID, err)
	}
	voted, err := s.search(ctx, src.exemplars, threshold, rejectionKeys(rejected))
	if err != nil {
		return Result{}, err
	}
	survivors := filterVoted(voted, minMatch, s.minFaceRel)

	built, err := s.build(ctx, subjectUID, survivors, src.acceptedVecs, rejected)
	if err != nil {
		return Result{}, err
	}
	sortByDistance(built)
	built = truncate(built, req.Limit)

	result.Candidates = built
	result.Counts = countActions(built)
	return result, nil
}

// baseResult seeds a Result with the source-set summary shared by every outcome
// (empty or not); the caller fills MinMatchCount, Reason, Counts and Candidates.
func (s *Service) baseResult(subjectUID string, src source, threshold float64) Result {
	return Result{
		SubjectUID:            subjectUID,
		SourcePhotoCount:      src.photoCount,
		SourceFaceCount:       src.faceCount,
		FacesWithoutEmbedding: src.withoutEmbedding,
		Threshold:             threshold,
		Candidates:            []Candidate{},
	}
}

// orDefaultFloat returns value when positive, else fallback.
func orDefaultFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

// orDefaultInt returns value when positive, else fallback.
func orDefaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// sortByDistance orders candidates nearest first, breaking ties on (photo, face)
// for a deterministic result.
func sortByDistance(cands []Candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		switch {
		case cands[i].Distance != cands[j].Distance:
			return cands[i].Distance < cands[j].Distance
		case cands[i].Photo.UID != cands[j].Photo.UID:
			return cands[i].Photo.UID < cands[j].Photo.UID
		default:
			return cands[i].FaceIndex < cands[j].FaceIndex
		}
	})
}

// truncate returns the first limit candidates, or all of them when limit is
// non-positive.
func truncate(cands []Candidate, limit int) []Candidate {
	if limit > 0 && len(cands) > limit {
		return cands[:limit]
	}
	return cands
}

// countActions tallies the candidates by action for the summary.
func countActions(cands []Candidate) Counts {
	var counts Counts
	for i := range cands {
		switch cands[i].Action {
		case ActionCreateMarker:
			counts.CreateMarker++
		case ActionAssignPerson:
			counts.AssignPerson++
		case ActionAlreadyDone:
			counts.AlreadyDone++
		}
	}
	return counts
}
