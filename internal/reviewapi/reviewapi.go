// Package reviewapi exposes the review game over HTTP: GET /review/queue hands
// the player a batch of one-at-a-time questions targeted at the uncertainty
// band, POST /review/answer applies a yes/no/skip verdict through the existing
// write paths. Both endpoints require the editor or admin role — answering
// mutates the library — via the injected RequireWrite guard, so the package
// stays decoupled from auth's wiring. GET /review/leaderboard ranks players by
// how many decisions they have made; it only exposes aggregate counts, so it is
// gated by the lighter RequireAuth guard (any logged-in user).
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

// Leaderboarder aggregates review decisions per user for a window; a
// *review.LeaderboardStore satisfies it. The indirection keeps handler tests
// store-free.
type Leaderboarder interface {
	// Leaderboard returns the per-user decision tally for window, highest total
	// first.
	Leaderboard(ctx context.Context, window review.LeaderboardWindow) ([]review.LeaderboardEntry, error)
}

// Config assembles the API: the review service and its guards.
type Config struct {
	// Service builds queues and applies answers; nil makes those endpoints
	// answer 503 so a partially wired server still boots.
	Service Service
	// Leaderboard aggregates the decision counts; nil makes the leaderboard
	// endpoint answer 503.
	Leaderboard Leaderboarder
	// RequireWrite guards the mutating endpoints; nil means no guard (tests only).
	RequireWrite func(http.Handler) http.Handler
	// RequireAuth guards the read-only leaderboard endpoint; nil means no guard
	// (tests only).
	RequireAuth func(http.Handler) http.Handler
}

// API carries the handlers' dependencies.
type API struct {
	service      Service
	leaderboard  Leaderboarder
	requireWrite func(http.Handler) http.Handler
	requireAuth  func(http.Handler) http.Handler
}

// NewAPI wires the review game endpoints from cfg.
func NewAPI(cfg Config) *API {
	passthrough := func(next http.Handler) http.Handler { return next }
	write := cfg.RequireWrite
	if write == nil {
		write = passthrough
	}
	authn := cfg.RequireAuth
	if authn == nil {
		authn = passthrough
	}
	return &API{
		service:      cfg.Service,
		leaderboard:  cfg.Leaderboard,
		requireWrite: write,
		requireAuth:  authn,
	}
}

// RegisterRoutes mounts the review game endpoints on r (already scoped to
// /api/v1): the mutating queue/answer endpoints behind the write guard, the
// read-only leaderboard behind the lighter auth guard.
func (a *API) RegisterRoutes(r chi.Router) {
	guarded := r.With(a.requireWrite)
	guarded.Get("/review/queue", a.handleQueue)
	guarded.Post("/review/answer", a.handleAnswer)
	r.With(a.requireAuth).Get("/review/leaderboard", a.handleLeaderboard)
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

// leaderboardEntry is one board row in the response: a review.LeaderboardEntry
// plus an is_me flag so the frontend can highlight the caller's own row without
// re-matching on uid.
type leaderboardEntry struct {
	review.LeaderboardEntry
	// IsMe is true for the authenticated caller's own row.
	IsMe bool `json:"is_me"`
}

// leaderboardResponse is the body of GET /review/leaderboard: the window that
// was applied, the caller's uid, and the ordered board.
type leaderboardResponse struct {
	// Window is the applied window ("all", "7d" or "today").
	Window review.LeaderboardWindow `json:"window"`
	// CallerUID is the authenticated caller's uid, so the frontend can locate
	// its own row even if the caller has no entries yet.
	CallerUID string `json:"caller_uid"`
	// Entries is the ranked board, highest total first; never null.
	Entries []leaderboardEntry `json:"entries"`
}

// handleLeaderboard answers GET /review/leaderboard?window=all|7d|today with the
// per-user decision counts for the window. A missing window defaults to all-time;
// an unrecognised one answers 400.
func (a *API) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if a.leaderboard == nil {
		writeError(w, http.StatusServiceUnavailable, "review leaderboard not available")
		return
	}
	window, err := review.ParseWindow(r.URL.Query().Get("window"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := a.leaderboard.Leaderboard(r.Context(), window)
	if err != nil {
		slog.ErrorContext(r.Context(), "review leaderboard failed", "error", err)
		writeError(w, http.StatusInternalServerError, "building leaderboard failed")
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, buildLeaderboardResponse(window, entries, user.UID))
}

// buildLeaderboardResponse assembles the response body, stamping is_me on the
// caller's own row and guaranteeing a non-null entries array.
func buildLeaderboardResponse(
	window review.LeaderboardWindow, entries []review.LeaderboardEntry, callerUID string,
) leaderboardResponse {
	out := make([]leaderboardEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, leaderboardEntry{LeaderboardEntry: e, IsMe: e.UserUID == callerUID})
	}
	return leaderboardResponse{Window: window, CallerUID: callerUID, Entries: out}
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
