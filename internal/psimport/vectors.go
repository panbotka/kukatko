package psimport

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/vectors"
)

// transferSatellites transfers every per-photo satellite from photo-sorter onto
// the resolved Kukátko photo. The embedding and faces are the core 1:1 transfer
// and a failure there fails the photo (so it is retried on the next run); the
// perceptual hashes, edits, markers and membership are best-effort (logged and
// recorded on state as per-satellite failures) so a secondary glitch never loses
// the migrated vectors yet still surfaces in the run's failure trail.
func (s *Service) transferSatellites(
	ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings, state *runState,
) error {
	if err := s.transferEmbedding(ctx, kkUID, ps); err != nil {
		return fmt.Errorf("psimport: transferring embedding for %s: %w", ps.UID, err)
	}
	if err := s.transferFaces(ctx, kkUID, ps, maps); err != nil {
		return fmt.Errorf("psimport: transferring faces for %s: %w", ps.UID, err)
	}
	s.transferPhash(ctx, kkUID, ps, state)
	s.transferEdit(ctx, kkUID, ps, state)
	s.transferMarkers(ctx, kkUID, ps, maps, state)
	s.transferMemberships(ctx, kkUID, ps, maps, state)
	return nil
}

// transferEmbedding copies the photo-sorter CLIP embedding onto the Kukátko photo
// as-is (1:1), preserving model and pretrained. When photo-sorter never embedded
// the photo, Kukátko's own embedding job is enqueued to fill the gap instead.
func (s *Service) transferEmbedding(ctx context.Context, kkUID string, ps photosorter.Photo) error {
	emb, ok, err := s.src.Embedding(ctx, ps.UID)
	if err != nil {
		return fmt.Errorf("reading photo-sorter embedding: %w", err)
	}
	if !ok {
		if err := s.enqueuer.EnqueueImageEmbed(ctx, kkUID); err != nil {
			return fmt.Errorf("enqueuing image embed: %w", err)
		}
		return nil
	}
	if _, err := s.vectors.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID:   kkUID,
		Vector:     emb.Vector,
		Model:      emb.Model,
		Pretrained: emb.Pretrained,
	}); err != nil {
		return fmt.Errorf("saving embedding: %w", err)
	}
	return nil
}

// transferFaces copies the photo-sorter faces onto the Kukátko photo as-is (1:1),
// remapping each face's cached subject UID onto Kukátko's subject and preserving
// the marker UID (markers migrate with the same UID), bounding box, detection
// score and render hints. The detection event is always recorded — even for zero
// faces — so the photo is not re-detected. A photo photo-sorter never processed is
// handed to Kukátko's own face-detection job instead.
func (s *Service) transferFaces(
	ctx context.Context, kkUID string, ps photosorter.Photo, maps mappings,
) error {
	faces, err := s.src.Faces(ctx, ps.UID)
	if err != nil {
		return fmt.Errorf("reading photo-sorter faces: %w", err)
	}
	_, processed, err := s.src.FacesProcessed(ctx, ps.UID)
	if err != nil {
		return fmt.Errorf("reading photo-sorter face detection: %w", err)
	}
	if !processed && len(faces) == 0 {
		if err := s.enqueuer.EnqueueFaceDetect(ctx, kkUID); err != nil {
			return fmt.Errorf("enqueuing face detect: %w", err)
		}
		return nil
	}
	converted := make([]vectors.Face, 0, len(faces))
	for i := range faces {
		converted = append(converted, convertFace(kkUID, faces[i], maps))
	}
	if err := s.vectors.RecordFaceDetection(ctx, kkUID, converted, faceModel(faces)); err != nil {
		return fmt.Errorf("recording face detection: %w", err)
	}
	return nil
}

// convertFace maps a photo-sorter face onto a Kukátko face row, remapping the
// cached subject UID and keeping the marker UID, bounding box and render hints.
func convertFace(kkUID string, f photosorter.Face, maps mappings) vectors.Face {
	return vectors.Face{
		PhotoUID:    kkUID,
		FaceIndex:   f.FaceIndex,
		Vector:      f.Vector,
		BBox:        f.BBox,
		DetScore:    f.DetScore,
		Model:       f.Model,
		MarkerUID:   f.MarkerUID,
		SubjectUID:  remapSubject(f.SubjectUID, maps.subjects),
		SubjectName: f.SubjectName,
		PhotoWidth:  f.PhotoWidth,
		PhotoHeight: f.PhotoHeight,
		Orientation: f.Orientation,
	}
}

// faceModel returns the model identifier to record for a photo's faces: the first
// non-empty model among them, or the empty string when none is set.
func faceModel(faces []photosorter.Face) string {
	for i := range faces {
		if faces[i].Model != "" {
			return faces[i].Model
		}
	}
	return ""
}
