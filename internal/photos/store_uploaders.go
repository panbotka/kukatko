package photos

import (
	"context"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/project"
)

// UploaderBucket is one uploader of the photos matching a filter, together with
// how many of them they contributed. It backs the library's uploader facet,
// where each contributor is offered with their count so the reader sees how much
// of the current view is theirs before selecting them.
//
// The photos nobody uploaded — the items an import brought in — are a bucket of
// their own, with every field but Count empty. They are reported rather than
// dropped so the buckets add up to the listing they describe; the caller names
// the group ("imported"), because only the caller knows the reader's language.
type UploaderBucket struct {
	// UID is the uploader's user UID, empty for the no-uploader bucket.
	UID string
	// Username is the uploader's login name, empty for the no-uploader bucket.
	Username string
	// DisplayName is the uploader's full name, empty when they never set one (and
	// for the no-uploader bucket).
	DisplayName string
	// Former reports an uploader who is no member of the project any more (or
	// never was — an upload credited before the upload link checked
	// membership): project.FormerMemberSQL. Username and DisplayName are then
	// empty, so the project never reads the account's live name; the client
	// says "former member".
	Former bool
	// Count is how many of the matching photos this bucket holds.
	Count int
}

// uploadersSQL groups the matching photos by their uploader, largest
// contribution first, and resolves each uploader's name in the same round trip.
//
// The aggregation is a subquery and the users join sits outside it on purpose:
// the %s placeholder receives the shared List/Count WHERE filters, which name
// photo columns unqualified (created_at, uid, …), and users carries columns of
// those very names — joined first, the filters would be ambiguous. The join is a
// LEFT one so the NULL uploaded_by group survives it, and the count is taken
// before the join, so it can never be multiplied by it.
//
// A former member of the project ($1, bound first by buildWhere) keeps their
// bucket — the photos are still theirs, and selecting it still filters by uid —
// but not their name: both name columns read NULL and the last column is set.
var uploadersSQL = `SELECT b.uploaded_by,
    CASE WHEN ` + uploaderFormerSQL + ` THEN NULL ELSE u.username END,
    CASE WHEN ` + uploaderFormerSQL + ` THEN NULL ELSE u.display_name END,
    b.cnt, COALESCE(` + uploaderFormerSQL + `, false)
FROM (
    SELECT uploaded_by, count(*)::int AS cnt
    FROM photos%s
    GROUP BY uploaded_by
) b
LEFT JOIN users u ON u.uid = b.uploaded_by
ORDER BY b.cnt DESC, b.uploaded_by ASC NULLS LAST`

// uploaderFormerSQL is project.FormerMemberSQL for the uploader row u of
// uploadersSQL in the project $1.
var uploaderFormerSQL = project.FormerMemberSQL("$1", "u")

// UploaderBuckets returns the uploaders of the photos matching params, the
// largest contribution first, each with its photo count — the option list behind
// the library's uploader facet. It reuses the shared buildWhere filters, so a
// bucket's count is exactly the number of photos List would return for the same
// filters plus that uploader; params' sort, order and pagination are ignored
// because the aggregation is always grouped by uploader.
//
// The photos with no uploader form their own bucket (empty UID), so the counts
// add up to Count over the same filters. Ties in the count are broken by UID so
// the order is total and a facet does not reshuffle between two requests. The
// slice is empty (not nil) when nothing matches.
func (s *Store) UploaderBuckets(
	ctx context.Context, projectUID string, params ListParams,
) ([]UploaderBucket, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	query, args := buildUploadersQuery(projectUID, params)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: querying uploaders: %w", err)
	}
	defer rows.Close()

	buckets := make([]UploaderBucket, 0)
	for rows.Next() {
		var (
			bucket                     UploaderBucket
			uid, username, displayName *string
		)
		if scanErr := rows.Scan(&uid, &username, &displayName, &bucket.Count, &bucket.Former); scanErr != nil {
			return nil, fmt.Errorf("photos: scanning uploader bucket: %w", scanErr)
		}
		bucket.UID = derefString(uid)
		bucket.Username = derefString(username)
		bucket.DisplayName = derefString(displayName)
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating uploader buckets: %w", err)
	}
	return buckets, nil
}

// derefString returns the string behind a nullable column, or "" when it is
// NULL — the no-uploader bucket, whose uploader columns are all absent.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// buildUploadersQuery assembles the parameterised uploader aggregation for
// UploaderBuckets, reusing List's WHERE filters so the buckets stay in step with
// List/Count. Grouping and ordering are fixed by the query, never taken from
// params.
func buildUploadersQuery(projectUID string, params ListParams) (string, []any) {
	where, args := buildWhere(projectUID, params)
	var filter string
	if len(where) > 0 {
		filter = "\n    WHERE " + strings.Join(where, " AND ")
	}
	return fmt.Sprintf(uploadersSQL, filter), args
}
