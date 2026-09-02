package bulkapi

import (
	"errors"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/photos"
)

// Coordinate bounds for a set-location operation.
const (
	minLat = -90.0
	maxLat = 90.0
	minLng = -180.0
	maxLng = 180.0
)

// bulkRequest is the JSON body of POST /photos/bulk: the target photos and the
// operation set to apply to each.
type bulkRequest struct {
	PhotoUIDs  []string        `json:"photo_uids"`
	Operations operationsInput `json:"operations"`
}

// locationSummaryRequest is the JSON body of POST /photos/bulk/location-summary:
// the selection to describe. It is the bulk request without its operations —
// the question is about the photos as they stand, not about a change.
type locationSummaryRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// operationsInput is the wire form of the operation set. Set/clear pairs are
// distinct keys (rather than presence/null) so the payload is unambiguous and
// mutually exclusive pairs can be rejected. "caption" maps to the photo title;
// "description" to the photo description.
type operationsInput struct {
	AddToAlbums      []string       `json:"add_to_albums"`
	RemoveFromAlbums []string       `json:"remove_from_albums"`
	AddLabels        []string       `json:"add_labels"`
	RemoveLabels     []string       `json:"remove_labels"`
	SetCaption       *string        `json:"set_caption"`
	ClearCaption     bool           `json:"clear_caption"`
	SetDescription   *string        `json:"set_description"`
	ClearDescription bool           `json:"clear_description"`
	SetTakenAt       *takenAtInput  `json:"set_taken_at"`
	ClearTakenAt     bool           `json:"clear_taken_at"`
	SetLocation      *locationInput `json:"set_location"`
	ClearLocation    bool           `json:"clear_location"`
	Archive          bool           `json:"archive"`
	Unarchive        bool           `json:"unarchive"`
	Hide             bool           `json:"hide"`
	Unhide           bool           `json:"unhide"`
	SetFavorite      *bool          `json:"set_favorite"`
	SetRating        *int           `json:"set_rating"`
	SetFlag          *string        `json:"set_flag"`
}

// The inclusive bounds of a star rating accepted by a set-rating operation,
// mirroring the SQL CHECK on user_ratings.rating.
const (
	minRating = 0
	maxRating = 5
)

// locationInput is the lat/lng pair of a set-location operation, plus what to do
// about the targets that already have a location. only_missing false (the
// default) overwrites every photo in the batch; true fills in the ones with no
// coordinates and leaves the rest untouched, which is how a box of scans with a
// few geotagged strays among them is placed without discarding the strays'
// evidence. The photos it leaves alone come back as skipped.
type locationInput struct {
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	OnlyMissing bool    `json:"only_missing"`
}

// takenAtInput is the wire form of a set-taken-date operation. The value's shape
// follows the precision, so the pair can never disagree about how much of a date
// was actually stated:
//
//	{"precision": "day",    "value": "1974-06-14"}
//	{"precision": "month",  "value": "1974-06"}
//	{"precision": "year",   "value": "1974"}
//	{"precision": "decade", "value": "1970"}
//
// There is no time of day: the operation exists for scans and inherited prints,
// where the hour is never known and a request that could carry one would invite
// stamping a made-up one on fifty photos at once. See resolveTakenAt for what the
// value resolves to.
type takenAtInput struct {
	Precision string `json:"precision"`
	Value     string `json:"value"`
}

// The layouts each precision's value is parsed with. A layout resolves to the
// first instant of the period it names, in UTC — Go's reference date fills the
// missing components with the first day of the month and midnight.
var takenAtLayouts = map[string]string{
	photos.TakenAtPrecisionDay:    "2006-01-02",
	photos.TakenAtPrecisionMonth:  "2006-01",
	photos.TakenAtPrecisionYear:   "2006",
	photos.TakenAtPrecisionDecade: "2006",
}

// yearsInDecade is the span a decade precision rounds its year down to.
const yearsInDecade = 10

// toOperations validates the input and resolves it into a bulk.Operations. It
// rejects mutually exclusive set/clear pairs, conflicting archive/unarchive and
// out-of-range coordinates.
func (in operationsInput) toOperations() (bulk.Operations, error) {
	ops := bulk.Operations{
		AddAlbums:    in.AddToAlbums,
		RemoveAlbums: in.RemoveFromAlbums,
		AddLabels:    in.AddLabels,
		RemoveLabels: in.RemoveLabels,
		Favorite:     in.SetFavorite,
	}
	title, err := resolveText(in.SetCaption, in.ClearCaption, "caption")
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Title = title

	description, err := resolveText(in.SetDescription, in.ClearDescription, "description")
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Description = description

	takenAt, err := in.resolveTakenAt()
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.TakenAt = takenAt
	ops.ClearTakenAt = in.ClearTakenAt

	location, clearLocation, err := in.resolveLocation()
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Location = location
	ops.ClearLocation = clearLocation

	archive, err := resolveToggle(in.Archive, in.Unarchive, "archive", "unarchive")
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Archive = archive

	hide, err := resolveToggle(in.Hide, in.Unhide, "hide", "unhide")
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Hide = hide

	rating, err := resolveRating(in.SetRating)
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Rating = rating

	flag, err := resolveFlag(in.SetFlag)
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Flag = flag
	return ops, nil
}

// resolveTakenAt validates a set-taken-date operation and resolves it to the
// instant that will be stored: the **first instant of the stated period, in
// UTC**. "1974" becomes 1974-01-01T00:00:00Z, "1974-06" the first of June, and a
// decade the 1 January of its first year — the year the value names is rounded
// down to its decade ("1974" and "1970" both mean the seventies), so a caller
// cannot state a decade that starts mid-decade.
//
// UTC is not a detail: the year facets and the period bounds read taken_at in
// UTC, so anchoring there is what makes a photo dated "1974" land in the 1974
// bucket rather than in December 1973 for readers east of Greenwich.
//
// It rejects set_taken_at together with clear_taken_at: one states a date and
// the other states that nobody knows it, so a request carrying both says nothing
// the server could honour and is answered with a 400 rather than with whichever
// clause the SQL happened to apply last.
//
// A nil pointer means no change.
func (in operationsInput) resolveTakenAt() (*bulk.TakenAt, error) {
	if in.SetTakenAt != nil && in.ClearTakenAt {
		return nil, errors.New("set_taken_at and clear_taken_at are mutually exclusive")
	}
	set := in.SetTakenAt
	if set == nil {
		// No date change requested: nil pointer, nil error is the "leave unchanged"
		// signal here.
		return nil, nil //nolint:nilnil
	}
	layout, ok := takenAtLayouts[set.Precision]
	if !ok {
		return nil, fmt.Errorf(
			"set_taken_at.precision %q must be day, month, year or decade", set.Precision)
	}
	at, err := time.ParseInLocation(layout, set.Value, time.UTC)
	if err != nil {
		return nil, fmt.Errorf(
			"set_taken_at.value %q is not a %s (expected %s)", set.Value, set.Precision, layout)
	}
	if set.Precision == photos.TakenAtPrecisionDecade {
		decade := at.Year() - at.Year()%yearsInDecade
		at = time.Date(decade, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return &bulk.TakenAt{At: at, Precision: set.Precision}, nil
}

// resolveRating validates a set-rating operation, rejecting a star value outside
// the 0–5 range. A nil pointer means no change.
func resolveRating(set *int) (*int, error) {
	if set == nil {
		// No rating change requested: nil pointer, nil error is the "leave
		// unchanged" signal here.
		return nil, nil //nolint:nilnil
	}
	if *set < minRating || *set > maxRating {
		return nil, fmt.Errorf("set_rating %d out of range [%d, %d]", *set, minRating, maxRating)
	}
	return set, nil
}

// resolveFlag validates a set-flag operation, rejecting anything other than the
// recognised personal-marking values "none", "pick", "reject" or "eye". A nil
// pointer means no change.
func resolveFlag(set *string) (*string, error) {
	if set == nil {
		// No flag change requested: nil pointer, nil error means "leave unchanged".
		return nil, nil //nolint:nilnil
	}
	switch *set {
	case "none", "pick", "reject", "eye":
		return set, nil
	default:
		return nil, fmt.Errorf("set_flag %q must be none, pick, reject or eye", *set)
	}
}

// resolveText turns a set/clear pair for a text field into a single optional
// value: nil for no change, an empty string to clear, or the set value. It
// rejects supplying both set and clear.
func resolveText(set *string, clearValue bool, name string) (*string, error) {
	if set != nil && clearValue {
		return nil, fmt.Errorf("set_%s and clear_%s are mutually exclusive", name, name)
	}
	if clearValue {
		empty := ""
		return &empty, nil
	}
	return set, nil
}

// resolveLocation resolves the set/clear location pair, validating coordinate
// bounds when setting. It returns the location to set (or nil), whether to clear,
// and any validation error.
func (in operationsInput) resolveLocation() (*bulk.Location, bool, error) {
	if in.SetLocation != nil && in.ClearLocation {
		return nil, false, errors.New("set_location and clear_location are mutually exclusive")
	}
	if in.ClearLocation {
		return nil, true, nil
	}
	if in.SetLocation == nil {
		return nil, false, nil
	}
	if err := validateCoords(in.SetLocation.Lat, in.SetLocation.Lng); err != nil {
		return nil, false, err
	}
	return &bulk.Location{
		Lat:         in.SetLocation.Lat,
		Lng:         in.SetLocation.Lng,
		OnlyMissing: in.SetLocation.OnlyMissing,
	}, false, nil
}

// resolveToggle turns a pair of opposing boolean flags (archive/unarchive,
// hide/unhide) into an optional directive: nil for no change, true for the
// on flag, false for the off one. onName/offName name the two request keys in
// the error returned when both are supplied.
func resolveToggle(on, off bool, onName, offName string) (*bool, error) {
	if on && off {
		return nil, fmt.Errorf("%s and %s are mutually exclusive", onName, offName)
	}
	if on {
		return new(true), nil
	}
	if off {
		return new(false), nil
	}
	// No change requested: a nil pointer with a nil error is the intended "leave
	// unchanged" signal here.
	//nolint:nilnil // optional value: nil means no change, not a missing result.
	return nil, nil
}

// validateCoords returns an error when lat/lng fall outside their valid ranges.
func validateCoords(lat, lng float64) error {
	if lat < minLat || lat > maxLat {
		return fmt.Errorf("latitude %g out of range [%g, %g]", lat, minLat, maxLat)
	}
	if lng < minLng || lng > maxLng {
		return fmt.Errorf("longitude %g out of range [%g, %g]", lng, minLng, maxLng)
	}
	return nil
}
