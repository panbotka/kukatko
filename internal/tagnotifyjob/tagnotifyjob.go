// Package tagnotifyjob turns face tagging into a handful of "you were tagged"
// notifications per window — one per album the photos are in, plus one for the
// rest — instead of one per photo, delivered to whichever channels (in-app,
// push, e-mail) the account wants through internal/notifydeliver.
//
// When somebody uploads an album from a village event and spends an afternoon
// naming the faces on it, the people they name must not receive forty separate
// notifications. So tagging is collected per account:
//
//   - The first tag opens a window. Recorder hears every assignment inside the
//     assignment's own transaction (it is the people store's TagObserver),
//     records one tag_notices row per (account, photo) and — when the account has
//     no window open yet — enqueues a `tag_notify` job whose run_after is one
//     window (notifications.tags.window) into the future.
//   - Everything tagged inside the window is counted: later tags add rows and
//     find the job already queued.
//   - When the window closes, Service.Handle consumes the account's rows,
//     groups the photos by the hand-made albums they are in (a private album
//     only for a recipient who may curate; at most MaxAlbumGroups albums, the
//     rest collapse) and delivers one notification per album — "you were
//     tagged in 5 photos in the album Pouť 2026", its frozen photo set newest
//     first — then one for the photos no album notification names, to the
//     account's channels; the e-mail is one for the whole window, listing the
//     albums with their counts. All in one transaction, so a crash part-way
//     re-runs cleanly instead of losing the notices or announcing them twice.
//
// The rules the recorder enforces are what keeps the message honest: an account
// is never told about its own work, a photo it may not see (private, hidden,
// archived) is never recorded, an account that would receive the kind through
// no channel records nothing, and an untagging before the window closes un-announces the photo.
// The job re-checks the last two when it runs — visibility and presence can
// change inside the window in more ways than an untagging (an archive, a merge,
// an invalidated marker) — so the notification never names a photo the person
// is no longer on or may no longer see.
//
// The message names nobody. Several people can tag inside one window, so a
// single name would be wrong and a list would not fit a lock screen.
package tagnotifyjob

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultWindow is how long a tagging window stays open when the configuration
// does not say (notifications.tags.window).
const DefaultWindow = time.Hour

// lockSQL serialises one account's window between the tagging transactions and
// the job that closes it. Both take it before touching the account's notices
// and hold it until they commit, which is what makes "is a window already open?"
// a question with a stable answer — see Recorder.open and Service.Handle.
const lockSQL = `SELECT pg_advisory_xact_lock(hashtextextended('tag_notify:' || $1, 0))`

// payload is a tag_notify job's payload: the account whose window it closes.
// The key is user_uid because the dedup index (migration 0089) reads exactly
// that field.
type payload struct {
	UserUID string `json:"user_uid"`
}

// errMissingUser is a payload that names no account.
var errMissingUser = errors.New("tagnotifyjob: payload names no account")

// encodePayload returns the payload of the job that closes userUID's window.
func encodePayload(userUID string) (json.RawMessage, error) {
	raw, err := json.Marshal(payload{UserUID: userUID})
	if err != nil {
		return nil, fmt.Errorf("tagnotifyjob: encoding the payload: %w", err)
	}
	return raw, nil
}

// decodePayload reads a tag_notify payload and returns the account it names.
// Malformed JSON and a blank account are errors no retry can fix.
func decodePayload(raw json.RawMessage) (string, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("tagnotifyjob: decoding the payload: %w", err)
	}
	if strings.TrimSpace(p.UserUID) == "" {
		return "", errMissingUser
	}
	return p.UserUID, nil
}
