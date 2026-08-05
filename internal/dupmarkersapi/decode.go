package dupmarkersapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// maxBodyBytes caps the request body size. Both repair bodies are a handful of
// short identifiers, so a tight 64 KiB limit guards against oversized payloads.
const maxBodyBytes = 64 << 10

// errNoPhotoUID is returned when a repair body omits the photo UID.
var errNoPhotoUID = errors.New("photo_uid is required")

// errNoSubjectUID is returned when a repair body omits the subject UID.
var errNoSubjectUID = errors.New("subject_uid is required")

// errNoKeepMarkerUID is returned when a keep body omits the marker to keep.
var errNoKeepMarkerUID = errors.New("keep_marker_uid is required")

// errNoMarkerUID is returned when an invalid-flag body omits the marker.
var errNoMarkerUID = errors.New("marker_uid is required")

// keepInput is the JSON body of POST /duplicate-markers/keep: the finding, named
// by its (photo, subject) pair, plus the one marker that survives it. The losing
// markers are deliberately NOT in the body — the server resolves the group itself,
// so a client list that went stale cannot detach a marker that has meanwhile been
// re-tagged.
type keepInput struct {
	PhotoUID      string `json:"photo_uid"`
	SubjectUID    string `json:"subject_uid"`
	KeepMarkerUID string `json:"keep_marker_uid"`
}

// invalidInput is the JSON body of POST /duplicate-markers/invalid: the one marker
// whose box holds no face at all.
type invalidInput struct {
	MarkerUID string `json:"marker_uid"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields and an
// oversized body. The returned error message is safe to surface to the client.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeKeep decodes and validates a keep body, requiring all three identifiers.
func decodeKeep(r *http.Request) (keepInput, error) {
	var in keepInput
	if err := decodeJSON(r, &in); err != nil {
		return keepInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	in.SubjectUID = strings.TrimSpace(in.SubjectUID)
	in.KeepMarkerUID = strings.TrimSpace(in.KeepMarkerUID)
	switch {
	case in.PhotoUID == "":
		return keepInput{}, errNoPhotoUID
	case in.SubjectUID == "":
		return keepInput{}, errNoSubjectUID
	case in.KeepMarkerUID == "":
		return keepInput{}, errNoKeepMarkerUID
	}
	return in, nil
}

// decodeInvalid decodes and validates an invalid-flag body, requiring the marker.
func decodeInvalid(r *http.Request) (invalidInput, error) {
	var in invalidInput
	if err := decodeJSON(r, &in); err != nil {
		return invalidInput{}, err
	}
	in.MarkerUID = strings.TrimSpace(in.MarkerUID)
	if in.MarkerUID == "" {
		return invalidInput{}, errNoMarkerUID
	}
	return in, nil
}
