package ppimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
)

// importMarkers seeds people from a photo's PhotoPrism face markers: every named,
// valid face region finds-or-creates its subject and a Kukátko marker assigned to
// it. Per-marker failures are logged and skipped; face detection re-runs later via
// the face_detect job, and matches the markers by IoU.
//
// The photo must come from the DETAIL endpoint. PhotoPrism's photo LISTING carries
// its files with an always-empty `Markers` array — feed it a listing photo and this
// silently imports nobody, which is exactly the bug this comment exists to prevent.
func (s *Service) importMarkers(ctx context.Context, photoUID string, pp photoprism.Photo) {
	primary, ok := pp.PrimaryFile()
	if !ok {
		return
	}
	for i := range primary.Markers {
		marker := primary.Markers[i]
		if !isNamedFaceMarker(marker) {
			continue
		}
		if err := s.importOneMarker(ctx, photoUID, marker); err != nil {
			s.log.Warn("ppimport: importing marker", "photo", photoUID, "name", marker.Name, "err", err)
		}
	}
}

// importOneMarker finds-or-creates the subject named by a marker and creates a face
// marker on the Kukátko photo assigned to that subject.
//
// The marker keeps its PhotoPrism UID, which makes the import idempotent (a marker
// already brought over is skipped, so a re-run neither duplicates people nor moves
// them) and keeps marker identity the same across both importers — photo-sorter's
// face rows reference these very UIDs, because its markers ARE PhotoPrism's.
func (s *Service) importOneMarker(ctx context.Context, photoUID string, m photoprism.Marker) error {
	if m.UID != "" {
		_, err := s.people.GetMarkerByUID(ctx, m.UID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, people.ErrMarkerNotFound) {
			return fmt.Errorf("ppimport: looking up marker %s: %w", m.UID, err)
		}
	}
	subject, err := s.findOrCreateSubject(ctx, m.Name)
	if err != nil {
		return err
	}
	subjectUID := subject.UID
	if _, err := s.people.CreateMarker(ctx, people.Marker{
		UID:        m.UID,
		PhotoUID:   photoUID,
		SubjectUID: &subjectUID,
		Type:       people.MarkerFace,
		X:          m.X,
		Y:          m.Y,
		W:          m.W,
		H:          m.H,
		Score:      m.Score,
		Reviewed:   !m.Review,
	}); err != nil {
		return fmt.Errorf("ppimport: creating marker for %q: %w", m.Name, err)
	}
	return nil
}

// findOrCreateSubject returns the existing subject whose slug matches the name, or
// creates a new person subject. It mirrors the find-or-create-by-name behaviour
// used elsewhere for assigning faces.
func (s *Service) findOrCreateSubject(ctx context.Context, name string) (people.Subject, error) {
	slug := people.Slugify(name)
	subject, err := s.people.GetSubjectBySlug(ctx, slug)
	if err == nil {
		return subject, nil
	}
	if !errors.Is(err, people.ErrSubjectNotFound) {
		return people.Subject{}, fmt.Errorf("ppimport: looking up subject %q: %w", name, err)
	}
	created, err := s.people.CreateSubject(ctx, people.Subject{Name: name, Type: people.SubjectPerson})
	if err != nil {
		return people.Subject{}, fmt.Errorf("ppimport: creating subject %q: %w", name, err)
	}
	return created, nil
}

// isNamedFaceMarker reports whether a PhotoPrism marker is a valid, named face
// region — the only kind the importer seeds as a person. Unnamed or invalid
// regions are left for Kukátko's own face detection to (re)discover.
func isNamedFaceMarker(m photoprism.Marker) bool {
	return strings.EqualFold(m.Type, "face") && !m.Invalid && strings.TrimSpace(m.Name) != ""
}
