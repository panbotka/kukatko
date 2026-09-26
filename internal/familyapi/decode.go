package familyapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/family"
)

// maxBodyBytes caps the request body size for family mutations; a relation and a
// family record are both small, so a tight limit guards against oversized
// payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// errNoRole is returned when a relation request omits the role, which is the one
// thing about it that cannot be inferred.
var errNoRole = errors.New("role is required: parent, child, partner or sibling")

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
