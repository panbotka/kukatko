package psfeedsimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/vectors"
)

// importFaces pages the faces feed and records each photo's faces together with
// one atomic replace, materialising the marker and subject the feed carries. The
// feed is ordered by face id and a photo's faces are inserted as one batch, so
// they arrive contiguously; the importer groups them and flushes a group when the
// photo_uid changes (and once more at the end). It checkpoints the counts after
// each page and returns only on an infrastructure error; a photo not yet imported
// (Skipped) or a face batch a photo's faces store rejects (Failed) is counted and
// the pass continues.
func (s *Service) importFaces(ctx context.Context, runID int64, st *runState) error {
	fi := &facesImport{svc: s, st: st, subjects: map[string]string{}, seen: map[string]struct{}{}}
	after := int64(0)
	for {
		page, err := s.feeds.ListFaces(ctx, s.pageSize, after)
		if err != nil {
			return fmt.Errorf("listing faces (after %d): %w", after, err)
		}
		for i := range page.Faces {
			if err := fi.process(ctx, page.Faces[i]); err != nil {
				return err
			}
		}
		if err := s.runs.UpdateCounts(ctx, runID, st.counts); err != nil {
			return fmt.Errorf("checkpointing face counts: %w", err)
		}
		if page.NextAfter == nil || *page.NextAfter == after {
			break
		}
		after = *page.NextAfter
	}
	return fi.flush(ctx)
}

// facesImport carries the state of one faces pass: the subject cache (name slug →
// Kukátko subject uid, reused across faces so a person is resolved once), the set
// of photo UIDs already grouped (to flag a non-contiguous feed), and the current
// photo group being accumulated.
type facesImport struct {
	svc      *Service
	st       *runState
	subjects map[string]string
	seen     map[string]struct{}

	psPhotoUID string
	photoUID   string
	havePhoto  bool
	faces      []vectors.Face
	model      string
}

// process routes one feed face into the current group, flushing and starting a
// new group when the photo changes. It returns an error only on an infrastructure
// failure.
func (fi *facesImport) process(ctx context.Context, f psfeeds.Face) error {
	fi.st.trackTime(f.CreatedAt)
	if f.PhotoUID != fi.psPhotoUID {
		if err := fi.flush(ctx); err != nil {
			return err
		}
		if err := fi.startGroup(ctx, f.PhotoUID); err != nil {
			return err
		}
	}
	if !fi.havePhoto {
		return nil
	}
	return fi.addFace(ctx, f)
}

// startGroup resolves the Kukátko photo a group of faces attaches to. A photo not
// yet imported leaves havePhoto false (its faces are skipped and counted once
// here); a real database error is returned.
func (fi *facesImport) startGroup(ctx context.Context, psPhotoUID string) error {
	fi.psPhotoUID = psPhotoUID
	fi.photoUID = ""
	fi.havePhoto = false
	fi.faces = nil
	fi.model = ""
	if _, ok := fi.seen[psPhotoUID]; ok {
		fi.svc.log.Warn("psfeedsimport: photo's faces are not contiguous in the feed; "+
			"the earlier batch was overwritten", "photo", psPhotoUID)
	}
	fi.seen[psPhotoUID] = struct{}{}

	photo, err := fi.svc.photos.GetByPhotoprismUID(ctx, psPhotoUID)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		fi.st.counts.Skipped++
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolving photo %q: %w", psPhotoUID, err)
	}
	fi.havePhoto = true
	fi.photoUID = photo.UID
	return nil
}

// addFace resolves the face's subject and marker and appends the converted face to
// the current group. It returns an error only on an infrastructure failure while
// resolving the subject; marker materialisation is best-effort.
func (fi *facesImport) addFace(ctx context.Context, f psfeeds.Face) error {
	subjectUID, err := fi.resolveSubject(ctx, f.SubjectName)
	if err != nil {
		return err
	}
	bbox := facejob.NormalizeBBox(toBBox(f.BBox), f.PhotoWidth, f.PhotoHeight, f.Orientation)

	var markerUID *string
	if f.MarkerUID != "" {
		uid := f.MarkerUID
		markerUID = &uid
		fi.ensureMarker(ctx, uid, subjectUID, bbox)
	}
	fi.faces = append(fi.faces, vectors.Face{
		PhotoUID:    fi.photoUID,
		FaceIndex:   f.FaceIndex,
		Vector:      f.Vector,
		BBox:        bbox,
		DetScore:    f.DetScore,
		Model:       f.Model,
		MarkerUID:   markerUID,
		SubjectUID:  subjectUID,
		SubjectName: f.SubjectName,
		PhotoWidth:  f.PhotoWidth,
		PhotoHeight: f.PhotoHeight,
		Orientation: f.Orientation,
	})
	if fi.model == "" {
		fi.model = f.Model
	}
	return nil
}

// resolveSubject returns the Kukátko subject uid for a face's subject name,
// matching an existing subject by name slug (reusing one the PhotoPrism import may
// have created) or creating a new one, and caching the result for the run. It
// returns nil (no error) when the face carries no name, and an error only on an
// infrastructure failure. Each call returns a fresh pointer so a face and its
// marker never alias one another's subject uid.
func (fi *facesImport) resolveSubject(ctx context.Context, name string) (*string, error) {
	slug := people.Slugify(name)
	if slug == "" {
		return nil, nil //nolint:nilnil // (nil, nil) means "no subject", not an error.
	}
	uid, ok := fi.subjects[slug]
	if ok {
		return &uid, nil
	}
	subject, err := fi.svc.people.GetSubjectBySlug(ctx, slug)
	switch {
	case err == nil:
		uid = subject.UID
	case errors.Is(err, people.ErrSubjectNotFound):
		created, cerr := fi.svc.people.CreateSubject(ctx, people.Subject{Name: name})
		if cerr != nil {
			return nil, fmt.Errorf("creating subject %q: %w", name, cerr)
		}
		uid = created.UID
	default:
		return nil, fmt.Errorf("resolving subject %q: %w", name, err)
	}
	fi.subjects[slug] = uid
	return &uid, nil
}

// ensureMarker materialises the face's marker under its preserved UID, unless a
// marker with that UID already exists (e.g. from the PhotoPrism import), in which
// case it is reused. Marker work is best-effort: a failure is logged but never
// aborts the run nor drops the face, whose denormalised assignment still carries
// the subject.
func (fi *facesImport) ensureMarker(ctx context.Context, markerUID string, subjectUID *string, bbox [4]float64) {
	_, err := fi.svc.people.GetMarkerByUID(ctx, markerUID)
	if err == nil {
		return
	}
	if !errors.Is(err, people.ErrMarkerNotFound) {
		fi.svc.log.Warn("psfeedsimport: looking up marker", "marker", markerUID, "err", err)
		return
	}
	_, err = fi.svc.people.CreateMarker(ctx, people.Marker{
		UID:        markerUID,
		PhotoUID:   fi.photoUID,
		SubjectUID: subjectUID,
		Type:       people.MarkerFace,
		X:          clamp01(bbox[0]),
		Y:          clamp01(bbox[1]),
		W:          clamp01(bbox[2]),
		H:          clamp01(bbox[3]),
	})
	if err != nil {
		fi.svc.log.Warn("psfeedsimport: creating marker",
			"marker", markerUID, "photo", fi.photoUID, "err", err)
	}
}

// flush records the current photo group's faces with one atomic replace, then
// resets the group. It returns an error only on an infrastructure failure; a face
// batch the store rejects (a bad dimension or a duplicate index) is counted Failed
// and the pass continues.
func (fi *facesImport) flush(ctx context.Context) error {
	if !fi.havePhoto || len(fi.faces) == 0 {
		return nil
	}
	err := fi.svc.vectors.RecordFaceDetection(ctx, fi.photoUID, fi.faces, fi.model)
	switch {
	case err == nil:
		fi.st.counts.Imported++
		return nil
	case errors.Is(err, vectors.ErrDimMismatch), errors.Is(err, vectors.ErrFaceIndexTaken):
		fi.svc.log.Warn("psfeedsimport: recording faces",
			"photo", fi.photoUID, "faces", len(fi.faces), "err", err)
		fi.st.counts.Failed++
		return nil
	default:
		return fmt.Errorf("recording faces for %q: %w", fi.photoUID, err)
	}
}

// toBBox copies a feed bbox slice into a fixed [x1, y1, x2, y2] array, tolerating
// a short or absent slice (missing coordinates stay zero).
func toBBox(b []float64) [4]float64 {
	var box [4]float64
	copy(box[:], b)
	return box
}

// clamp01 clamps a normalised coordinate into [0, 1], since a face box may reach
// slightly past the frame edge and markers require every coordinate in range.
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
