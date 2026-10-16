package projectapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// The machine-readable error codes of the package, the "error" field of every
// refusal. A client maps them to its own words; the "message" that some carry
// is the rule in English, for a client that has none.
const (
	codeInvalidBody    = "invalid_body"
	codeNotFound       = "not_found"
	codeSlugTaken      = "slug_taken"
	codeInvalidSlug    = "invalid_slug"
	codeSlugImmutable  = "slug_immutable"
	codeInvalidName    = "invalid_name"
	codeInvalidLimit   = "invalid_limit"
	codeInvalidRole    = "invalid_role"
	codeUserNotFound   = "user_not_found"
	codeAlreadyMember  = "already_member"
	codeNotMember      = "not_member"
	codeLastAdmin      = "last_admin"
	codeSelf           = "self"
	codeLastMaintainer = "last_maintainer"
	codeInvalidEmail   = "invalid_email"
	codeInvalidUser    = "invalid_username"
	codeUsernameTaken  = "username_taken"
	codeEmailAmbiguous = "email_ambiguous"
	codeUserDisabled   = "user_disabled"
	codePasswordSet    = "password_set"
	codeInternal       = "internal"
)

// errorBody is the wire shape of every refusal.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// apiError is one refusal: the status, the code and an optional message.
type apiError struct {
	status  int
	code    string
	message string
}

// projectErrors maps the project store's sentinel errors onto refusals; the
// first match wins. A rule the client should be able to show (slug, limit)
// carries the store's own explanation as the message.
var projectErrors = []struct {
	sentinel error
	status   int
	code     string
	explain  bool
}{
	{project.ErrNotFound, http.StatusNotFound, codeNotFound, false},
	{project.ErrSlugTaken, http.StatusConflict, codeSlugTaken, false},
	{project.ErrInvalidSlug, http.StatusUnprocessableEntity, codeInvalidSlug, true},
	{project.ErrInvalidName, http.StatusUnprocessableEntity, codeInvalidName, false},
	{project.ErrInvalidLimit, http.StatusUnprocessableEntity, codeInvalidLimit, true},
	{project.ErrInvalidRole, http.StatusUnprocessableEntity, codeInvalidRole, false},
	{project.ErrUserNotFound, http.StatusUnprocessableEntity, codeUserNotFound, false},
	{project.ErrAlreadyMember, http.StatusConflict, codeAlreadyMember, false},
	{project.ErrNotMember, http.StatusNotFound, codeNotMember, false},
	{project.ErrLastAdmin, http.StatusConflict, codeLastAdmin, false},
	{auth.ErrUserNotFound, http.StatusNotFound, codeNotFound, false},
	{auth.ErrInvalidRole, http.StatusUnprocessableEntity, codeInvalidRole, false},
	{auth.ErrLastMaintainer, http.StatusConflict, codeLastMaintainer, false},
	{auth.ErrInvalidEmail, http.StatusUnprocessableEntity, codeInvalidEmail, false},
	{auth.ErrUsernameRequired, http.StatusUnprocessableEntity, codeInvalidUser, false},
	{auth.ErrUsernameTooLong, http.StatusUnprocessableEntity, codeInvalidUser, false},
	{auth.ErrUsernameTaken, http.StatusConflict, codeUsernameTaken, false},
	{auth.ErrEmailAmbiguous, http.StatusConflict, codeEmailAmbiguous, false},
	{auth.ErrUserDisabled, http.StatusConflict, codeUserDisabled, false},
	{auth.ErrPasswordAlreadySet, http.StatusConflict, codePasswordSet, false},
}

// classify turns a store error into its refusal, or a 500 for anything that is
// not one of the known sentinels.
func classify(err error) apiError {
	for _, known := range projectErrors {
		if !errors.Is(err, known.sentinel) {
			continue
		}
		refusal := apiError{status: known.status, code: known.code}
		if known.explain {
			refusal.message = explanation(err, known.sentinel)
		}
		return refusal
	}
	return apiError{status: http.StatusInternalServerError, code: codeInternal}
}

// explanation is err's text without the sentinel's own prefix, so
// "invalid project slug: \"Ab\" must be 1–40 lowercase letters…" reads as the
// rule alone.
func explanation(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

// writeStoreError writes the refusal err stands for, logging the unexpected
// ones (the client only learns "internal") with what was being done.
func writeStoreError(w http.ResponseWriter, err error, doing string) {
	refusal := classify(err)
	if refusal.status == http.StatusInternalServerError {
		log.Printf("projectapi: %s: %v", doing, err)
	}
	writeError(w, refusal.status, refusal.code, refusal.message)
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
		log.Printf("projectapi: writing response: %v", err)
	}
}

// decodeBody decodes the request body into v, refusing unknown fields and
// anything past maxBodyBytes, so a typo in a field name is an error rather than
// a silently ignored edit.
func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding the request body: %w", err)
	}
	return nil
}
