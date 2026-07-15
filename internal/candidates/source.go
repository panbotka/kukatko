package candidates

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/panbotka/kukatko/internal/vectors"
)

// source is the subject's evidence for one search: the deduplicated exemplars that
// seed the kNN, every embedded face's vector (the positive set for the
// negative-exemplar rule), and the counts the Result reports.
type source struct {
	// exemplars is one face per source photo — the highest-confidence face — so a
	// photo with three faces of the person casts a single vote, not three.
	exemplars []vectors.Face
	// acceptedVecs is every embedded face's vector, the full positive evidence.
	acceptedVecs [][]float32
	// photoCount is len(exemplars); faceCount is every embedded face.
	photoCount int
	faceCount  int
	// withoutEmbedding is how many marked photos have no embedded face to search
	// from.
	withoutEmbedding int
	// emptyReason is set only when exemplars is empty.
	emptyReason string
}

// loadSource loads the subject's tagged faces, deduplicates them to one exemplar
// per photo, and computes the source-set summary — including how many of the
// subject's marked photos lack an embedded face (the "sidecar was offline" gap).
func (s *Service) loadSource(ctx context.Context, subjectUID string) (source, error) {
	faces, err := s.faces.ListFacesBySubject(ctx, subjectUID)
	if err != nil {
		return source{}, fmt.Errorf("listing faces for subject %s: %w", subjectUID, err)
	}
	markedPhotos, err := s.people.ListPhotoUIDsBySubject(ctx, subjectUID)
	if err != nil {
		return source{}, fmt.Errorf("listing marked photos for subject %s: %w", subjectUID, err)
	}

	exemplars := dedupExemplars(faces)
	src := source{
		exemplars:        exemplars,
		acceptedVecs:     vectorsOf(faces),
		photoCount:       len(exemplars),
		faceCount:        len(faces),
		withoutEmbedding: countWithoutEmbedding(markedPhotos, faces),
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

// countWithoutEmbedding returns how many marked photos have no embedded face for the
// subject: the set difference between photos carrying a marker and photos carrying
// an embedded face. This is the "faces have no embeddings" count the UI surfaces.
func countWithoutEmbedding(markedPhotos []string, faces []vectors.Face) int {
	withFace := make(map[string]struct{}, len(faces))
	for i := range faces {
		withFace[faces[i].PhotoUID] = struct{}{}
	}
	missing := 0
	for _, photoUID := range markedPhotos {
		if _, ok := withFace[photoUID]; !ok {
			missing++
		}
	}
	return missing
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
