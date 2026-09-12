package familyapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/family"
)

// maxBodyBytes caps the request body size for family mutations; a relation and a
// family record are both small, so a tight limit guards against oversized
// payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// errNoRole is returned when a relation request omits the role, which is the one
// thing about it that cannot be inferred.
var errNoRole = errors.New("role is required: parent, child or partner")

// decodeAddRelation reads and validates the JSON relation body from r, rejecting
// unknown fields and an oversized body. It checks only what the request must
// carry; whether the relation itself is possible is the store's business, decided
// against the rows inside the transaction.
func decodeAddRelation(r *http.Request) (family.AddRelation, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var in family.AddRelation
	if err := dec.Decode(&in); err != nil {
		return family.AddRelation{}, errors.New("invalid request body: " + err.Error())
	}
	in.SubjectUID = strings.TrimSpace(in.SubjectUID)
	if in.Role == "" {
		return family.AddRelation{}, errNoRole
	}
	if in.New != nil {
		in.New.Name = strings.TrimSpace(in.New.Name)
	}
	return in, nil
}

// decodeFamilyUpdate reads and validates the JSON family body from r, rejecting
// unknown fields and an oversized body. An omitted kind means the family is a
// plain partnership — the same default the store applies — resolved here so the
// audit diff records the value that will actually be stored.
func decodeFamilyUpdate(r *http.Request) (family.Update, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var in family.Update
	if err := dec.Decode(&in); err != nil {
		return family.Update{}, errors.New("invalid request body: " + err.Error())
	}
	if in.Kind == "" {
		in.Kind = family.KindPartnership
	}
	in.Note = strings.TrimSpace(in.Note)
	return in, nil
}

// parseTreeParams reads the tree endpoint's query parameters: which way to walk
// and how far. An omitted direction walks down, which is the tree people mean
// when they say "the Nečas family"; an omitted generations means the whole
// bounded walk. An unrecognised direction or a non-numeric generations is
// rejected rather than silently defaulted, because quietly answering a different
// question than the one asked is worse than an error.
func parseTreeParams(r *http.Request) (family.Direction, int, error) {
	query := r.URL.Query()
	direction := family.DirectionDescendants
	if raw := query.Get("direction"); raw != "" {
		direction = family.Direction(raw)
		if direction != family.DirectionDescendants && direction != family.DirectionAncestors {
			return "", 0, errors.New("direction must be descendants or ancestors")
		}
	}
	generations, err := parseGenerations(query.Get("generations"))
	if err != nil {
		return "", 0, err
	}
	return direction, generations, nil
}

// parseGenerations reads how many generations a tree request asks for. An empty
// value means 0 — the whole bounded walk — and a negative or non-numeric value is
// rejected. The upper bound is the store's business: it clamps to family.MaxDepth,
// so a client asking for a thousand generations gets the deepest walk there is
// rather than an error about a limit it has no reason to know.
func parseGenerations(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("generations must be an integer")
	}
	if value < 0 {
		return 0, errors.New("generations must not be negative")
	}
	return value, nil
}
