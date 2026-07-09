package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNoPhotoUIDs indicates a photo-set command was given no photos at all, which
// the API rejects with 400.
var ErrNoPhotoUIDs = errors.New("ctl: at least one photo uid is required")

// ConfirmThreshold is how many photos a single command may affect before it asks
// the operator to confirm. A curation mistake on a handful of photos is trivially
// undone; one on a whole search result is not, so anything larger needs either an
// answered prompt or an explicit --yes.
const ConfirmThreshold = 50

// maxUIDInput caps how much of a piped photo-UID list is read. A full page of 500
// photo objects is far below this; the cap only stops an unbounded pipe from
// exhausting the client.
const maxUIDInput = 32 << 20

// requireUID rejects a blank resource uid before a request is spent on it, naming
// the resource whose uid is missing. The returned error matches ErrEmptyUID.
func requireUID(resource, uid string) error {
	if strings.TrimSpace(uid) == "" {
		return fmt.Errorf("%w: %s uid is blank", ErrEmptyUID, resource)
	}
	return nil
}

// ParsePhotoUIDs reads a photo-UID set from r, so a bulk or membership command
// composes with whatever produced the list. It accepts, in order of detection:
//
//   - a /photos or /search envelope, exactly as `ctl photos list -o json` prints
//     it: {"photos":[{"uid":…}, …], …};
//   - a bare JSON array, either of uid strings or of objects carrying a uid, as
//     `jq '.photos'` yields;
//   - plain whitespace- or newline-separated uids, as `jq -r '.photos[].uid'`
//     yields.
//
// The result preserves first-seen order, drops blanks and de-duplicates, so a
// repeated uid neither inflates the confirmation count nor is sent twice. An
// input that yields no uid at all returns ErrNoPhotoUIDs.
func ParsePhotoUIDs(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxUIDInput))
	if err != nil {
		return nil, fmt.Errorf("reading the photo uid list: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, ErrNoPhotoUIDs
	}
	var uids []string
	switch trimmed[0] {
	case '{':
		uids, err = uidsFromEnvelope(trimmed)
	case '[':
		uids, err = uidsFromArray(trimmed)
	default:
		uids = strings.Fields(string(trimmed))
	}
	if err != nil {
		return nil, err
	}
	return NormalizeUIDs(uids)
}

// photoUIDRow is a photo row reduced to the only field a UID list needs.
type photoUIDRow struct {
	UID string `json:"uid"`
}

// uidsFromEnvelope pulls the uids out of a {"photos":[…]} list envelope.
func uidsFromEnvelope(data []byte) ([]string, error) {
	var payload struct {
		Photos []photoUIDRow `json:"photos"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decoding the piped photo list: %w", err)
	}
	return rowUIDs(payload.Photos), nil
}

// uidsFromArray pulls the uids out of a bare JSON array, which may hold uid
// strings or whole photo objects.
func uidsFromArray(data []byte) ([]string, error) {
	var strs []string
	if err := json.Unmarshal(data, &strs); err == nil {
		return strs, nil
	}
	var rows []photoUIDRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decoding the piped photo list: %w", err)
	}
	return rowUIDs(rows), nil
}

// rowUIDs projects decoded photo rows onto their uids.
func rowUIDs(rows []photoUIDRow) []string {
	uids := make([]string, 0, len(rows))
	for _, row := range rows {
		uids = append(uids, row.UID)
	}
	return uids
}

// NormalizeUIDs trims, drops blanks and de-duplicates a photo-uid set, preserving
// first-seen order. It returns ErrNoPhotoUIDs when nothing survives, so a command
// never sends an empty batch the API would only answer with a 400.
func NormalizeUIDs(uids []string) ([]string, error) {
	seen := make(map[string]struct{}, len(uids))
	out := make([]string, 0, len(uids))
	for _, uid := range uids {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	if len(out) == 0 {
		return nil, ErrNoPhotoUIDs
	}
	return out, nil
}
