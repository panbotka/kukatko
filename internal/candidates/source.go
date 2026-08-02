package candidates

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/panbotka/kukatko/internal/vectors"
)

// source is the subject's evidence for one search: the deduplicated exemplars that
// seed the kNN, the embedded faces' vectors (the positive set for the
// negative-exemplar rule), and the counts the Result reports. Both sets are capped
// at maxExemplars — see sampleFaces for why.
type source struct {
	// exemplars is one face per source photo — the highest-confidence face — so a
	// photo with three faces of the person casts a single vote, not three.
	exemplars []vectors.Face
	// acceptedVecs is the embedded faces' vectors, the positive evidence.
	acceptedVecs [][]float32
	// photoCount is how many photos contributed an exemplar before the cap;
	// faceCount is every embedded face.
	photoCount int
	faceCount  int
	// capped reports that the subject has more exemplars than the cap allows, so
	// exemplars is a sample of them rather than all of them.
	capped bool
	// withoutEmbedding is how many marked photos have no embedded face to search
	// from.
	withoutEmbedding int
	// emptyReason is set only when exemplars is empty.
	emptyReason string
}

// loadSource reads a bounded sample of the subject's tagged faces, deduplicates it
// to one exemplar per photo, and computes the source-set summary — including how
// many of the subject's marked photos lack an embedded face (the "sidecar was
// offline" gap). The counts come from the sample's totals, not from its length, so
// capping the source set does not distort what the search reports about it.
//
// The cap is a memory bound, and it is the reason this reads a sample at all: one
// exemplar means one kNN query and one result set to merge, so an uncapped source
// set puts a single request's allocation on the library's growth curve. One subject
// holding 16 532 exemplars is what let a GET /review/queue grow the process to
// 10.9 GB and take the host down with it. It costs little recall — the vote rule
// clamps at five agreeing exemplars, which hundreds supply as well as thousands.
func (s *Service) loadSource(ctx context.Context, subjectUID string) (source, error) {
	sample, err := s.faces.SampleFacesBySubject(ctx, subjectUID, s.maxExemplars)
	if err != nil {
		return source{}, fmt.Errorf("sampling faces for subject %s: %w", subjectUID, err)
	}
	markedPhotos, err := s.people.ListPhotoUIDsBySubject(ctx, subjectUID)
	if err != nil {
		return source{}, fmt.Errorf("listing marked photos for subject %s: %w", subjectUID, err)
	}

	exemplars := dedupExemplars(sample.Faces)
	src := source{
		exemplars:        exemplars,
		acceptedVecs:     vectorsOf(sample.Faces),
		photoCount:       sample.Photos,
		faceCount:        sample.Total,
		capped:           sample.Total > len(sample.Faces),
		withoutEmbedding: max(0, len(markedPhotos)-sample.Photos),
	}
	if len(exemplars) == 0 {
		src.emptyReason = emptyReason(len(markedPhotos))
	}
	return src, nil
}

// dedupExemplars keeps one face per photo — the highest det_score, breaking ties on
// the lowest face_index — and returns them in a deterministic (photo, face) order.
// This is the "one exemplar per source photo" rule that stops a photo with several
// faces of the same person from over-voting.
func dedupExemplars(faces []vectors.Face) []vectors.Face {
	best := make(map[string]vectors.Face, len(faces))
	for _, face := range faces {
		if current, ok := best[face.PhotoUID]; !ok || betterExemplar(face, current) {
			best[face.PhotoUID] = face
		}
	}
	out := make([]vectors.Face, 0, len(best))
	for _, face := range best {
		out = append(out, face)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PhotoUID != out[j].PhotoUID {
			return out[i].PhotoUID < out[j].PhotoUID
		}
		return out[i].FaceIndex < out[j].FaceIndex
	})
	return out
}

// betterExemplar reports whether a is a better exemplar than b: higher detector
// confidence wins, ties break on the lower face index.
func betterExemplar(a, b vectors.Face) bool {
	if a.DetScore != b.DetScore {
		return a.DetScore > b.DetScore
	}
	return a.FaceIndex < b.FaceIndex
}

// vectorsOf projects the faces' embeddings into the [][]float32 the vectors margin
// helpers expect.
func vectorsOf(faces []vectors.Face) [][]float32 {
	out := make([][]float32, 0, len(faces))
	for i := range faces {
		out = append(out, faces[i].Vector)
	}
	return out
}

// emptyReason distinguishes a subject with nothing tagged (ReasonNoFaces) from one
// tagged on photos whose faces carry no embedding (ReasonNoEmbeddings).
func emptyReason(markedPhotoCount int) string {
	if markedPhotoCount > 0 {
		return ReasonNoEmbeddings
	}
	return ReasonNoFaces
}

// computeMinMatchCount is the vote rule: how many distinct exemplars must return a
// candidate for it to survive. It scales with the square root of the exemplar count
// (more exemplars — more chances for a spurious single match — so demand more
// agreement) and linearly with how loose the threshold is versus the configured
// baseline, then clamps to 1..5 and never exceeds the exemplar count. A one-exemplar
// subject therefore always yields 1. This is the single most important quality
// lever; returning it lets the UI explain the filter.
func computeMinMatchCount(exemplarCount int, threshold, baseThreshold float64) int {
	if exemplarCount <= 0 {
		return 0
	}
	ratio := 1.0
	if baseThreshold > 0 {
		ratio = threshold / baseThreshold
	}
	raw := int(math.Round(math.Sqrt(float64(exemplarCount)) * ratio / minMatchDivisor))
	return clampMatchCount(raw, exemplarCount)
}

// clampMatchCount confines a raw vote count to 1..5 and to at most exemplarCount, so
// the rule can never demand more votes than there are exemplars to cast them.
func clampMatchCount(raw, exemplarCount int) int {
	ceiling := min(5, exemplarCount)
	if raw < 1 {
		return 1
	}
	if raw > ceiling {
		return ceiling
	}
	return raw
}
