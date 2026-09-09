package photos

import (
	"context"
	"fmt"
)

// listVideosMissingHLSSQL selects the uids of non-archived videos that have not
// been encoded into a single streaming rendition, newest first. The trailing %s
// receives an optional LIMIT clause.
//
// The predicate is "no rendition row at all" rather than "fewer rows than the
// configured plan": a rendition list is an instance's configuration, not a
// property of the catalogue, so counting against it here would make the backfill
// re-schedule every video the moment a quality level is added — which is what
// ?all=true is for.
const listVideosMissingHLSSQL = `
SELECT p.uid
FROM photos p
LEFT JOIN photo_hls_renditions h ON h.photo_uid = p.uid
WHERE p.media_type = 'video' AND p.archived_at IS NULL AND h.photo_uid IS NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListVideosMissingHLS returns the uids of non-archived videos with no recorded
// HLS rendition, newest first. A positive limit caps the result; a non-positive
// limit returns every pending video. It backs the HLS backfill, which enqueues an
// `hls_transcode` job per returned uid.
//
// Live photos are deliberately excluded (media_type is exactly 'video'): their
// motion clip is a one-to-three second hover preview, not something anybody
// streams, and the transcode job skips them for the same reason.
func (s *Store) ListVideosMissingHLS(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listVideosMissingHLSSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listVideosMissingHLSSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	return s.queryUIDs(ctx, "listing videos missing HLS", query, args...)
}

// listActiveVideoUIDsSQL selects the uids of every non-archived video, newest
// first.
const listActiveVideoUIDsSQL = `
SELECT uid
FROM photos
WHERE media_type = 'video' AND archived_at IS NULL
ORDER BY created_at DESC, uid DESC`

// ListActiveVideoUIDs returns the uids of every non-archived video, newest
// first. It backs the forced full HLS backfill (`?all=true`), which re-encodes
// videos that already have renditions — how a library picks up a newly enabled
// quality level or a changed segment length.
func (s *Store) ListActiveVideoUIDs(ctx context.Context) ([]string, error) {
	return s.queryUIDs(ctx, "listing active videos", listActiveVideoUIDsSQL)
}
