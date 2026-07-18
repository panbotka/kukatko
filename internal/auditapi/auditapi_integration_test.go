//go:build integration

package auditapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auditapi"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and audit APIs behind an httptest server over the
// integration database.
type env struct {
	baseURL string
	authSvc *auth.Service
	store   *audit.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database and
// seeds a representative set of audit entries.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	store := audit.NewStore(db.Pool())
	api := auditapi.NewAPI(auditapi.Config{Store: store, RequireAdmin: authAPI.RequireAdmin})

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, authSvc: authSvc, store: store}
}

// seed writes the given entries to the audit log in order.
func (e *env) seed(t *testing.T, entries ...audit.Entry) {
	t.Helper()
	for _, entry := range entries {
		if err := e.store.Record(t.Context(), entry); err != nil {
			t.Fatalf("seed audit entry %q: %v", entry.Action, err)
		}
	}
}

// createUser creates a user with the given role and returns its UID, so a seeded
// audit row can be attributed to it (audit_log.actor_uid references users.uid).
func (e *env) createUser(t *testing.T, username string, role auth.Role) string {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return user.UID
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := do(t, client, http.MethodPost, e.baseURL+"/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// listResponse mirrors the endpoint's JSON body for decoding in tests.
type listResponse struct {
	Entries    []audit.Record `json:"entries"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset"`
}

// TestListForbiddenForNonAdmin verifies a non-admin cannot read the audit log.
func TestListForbiddenForNonAdmin(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	resp := do(t, editor, http.MethodGet, env.baseURL+"/api/v1/audit", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("audit status for editor = %d, want 403", resp.StatusCode)
	}
}

// TestListFiltersAndPagination verifies the admin endpoint lists entries
// newest-first, applies the action and entity filters, and paginates with a
// total and next_offset.
func TestListFiltersAndPagination(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)
	env.seed(t,
		audit.Entry{Action: audit.ActionPhotoArchive, TargetType: "photos", TargetUID: "ph-1"},
		audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos", TargetUID: "ph-1"},
		audit.Entry{Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-1"},
	)

	all := list(t, admin, env.baseURL+"/api/v1/audit")
	if all.Total != 3 || len(all.Entries) != 3 {
		t.Fatalf("unfiltered list total/len = %d/%d, want 3/3", all.Total, len(all.Entries))
	}
	// Newest first.
	if all.Entries[0].Action != audit.ActionAlbumCreate {
		t.Errorf("entries[0].Action = %q, want %s", all.Entries[0].Action, audit.ActionAlbumCreate)
	}

	byAction := list(t, admin, env.baseURL+"/api/v1/audit?action="+audit.ActionPhotoArchive)
	if byAction.Total != 1 || byAction.Entries[0].Action != audit.ActionPhotoArchive {
		t.Errorf("action filter total = %d, want 1", byAction.Total)
	}

	byEntity := list(t, admin, env.baseURL+"/api/v1/audit?entity_type=photos&entity_uid=ph-1")
	if byEntity.Total != 2 {
		t.Errorf("entity filter total = %d, want 2", byEntity.Total)
	}

	// Pagination: first page of size 2 carries a next_offset; second page does not.
	page1 := list(t, admin, env.baseURL+"/api/v1/audit?limit=2&offset=0")
	if len(page1.Entries) != 2 || page1.NextOffset == nil || *page1.NextOffset != 2 {
		t.Errorf("page1 entries/next = %d/%v, want 2 / 2", len(page1.Entries), page1.NextOffset)
	}
	page2 := list(t, admin, env.baseURL+"/api/v1/audit?limit=2&offset=2")
	if len(page2.Entries) != 1 || page2.NextOffset != nil {
		t.Errorf("page2 entries/next = %d/%v, want 1 / nil", len(page2.Entries), page2.NextOffset)
	}

	// An invalid timestamp filter is rejected with 400.
	bad := do(t, admin, http.MethodGet, env.baseURL+"/api/v1/audit?since=not-a-time", nil)
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid since status = %d, want 400", bad.StatusCode)
	}
}

// reviewRows returns the four review-game decisions (two Ano, two Ne) attributed
// to actorUID, tagged details.via = "review" as the answer path stamps them.
func reviewRows(actorUID string) []audit.Entry {
	return []audit.Entry{
		{ActorUID: actorUID, Action: audit.ActionFaceAssign, TargetType: "markers", TargetUID: "mk-1",
			Details: map[string]any{
				"via": "review", "photo_uid": "ph-1", "subject_uid": "su-1", "subject_name": "Alice", "face_index": 0,
			}},
		{ActorUID: actorUID, Action: audit.ActionLabelAttach, TargetType: "labels", TargetUID: "lb-1",
			Details: map[string]any{"via": "review", "photo_uid": "ph-2", "source": "manual"}},
		{ActorUID: actorUID, Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: "su-2",
			Details: map[string]any{"via": "review", "photo_uid": "ph-3", "face_index": 1}},
		{ActorUID: actorUID, Action: audit.ActionLabelReject, TargetType: "labels", TargetUID: "lb-2",
			Details: map[string]any{"via": "review", "photo_uid": "ph-4"}},
	}
}

// TestReviewDecisionFilter verifies the admin endpoint can isolate one user's
// review-game decisions, split them into the Ano/Ne buckets, page them, and stays
// admin-only. It seeds review and non-review rows for two users so the filter's
// selectivity (by actor and by via=review) is exercised.
func TestReviewDecisionFilter(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)
	actorA := env.createUser(t, "actor-a", auth.RoleEditor)
	actorB := env.createUser(t, "actor-b", auth.RoleEditor)

	seed := reviewRows(actorA)
	// Non-review noise for A: an ordinary edit and a non-review face assign (no
	// via marker) must both be excluded by via=review.
	seed = append(seed,
		audit.Entry{ActorUID: actorA, Action: audit.ActionPhotoUpdate, TargetType: "photos", TargetUID: "ph-9"},
		audit.Entry{ActorUID: actorA, Action: audit.ActionFaceAssign, TargetType: "markers", TargetUID: "mk-9",
			Details: map[string]any{"photo_uid": "ph-9", "subject_uid": "su-9"}},
	)
	// B's review rows must not leak into A's view.
	seed = append(seed, reviewRows(actorB)...)
	env.seed(t, seed...)

	// Only A's four via=review rows, newest-first, nothing from B or the noise.
	got := list(t, admin, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review")
	if got.Total != 4 || len(got.Entries) != 4 {
		t.Fatalf("review filter total/len = %d/%d, want 4/4", got.Total, len(got.Entries))
	}
	for _, e := range got.Entries {
		if e.ActorUID == nil || *e.ActorUID != actorA {
			t.Errorf("entry actor = %v, want %s", e.ActorUID, actorA)
		}
		if e.Details["via"] != "review" {
			t.Errorf("entry %s via = %v, want review", e.Action, e.Details["via"])
		}
	}
	// The confirmation carries enough to render: the resolved subject and photo.
	newest := got.Entries[0]
	if newest.Action != audit.ActionLabelReject || newest.Details["photo_uid"] != "ph-4" {
		t.Errorf("newest = %s/%v, want label.reject/ph-4", newest.Action, newest.Details["photo_uid"])
	}

	// Ano bucket: face.assign + label.attach.
	yes := list(t, admin, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review&decision=yes")
	if yes.Total != 2 {
		t.Errorf("decision=yes total = %d, want 2", yes.Total)
	}
	for _, e := range yes.Entries {
		if e.Action != audit.ActionFaceAssign && e.Action != audit.ActionLabelAttach {
			t.Errorf("decision=yes action = %s, want assign/attach", e.Action)
		}
	}
	// Ne bucket: face.reject + label.reject.
	no := list(t, admin, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review&decision=no")
	if no.Total != 2 {
		t.Errorf("decision=no total = %d, want 2", no.Total)
	}
	for _, e := range no.Entries {
		if e.Action != audit.ActionFaceReject && e.Action != audit.ActionLabelReject {
			t.Errorf("decision=no action = %s, want reject", e.Action)
		}
	}

	// Paging over the four rows: first page carries a next offset, second does not.
	page1 := list(t, admin, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review&limit=2&offset=0")
	if len(page1.Entries) != 2 || page1.NextOffset == nil || *page1.NextOffset != 2 {
		t.Errorf("page1 entries/next = %d/%v, want 2 / 2", len(page1.Entries), page1.NextOffset)
	}
	page2 := list(t, admin, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review&limit=2&offset=2")
	if len(page2.Entries) != 2 || page2.NextOffset != nil {
		t.Errorf("page2 entries/next = %d/%v, want 2 / nil", len(page2.Entries), page2.NextOffset)
	}

	// Admin-only: an editor is forbidden, an admin is served (list() asserts 200).
	editor := env.login(t, "editor", auth.RoleEditor)
	forbidden := do(t, editor, http.MethodGet, env.baseURL+"/api/v1/audit?user="+actorA+"&via=review", nil)
	defer func() { _ = forbidden.Body.Close() }()
	if forbidden.StatusCode != http.StatusForbidden {
		t.Errorf("editor review filter status = %d, want 403", forbidden.StatusCode)
	}
}

// list GETs url as admin and returns the decoded list response.
func list(t *testing.T, client *http.Client, url string) listResponse {
	t.Helper()
	resp := do(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list %s status = %d, want 200", url, resp.StatusCode)
	}
	var body listResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return body
}

// do issues an HTTP request with the optional JSON body and returns the response.
func do(t *testing.T, client *http.Client, method, url string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}
