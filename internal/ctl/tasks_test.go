package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// taskBody is a realistic single-task payload, as the server serves one.
const taskBody = `{"uid":"tk1","title":"V kterém roce se přestavoval dům?","body":"Tři fotky.",
"state":"question","resolution":"","query":"camera:Olympus","created_by":"us1",
"created_by_name":"Pan Botka","created_at":"2026-09-18T10:00:00Z","updated_at":"2026-09-18T10:00:00Z",
"state_at":"2026-09-18T10:00:00Z","photo_count":3,"cover_photo_uid":"ph1","comment_count":2,
"last_comment_at":"2026-09-18T12:00:00Z","has_new_answer":true,
"last_activity_at":"2026-09-18T12:00:00Z","last_activity_by":"us2","last_activity_by_name":"Tomáš Kozák",
"waiting_on_me":true}`

// taskListBody is a one-task page with more behind it, so the summary has a next
// offset to print.
var taskListBody = `{"tasks":[` + taskBody + `],"total":3,"limit":1,"offset":0}`

// TestListTasks_query verifies every filter reaches the wire, and that the zero
// options send no parameters at all.
func TestListTasks_query(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(taskListBody))
	})
	_, err := client.ListTasks(t.Context(), TaskListOptions{
		States: []string{"question", "review"}, Answered: true, Waiting: true, Search: "dům",
		PhotoUID: "ph1", Limit: 10, Offset: 20,
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotPath != "/api/v1/tasks" {
		t.Errorf("path = %q, want /api/v1/tasks", gotPath)
	}
	for _, want := range []string{
		"state=question%2Creview", "answered=true", "waiting=true", "q=d%C5%AFm", "photo=ph1",
		"limit=10", "offset=20",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q does not contain %q", gotQuery, want)
		}
	}

	if _, err := client.ListTasks(t.Context(), TaskListOptions{}); err != nil {
		t.Fatalf("ListTasks(zero): %v", err)
	}
	if gotQuery != "" {
		t.Errorf("the zero options sent %q, want no parameters", gotQuery)
	}
}

// TestListTasks_openShorthand verifies the open flag is sent as its own
// parameter rather than expanded into a state list client-side.
func TestListTasks_openShorthand(t *testing.T) {
	t.Parallel()

	var gotQuery string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(taskListBody))
	})
	if _, err := client.ListTasks(t.Context(), TaskListOptions{Open: true}); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotQuery != "open=true" {
		t.Errorf("query = %q, want open=true", gotQuery)
	}
}

// TestListTasks_negativePaging verifies bad paging costs no round trip.
func TestListTasks_negativePaging(t *testing.T) {
	t.Parallel()

	client := testClient(t, "tok", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid paging")
	})
	if _, err := client.ListTasks(t.Context(), TaskListOptions{Limit: -1}); !errors.Is(
		err, ErrInvalidPaging,
	) {
		t.Errorf("error = %v, want ErrInvalidPaging", err)
	}
}

// TestCreateTask verifies the body sent and the client-side refusal of a task
// with no question.
func TestCreateTask(t *testing.T) {
	t.Parallel()

	var gotBody map[string]json.RawMessage
	var gotMethod string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(taskBody))
	})
	_, err := client.CreateTask(t.Context(), TaskInput{
		Title: "Kdy?", Query: "camera:Olympus", PhotoUIDs: []string{"ph1", "ph2"},
		Options: []string{"1936", "1938"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if string(gotBody["title"]) != `"Kdy?"` {
		t.Errorf("title = %s", gotBody["title"])
	}
	if string(gotBody["photo_uids"]) != `["ph1","ph2"]` {
		t.Errorf("photo_uids = %s", gotBody["photo_uids"])
	}
	if _, ok := gotBody["body"]; ok {
		t.Error("an empty body was sent, want it omitted")
	}
	if string(gotBody["options"]) != `["1936","1938"]` {
		t.Errorf("options = %s", gotBody["options"])
	}
}

// TestCreateTask_noTitle verifies a task with no question is refused before the
// server is contacted.
func TestCreateTask_noTitle(t *testing.T) {
	t.Parallel()

	client := testClient(t, "tok", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite an empty title")
	})
	if _, err := client.CreateTask(t.Context(), TaskInput{Title: "  "}); !errors.Is(
		err, ErrEmptyTaskTitle,
	) {
		t.Errorf("error = %v, want ErrEmptyTaskTitle", err)
	}
}

// TestUpdateTask verifies a partial update sends only the fields it names, and
// that an update naming none is refused without a read first.
func TestUpdateTask(t *testing.T) {
	t.Parallel()

	var gotBody map[string]json.RawMessage
	var methods []string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(taskBody))
	})
	state, resolution := "done", "1987 podle kroniky"
	if _, err := client.UpdateTask(t.Context(), "tk1", TaskUpdate{
		State: &state, Resolution: &resolution,
	}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if len(methods) != 1 || methods[0] != http.MethodPatch {
		t.Errorf("methods = %v, want one PATCH and no read first", methods)
	}
	if _, ok := gotBody["title"]; ok {
		t.Error("an untouched field was sent, want it omitted")
	}
	if string(gotBody["state"]) != `"done"` {
		t.Errorf("state = %s", gotBody["state"])
	}

	if _, err := client.UpdateTask(t.Context(), "tk1", TaskUpdate{}); !errors.Is(
		err, ErrNoTaskEdits,
	) {
		t.Errorf("empty update error = %v, want ErrNoTaskEdits", err)
	}

	// Clearing the options is an edit in its own right, and it must reach the
	// wire as an empty array rather than be dropped as "nothing to send".
	if _, err := client.UpdateTask(t.Context(), "tk1", TaskUpdate{Options: &[]string{}}); err != nil {
		t.Fatalf("clearing options: %v", err)
	}
	if string(gotBody["options"]) != `[]` {
		t.Errorf("cleared options = %s, want []", gotBody["options"])
	}
}

// TestTaskMembership verifies the two membership calls differ only in the verb,
// and that an empty list is refused client-side.
func TestTaskMembership(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotPath string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"changed":1,"task":` + taskBody + `}`))
	})
	if _, err := client.AddTaskPhotos(t.Context(), "tk1", []string{"ph1"}); err != nil {
		t.Fatalf("AddTaskPhotos: %v", err)
	}
	if _, err := client.RemoveTaskPhotos(t.Context(), "tk1", []string{"ph1"}); err != nil {
		t.Fatalf("RemoveTaskPhotos: %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Errorf("methods = %v, want POST then DELETE", methods)
	}
	if gotPath != "/api/v1/tasks/tk1/photos" {
		t.Errorf("path = %q", gotPath)
	}
	if _, err := client.AddTaskPhotos(t.Context(), "tk1", nil); !errors.Is(err, ErrNoTaskPhotos) {
		t.Errorf("empty membership error = %v, want ErrNoTaskPhotos", err)
	}
}

// TestTaskUIDRequired verifies every uid-taking call refuses an empty one.
func TestTaskUIDRequired(t *testing.T) {
	t.Parallel()

	client := testClient(t, "tok", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite an empty uid")
	})
	if _, err := client.GetTask(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("GetTask error = %v, want ErrEmptyUID", err)
	}
	if err := client.DeleteTask(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("DeleteTask error = %v, want ErrEmptyUID", err)
	}
	if _, err := client.ListTaskComments(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("ListTaskComments error = %v, want ErrEmptyUID", err)
	}
}

// TestAddTaskComment verifies the thread path and the client-side body checks.
func TestAddTaskComment(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"uid":"cm1","task_uid":"tk1","body":"1987","created_at":"2026-09-18T12:00:00Z"}`))
	})
	raw, err := client.AddTaskComment(t.Context(), "tk1", "  1987  ")
	if err != nil {
		t.Fatalf("AddTaskComment: %v", err)
	}
	if gotPath != "/api/v1/tasks/tk1/comments" {
		t.Errorf("path = %q", gotPath)
	}
	comment, err := DecodeComment(raw)
	if err != nil {
		t.Fatalf("DecodeComment: %v", err)
	}
	if comment.TaskUID != "tk1" || comment.PhotoUID != "" {
		t.Errorf("comment = %+v, want it hung off the task alone", comment)
	}
	if _, err := client.AddTaskComment(t.Context(), "tk1", "   "); !errors.Is(err, ErrEmptyComment) {
		t.Errorf("empty body error = %v, want ErrEmptyComment", err)
	}
	if _, err := client.AddTaskComment(t.Context(), "tk1",
		strings.Repeat("a", MaxCommentLen+1)); !errors.Is(err, ErrCommentTooLong) {
		t.Errorf("over-long body error = %v, want ErrCommentTooLong", err)
	}
}

// TestWriteTaskPage verifies the listing table, its paging summary and the empty
// case.
func TestWriteTaskPage(t *testing.T) {
	t.Parallel()

	page, err := DecodeTaskPage(json.RawMessage(taskListBody))
	if err != nil {
		t.Fatalf("DecodeTaskPage: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteTaskPage(&buf, page); err != nil {
		t.Fatalf("WriteTaskPage: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"UID", "QUESTION", "NEW", "tk1", "question", "yes",
		"1 of 3 tasks", "next offset 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}

	buf.Reset()
	if err := WriteTaskPage(&buf, TaskPage{}); err != nil {
		t.Fatalf("WriteTaskPage(empty): %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no tasks found" {
		t.Errorf("empty output = %q", buf.String())
	}
}

// TestWriteTask verifies the detail view, and that the closing block appears only
// once a task is closed.
func TestWriteTask(t *testing.T) {
	t.Parallel()

	task, err := DecodeTask(json.RawMessage(taskBody))
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteTask(&buf, task); err != nil {
		t.Fatalf("WriteTask: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"tk1", "V kterém roce", "question", "Pan Botka", "Tři fotky.",
		"Last activity   2026-09-18 12:00 by Tomáš Kozák", "Waiting on you  yes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Resolution") {
		t.Error("an open task printed a resolution")
	}
	if strings.Contains(got, "Options") {
		t.Error("a task with no options printed an Options line")
	}

	withOptions := task
	withOptions.Options = []string{"1936", "1938", "nevím"}
	buf.Reset()
	if err := WriteTask(&buf, withOptions); err != nil {
		t.Fatalf("WriteTask(options): %v", err)
	}
	if !strings.Contains(buf.String(), "Options         1936 · 1938 · nevím") {
		t.Errorf("options line missing:\n%s", buf.String())
	}

	closed := task
	closed.State = "done"
	closed.Resolution = "1987"
	closed.ClosedAt = &task.CreatedAt
	buf.Reset()
	if err := WriteTask(&buf, closed); err != nil {
		t.Fatalf("WriteTask(closed): %v", err)
	}
	if !strings.Contains(buf.String(), "Resolution") {
		t.Errorf("a closed task printed no resolution:\n%s", buf.String())
	}
}

// TestWriteTaskMembership verifies the one-line answer to a membership change.
func TestWriteTaskMembership(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteTaskMembership(&buf, TaskMembership{
		Changed: 1, Task: Task{PhotoCount: 4},
	}); err != nil {
		t.Fatalf("WriteTaskMembership: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "1 photo changed · 4 in the task" {
		t.Errorf("output = %q", got)
	}
}

// TestDecodeTask_malformed verifies a broken payload reports where it broke.
func TestDecodeTask_malformed(t *testing.T) {
	t.Parallel()

	if _, err := DecodeTask(json.RawMessage(`{`)); err == nil {
		t.Error("DecodeTask accepted malformed JSON")
	}
	if _, err := DecodeTaskPage(json.RawMessage(`{`)); err == nil {
		t.Error("DecodeTaskPage accepted malformed JSON")
	}
}

// TestLastActivity verifies the one line `tasks show` prints about whose move it
// is, and its fallbacks when the name or the whole actor is unknown.
func TestLastActivity(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 19, 17, 21, 0, 0, time.UTC)
	tests := []struct {
		name string
		task Task
		want string
	}{
		{name: "named", task: Task{LastActivityAt: at, LastActivityBy: "us2", LastActivityByName: "Tomáš Kozák"},
			want: "2026-09-19 17:21 by Tomáš Kozák"},
		{name: "uid only", task: Task{LastActivityAt: at, LastActivityBy: "us2"}, want: "2026-09-19 17:21 by us2"},
		{name: "account gone", task: Task{LastActivityAt: at}, want: "2026-09-19 17:21"},
		{name: "old server", task: Task{}, want: "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := lastActivity(tt.task); got != tt.want {
				t.Errorf("lastActivity() = %q, want %q", got, tt.want)
			}
		})
	}
}
