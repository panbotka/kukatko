package photoapi

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// maxListLimit caps the number of photos a single page may request, bounding the
// work a hostile or careless caller can demand. Larger values are clamped.
const maxListLimit = 500

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
	return params, nil
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
// date range, camera, lens, uploader and free-text search, plus the album and
// label scope filters that restrict the list to one album's or one label's
// photos so the same endpoint serves a scoped grid.
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
	params.AlbumUID = q.Get("album")
	params.LabelUID = q.Get("label")
	return nil
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
