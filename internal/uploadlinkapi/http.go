package uploadlinkapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// maxBodyBytes caps a management request body; a link's fields are short.
const maxBodyBytes = 64 << 10

// errorBody is the JSON body of a request-level error.
type errorBody struct {
	Error string `json:"error"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields, a
// trailing second value and an oversized body. The error is safe to show.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	if dec.More() {
		return errors.New("invalid request body: trailing data")
	}
	return nil
}

// writeJSON writes payload as a JSON response with status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Warn("uploadlinkapi: encoding response", slog.String("error", err.Error()))
	}
}

// writeError writes a request-level error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// serverError logs err and answers a bare 500, so nothing internal leaks.
func (a *API) serverError(w http.ResponseWriter, r *http.Request, doing string, err error) {
	a.log.ErrorContext(r.Context(), "uploadlinkapi: "+doing, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

// trimmed returns s without surrounding whitespace.
func trimmed(s string) string {
	return strings.TrimSpace(s)
}
