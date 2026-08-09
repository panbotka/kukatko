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
	"fmt"
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
	// ErrInvalidLifeYears indicates a birth/death year outside the accepted range
	// or a death that precedes the birth.
	ErrInvalidLifeYears = errors.New("people: invalid birth or death year")
)

// MinLifeYear is the earliest birth or death year a subject may carry. It sits
// well below photography itself, so a mistyped year (198, 19) is rejected while
// every person a photo archive can hold still fits. Mirrored by the SQL CHECK
// constraints of migration 0051.
const MinLifeYear = 1800

// validateLifeYears reports whether the optional birth and death years are a
// combination worth storing, given nowYear as "today". A nil year is unknown and
// always allowed — an archive is mostly people whose dates nobody wrote down.
//
// The rules are: both years lie within [MinLifeYear, nowYear] (a year in the
// future is a typo, not a fact), and a death does not precede the birth. It
// returns ErrInvalidLifeYears, wrapped with what was wrong, for anything else.
//
// The upper bound is passed in rather than read from the clock so the rule can
// be tested at a fixed "now"; callers pass time.Now().Year().
func validateLifeYears(birth, death *int, nowYear int) error {
	for _, y := range []struct {
		name  string
		value *int
	}{{name: "birth_year", value: birth}, {name: "death_year", value: death}} {
		if y.value == nil {
			continue
		}
		if *y.value < MinLifeYear || *y.value > nowYear {
			return fmt.Errorf("%w: %s %d is outside %d..%d",
				ErrInvalidLifeYears, y.name, *y.value, MinLifeYear, nowYear)
		}
	}
	if birth != nil && death != nil && *death < *birth {
		return fmt.Errorf("%w: death_year %d precedes birth_year %d",
			ErrInvalidLifeYears, *death, *birth)
	}
	return nil
}

// checkLifeYears validates the optional birth and death years against the
// current calendar year. It is the one entry point the store's write paths call,
// so "not in the future" always means the same thing.
func checkLifeYears(birth, death *int) error {
	return validateLifeYears(birth, death, time.Now().Year())
}

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
	// BirthYear and DeathYear are the person's life span, nil when unknown —
	// which is the normal case. They carry a year rather than a date because a
	// year is what anybody actually knows about the people in a family archive,
	// and an age derived from them is an approximation the UI marks as one.
	//
	// They are meant for SubjectPerson, but nothing rejects them on another type:
	// the type is editable, and refusing a save because a person was
	// reclassified would punish the reclassification, not the data. They are
	// serialised without omitempty so "unknown" arrives at the client as an
	// explicit null rather than as a missing key.
	BirthYear *int      `json:"birth_year"`
	DeathYear *int      `json:"death_year"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SubjectFace is the face automatically picked to illustrate a subject on the
// people index, so a page about people shows people rather than placeholders. It
// names a photo and the normalised box of the subject's face within it; the
// caller crops the photo's cached thumbnail to that box itself, since there is no
// face-thumbnail endpoint. See listSubjectsSQL for which of a subject's faces
// wins and why.
type SubjectFace struct {
	// PhotoUID is the photo the face was found on.
	PhotoUID string `json:"photo_uid"`
	// X, Y, W and H are the marker's normalised box in 0..1 display space.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
	// Width, Height and Orientation are the source photo's stored frame, reported
	// exactly as GET /photos/{uid}/faces reports them: the box is normalised, so a
	// crop that keeps the face's proportions needs to know the frame's.
	Width       int `json:"width"`
	Height      int `json:"height"`
	Orientation int `json:"orientation"`
}

// SubjectCount is a subject paired with how much of the library it appears on,
// as returned by ListSubjects. It carries both counts because they answer
// different questions and the caller knows which one it is asking: the face tools
// work marker by marker, the people index shows photos.
type SubjectCount struct {
	Subject
	// MarkerCount is the number of non-invalid markers assigned to the subject
	// that fall on a visible photo.
	MarkerCount int `json:"marker_count"`
	// PhotoCount is how many visible photos the subject appears on. It is at most
	// MarkerCount and lower whenever one photo carries several of the subject's
	// markers, which is why the two are not interchangeable. This is the figure
	// the subject's gallery pages through.
	PhotoCount int `json:"photo_count"`
	// CoverFace is the subject's automatically picked face, nil when none of its
	// markers is usable. It is a fallback the client uses only when the subject has
	// no CoverPhotoUID: an explicitly chosen cover is a decision, and this is a
	// guess, so the guess never overrides it.
	CoverFace *SubjectFace `json:"cover_face,omitempty"`
}

// SubjectUpdate carries the user-editable fields applied by Store.UpdateSubject.
// Name is re-slugged on change; CoverPhotoUID clears (sets NULL) when nil.
// BirthYear and DeathYear clear (set NULL) when nil, like CoverPhotoUID: the
// update rewrites the whole editable set, so an omitted year means "unknown".
type SubjectUpdate struct {
	Name          string      `json:"name"`
	Type          SubjectType `json:"type"`
	Favorite      bool        `json:"favorite"`
	Private       bool        `json:"private"`
	Notes         string      `json:"notes"`
	CoverPhotoUID *string     `json:"cover_photo_uid"`
	BirthYear     *int        `json:"birth_year"`
	DeathYear     *int        `json:"death_year"`
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
