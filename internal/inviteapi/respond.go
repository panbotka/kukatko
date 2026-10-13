package inviteapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/panbotka/kukatko/internal/invite"
)

// maxBodyBytes caps a request body; an invite's fields are three short values.
const maxBodyBytes = 16 << 10

// The machine-readable error codes of the package, the "error" field of every
// refusal.
const (
	codeInvalidBody    = "invalid_body"
	codeInvalidRole    = "invalid_role"
	codeInvalidDays    = "invalid_days"
	codeInvalidMaxUses = "invalid_max_uses"
	codeNotFound       = "not_found"
	codeInternal       = "internal"
)

// errorBody is the wire shape of every refusal.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// storeErrors maps the store's sentinel errors onto refusals; the first match
// wins. ErrInvalid and ErrNotFound share one answer, so an unusable code and a
// foreign UID look like a missing one.
var storeErrors = []struct {
	sentinel error
	status   int
	code     string
	message  string
}{
	{invite.ErrInvalid, http.StatusNotFound, codeNotFound, ""},
	{invite.ErrNotFound, http.StatusNotFound, codeNotFound, ""},
	{invite.ErrInvalidRole, http.StatusUnprocessableEntity, codeInvalidRole, ""},
	{invite.ErrInvalidDays, http.StatusUnprocessableEntity, codeInvalidDays, invite.ErrInvalidDays.Error()},
	{invite.ErrInvalidMaxUses, http.StatusUnprocessableEntity, codeInvalidMaxUses, invite.ErrInvalidMaxUses.Error()},
}

// writeStoreError writes the refusal err stands for, or a logged 500 for
// anything that is not one of the known sentinels (the client only learns
// "internal").
func (a *API) writeStoreError(w http.ResponseWriter, r *http.Request, err error, doing string) {
	for _, known := range storeErrors {
		if errors.Is(err, known.sentinel) {
			writeError(w, known.status, known.code, known.message)
			return
		}
	}
	a.log.ErrorContext(r.Context(), "inviteapi: "+doing, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, codeInternal, "")
}

// writeError writes a refusal with its code and an optional message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

// writeJSON encodes v as the response body with status. Nothing here may be
// cached: an invite's state and code are live data.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Warn("inviteapi: writing response", slog.String("error", err.Error()))
	}
}

// decodeBody decodes the request body into v, refusing unknown fields and
// anything past maxBodyBytes.
func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding the request body: %w", err)
	}
	return nil
}
