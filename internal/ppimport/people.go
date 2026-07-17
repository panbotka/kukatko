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
// idx is the run's subject index: a newly seeded subject is enriched from the
// source subject it resolves (type, favorite, private). It may be nil/empty, in
// which case the subject falls back to a plain, public person.
//
// The photo must come from the DETAIL endpoint. PhotoPrism's photo LISTING carries
// its files with an always-empty `Markers` array — feed it a listing photo and this
// silently imports nobody, which is exactly the bug this comment exists to prevent.
func (s *Service) importMarkers(ctx context.Context, photoUID string, pp photoprism.Photo, idx *subjectIndex) {
	primary, ok := pp.PrimaryFile()
	if !ok {
		return
	}
	for i := range primary.Markers {
		marker := primary.Markers[i]
		if !isNamedFaceMarker(marker) {
			continue
		}
		if err := s.importOneMarker(ctx, photoUID, marker, idx); err != nil {
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
func (s *Service) importOneMarker(ctx context.Context, photoUID string, m photoprism.Marker, idx *subjectIndex) error {
	if m.UID != "" {
		_, err := s.people.GetMarkerByUID(ctx, m.UID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, people.ErrMarkerNotFound) {
			return fmt.Errorf("ppimport: looking up marker %s: %w", m.UID, err)
		}
	}
	subject, err := s.findOrCreateSubject(ctx, m, idx)
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

// findOrCreateSubject returns the existing subject whose slug matches the marker's
// name, or creates a new one — carrying the source subject's type and its
// favorite/private flags when the run's index resolves the marker, so a pet stays
// a pet and a private person stays private instead of collapsing to a plain,
// public person (the PhotoPrism import's historical loss).
//
// Enrichment happens on CREATE only: an existing subject — already seeded, perhaps
// since edited in Kukátko — is returned untouched. That keeps the import
// idempotent (a re-run neither re-flags nor re-types a subject) and never clobbers
// a local edit, matching photo-sorter's importer.
func (s *Service) findOrCreateSubject(
	ctx context.Context, m photoprism.Marker, idx *subjectIndex,
) (people.Subject, error) {
	name := m.Name
	subject, err := s.people.GetSubjectBySlug(ctx, people.Slugify(name))
	if err == nil {
		return subject, nil
	}
	if !errors.Is(err, people.ErrSubjectNotFound) {
		return people.Subject{}, fmt.Errorf("ppimport: looking up subject %q: %w", name, err)
	}
	created, err := s.people.CreateSubject(ctx, newSubject(name, m, idx))
	if err != nil {
		return people.Subject{}, fmt.Errorf("ppimport: creating subject %q: %w", name, err)
	}
	return created, nil
}

// newSubject builds the subject to create for a named marker. A subject the run's
// index resolves contributes its type and its favorite/private flags; an
// unresolved marker (or a nil/empty index) yields a plain public person — the
// pre-enrichment default, so nothing regresses when the source subject cannot be
// found.
func newSubject(name string, m photoprism.Marker, idx *subjectIndex) people.Subject {
	subj := people.Subject{Name: name, Type: people.SubjectPerson}
	src, ok := idx.lookup(m)
	if !ok {
		return subj
	}
	subj.Type = mapSubjectType(src.Type)
	subj.Favorite = src.Favorite
	subj.Private = src.Private
	return subj
}

// mapSubjectType maps a PhotoPrism subject type onto Kukátko's, defaulting an
// unknown or empty value to a person. It mirrors psimport.mapSubjectType so both
// importers classify a subject the same way.
func mapSubjectType(ppType string) people.SubjectType {
	switch strings.ToLower(strings.TrimSpace(ppType)) {
	case string(people.SubjectPet):
		return people.SubjectPet
	case string(people.SubjectOther):
		return people.SubjectOther
	default:
		return people.SubjectPerson
	}
}

// subjectIndex resolves a face marker to the PhotoPrism subject it names, so a
// subject seeded by the import carries the source's type and flags. It is read
// once per run (loadSubjectIndex) because the markers, which carry only a name and
// a subject UID, do not have that data.
type subjectIndex struct {
	// byUID maps a source subject UID to its subject — the exact link a marker
	// carries in SubjUID.
	byUID map[string]photoprism.Subject
	// bySlug maps the slug of a subject's name to its subject — the key Kukátko
	// itself pairs subjects on, used when a marker has no usable SubjUID.
	bySlug map[string]photoprism.Subject
}

// lookup returns the source subject a marker names, matching first by the marker's
// subject UID (the exact link) and then by the slug of its name (the key Kukátko
// pairs subjects on). It reports false — a plain public person default — when the
// index is nil or neither key resolves.
func (idx *subjectIndex) lookup(m photoprism.Marker) (photoprism.Subject, bool) {
	if idx == nil {
		return photoprism.Subject{}, false
	}
	if m.SubjUID != "" {
		if subj, ok := idx.byUID[m.SubjUID]; ok {
			return subj, true
		}
	}
	if slug := people.Slugify(m.Name); slug != "" {
		if subj, ok := idx.bySlug[slug]; ok {
			return subj, true
		}
	}
	return photoprism.Subject{}, false
}

// loadSubjectIndex reads the source's subjects once so the markers imported after
// it can carry each person's type and favorite/private flags. It is BEST EFFORT:
// the subjects endpoint failing must never fail an import that can still bring the
// photos and their people across — it just leaves new subjects at Kukátko's plain,
// public person default, exactly as before this enrichment existed. Callers always
// get a non-nil (possibly empty) index.
func (s *Service) loadSubjectIndex(ctx context.Context) *subjectIndex {
	idx := &subjectIndex{
		byUID:  map[string]photoprism.Subject{},
		bySlug: map[string]photoprism.Subject{},
	}
	for offset := 0; ; {
		page, err := s.client.ListSubjects(ctx, photoprism.ListParams{Count: s.pageSize, Offset: offset})
		if err != nil {
			s.log.Warn("ppimport: listing photoprism subjects", "offset", offset, "err", err)
			return idx
		}
		for i := range page {
			subj := page[i]
			if subj.UID != "" {
				idx.byUID[subj.UID] = subj
			}
			if slug := people.Slugify(subj.Name); slug != "" {
				idx.bySlug[slug] = subj
			}
		}
		if len(page) < s.pageSize {
			return idx
		}
		offset += len(page)
	}
}

// isNamedFaceMarker reports whether a PhotoPrism marker is a valid, named face
// region — the only kind the importer seeds as a person. Unnamed or invalid
// regions are left for Kukátko's own face detection to (re)discover.
func isNamedFaceMarker(m photoprism.Marker) bool {
	return strings.EqualFold(m.Type, "face") && !m.Invalid && strings.TrimSpace(m.Name) != ""
}
