package hlsjob

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/jobs"
)

// Recorded identifies one rendition row together with the file hash whose prefix
// holds the objects it describes. It is the smallest thing a whole-library
// reconciliation of the catalogue against the store needs: a row promises that
// hls/<file_hash>/<rendition>/ can be fetched, so answering "is that still true?"
// takes exactly these three strings and none of the playlist text.
type Recorded struct {
	// PhotoUID identifies the video the rendition belongs to.
	PhotoUID string `json:"photo_uid"`
	// Rendition is the rendition's name, verbatim as it appears in object keys.
	Rendition string `json:"rendition"`
	// FileHash is the video's content hash, which names its prefix in the store.
	FileHash string `json:"file_hash"`
}

// listRecordedSQL selects every rendition row with the file hash of the video it
// belongs to, in a stable order. It reads no playlist: the column holds tens of
// kilobytes per row, and a reconciliation over the whole library needs none of it.
const listRecordedSQL = `
SELECT h.photo_uid, h.rendition, p.file_hash
FROM photo_hls_renditions h
JOIN photos p ON p.uid = h.photo_uid
ORDER BY h.photo_uid, h.rendition`

// ListRecorded returns every rendition the catalogue records, each with the file
// hash naming its prefix in the object store, ordered by photo and rendition. A
// library that has never been encoded yields an empty (non-nil) slice.
//
// It backs the integrity scan's streaming check, which asks the store whether the
// objects each row promises are still there.
func (s *Store) ListRecorded(ctx context.Context) ([]Recorded, error) {
	rows, err := s.pool.Query(ctx, listRecordedSQL)
	if err != nil {
		return nil, fmt.Errorf("hlsjob: listing recorded renditions: %w", err)
	}
	defer rows.Close()

	out := []Recorded{}
	for rows.Next() {
		var rec Recorded
		if scanErr := rows.Scan(&rec.PhotoUID, &rec.Rendition, &rec.FileHash); scanErr != nil {
			return nil, fmt.Errorf("hlsjob: scanning recorded renditions: %w", scanErr)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hlsjob: iterating recorded renditions: %w", err)
	}
	return out, nil
}

// encodingHashesSQL selects the content hash of every video the queue still owes
// an encode. A done job is finished and a dead one will never run again, so
// neither can be writing anything; the three states in between all can.
const encodingHashesSQL = `
SELECT DISTINCT p.file_hash
FROM jobs j
JOIN photos p ON p.uid = j.payload ->> 'photo_uid'
WHERE j.type = $1 AND j.state = ANY($2)`

// encodingStates are the job states from which an hls_transcode run can still
// publish objects: one waiting to start, one running now, and one that failed and
// is waiting for its retry.
var encodingStates = []string{
	string(jobs.StateQueued), string(jobs.StateRunning), string(jobs.StateFailed),
}

// EncodingHashes returns the file hash of every video with an unfinished
// hls_transcode job — queued, running, or waiting to be retried — in no
// particular order.
//
// It exists because the encode publishes its segments before it writes the row
// that describes them (see encodeOne): between those two moments the objects of a
// perfectly healthy encode are indistinguishable from abandoned ones. Anything
// that reasons about "objects no rendition claims" has to subtract these hashes
// first, or it will report a job in flight as rot — and, worse, offer to delete
// what that job is still writing.
func (s *Store) EncodingHashes(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, encodingHashesSQL, jobs.TypeHLSTranscode, encodingStates)
	if err != nil {
		return nil, fmt.Errorf("hlsjob: listing videos being encoded: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var hash string
		if scanErr := rows.Scan(&hash); scanErr != nil {
			return nil, fmt.Errorf("hlsjob: scanning videos being encoded: %w", scanErr)
		}
		out = append(out, hash)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hlsjob: iterating videos being encoded: %w", err)
	}
	return out, nil
}

// deleteSQL removes one photo's row for one rendition.
const deleteSQL = `DELETE FROM photo_hls_renditions WHERE photo_uid = $1 AND rendition = $2`

// Delete removes the recorded rendition of the given name for photoUID,
// reporting whether there was a row to remove. Deleting one that is not there is
// not an error, so a repair that runs twice converges.
//
// It withdraws a promise, nothing more: the row says a player can fetch
// hls/<file_hash>/<rendition>/, and when those objects are gone the honest state
// of the catalogue is that the clip does not stream. The video itself is
// untouched, and with no rendition row left it is exactly what the ordinary
// encode backfill (POST /process/hls) looks for.
func (s *Store) Delete(ctx context.Context, photoUID, rendition string) (bool, error) {
	tag, err := s.pool.Exec(ctx, deleteSQL, photoUID, rendition)
	if err != nil {
		return false, fmt.Errorf("hlsjob: deleting rendition %s of %s: %w", rendition, photoUID, err)
	}
	return tag.RowsAffected() > 0, nil
}
