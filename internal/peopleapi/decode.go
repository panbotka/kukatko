package peopleapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/people"
)

// maxBodyBytes caps the request body size for subject mutations; a subject record
// is small, so a tight limit guards against oversized payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// errEmptyName is returned when a create or update request omits the subject
// name, which the slug derivation and display both require.
var errEmptyName = errors.New("subject name is required")

// errUnusableName is returned when a name is present but names nobody —
// punctuation or symbols alone. Such a name has no slug of its own and would be
// stored under the shared fallback slug, so the subject would read as unnamed in
// every listing and act as a magnet for later find-or-create-by-name lookups.
var errUnusableName = errors.New("subject name must contain a letter or a digit")

// errEmptyKeeper is returned when a merge request names no keeper, which leaves
// nothing to merge into.
var errEmptyKeeper = errors.New("keeper_uid is required")

// decodeMergeInput reads and validates the JSON merge body from r, by the same
// rules as decodeSubjectInput (unknown fields and oversized bodies rejected).
func decodeMergeInput(r *http.Request) (mergeInput, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var in mergeInput
	if err := dec.Decode(&in); err != nil {
		return mergeInput{}, errors.New("invalid request body: " + err.Error())
	}
	in.KeeperUID = strings.TrimSpace(in.KeeperUID)
	if in.KeeperUID == "" {
		return mergeInput{}, errEmptyKeeper
	}
	return in, nil
}

// decodeSubjectInput reads and validates the JSON subject body from r. It rejects
// unknown fields, an oversized body, and a name that identifies nobody, returning
// an error whose message is safe to surface to the client.
func decodeSubjectInput(r *http.Request) (subjectInput, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var in subjectInput
	if err := dec.Decode(&in); err != nil {
		return subjectInput{}, errors.New("invalid request body: " + err.Error())
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return subjectInput{}, errEmptyName
	}
	if people.NameSlug(in.Name) == "" {
		return subjectInput{}, errUnusableName
	}
	return in, nil
}
