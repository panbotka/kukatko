package psimport

import (
	"strings"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
)

// remapSubject translates a photo-sorter subject UID pointer onto its Kukátko
// counterpart via the subject map, returning nil when the input is nil or the
// subject was not mapped (so an unknown subject simply leaves the row unassigned).
func remapSubject(psUID *string, subjects map[string]string) *string {
	if psUID == nil {
		return nil
	}
	kkUID, ok := subjects[*psUID]
	if !ok || kkUID == "" {
		return nil
	}
	return &kkUID
}

// mapSubjectType maps a photo-sorter subject type onto Kukátko's, defaulting an
// unknown or empty value to a person.
func mapSubjectType(psType string) people.SubjectType {
	switch strings.ToLower(strings.TrimSpace(psType)) {
	case string(people.SubjectPet):
		return people.SubjectPet
	case string(people.SubjectOther):
		return people.SubjectOther
	default:
		return people.SubjectPerson
	}
}

// mapMarkerType maps a photo-sorter marker type onto Kukátko's, defaulting an
// unknown or empty value to a face.
func mapMarkerType(psType string) people.MarkerType {
	if strings.EqualFold(strings.TrimSpace(psType), string(people.MarkerLabel)) {
		return people.MarkerLabel
	}
	return people.MarkerFace
}

// mapAlbumType maps a photo-sorter album type onto Kukátko's, defaulting an
// unknown or empty value to a manual album.
func mapAlbumType(psType string) organize.AlbumType {
	switch strings.ToLower(strings.TrimSpace(psType)) {
	case string(organize.AlbumFolder):
		return organize.AlbumFolder
	case string(organize.AlbumMoment):
		return organize.AlbumMoment
	case string(organize.AlbumState):
		return organize.AlbumState
	case string(organize.AlbumMonth):
		return organize.AlbumMonth
	default:
		return organize.AlbumManual
	}
}

// mapLabelSource maps a photo-sorter photo-label source onto Kukátko's,
// defaulting an unknown or empty value to import (the provenance of a migrated
// attachment).
func mapLabelSource(psSource string) organize.LabelSource {
	switch strings.ToLower(strings.TrimSpace(psSource)) {
	case string(organize.SourceManual):
		return organize.SourceManual
	case string(organize.SourceAI):
		return organize.SourceAI
	default:
		return organize.SourceImport
	}
}
