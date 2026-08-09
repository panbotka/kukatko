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
	queueRes    review.QueueResult
	queueErr    error
	queueLimit  int
	queueSource review.Source
	answerRes   review.AnswerResult
	answerErr   error
	questionID  string
	answer      review.Answer
}

// Queue records the source and limit and returns the scripted result.
func (f *fakeService) Queue(
	_ context.Context, _ string, src review.Source, limit int,
) (review.QueueResult, error) {
	f.queueSource = src
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

func TestHandleQueue_passesTheSourceThrough(t *testing.T) {
	t.Parallel()
	for query, want := range map[string]review.Source{
		"":               review.SourceBoth,
		"?source=both":   review.SourceBoth,
		"?source=people": review.SourcePeople,
		"?source=labels": review.SourceLabels,
	} {
		svc := &fakeService{}
		server := newServer(t, svc)
		if status := doJSON(t, http.MethodGet, server.URL+"/review/queue"+query, "", nil); status != http.StatusOK {
			t.Fatalf("%q status = %d, want 200", query, status)
		}
		if svc.queueSource != want {
			t.Errorf("%q passed source %q, want %q", query, svc.queueSource, want)
		}
	}
}

func TestHandleQueue_badSource(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{})
	for _, raw := range []string{"faces", "people,labels", "everything"} {
		status := doJSON(t, http.MethodGet, server.URL+"/review/queue?source="+raw, "", nil)
		if status != http.StatusBadRequest {
			t.Errorf("source=%q status = %d, want 400", raw, status)
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

// The round metadata, the breather cards, the answer reveal and the leaderboard
// streak all reach the client as extra fields on bodies that already existed.
// These tests read the raw JSON rather than decoding back into the Go types,
// because the thing that can break is the wire shape: a client that has never
// heard of rounds must still find `questions` where it always was.

func TestHandleQueue_carriesTheRoundAndItsBreathers(t *testing.T) {
	t.Parallel()
	svc := &fakeService{queueRes: review.QueueResult{
		Questions: []review.Question{{ID: "label:p1:l1", Kind: review.KindLabel}},
		Round: review.RoundInfo{
			Index: 2, Size: 10, Remaining: 1, Sure: 7, Band: 3, Entities: 6,
			Kinds: map[string]int{"face": 6, "label": 4}, Last: true,
		},
		Breathers: []review.Breather{{
			Kind: review.BreatherKind, Title: "Svatba", Year: 1998,
			Reason: review.BreatherReasonFavorite,
		}},
	}}
	server := newServer(t, svc)
	var got map[string]any
	if status := doJSON(t, http.MethodGet, server.URL+"/review/queue", "", &got); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	round, ok := got["round"].(map[string]any)
	if !ok {
		t.Fatalf("body has no round object: %+v", got)
	}
	for key, want := range map[string]float64{
		"index": 2, "size": 10, "remaining": 1, "sure": 7, "band": 3, "entities": 6,
	} {
		if round[key] != want {
			t.Errorf("round.%s = %v, want %v", key, round[key], want)
		}
	}
	if round["last"] != true {
		t.Errorf("round.last = %v, want true", round["last"])
	}
	breathers, ok := got["breathers"].([]any)
	if !ok || len(breathers) != 1 {
		t.Fatalf("body has no breathers array: %+v", got["breathers"])
	}
	card, _ := breathers[0].(map[string]any)
	if card["kind"] != review.BreatherKind || card["title"] != "Svatba" || card["year"] != float64(1998) {
		t.Errorf("breather = %+v, want the typed card with its title and year", card)
	}
	// The questions array is untouched, which is what keeps an older client working.
	if questions, ok := got["questions"].([]any); !ok || len(questions) != 1 {
		t.Errorf("questions = %+v, want the one scripted question", got["questions"])
	}
}

func TestHandleQueue_omitsBreathersWhenThereAreNone(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{})
	var got map[string]any
	if status := doJSON(t, http.MethodGet, server.URL+"/review/queue", "", &got); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := got["breathers"]; ok {
		t.Errorf("breathers = %v, want the field omitted entirely", got["breathers"])
	}
}

func TestHandleAnswer_carriesTheReveal(t *testing.T) {
	t.Parallel()
	svc := &fakeService{answerRes: review.AnswerResult{
		Result: "assigned", Answered: 3, Remaining: 9,
		Reveal: &review.Reveal{
			SubjectUID: "s1", Name: "Anna", PhotoCount: 42, OldestYear: 1961, NewestYear: 2019,
		},
	}}
	server := newServer(t, svc)
	var got map[string]any
	body := `{"question_id":"face:p1:0:s1","answer":"yes"}`
	if status := doJSON(t, http.MethodPost, server.URL+"/review/answer", body, &got); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	reveal, ok := got["reveal"].(map[string]any)
	if !ok {
		t.Fatalf("body has no reveal object: %+v", got)
	}
	if reveal["name"] != "Anna" || reveal["photo_count"] != float64(42) ||
		reveal["oldest_year"] != float64(1961) || reveal["newest_year"] != float64(2019) {
		t.Errorf("reveal = %+v, want Anna's numbers", reveal)
	}
}

func TestHandleAnswer_omitsTheRevealWhenThereIsNone(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{answerRes: review.AnswerResult{Result: "rejected"}})
	var got map[string]any
	body := `{"question_id":"face:p1:0:s1","answer":"no"}`
	if status := doJSON(t, http.MethodPost, server.URL+"/review/answer", body, &got); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := got["reveal"]; ok {
		t.Errorf("reveal = %v, want the field omitted entirely", got["reveal"])
	}
}

func TestHandleLeaderboard_carriesTheStreak(t *testing.T) {
	t.Parallel()
	lb := &fakeLeaderboard{entries: []review.LeaderboardEntry{
		{UserUID: "u1", DisplayName: "Alice", YesCount: 3, NoCount: 1, Total: 4, StreakDays: 5},
		{UserUID: "u2", DisplayName: "Bob", YesCount: 1, NoCount: 0, Total: 1},
	}}
	server := serveConfig(t, Config{Leaderboard: lb})
	var got map[string]any
	if status := doJSON(t, http.MethodGet, server.URL+"/review/leaderboard", "", &got); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	entries, ok := got["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries = %+v, want the two scripted rows", got["entries"])
	}
	first, _ := entries[0].(map[string]any)
	second, _ := entries[1].(map[string]any)
	if first["streak_days"] != float64(5) {
		t.Errorf("first row streak_days = %v, want 5", first["streak_days"])
	}
	// A player with no live run reports zero rather than nothing, so the client
	// never has to tell "no streak" from "the server does not know".
	if second["streak_days"] != float64(0) {
		t.Errorf("second row streak_days = %v, want 0", second["streak_days"])
	}
}
