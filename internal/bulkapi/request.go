package bulkapi

import (
	"errors"
	"fmt"

	"github.com/panbotka/kukatko/internal/bulk"
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
	SetLocation      *locationInput `json:"set_location"`
	ClearLocation    bool           `json:"clear_location"`
	SetPrivate       *bool          `json:"set_private"`
	Archive          bool           `json:"archive"`
	Unarchive        bool           `json:"unarchive"`
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

// locationInput is the lat/lng pair of a set-location operation.
type locationInput struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// toOperations validates the input and resolves it into a bulk.Operations. It
// rejects mutually exclusive set/clear pairs, conflicting archive/unarchive and
// out-of-range coordinates.
func (in operationsInput) toOperations() (bulk.Operations, error) {
	ops := bulk.Operations{
		AddAlbums:    in.AddToAlbums,
		RemoveAlbums: in.RemoveFromAlbums,
		AddLabels:    in.AddLabels,
		RemoveLabels: in.RemoveLabels,
		Private:      in.SetPrivate,
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

	location, clearLocation, err := in.resolveLocation()
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Location = location
	ops.ClearLocation = clearLocation

	archive, err := resolveArchive(in.Archive, in.Unarchive)
	if err != nil {
		return bulk.Operations{}, err
	}
	ops.Archive = archive

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
// recognised personal marks "none", "pick", "reject" or "eye". A nil pointer means
// no change.
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
	return &bulk.Location{Lat: in.SetLocation.Lat, Lng: in.SetLocation.Lng}, false, nil
}

// resolveArchive turns the archive/unarchive flags into an optional archive
// directive: nil for no change, true to archive, false to unarchive. It rejects
// supplying both.
func resolveArchive(archive, unarchive bool) (*bool, error) {
	if archive && unarchive {
		return nil, errors.New("archive and unarchive are mutually exclusive")
	}
	if archive {
		value := true
		return &value, nil
	}
	if unarchive {
		value := false
		return &value, nil
	}
	// No archive change requested: a nil pointer with a nil error is the intended
	// "leave unchanged" signal here.
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
