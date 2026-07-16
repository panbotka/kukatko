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

// newServer mounts the API over the fake with a pass-through write guard.
func newServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Service: svc})
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
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
