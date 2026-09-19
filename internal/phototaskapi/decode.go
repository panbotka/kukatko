package phototaskapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/phototask"
)

// maxBodyBytes caps a task mutation's request body. The body field is the
// largest thing in it and is bounded at 8000 characters by the model, so this is
// generous while still refusing an unbounded upload.
const maxBodyBytes = 1 << 20 // 1 MiB

// maxPhotoUIDs caps how many photographs one membership request may name. It
// matches the store's own limit, so a request that would be refused later is
// refused before it is decoded into memory.
const maxPhotoUIDs = phototask.MaxPhotos

// createRequest is the JSON body of POST /tasks.
type createRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Query     string   `json:"query"`
	State     string   `json:"state"`
	PhotoUIDs []string `json:"photo_uids"`
	// Options are the answers the question offers, at most phototask.MaxOptions.
	Options []string `json:"options"`
}

// updateRequest is the JSON body of PATCH /tasks/{uid}. Every field is a pointer
// so an absent one is left alone and an explicit empty string clears the value —
// the difference matters for a resolution.
type updateRequest struct {
	Title      *string `json:"title"`
	Body       *string `json:"body"`
	Query      *string `json:"query"`
	State      *string `json:"state"`
	Resolution *string `json:"resolution"`
	// Options replaces the whole set: absent leaves it alone, an empty array
	// clears it.
	Options *[]string `json:"options"`
}

// membershipRequest is the JSON body of the two membership endpoints.
type membershipRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// listResponse is the JSON body of GET /tasks: the page, and the total before
// paging so a client can say "1-25 of 63" without a second request.
type listResponse struct {
	Tasks  []phototask.Task `json:"tasks"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

// membershipResponse is the JSON body of the two membership endpoints: how many
// rows actually changed, and the task as it now stands.
type membershipResponse struct {
	Changed int            `json:"changed"`
	Task    phototask.Task `json:"task"`
}

// decodeJSON decodes a request body into dst, rejecting unknown fields and a
// body larger than maxBodyBytes.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeMembership decodes and checks a membership request: at least one
// photograph, and no more than the store would accept.
func decodeMembership(r *http.Request) ([]string, error) {
	var body membershipRequest
	if err := decodeJSON(r, &body); err != nil {
		return nil, err
	}
	if len(body.PhotoUIDs) == 0 {
		return nil, errors.New("photo_uids is required")
	}
	if len(body.PhotoUIDs) > maxPhotoUIDs {
		return nil, fmt.Errorf("photo_uids holds %d entries, the limit is %d",
			len(body.PhotoUIDs), maxPhotoUIDs)
	}
	return body.PhotoUIDs, nil
}

// toUpdate converts a decoded PATCH body into the store's partial update. The
// state is only carried across, not validated: the store owns that rule, and
// owning it twice is how the two come to disagree.
func (u updateRequest) toUpdate() phototask.Update {
	upd := phototask.Update{
		Title: u.Title, Body: u.Body, Query: u.Query, Resolution: u.Resolution, Options: u.Options,
	}
	if u.State != nil {
		state := phototask.State(*u.State)
		upd.State = &state
	}
	return upd
}

// parseFilter reads a listing's query parameters into a store filter. An unknown
// state is an error rather than an empty result, so a typo says so instead of
// looking like "nothing matches". callerUID is who is asking: the participant
// filter's "me" alias resolves to it, and the two caller-relative flags — and
// the answered/waiting filters over them — are computed for it.
func parseFilter(q url.Values, callerUID string) (phototask.Filter, error) {
	states, err := parseStates(q["state"])
	if err != nil {
		return phototask.Filter{}, err
	}
	limit, err := parseInt(q.Get("limit"), "limit")
	if err != nil {
		return phototask.Filter{}, err
	}
	offset, err := parseInt(q.Get("offset"), "offset")
	if err != nil {
		return phototask.Filter{}, err
	}
	return phototask.Filter{
		States:         states,
		CallerUID:      callerUID,
		Open:           parseBool(q.Get("open")),
		Answered:       parseBool(q.Get("answered")),
		Waiting:        parseBool(q.Get("waiting")),
		Search:         strings.TrimSpace(q.Get("q")),
		PhotoUID:       strings.TrimSpace(q.Get("photo")),
		ParticipantUID: resolveParticipant(q.Get("participant"), callerUID),
		Limit:          limit,
		Offset:         offset,
	}, nil
}

// resolveParticipant reads the participant filter, translating the literal "me"
// to the caller's own account. The alias exists because "what am I on?" is the
// question the filter is almost always asked, and a client that had to know its
// own uid to ask it would have to fetch it first.
func resolveParticipant(value, callerUID string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "me" {
		return callerUID
	}
	return trimmed
}

// parseStates reads the repeatable state parameter, which may also be
// comma-separated, and checks every value against the known states.
func parseStates(values []string) ([]phototask.State, error) {
	var out []phototask.State
	for _, value := range values {
		for name := range strings.SplitSeq(value, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			state := phototask.State(name)
			if !state.Valid() {
				return nil, fmt.Errorf("unknown state %q", name)
			}
			out = append(out, state)
		}
	}
	return out, nil
}

// parseBool reads a flag parameter, treating the usual affirmatives as true and
// everything else — including an absent parameter — as false.
func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// parseInt reads an optional non-negative integer parameter, naming it in the
// error. An absent parameter is zero, which the store reads as "use the default".
func parseInt(value, name string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative whole number", name)
	}
	return n, nil
}
