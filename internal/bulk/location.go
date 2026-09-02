package bulk

import (
	"context"
	"fmt"
)

// LocationSummary counts, over one selection of photos, how many of them already
// have coordinates. It is what a "set the location of these 60 scans" dialog
// needs before it writes anything: overwriting a photo's location destroys
// evidence — a coordinate the camera recorded, or one somebody placed by hand —
// so the reader is told how much of the batch that would affect and chooses.
//
// Total counts the photos that exist, each once: a UID repeated in the request
// is one photo, and a UID of a photo that is gone is none.
type LocationSummary struct {
	// Total is how many of the requested photos exist.
	Total int `json:"total"`
	// WithLocation is how many of them already carry a full coordinate.
	WithLocation int `json:"with_location"`
}

// locationSummarySQL counts the existing target photos and, among them, the ones
// already placed on the map. Half a coordinate does not count as placed, matching
// what a fill-the-gaps set-location operation would then complete.
const locationSummarySQL = `
SELECT count(*), count(*) FILTER (WHERE lat IS NOT NULL AND lng IS NOT NULL)
FROM photos
WHERE uid = ANY($1)`

// LocationSummary reports how many of the given photos already have coordinates,
// so a bulk set-location can state the cost of overwriting before it is applied.
// It reads and never writes.
//
// It rejects the same batches Apply does — an empty list is ErrNoPhotos and an
// oversized one ErrBatchTooLarge — so the dialog cannot promise a preview of a
// batch the apply behind it would refuse.
func (s *Service) LocationSummary(ctx context.Context, photoUIDs []string) (LocationSummary, error) {
	if len(photoUIDs) == 0 {
		return LocationSummary{}, ErrNoPhotos
	}
	if len(photoUIDs) > s.maxBatch {
		return LocationSummary{}, fmt.Errorf(
			"%w: %d exceeds limit %d", ErrBatchTooLarge, len(photoUIDs), s.maxBatch)
	}
	var summary LocationSummary
	row := s.pool.QueryRow(ctx, locationSummarySQL, photoUIDs)
	if err := row.Scan(&summary.Total, &summary.WithLocation); err != nil {
		return LocationSummary{}, fmt.Errorf("bulk: summarising locations: %w", err)
	}
	return summary, nil
}
