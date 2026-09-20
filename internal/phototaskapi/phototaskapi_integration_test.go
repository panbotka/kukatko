//go:build integration

package phototaskapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/phototaskapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and task APIs behind an httptest server over the
// integration database.
type env struct {
	db      *database.DB
	server  *httptest.Server
	authSvc *auth.Service
	photos  *photos.Store
	// uids remembers the uid of every account login created, by username, so a
	// test can put one person on a task by the other's hand.
	uids map[string]string
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := phototaskapi.NewAPI(phototaskapi.Config{
		Store:        phototask.NewStore(db.Pool()),
		Comments:     comments.NewStore(db.Pool()),
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{db: db, server: server, authSvc: authSvc, photos: photos.NewStore(db.Pool()),
		uids: make(map[string]string)}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	created, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	e.uids[username] = created.UID
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// mustDo issues a request with an optional JSON body and returns the response.
func (e *env) mustDo(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// do issues a request and decodes a JSON response into out, asserting the status.
func (e *env) do(
	t *testing.T, c *http.Client, method, path string, body []byte, wantStatus int, out any,
) {
	t.Helper()
	resp := e.mustDo(t, c, method, path, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s status = %d, want %d (%s)", method, path, resp.StatusCode, wantStatus,
			strings.TrimSpace(string(payload)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decoding %s %s: %v", method, path, err)
		}
	}
}

// makePhoto catalogues a photo so a task has something to be about.
func (e *env) makePhoto(t *testing.T, name string) photos.Photo {
	t.Helper()
	created, err := e.photos.Create(context.Background(), photos.Photo{
		FileHash: (name + strings.Repeat("0", 64))[:64],
		FilePath: "2026/09/" + name + ".jpg",
		FileName: name + ".jpg",
		FileSize: 1024,
		FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", name, err)
	}
	return created
}

// openTask opens a task as the given client and returns it.
func (e *env) openTask(t *testing.T, c *http.Client, title string, photoUIDs ...string) phototask.Task {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title": title, "body": "kontext", "query": "camera:Olympus", "photo_uids": photoUIDs,
	})
	var task phototask.Task
	e.do(t, c, http.MethodPost, "/api/v1/tasks", body, http.StatusCreated, &task)
	return task
}

// TestTaskLifecycle walks a task from the question to the closed record: open it,
// add a photograph, advance it, refuse to close it silently, then close it.
func TestTaskLifecycle(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	one := e.makePhoto(t, "a")
	two := e.makePhoto(t, "b")

	task := e.openTask(t, editor, "V kterém roce se přestavoval dům?", one.UID)
	if task.State != phototask.StateQuestion || task.PhotoCount != 1 {
		t.Fatalf("new task = %+v, want one photo and the question state", task)
	}

	var added struct {
		Changed int            `json:"changed"`
		Task    phototask.Task `json:"task"`
	}
	body, _ := json.Marshal(map[string]any{"photo_uids": []string{two.UID}})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+task.UID+"/photos", body, http.StatusOK, &added)
	if added.Changed != 1 || added.Task.PhotoCount != 2 {
		t.Errorf("after adding: changed = %d, photo_count = %d", added.Changed, added.Task.PhotoCount)
	}

	silent, _ := json.Marshal(map[string]any{"state": "done"})
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, silent, http.StatusBadRequest, nil)

	closing, _ := json.Marshal(map[string]any{"state": "done", "resolution": "1987 podle kroniky"})
	var closed phototask.Task
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, closing, http.StatusOK, &closed)
	if closed.ClosedAt == nil || closed.Resolution == "" {
		t.Errorf("closed task = %+v, want a closing stamp and a resolution", closed)
	}

	e.do(t, editor, http.MethodDelete, "/api/v1/tasks/"+task.UID, nil, http.StatusNoContent, nil)
	e.do(t, editor, http.MethodGet, "/api/v1/tasks/"+task.UID, nil, http.StatusNotFound, nil)
}

// TestTaskOptions walks the answer options through the API: opened with a set,
// read back on the listing and the detail, replaced and cleared by PATCH, and
// refused with a 400 that names the rule when the set is malformed.
func TestTaskOptions(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)

	body, _ := json.Marshal(map[string]any{
		"title": "Je na nápisu rok 1936, nebo 1938?", "options": []string{" 1936", "1938", "nevím"},
	})
	var task phototask.Task
	e.do(t, editor, http.MethodPost, "/api/v1/tasks", body, http.StatusCreated, &task)
	if !slices.Equal(task.Options, []string{"1936", "1938", "nevím"}) {
		t.Fatalf("options after create = %q", task.Options)
	}

	var page struct {
		Tasks []phototask.Task `json:"tasks"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks", nil, http.StatusOK, &page)
	if len(page.Tasks) != 1 || !slices.Equal(page.Tasks[0].Options, task.Options) {
		t.Errorf("listing options = %v, want %q", page.Tasks, task.Options)
	}

	for name, bad := range map[string][]string{
		"too many": {"a", "b", "c", "d", "e", "f"},
		"blank":    {"a", " "},
		"too long": {strings.Repeat("x", phototask.MaxOptionLen+1)},
		"repeated": {"1936", "1936"},
	} {
		patch, _ := json.Marshal(map[string]any{"options": bad})
		resp := e.mustDo(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, patch)
		payload, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(payload), "option") {
			t.Errorf("%s: status = %d, body = %s; want 400 naming the option", name, resp.StatusCode, payload)
		}
	}

	untouched, _ := json.Marshal(map[string]any{"body": "jiný kontext"})
	var edited phototask.Task
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, untouched, http.StatusOK, &edited)
	if !slices.Equal(edited.Options, task.Options) {
		t.Errorf("an edit naming no options changed them to %q", edited.Options)
	}

	cleared, _ := json.Marshal(map[string]any{"options": []string{}})
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, cleared, http.StatusOK, &edited)
	if edited.Options == nil || len(edited.Options) != 0 {
		t.Errorf("options after clearing = %v, want an empty array", edited.Options)
	}
	// The wire must say [] rather than null: a client is promised an array.
	resp := e.mustDo(t, editor, http.MethodGet, "/api/v1/tasks/"+task.UID, nil)
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(raw), `"options":[]`) {
		t.Errorf("detail payload lacks an empty options array: %s", raw)
	}
}

// TestViewerMayAnswerButNotCurate is the point of the feature: the person who
// knows the answer signs in as a viewer, reads the question and replies — and
// cannot open, edit, close or delete a task.
func TestViewerMayAnswerButNotCurate(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)
	task := e.openTask(t, editor, "V kterém roce?")

	// Reading is open to them.
	e.do(t, viewer, http.MethodGet, "/api/v1/tasks/"+task.UID, nil, http.StatusOK, nil)
	e.do(t, viewer, http.MethodGet, "/api/v1/tasks", nil, http.StatusOK, nil)

	// Answering is open to them.
	answer, _ := json.Marshal(map[string]string{"body": "Bylo to v roce 1987."})
	var created comments.Comment
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", answer,
		http.StatusCreated, &created)
	if created.TaskUID != task.UID || created.PhotoUID != "" {
		t.Errorf("comment = %+v, want it hung off the task alone", created)
	}

	// Curating is not.
	open, _ := json.Marshal(map[string]any{"title": "Vlastní úkol"})
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks", open, http.StatusForbidden, nil)
	patch, _ := json.Marshal(map[string]any{"title": "Přepsáno"})
	e.do(t, viewer, http.MethodPatch, "/api/v1/tasks/"+task.UID, patch, http.StatusForbidden, nil)
	e.do(t, viewer, http.MethodDelete, "/api/v1/tasks/"+task.UID, nil, http.StatusForbidden, nil)

	// The answer is visible to everybody, and marks the task as replied to.
	var thread struct {
		Comments []comments.Comment `json:"comments"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks/"+task.UID+"/comments", nil, http.StatusOK, &thread)
	if len(thread.Comments) != 1 || thread.Comments[0].AuthorName == "" {
		t.Errorf("thread = %+v, want one comment with its author resolved", thread.Comments)
	}

	var listed struct {
		Tasks []phototask.Task `json:"tasks"`
		Total int              `json:"total"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?answered=true", nil, http.StatusOK, &listed)
	if listed.Total != 1 || !listed.Tasks[0].HasNewAnswer {
		t.Errorf("answered listing = %+v, want the replied-to task flagged", listed)
	}
}

// TestCommentScoping verifies a comment can only be reached through the task it
// belongs to, and that only its author may rewrite it.
func TestCommentScoping(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)
	task := e.openTask(t, editor, "První")
	other := e.openTask(t, editor, "Druhý")

	answer, _ := json.Marshal(map[string]string{"body": "1987"})
	var created comments.Comment
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", answer,
		http.StatusCreated, &created)

	edit, _ := json.Marshal(map[string]string{"body": "Vlastně 1988"})
	e.do(t, viewer, http.MethodPatch,
		"/api/v1/tasks/"+other.UID+"/comments/"+created.UID, edit, http.StatusNotFound, nil)
	e.do(t, editor, http.MethodPatch,
		"/api/v1/tasks/"+task.UID+"/comments/"+created.UID, edit, http.StatusForbidden, nil)
	e.do(t, viewer, http.MethodPatch,
		"/api/v1/tasks/"+task.UID+"/comments/"+created.UID, edit, http.StatusOK, nil)
	e.do(t, viewer, http.MethodDelete,
		"/api/v1/tasks/"+task.UID+"/comments/"+created.UID, nil, http.StatusNoContent, nil)
}

// TestListFiltering verifies the listing's parameters, including the refusal of
// a state nobody has heard of — a typo must say so, not look like "no matches".
func TestListFiltering(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	e.openTask(t, editor, "Otevřená")
	closed := e.openTask(t, editor, "Zavřená")
	closing, _ := json.Marshal(map[string]any{"state": "rejected", "resolution": "nedohledatelné"})
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+closed.UID, closing, http.StatusOK, nil)

	var all struct {
		Tasks  []phototask.Task `json:"tasks"`
		Total  int              `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks", nil, http.StatusOK, &all)
	if all.Total != 2 || all.Limit != phototask.DefaultLimit {
		t.Errorf("listing = %+v, want both tasks and the default page size echoed", all)
	}

	var open struct {
		Total int `json:"total"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?open=true", nil, http.StatusOK, &open)
	if open.Total != 1 {
		t.Errorf("open listing total = %d, want 1", open.Total)
	}

	var byState struct {
		Total int `json:"total"`
	}
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?state=rejected", nil, http.StatusOK, &byState)
	if byState.Total != 1 {
		t.Errorf("rejected listing total = %d, want 1", byState.Total)
	}

	e.do(t, editor, http.MethodGet, "/api/v1/tasks?state=parked", nil, http.StatusBadRequest, nil)
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?limit=-1", nil, http.StatusBadRequest, nil)
}

// TestAnonymousIsRefused verifies nothing is readable without signing in — the
// link is sent to somebody who has an account, not published.
func TestAnonymousIsRefused(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	task := e.openTask(t, editor, "V kterém roce?")

	anon := &http.Client{}
	for _, path := range []string{"/api/v1/tasks", "/api/v1/tasks/" + task.UID,
		"/api/v1/tasks/" + task.UID + "/comments"} {
		resp := e.mustDo(t, anon, http.MethodGet, path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s as anonymous = %d, want 401", path, resp.StatusCode)
		}
	}
}

// TestWhoseTurn walks the loop the queue exists for over HTTP: the editor opens a
// question and asks the viewer, the viewer answers, the editor moves on — and at
// every step `waiting=1`, `waiting_on_me`, the caller-relative `has_new_answer`
// and the last-activity fields say whose move it is, each as its own reader.
func TestWhoseTurn(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)
	task := e.openTask(t, editor, "V kterém roce?")

	type page struct {
		Tasks []phototask.Task `json:"tasks"`
		Total int              `json:"total"`
	}
	waitingOn := func(c *http.Client) page {
		t.Helper()
		var got page
		e.do(t, c, http.MethodGet, "/api/v1/tasks?waiting=1", nil, http.StatusOK, &got)
		return got
	}
	detail := func(c *http.Client) phototask.Task {
		t.Helper()
		var got phototask.Task
		e.do(t, c, http.MethodGet, "/api/v1/tasks/"+task.UID, nil, http.StatusOK, &got)
		return got
	}

	// Asked and untouched: the viewer's move, not the editor's.
	ask, _ := json.Marshal(map[string]string{"user_uid": e.uids["pametnik"]})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+task.UID+"/participants", ask, http.StatusOK, nil)
	if got := waitingOn(viewer); got.Total != 1 || !got.Tasks[0].WaitingOnMe {
		t.Errorf("waiting=1 for the person asked = %+v, want the task", got)
	}
	if got := waitingOn(editor); got.Total != 0 {
		t.Errorf("waiting=1 for the asker = %+v, want nothing", got)
	}
	asked := detail(viewer)
	if asked.LastActivityByUID != e.uids["editor"] || asked.LastActivityByName != "editor" ||
		asked.LastActivityAt.IsZero() {
		t.Errorf("last activity = %q (%q) at %v, want the editor's opening",
			asked.LastActivityByUID, asked.LastActivityByName, asked.LastActivityAt)
	}

	// The viewer answers: the editor's move now, and an answer for the editor
	// alone — the viewer sees no new answer in their own words.
	time.Sleep(10 * time.Millisecond)
	answer, _ := json.Marshal(map[string]string{"body": "Bylo to v roce 1987."})
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", answer, http.StatusCreated, nil)
	if got := detail(editor); !got.HasNewAnswer || !got.WaitingOnMe || got.LastActivityByUID != e.uids["pametnik"] {
		t.Errorf("editor's read after the answer = answer %v / waiting %v / by %q, want true / true / the viewer",
			got.HasNewAnswer, got.WaitingOnMe, got.LastActivityByUID)
	}
	if got := detail(viewer); got.HasNewAnswer || got.WaitingOnMe {
		t.Errorf("viewer's read of their own answer = answer %v / waiting %v, want neither",
			got.HasNewAnswer, got.WaitingOnMe)
	}
	if got := waitingOn(editor); got.Total != 1 {
		t.Errorf("waiting=1 for the editor after the answer = %+v, want the task", got)
	}

	// The waiting filter combines with the others and is a plain boolean.
	var combined page
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?waiting=true&state=question&participant=me",
		nil, http.StatusOK, &combined)
	if combined.Total != 1 {
		t.Errorf("waiting+state+participant = %+v, want the task", combined)
	}

	// The editor moves the state: seen, and nobody's move until the viewer acts.
	time.Sleep(10 * time.Millisecond)
	advance, _ := json.Marshal(map[string]string{"state": "working"})
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+task.UID, advance, http.StatusOK, nil)
	if got := detail(editor); got.HasNewAnswer || got.WaitingOnMe || got.StateByUID != e.uids["editor"] {
		t.Errorf("editor's read after moving = answer %v / waiting %v / state_by %q, want false / false / self",
			got.HasNewAnswer, got.WaitingOnMe, got.StateByUID)
	}
	if got := waitingOn(viewer); got.Total != 1 {
		t.Errorf("waiting=1 for the viewer after the editor moved = %+v, want the task", got)
	}
}
