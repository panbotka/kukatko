// Package albumfeaturedjob turns a curator featuring an album — putting it on
// the homepage's "Doporučujeme" strip — into one "a new album is featured"
// notification per account that asked for the kind, delivered to whichever
// channels (in-app, push, e-mail) the account wants through
// internal/notifydeliver.
//
// The fan-out never runs inside the feature's request. The organize store's
// feature observer (Scheduler) enqueues one `album_featured_notify` job in the
// feature's transaction, payload = the album's uid, and only when the album
// was not featured already — prolonging or shortening a running feature is
// silent. The job (Service) then reads the album as the strip would list it,
// walks the accounts of the album's project (its members and the maintainers)
// and delivers to the ones the rules allow, into that project's inbox with a
// link that opens there:
//
//   - an account that is no member of the album's project hears nothing;
//   - the account that featured the album is never told about its own work;
//   - a private album is announced only to accounts that may curate, the same
//     rule the strip itself applies;
//   - a disabled account hears nothing;
//   - an account that wants the kind through no channel gets no record (that
//     is notifydeliver's rule, applied per account).
//
// An album unfeatured before the job runs is simply no longer featured when the
// job reads it, and nothing is sent. The payload's album uid is the job's
// dedup key (migration 0097): a feature → unfeature → feature sequence while
// the job is still queued collapses into that one job, which reads the album
// once it runs.
//
// The message names no curator — the strip is the library's recommendation,
// not one person's.
package albumfeaturedjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
)

// AlbumPathPrefix is the in-app route of an album's page, the uid appended. It
// is what the notification opens.
const AlbumPathPrefix = "/albums/"

// AlbumPath returns the in-app path of album uid's page.
func AlbumPath(uid string) string {
	return AlbumPathPrefix + uid
}

// payload is an album_featured_notify job's payload: the album to announce.
// The key is album_uid because the dedup index (migration 0097) reads exactly
// that field.
type payload struct {
	AlbumUID string `json:"album_uid"`
}

// errMissingAlbum is a payload that names no album.
var errMissingAlbum = errors.New("albumfeaturedjob: payload names no album")

// encodePayload returns the payload of the job that announces albumUID.
func encodePayload(albumUID string) (json.RawMessage, error) {
	raw, err := json.Marshal(payload{AlbumUID: albumUID})
	if err != nil {
		return nil, fmt.Errorf("albumfeaturedjob: encoding the payload: %w", err)
	}
	return raw, nil
}

// decodePayload reads an album_featured_notify payload and returns the album it
// names. Malformed JSON and a blank album are errors no retry can fix.
func decodePayload(raw json.RawMessage) (string, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("albumfeaturedjob: decoding the payload: %w", err)
	}
	if strings.TrimSpace(p.AlbumUID) == "" {
		return "", errMissingAlbum
	}
	return p.AlbumUID, nil
}

// Enqueue inserts a job into the persistent queue through a caller-supplied
// executor without aborting the caller's transaction on a duplicate. It is
// satisfied by jobs.EnqueueOrSkip, which is what production wires; a test
// substitutes its own function and needs no database.
type Enqueue func(
	ctx context.Context, exec jobs.Execer, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions,
) (jobs.Job, error)

// Scheduler is the organize store's feature observer: it schedules the
// announcement of every freshly featured album inside the feature's
// transaction.
type Scheduler struct {
	enqueue Enqueue
}

// NewScheduler returns a Scheduler enqueuing through enqueue; nil uses
// jobs.EnqueueOrSkip, the production queue.
func NewScheduler(enqueue Enqueue) *Scheduler {
	if enqueue == nil {
		enqueue = jobs.EnqueueOrSkip
	}
	return &Scheduler{enqueue: enqueue}
}

// Featured enqueues the album_featured_notify job for f.AlbumUID on tx, so the
// announcement is scheduled if and only if the feature commits. A job for the
// album already queued (jobs.ErrDuplicate) is left alone: the album will be
// read when it runs, and the insert used skips the duplicate without aborting
// tx. The actor is not recorded — the job reads albums.featured_by, which the
// same transaction wrote.
func (s *Scheduler) Featured(ctx context.Context, tx pgx.Tx, f organize.Featuring) error {
	raw, err := encodePayload(f.AlbumUID)
	if err != nil {
		return err
	}
	if _, err := s.enqueue(ctx, tx, jobs.TypeAlbumFeaturedNotify, raw, jobs.EnqueueOptions{}); err != nil {
		if errors.Is(err, jobs.ErrDuplicate) {
			return nil
		}
		return fmt.Errorf("albumfeaturedjob: scheduling the announcement of album %s: %w", f.AlbumUID, err)
	}
	return nil
}
