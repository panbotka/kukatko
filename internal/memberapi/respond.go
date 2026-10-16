package memberapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/project"
)

// The machine-readable error codes of the package, the "error" field of every
// refusal. A client maps them to its own words; the "message" that some carry
// is the rule in English, for a client that has none.
const (
	codeInvalidBody       = "invalid_body"
	codeInvalidRole       = "invalid_role"
	codeInvalidIdentifier = "invalid_identifier"
	codeNotFound          = "not_found"
	codeNotMember         = "not_member"
	codeLastAdmin         = "last_admin"
	codeSelf              = "self"
	codeInternal          = "internal"
)

// errorBody is the wire shape of every refusal.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// memberErrors maps the project store's sentinel errors onto refusals; the
// first match wins.
var memberErrors = []struct {
	sentinel error
	status   int
	code     string
}{
	{project.ErrNotMember, http.StatusNotFound, codeNotMember},
	{project.ErrNotFound, http.StatusNotFound, codeNotFound},
	{project.ErrLastAdmin, http.StatusConflict, codeLastAdmin},
	{project.ErrInvalidRole, http.StatusUnprocessableEntity, codeInvalidRole},
	{project.ErrEmptyIdentifier, http.StatusUnprocessableEntity, codeInvalidIdentifier},
}

// writeStoreError writes the refusal err stands for, or a logged 500 for
// anything that is not one of the known sentinels (the client only learns
// "internal").
func writeStoreError(w http.ResponseWriter, err error, doing string) {
	for _, known := range memberErrors {
		if errors.Is(err, known.sentinel) {
			writeError(w, known.status, known.code, "")
			return
		}
	}
	log.Printf("memberapi: %s: %v", doing, err)
	writeError(w, http.StatusInternalServerError, codeInternal, "")
}

// writeError writes a refusal with its code and an optional message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

// writeJSON encodes v as the response body with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("memberapi: writing response: %v", err)
	}
}

// decodeBody decodes the request body into v, refusing unknown fields and
// anything past maxBodyBytes, so a typo in a field name is an error rather than
// a silently ignored change.
func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding the request body: %w", err)
	}
	return nil
}
