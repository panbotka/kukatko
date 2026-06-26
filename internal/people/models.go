// Package people is Kukátko's database access layer for named subjects (people,
// pets, other) and the markers that tie photo regions to them. A subject groups
// photos by a named entity; a marker is a normalised [x, y, w, h] region on a
// single photo — a detected face or a manually drawn label box — that may be
// assigned to a subject.
//
// The faces table (migration 0006) caches marker_uid/subject_uid/subject_name for
// fast rendering. This package keeps those denormalised columns consistent: when a
// marker's subject changes, or a subject is renamed, the matching faces rows are
// updated in the same transaction.
package people

import (
	"errors"
	"time"
)

// Sentinel errors returned by the store so callers (handlers, importers, tests)
// can branch with errors.Is.
var (
	// ErrSubjectNotFound indicates no subject matched the given key.
	ErrSubjectNotFound = errors.New("people: subject not found")
	// ErrMarkerNotFound indicates no marker matched the given key.
	ErrMarkerNotFound = errors.New("people: marker not found")
	// ErrSlugExhausted indicates a unique slug could not be generated for a name
	// after exhausting the numeric-suffix attempts (effectively never in practice).
	ErrSlugExhausted = errors.New("people: could not generate a unique slug")
	// ErrInvalidType indicates a subject or marker type outside the allowed set.
	ErrInvalidType = errors.New("people: invalid type")
	// ErrInvalidBounds indicates a marker bounding box with a coordinate outside
	// the normalised 0..1 range.
	ErrInvalidBounds = errors.New("people: marker bounds out of range")
)

// SubjectType classifies a subject, mirrored by the SQL CHECK constraint on
// subjects.type.
type SubjectType string

// The recognised subject types.
const (
	// SubjectPerson is a human subject (the default).
	SubjectPerson SubjectType = "person"
	// SubjectPet is an animal subject.
	SubjectPet SubjectType = "pet"
	// SubjectOther is any other named subject.
	SubjectOther SubjectType = "other"
)

// valid reports whether t is one of the recognised subject types.
func (t SubjectType) valid() bool {
	switch t {
	case SubjectPerson, SubjectPet, SubjectOther:
		return true
	default:
		return false
	}
}

// MarkerType classifies a marker, mirrored by the SQL CHECK constraint on
// markers.type.
type MarkerType string

// The recognised marker types.
const (
	// MarkerFace is a detected (or hand-drawn) face region.
	MarkerFace MarkerType = "face"
	// MarkerLabel is a manually drawn label region.
	MarkerLabel MarkerType = "label"
)

// valid reports whether t is one of the recognised marker types.
func (t MarkerType) valid() bool {
	switch t {
	case MarkerFace, MarkerLabel:
		return true
	default:
		return false
	}
}

// Subject is one named entity photos can be grouped by. Slug is generated from
// Name and made unique by the store. CoverPhotoUID is nil until a cover photo is
// chosen and is cleared if that photo is deleted.
type Subject struct {
	UID           string      `json:"uid"`
	Slug          string      `json:"slug"`
	Name          string      `json:"name"`
	Type          SubjectType `json:"type"`
	Favorite      bool        `json:"favorite"`
	Private       bool        `json:"private"`
	Notes         string      `json:"notes"`
	CoverPhotoUID *string     `json:"cover_photo_uid,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// SubjectCount is a subject paired with how many valid (non-invalid) markers
// reference it, as returned by ListSubjects.
type SubjectCount struct {
	Subject
	// MarkerCount is the number of non-invalid markers assigned to the subject.
	MarkerCount int `json:"marker_count"`
}

// SubjectUpdate carries the user-editable fields applied by Store.UpdateSubject.
// Name is re-slugged on change; CoverPhotoUID clears (sets NULL) when nil.
type SubjectUpdate struct {
	Name          string      `json:"name"`
	Type          SubjectType `json:"type"`
	Favorite      bool        `json:"favorite"`
	Private       bool        `json:"private"`
	Notes         string      `json:"notes"`
	CoverPhotoUID *string     `json:"cover_photo_uid"`
}

// Marker is a normalised region on one photo, optionally assigned to a subject.
// X, Y, W and H are in 0..1 display space (EXIF-aware), matching faces.bbox.
type Marker struct {
	UID        string     `json:"uid"`
	PhotoUID   string     `json:"photo_uid"`
	SubjectUID *string    `json:"subject_uid,omitempty"`
	Type       MarkerType `json:"type"`
	X          float64    `json:"x"`
	Y          float64    `json:"y"`
	W          float64    `json:"w"`
	H          float64    `json:"h"`
	Score      int        `json:"score"`
	Invalid    bool       `json:"invalid"`
	Reviewed   bool       `json:"reviewed"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// validBounds reports whether the marker's normalised box lies within 0..1 on
// every coordinate, the invariant enforced before a marker is written.
func (m Marker) validBounds() bool {
	return inUnit(m.X) && inUnit(m.Y) && inUnit(m.W) && inUnit(m.H)
}

// inUnit reports whether v lies within the closed unit interval [0, 1].
func inUnit(v float64) bool {
	return v >= 0 && v <= 1
}
