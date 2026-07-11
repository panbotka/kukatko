package photoapi

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// maxListLimit caps the number of photos a single page may request, bounding the
// work a hostile or careless caller can demand. Larger values are clamped.
const maxListLimit = 500

// minYear and maxYear bound the accepted year filter to a four-digit calendar
// year. Photography predates neither, and no EXIF capture time the importers
// accept falls outside it, so anything else is a malformed request.
const (
	minYear = 1000
	maxYear = 9999
)

// sortSpec pairs a list sort column with its natural default direction so a
// caller can pick a sort key by intent ("newest", "title") and still override
// the direction with an explicit order parameter.
type sortSpec struct {
	field photos.SortField
	order photos.SortOrder
}

// sortAliases maps the public sort query values to their column and default
// direction. The keys are the only accepted values; anything else is a 400.
var sortAliases = map[string]sortSpec{
	"newest":   {photos.SortByTakenAt, photos.OrderDesc},
	"oldest":   {photos.SortByTakenAt, photos.OrderAsc},
	"taken_at": {photos.SortByTakenAt, photos.OrderDesc},
	"added":    {photos.SortByCreatedAt, photos.OrderDesc},
	"title":    {photos.SortByTitle, photos.OrderAsc},
	"size":     {photos.SortBySize, photos.OrderDesc},
	"rating":   {photos.SortByRating, photos.OrderDesc},
}

// parseListParams turns the request's query string into validated photos.List
// parameters. Every recognised filter, sort and pagination value is range- and
// type-checked; the first invalid value yields a descriptive error so the caller
// can answer 400. Unknown query keys are ignored.
func parseListParams(q url.Values) (photos.ListParams, error) {
	var params photos.ListParams
	if err := applyPagination(q, &params); err != nil {
		return photos.ListParams{}, err
	}
	if err := applySort(q, &params); err != nil {
		return photos.ListParams{}, err
	}
	if err := applyArchived(q, &params); err != nil {
		return photos.ListParams{}, err
	}
	if err := applyFilters(q, &params); err != nil {
		return photos.ListParams{}, err
	}
	if err := applyRatingFilters(q, &params); err != nil {
		return photos.ListParams{}, err
	}
	// An album is always presented chronologically, oldest first, whatever sort
	// or order the query carries. The override lives here — where the album
	// scope enters the shared list path — so the endpoint's defaults stay
	// untouched for every other view. It applies as soon as at least one album is
	// selected (multiple albums are still an album view). Photos with no capture
	// time fall back to their upload time inside SortByChronology, keeping the
	// order total.
	if len(params.AlbumUIDs) > 0 {
		params.Sort = photos.SortByChronology
		params.Order = photos.OrderAsc
	}
	return params, nil
}

// applyRatingFilters validates and applies the per-user rating filters: min_rating
// keeps photos the caller has rated at or above the given star value, and flag
// keeps photos the caller has marked pick or reject. Both take effect only when
// the handler binds RatedBy to the caller; a photo without a rating row counts as
// rating 0 / flag "none".
func applyRatingFilters(q url.Values, params *photos.ListParams) error {
	if raw := q.Get("min_rating"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return errors.New("min_rating must be an integer")
		}
		params.MinRating = &n
	}
	if raw := q.Get("flag"); raw != "" {
		switch raw {
		case "pick", "reject":
			flag := raw
			params.Flag = &flag
		default:
			return fmt.Errorf("unknown flag %q (want pick or reject)", raw)
		}
	}
	return nil
}

// applyPagination validates and applies the limit and offset query parameters.
// limit is clamped to maxListLimit; both must be non-negative integers.
func applyPagination(q url.Values, params *photos.ListParams) error {
	limit, err := intParam(q, "limit", 0)
	if err != nil {
		return err
	}
	if limit < 0 {
		return errors.New("limit must not be negative")
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	params.Limit = limit

	offset, err := intParam(q, "offset", 0)
	if err != nil {
		return err
	}
	if offset < 0 {
		return errors.New("offset must not be negative")
	}
	params.Offset = offset
	return nil
}

// applySort validates and applies the sort and order query parameters, mapping
// the public sort alias to a column and direction and letting order override the
// alias's default direction.
func applySort(q url.Values, params *photos.ListParams) error {
	if raw := q.Get("sort"); raw != "" {
		spec, ok := sortAliases[raw]
		if !ok {
			return fmt.Errorf("unknown sort %q", raw)
		}
		params.Sort = spec.field
		params.Order = spec.order
	}
	if raw := q.Get("order"); raw != "" {
		switch photos.SortOrder(raw) {
		case photos.OrderAsc, photos.OrderDesc:
			params.Order = photos.SortOrder(raw)
		default:
			return fmt.Errorf("unknown order %q (want asc or desc)", raw)
		}
	}
	return nil
}

// applyArchived validates and applies the archived query parameter, which
// selects whether archived photos are excluded (the default), included, or shown
// exclusively.
func applyArchived(q url.Values, params *photos.ListParams) error {
	switch raw := q.Get("archived"); raw {
	case "", "false":
		// Default: live photos only.
	case "true":
		params.IncludeArchived = true
	case "only":
		params.OnlyArchived = true
	default:
		return fmt.Errorf("unknown archived %q (want true, false or only)", raw)
	}
	return nil
}

// applyFilters validates and applies the metadata filters: private, has-GPS,
// capture year, date range, camera, lens, uploader and free-text search, plus the
// album and label scope filters and the country/city place scope filters that
// restrict the list to one album's, one label's or one place's photos so the same
// endpoint serves a scoped grid.
func applyFilters(q url.Values, params *photos.ListParams) error {
	private, err := boolParam(q, "private")
	if err != nil {
		return err
	}
	params.Private = private

	hasGPS, err := boolParam(q, "has_gps")
	if err != nil {
		return err
	}
	params.HasGPS = hasGPS

	year, err := yearParam(q, "year")
	if err != nil {
		return err
	}
	params.Year = year

	after, err := timeParam(q, "taken_after")
	if err != nil {
		return err
	}
	params.TakenAfter = after

	before, err := timeParam(q, "taken_before")
	if err != nil {
		return err
	}
	params.TakenBefore = before

	params.Camera = q.Get("camera")
	params.Lens = q.Get("lens")
	params.UploadedBy = q.Get("uploader")
	params.Search = q.Get("q")
	params.AlbumUIDs = multiValueParam(q, "album")
	params.LabelUIDs = multiValueParam(q, "label")
	params.Country = q.Get("country")
	params.City = q.Get("city")
	return nil
}

// multiValueParam collects every value of the named repeated query parameter
// (e.g. ?album=a&album=b) into a slice, additionally splitting each value on
// commas so a single comma-joined value (?album=a,b) is accepted too — the form
// the SPA keeps in its own URL. Whitespace is trimmed and empty entries are
// dropped, so a single value still yields a one-element slice (backward compatible
// with ?album=a) and an absent parameter yields nil (no filter).
func multiValueParam(q url.Values, name string) []string {
	raw := q[name]
	if len(raw) == 0 {
		return nil
	}
	var values []string
	for _, value := range raw {
		for part := range strings.SplitSeq(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	}
	return values
}

// favoriteRequested reports whether the favorite=true filter is set on the query.
// It returns a descriptive error for a non-boolean value so the caller can answer
// 400, mirroring the other boolean filters. An absent or false value yields false.
func favoriteRequested(q url.Values) (bool, error) {
	b, err := boolParam(q, "favorite")
	if err != nil {
		return false, err
	}
	return b != nil && *b, nil
}

// intParam parses the named integer query parameter, returning fallback when it
// is absent and a descriptive error when present but not a valid integer.
func intParam(q url.Values, name string, fallback int) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return n, nil
}

// yearParam parses the named calendar-year query parameter, returning nil when it
// is absent (no filter). It accepts only a four-digit year in minYear..maxYear —
// the range GET /photos/years can ever offer — so a tampered value cannot reach
// the timestamp arithmetic the year filter builds from it.
func yearParam(q url.Values, name string) (*int, error) {
	raw := q.Get(name)
	if raw == "" {
		// An absent optional filter legitimately yields no value and no error.
		return nil, nil //nolint:nilnil
	}
	year, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", name)
	}
	if year < minYear || year > maxYear {
		return nil, fmt.Errorf("%s must be between %d and %d", name, minYear, maxYear)
	}
	return &year, nil
}

// boolParam parses the named boolean query parameter, returning nil when it is
// absent (no filter) and a descriptive error when present but not parseable.
func boolParam(q url.Values, name string) (*bool, error) {
	raw := q.Get(name)
	if raw == "" {
		// An absent optional filter legitimately yields no value and no error.
		return nil, nil //nolint:nilnil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false", name)
	}
	return &b, nil
}

// timeParam parses the named timestamp query parameter, accepting either a full
// RFC 3339 timestamp or a bare YYYY-MM-DD date (interpreted as UTC midnight). It
// returns nil when the parameter is absent and a descriptive error when present
// but unparseable.
func timeParam(q url.Values, name string) (*time.Time, error) {
	raw := q.Get(name)
	if raw == "" {
		// An absent optional filter legitimately yields no value and no error.
		return nil, nil //nolint:nilnil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return &t, nil
	}
	return nil, fmt.Errorf("%s must be an RFC3339 timestamp or YYYY-MM-DD date", name)
}
