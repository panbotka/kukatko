// Package tagnotifyjob turns face tagging into one "you were tagged in N photos"
// push notification per window, instead of one per photo.
//
// When somebody uploads an album from a village event and spends an afternoon
// naming the faces on it, the people they name must not receive forty separate
// notifications. So tagging is collected per account:
//
//   - The first tag opens a window. Recorder hears every assignment inside the
//     assignment's own transaction (it is the people store's TagObserver),
//     records one tag_notices row per (account, photo) and — when the account has
//     no window open yet — enqueues a `tag_notify` job whose run_after is one
//     window (push.tags.window) into the future.
//   - Everything tagged inside the window is counted: later tags add rows and
//     find the job already queued.
//   - When the window closes, Service.Handle consumes the account's rows and
//     records one notification with the frozen photo set, newest photo first,
//     schedules its push delivery, and deletes the rows — all in one
//     transaction, so a crash part-way re-runs cleanly instead of losing the
//     notices or announcing them twice.
//
// The rules the recorder enforces are what keeps the message honest: an account
// is never told about its own work, a photo it may not see (private, hidden,
// archived) is never recorded, an account that turned the kind off records
// nothing, and an untagging before the window closes un-announces the photo.
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
// does not say (push.tags.window).
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
