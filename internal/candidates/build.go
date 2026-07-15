package candidates

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// build hydrates the surviving voted candidates into the response shape: it loads
// their photos, drops faces too small in absolute pixels, drops faces that trip the
// negative-exemplar rule, and classifies each remaining face's action. Only this
// filtered set is ever loaded into memory, keeping the work bounded.
func (s *Service) build(
	ctx context.Context, subjectUID string, survivors []votedCandidate,
	acceptedVecs [][]float32, rejected []feedback.FaceRef,
) ([]Candidate, error) {
	if len(survivors) == 0 {
		return []Candidate{}, nil
	}
	photoByUID, err := s.loadPhotos(ctx, survivors)
	if err != nil {
		return nil, err
	}
	survivors = filterByPixel(survivors, photoByUID, s.minFacePx)
	survivors, err = s.filterNegatives(ctx, survivors, acceptedVecs, rejected)
	if err != nil {
		return nil, err
	}
	return s.assemble(ctx, subjectUID, survivors, photoByUID)
}

// loadPhotos fetches the distinct photos of the surviving candidates in one query
// and stamps their media URLs, returning them keyed by uid for O(1) lookup.
func (s *Service) loadPhotos(ctx context.Context, survivors []votedCandidate) (map[string]photos.Photo, error) {
	list, err := s.photos.ListByUIDs(ctx, distinctPhotoUIDs(survivors))
	if err != nil {
		return nil, fmt.Errorf("loading candidate photos: %w", err)
	}
	s.media.Decorate(list)
	byUID := make(map[string]photos.Photo, len(list))
	for i := range list {
		byUID[list[i].UID] = list[i]
	}
	return byUID, nil
}

// distinctPhotoUIDs returns the unique photo uids among the candidates.
func distinctPhotoUIDs(cands []votedCandidate) []string {
	seen := make(map[string]struct{}, len(cands))
	uids := make([]string, 0, len(cands))
	for i := range cands {
		uid := cands[i].key.PhotoUID
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		uids = append(uids, uid)
	}
	return uids
}

// filterByPixel drops candidates whose face is narrower than minPx display pixels,
// or whose photo could not be loaded (nothing to render). A non-positive minPx
// disables the absolute floor but the missing-photo drop still applies. A photo with
// unknown dimensions skips the pixel check (the relative floor already guarded it)
// rather than being dropped on a computed width of zero.
func filterByPixel(survivors []votedCandidate, photoByUID map[string]photos.Photo, minPx int) []votedCandidate {
	out := survivors[:0]
	for _, candidate := range survivors {
		photo, ok := photoByUID[candidate.key.PhotoUID]
		if !ok {
			continue
		}
		displayWidth, _ := displayDims(photo)
		if minPx > 0 && displayWidth > 0 && float64(displayWidth)*candidate.bbox[2] < float64(minPx) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// filterNegatives drops candidates that sit closer to a face the user rejected for
// this subject than to any accepted face (the negative-exemplar margin rule). It is
// a no-op — and loads no embeddings — when the subject has no rejections, so the
// common path pays nothing. Candidate and rejected embeddings are fetched in one
// batch each.
func (s *Service) filterNegatives(
	ctx context.Context, survivors []votedCandidate, acceptedVecs [][]float32, rejected []feedback.FaceRef,
) ([]votedCandidate, error) {
	if len(rejected) == 0 || len(survivors) == 0 {
		return survivors, nil
	}
	rejectedVecs, err := s.embeddings(ctx, rejectionKeys(rejected))
	if err != nil {
		return nil, err
	}
	if len(rejectedVecs) == 0 {
		return survivors, nil
	}
	candidateVecs, err := s.embeddingMap(ctx, survivorKeys(survivors))
	if err != nil {
		return nil, err
	}
	out := survivors[:0]
	for _, candidate := range survivors {
		vec, ok := candidateVecs[candidate.key]
		if ok && vectors.IsNegativeExemplar(vec, acceptedVecs, rejectedVecs) {
			continue
		}
		out = append(out, candidate)
	}
	return out, nil
}

// embeddings loads the embedding vectors for the given keys, dropping keys with no
// row (a face removed by re-detection).
func (s *Service) embeddings(ctx context.Context, keys []vectors.FaceKey) ([][]float32, error) {
	faces, err := s.faces.FacesByKeys(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("loading face embeddings: %w", err)
	}
	return vectorsOf(faces), nil
}

// embeddingMap loads the embedding vectors for the given keys, indexed by key so a
// candidate can be matched to its vector.
func (s *Service) embeddingMap(ctx context.Context, keys []vectors.FaceKey) (map[vectors.FaceKey][]float32, error) {
	faces, err := s.faces.FacesByKeys(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("loading candidate embeddings: %w", err)
	}
	byKey := make(map[vectors.FaceKey][]float32, len(faces))
	for i := range faces {
		byKey[vectors.FaceKey{PhotoUID: faces[i].PhotoUID, FaceIndex: faces[i].FaceIndex}] = faces[i].Vector
	}
	return byKey, nil
}

// survivorKeys extracts the face keys of the surviving candidates.
func survivorKeys(survivors []votedCandidate) []vectors.FaceKey {
	keys := make([]vectors.FaceKey, len(survivors))
	for i := range survivors {
		keys[i] = survivors[i].key
	}
	return keys
}

// assemble projects the fully-filtered candidates into the API shape, classifying
// each action against the subject. Markers are resolved through a per-call cache so
// several candidates sharing a marker cost one lookup.
func (s *Service) assemble(
	ctx context.Context, subjectUID string, survivors []votedCandidate, photoByUID map[string]photos.Photo,
) ([]Candidate, error) {
	markerSubjects := make(map[string]string)
	out := make([]Candidate, 0, len(survivors))
	for _, candidate := range survivors {
		photo := photoByUID[candidate.key.PhotoUID]
		action, err := s.classify(ctx, subjectUID, candidate.markerUID, markerSubjects)
		if err != nil {
			return nil, err
		}
		out = append(out, Candidate{
			Photo:      photo,
			FaceIndex:  candidate.key.FaceIndex,
			BBox:       faceBox(candidate.bbox, photo),
			Distance:   candidate.distance,
			MatchCount: candidate.matchCount,
			Action:     action,
			MarkerUID:  derefMarker(candidate.markerUID),
		})
	}
	return out, nil
}

// classify decides what confirming a candidate would do. No marker means the face is
// unmarked (create_marker). A marker already pointing at this subject is the rare
// stale-cache case (already_done); any other marker means the person still has to be
// assigned (assign_person).
func (s *Service) classify(
	ctx context.Context, subjectUID string, markerUID *string, cache map[string]string,
) (Action, error) {
	if markerUID == nil || *markerUID == "" {
		return ActionCreateMarker, nil
	}
	markerSubject, err := s.markerSubject(ctx, *markerUID, cache)
	if err != nil {
		return "", err
	}
	if markerSubject == subjectUID {
		return ActionAlreadyDone, nil
	}
	return ActionAssignPerson, nil
}

// markerSubject returns the subject a marker points at (empty when it points at
// none), resolving through cache. A marker that has vanished is treated as pointing
// at no subject rather than failing the whole search.
func (s *Service) markerSubject(ctx context.Context, markerUID string, cache map[string]string) (string, error) {
	if subject, ok := cache[markerUID]; ok {
		return subject, nil
	}
	marker, err := s.people.GetMarkerByUID(ctx, markerUID)
	if err != nil {
		if errors.Is(err, people.ErrMarkerNotFound) {
			cache[markerUID] = ""
			return "", nil
		}
		return "", fmt.Errorf("resolving marker %s: %w", markerUID, err)
	}
	subject := ""
	if marker.SubjectUID != nil {
		subject = *marker.SubjectUID
	}
	cache[markerUID] = subject
	return subject, nil
}

// faceBox projects a normalised bounding box into both the relative and the
// display-pixel spaces the UI draws in.
func faceBox(bbox [4]float64, photo photos.Photo) FaceBox {
	displayWidth, displayHeight := displayDims(photo)
	return FaceBox{
		Relative: bbox,
		Pixel: [4]int{
			int(math.Round(bbox[0] * float64(displayWidth))),
			int(math.Round(bbox[1] * float64(displayHeight))),
			int(math.Round(bbox[2] * float64(displayWidth))),
			int(math.Round(bbox[3] * float64(displayHeight))),
		},
	}
}

// derefMarker returns the marker UID a candidate carries, or the empty string when
// the face has no overlapping marker (the create_marker case).
func derefMarker(markerUID *string) string {
	if markerUID == nil {
		return ""
	}
	return *markerUID
}

// displayDims returns the photo's pixel dimensions in display (EXIF-oriented) space:
// for orientations 5–8 the stored raw width and height are swapped, matching how the
// normalised box was derived at detection time.
func displayDims(photo photos.Photo) (int, int) {
	if photo.FileOrientation >= 5 && photo.FileOrientation <= 8 {
		return photo.FileHeight, photo.FileWidth
	}
	return photo.FileWidth, photo.FileHeight
}
