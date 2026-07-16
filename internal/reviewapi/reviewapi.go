// Package reviewapi exposes the review game over HTTP: GET /review/queue hands
// the player a batch of one-at-a-time questions targeted at the uncertainty
// band, POST /review/answer applies a yes/no/skip verdict through the existing
// write paths. Both endpoints require the editor or admin role — answering
// mutates the library — via the injected RequireWrite guard, so the package
// stays decoupled from auth's wiring.
package reviewapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/review"
)

// maxBodyBytes caps the answer request body; the payload is a question id and
// a one-word verdict, so 64 KiB is generous.
const maxBodyBytes = 64 << 10

// Service is the slice of *review.Service the handlers need; the indirection
// keeps handler tests store-free.
type Service interface {
	// Queue returns the user's next batch of questions plus session counters.
	Queue(ctx context.Context, userUID string, limit int) (review.QueueResult, error)
	// Answer applies one verdict and returns the outcome plus session counters.
	Answer(ctx context.Context, userUID, questionID string, answer review.Answer,
		meta audit.Meta) (review.AnswerResult, error)
}

// Config assembles the API: the review service and the editor/admin guard.
type Config struct {
	// Service builds queues and applies answers; nil makes the endpoints
	// answer 503 so a partially wired server still boots.
	Service Service
	// RequireWrite guards both endpoints; nil means no guard (tests only).
	RequireWrite func(http.Handler) http.Handler
}

// API carries the handlers' dependencies.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// NewAPI wires the review game endpoints from cfg.
func NewAPI(cfg Config) *API {
	guard := cfg.RequireWrite
	if guard == nil {
		guard = func(next http.Handler) http.Handler { return next }
	}
	return &API{service: cfg.Service, requireWrite: guard}
}

// RegisterRoutes mounts the review game endpoints on r (already scoped to
// /api/v1), both behind the write guard.
func (a *API) RegisterRoutes(r chi.Router) {
	guarded := r.With(a.requireWrite)
	guarded.Get("/review/queue", a.handleQueue)
	guarded.Post("/review/answer", a.handleAnswer)
}

// handleQueue answers GET /review/queue?limit=N with the next batch of
// questions for the authenticated user. A missing or zero limit uses the
// configured default; a malformed one answers 400.
func (a *API) handleQueue(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "review game not available")
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	res, err := a.service.Queue(r.Context(), user.UID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "review queue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "building review queue failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// answerInput is the JSON body of POST /review/answer.
type answerInput struct {
	// QuestionID is the id served by the queue endpoint.
	QuestionID string `json:"question_id"`
	// Answer is yes, no or skip.
	Answer string `json:"answer"`
}

// handleAnswer answers POST /review/answer by applying one verdict. A
// malformed body, unknown answer or unparseable question id answers 400; a
// question whose target vanished still answers 200 with result "gone" so the
// UI simply moves on.
func (a *API) handleAnswer(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "review game not available")
		return
	}
	in, err := decodeAnswer(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	meta := audit.FromRequest(r, user.UID)
	res, err := a.service.Answer(r.Context(), user.UID, in.QuestionID, review.Answer(in.Answer), meta)
	switch {
	case errors.Is(err, review.ErrInvalidQuestion), errors.Is(err, review.ErrInvalidAnswer):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		slog.ErrorContext(r.Context(), "review answer failed", "error", err)
		writeError(w, http.StatusInternalServerError, "applying answer failed")
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// decodeAnswer parses and validates the answer body.
func decodeAnswer(r *http.Request) (answerInput, error) {
	var in answerInput
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return answerInput{}, errors.New("malformed JSON body")
	}
	in.QuestionID = strings.TrimSpace(in.QuestionID)
	in.Answer = strings.TrimSpace(in.Answer)
	if in.QuestionID == "" {
		return answerInput{}, errors.New("question_id is required")
	}
	if in.Answer == "" {
		return answerInput{}, errors.New("answer is required")
	}
	return in, nil
}

// parseLimit parses the optional limit query parameter; empty means "use the
// configured default" (0), anything non-numeric or negative is a client error.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, errors.New("limit must be a non-negative integer")
	}
	return limit, nil
}

// writeJSON encodes v as the response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("reviewapi: encoding response", "error", err)
	}
}

// writeError answers with the package's {"error": …} body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
