package reviewapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/review"
)

// fakeService records calls and returns scripted results.
type fakeService struct {
	queueRes   review.QueueResult
	queueErr   error
	queueLimit int
	answerRes  review.AnswerResult
	answerErr  error
	questionID string
	answer     review.Answer
}

// Queue records the limit and returns the scripted result.
func (f *fakeService) Queue(_ context.Context, _ string, limit int) (review.QueueResult, error) {
	f.queueLimit = limit
	return f.queueRes, f.queueErr
}

// Answer records the inputs and returns the scripted result.
func (f *fakeService) Answer(
	_ context.Context, _ string, questionID string, answer review.Answer, _ audit.Meta,
) (review.AnswerResult, error) {
	f.questionID = questionID
	f.answer = answer
	return f.answerRes, f.answerErr
}

// fakeLeaderboard returns a scripted board and records the requested window.
type fakeLeaderboard struct {
	entries []review.LeaderboardEntry
	err     error
	window  review.LeaderboardWindow
}

// Leaderboard records the window and returns the scripted board.
func (f *fakeLeaderboard) Leaderboard(
	_ context.Context, window review.LeaderboardWindow,
) ([]review.LeaderboardEntry, error) {
	f.window = window
	return f.entries, f.err
}

// newServer mounts the API over the fake with a pass-through write guard.
func newServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	return serveConfig(t, Config{Service: svc})
}

// serveConfig mounts an API built from cfg and returns a running test server.
func serveConfig(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	api := NewAPI(cfg)
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

// leaderboardBody mirrors the leaderboard response for decoding in tests.
type leaderboardBody struct {
	Window    string `json:"window"`
	CallerUID string `json:"caller_uid"`
	Entries   []struct {
		UserUID     string `json:"user_uid"`
		DisplayName string `json:"display_name"`
		YesCount    int    `json:"yes_count"`
		NoCount     int    `json:"no_count"`
		Total       int    `json:"total"`
		IsMe        bool   `json:"is_me"`
	} `json:"entries"`
}

// doJSON runs one request and decodes the JSON response body into out.
func doJSON(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
	}
	return resp.StatusCode
}

func TestHandleQueue_returnsBatch(t *testing.T) {
	t.Parallel()
	svc := &fakeService{queueRes: review.QueueResult{
		Questions: []review.Question{{ID: "label:p1:l1", Kind: review.KindLabel, Confidence: 0.6}},
		Remaining: 5,
	}}
	server := newServer(t, svc)
	var got review.QueueResult
	status := doJSON(t, http.MethodGet, server.URL+"/review/queue?limit=7", "", &got)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if svc.queueLimit != 7 {
		t.Errorf("limit passed = %d, want 7", svc.queueLimit)
	}
	if len(got.Questions) != 1 || got.Questions[0].ID != "label:p1:l1" || got.Remaining != 5 {
		t.Errorf("body = %+v, want the scripted queue", got)
	}
}

func TestHandleQueue_badLimit(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{})
	for _, raw := range []string{"abc", "-1", "1.5"} {
		if status := doJSON(t, http.MethodGet, server.URL+"/review/queue?limit="+raw, "", nil); status != http.StatusBadRequest {
			t.Errorf("limit=%q status = %d, want 400", raw, status)
		}
	}
}

func TestHandleAnswer_appliesAnswer(t *testing.T) {
	t.Parallel()
	svc := &fakeService{answerRes: review.AnswerResult{Result: "assigned", Answered: 3, Remaining: 9}}
	server := newServer(t, svc)
	var got review.AnswerResult
	body := `{"question_id":"face:p1:0:s1","answer":"yes"}`
	status := doJSON(t, http.MethodPost, server.URL+"/review/answer", body, &got)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if svc.questionID != "face:p1:0:s1" || svc.answer != review.AnswerYes {
		t.Errorf("service got (%q, %q), want the decoded body", svc.questionID, svc.answer)
	}
	if got.Result != "assigned" || got.Answered != 3 || got.Remaining != 9 {
		t.Errorf("body = %+v, want the scripted result", got)
	}
}

func TestHandleAnswer_badBodies(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{})
	tests := []struct {
		name, body string
	}{
		{"malformed JSON", "{"},
		{"missing question id", `{"answer":"yes"}`},
		{"missing answer", `{"question_id":"label:p1:l1"}`},
		{"unknown field", `{"question_id":"label:p1:l1","answer":"yes","x":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if status := doJSON(t, http.MethodPost, server.URL+"/review/answer", tt.body, nil); status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", status)
			}
		})
	}
}

func TestHandleAnswer_serviceErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid question", review.ErrInvalidQuestion, http.StatusBadRequest},
		{"invalid answer", review.ErrInvalidAnswer, http.StatusBadRequest},
		{"internal failure", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newServer(t, &fakeService{answerErr: tt.err})
			body := `{"question_id":"label:p1:l1","answer":"no"}`
			if status := doJSON(t, http.MethodPost, server.URL+"/review/answer", body, nil); status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
		})
	}
}

func TestEndpoints_nilServiceUnavailable(t *testing.T) {
	t.Parallel()
	server := newServer(t, nil)
	if status := doJSON(t, http.MethodGet, server.URL+"/review/queue", "", nil); status != http.StatusServiceUnavailable {
		t.Errorf("queue status = %d, want 503", status)
	}
	body := `{"question_id":"label:p1:l1","answer":"no"}`
	if status := doJSON(t, http.MethodPost, server.URL+"/review/answer", body, nil); status != http.StatusServiceUnavailable {
		t.Errorf("answer status = %d, want 503", status)
	}
}

func TestBuildLeaderboardResponse_flagsCaller(t *testing.T) {
	t.Parallel()
	entries := []review.LeaderboardEntry{
		{UserUID: "u1", DisplayName: "Alice", YesCount: 3, NoCount: 1, Total: 4},
		{UserUID: "u2", DisplayName: "Bob", YesCount: 2, NoCount: 0, Total: 2},
	}
	resp := buildLeaderboardResponse(review.WindowWeek, entries, "u2")
	if resp.Window != review.WindowWeek || resp.CallerUID != "u2" {
		t.Fatalf("resp window/caller = %q/%q, want 7d/u2", resp.Window, resp.CallerUID)
	}
	if len(resp.Entries) != 2 || resp.Entries[0].IsMe || !resp.Entries[1].IsMe {
		t.Errorf("is_me flags = %v/%v, want only u2's row flagged",
			resp.Entries[0].IsMe, resp.Entries[1].IsMe)
	}
	// A caller with no rows still yields a non-null (empty) entries array.
	empty := buildLeaderboardResponse(review.WindowAllTime, nil, "u9")
	if empty.Entries == nil {
		t.Error("entries = nil, want a non-null empty slice")
	}
}

func TestHandleLeaderboard_returnsBoard(t *testing.T) {
	t.Parallel()
	lb := &fakeLeaderboard{entries: []review.LeaderboardEntry{
		{UserUID: "u1", DisplayName: "Alice", YesCount: 3, NoCount: 1, Total: 4},
	}}
	server := serveConfig(t, Config{Leaderboard: lb})
	var got leaderboardBody
	status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard", "", &got)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if lb.window != review.WindowAllTime {
		t.Errorf("window passed = %q, want all (default)", lb.window)
	}
	if got.Window != "all" || len(got.Entries) != 1 || got.Entries[0].Total != 4 {
		t.Errorf("body = %+v, want the scripted board with window all", got)
	}
}

func TestHandleLeaderboard_windowParam(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want review.LeaderboardWindow
	}{
		{"7d", review.WindowWeek},
		{"today", review.WindowToday},
		{"all", review.WindowAllTime},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			lb := &fakeLeaderboard{}
			server := serveConfig(t, Config{Leaderboard: lb})
			status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard?window="+tt.raw, "", nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if lb.window != tt.want {
				t.Errorf("window passed = %q, want %q", lb.window, tt.want)
			}
		})
	}
}

func TestHandleLeaderboard_badWindow(t *testing.T) {
	t.Parallel()
	server := serveConfig(t, Config{Leaderboard: &fakeLeaderboard{}})
	if status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard?window=month", "", nil); status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestHandleLeaderboard_serviceError(t *testing.T) {
	t.Parallel()
	server := serveConfig(t, Config{Leaderboard: &fakeLeaderboard{err: context.DeadlineExceeded}})
	if status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard", "", nil); status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
}

func TestHandleLeaderboard_nilUnavailable(t *testing.T) {
	t.Parallel()
	server := serveConfig(t, Config{})
	if status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard", "", nil); status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
}

func TestHandleLeaderboard_authGuard(t *testing.T) {
	t.Parallel()
	deny := func(status int) func(http.Handler) http.Handler {
		return func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})
		}
	}
	// An unauthenticated request is rejected by the auth guard (401).
	server := serveConfig(t, Config{Leaderboard: &fakeLeaderboard{}, RequireAuth: deny(http.StatusUnauthorized)})
	if status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard", "", nil); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", status)
	}
	// A viewer (no write permission) still reads the board: it is gated by auth,
	// not the write guard, so a denying write guard does not block it.
	viewer := serveConfig(t, Config{
		Leaderboard:  &fakeLeaderboard{},
		RequireWrite: deny(http.StatusForbidden),
	})
	if status := doJSON(t, http.MethodGet, viewer.URL+"/review/leaderboard", "", nil); status != http.StatusOK {
		t.Errorf("viewer status = %d, want 200", status)
	}
}

func TestRegisterRoutes_writeGuardApplied(t *testing.T) {
	t.Parallel()
	guarded := 0
	deny := func(http.Handler) http.Handler {
		guarded++
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}
	api := NewAPI(Config{Service: &fakeService{}, RequireWrite: deny})
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	if status := doJSON(t, http.MethodGet, server.URL+"/review/queue", "", nil); status != http.StatusForbidden {
		t.Errorf("guarded queue status = %d, want 403", status)
	}
	if guarded == 0 {
		t.Error("RequireWrite middleware was never applied")
	}
}
