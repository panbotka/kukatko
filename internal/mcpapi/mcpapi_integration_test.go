//go:build integration

package mcpapi_test

// These tests drive the real MCP transport over HTTP against the database named
// by KUKATKO_TEST_DATABASE_URL, with the real auth middleware and real API
// tokens — the point is to pin the boundary an agent actually meets, not a
// hand-built approximation of it. They share one database and truncate per case,
// so they do not run in parallel.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mcpapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

const testPassword = "correct horse battery staple"

// env wires the auth and MCP APIs behind an httptest server over the integration
// database.
type env struct {
	server   *httptest.Server
	authSvc  *auth.Service
	db       *database.DB
	photos   *photos.Store
	organize *organize.Store
	people   *people.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvWith(t, true)
}

// newEnvWith builds the environment with the MCP server enabled or disabled.
func newEnvWith(t *testing.T, enabled bool) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authSvc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := mcpapi.NewAPI(mcpapi.Config{
		Enabled:     enabled,
		Photos:      photos.NewStore(db.Pool()),
		Organize:    organize.NewStore(db.Pool()),
		People:      people.NewStore(db.Pool()),
		Bulk:        bulk.NewService(db.Pool(), 100),
		RequireAuth: authAPI.RequireAuth,
		PageSize:    2,
		MaxPageSize: 10,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &env{
		server:   server,
		authSvc:  authSvc,
		db:       db,
		photos:   photos.NewStore(db.Pool()),
		organize: organize.NewStore(db.Pool()),
		people:   people.NewStore(db.Pool()),
	}
}

// token creates a user with the given role and mints an API token for it,
// returning the plaintext `kkt_…` an agent would put in its Authorization header.
func (e *env) token(t *testing.T, username string, role auth.Role) string {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	_, plaintext, err := e.authSvc.CreateAPIToken(t.Context(), user.UID,
		auth.CreateAPITokenInput{Name: "agent"},
		audit.Entry{ActorUID: user.UID, Action: audit.ActionAPITokenCreate, TargetType: "api_tokens"})
	if err != nil {
		t.Fatalf("CreateAPIToken(%s): %v", username, err)
	}
	if !strings.HasPrefix(plaintext, "kkt_") {
		t.Fatalf("token = %q, want the kkt_ scheme", plaintext)
	}
	return plaintext
}

// seedPhoto inserts a minimal photo and returns its UID.
func (e *env) seedPhoto(t *testing.T, hash string) string {
	t.Helper()
	p, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg",
		FileName: hash + ".jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("seed photo %s: %v", hash, err)
	}
	return p.UID
}

// auditCount counts the audit rows for an action written by an actor.
func (e *env) auditCount(t *testing.T, action, actorUID string) int {
	t.Helper()
	var n int
	err := e.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND actor_uid = $2`, action, actorUID).Scan(&n)
	if err != nil {
		t.Fatalf("counting audit rows for %s: %v", action, err)
	}
	return n
}

// userUID looks a user's UID up by username, to attribute audit rows.
func (e *env) userUID(t *testing.T, username string) string {
	t.Helper()
	var uid string
	err := e.db.Pool().QueryRow(t.Context(),
		`SELECT uid FROM users WHERE username = $1`, username).Scan(&uid)
	if err != nil {
		t.Fatalf("looking up user %s: %v", username, err)
	}
	return uid
}

// rpcResponse is a JSON-RPC reply.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callResult is the payload of a tools/call reply.
type callResult struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// toolInfo is one entry of a tools/list reply.
type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// rpc issues one JSON-RPC request to the MCP endpoint with the given bearer
// token, returning the HTTP status and the decoded reply.
func (e *env) rpc(t *testing.T, bearer, method string, params any) (int, rpcResponse) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		e.server.URL+"/api/v1/mcp", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out rpcResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode %s reply: %v (body %s)", method, err, raw)
		}
	}
	return resp.StatusCode, out
}

// listTools returns the tool names the given token can see.
func (e *env) listTools(t *testing.T, bearer string) map[string]toolInfo {
	t.Helper()
	status, resp := e.rpc(t, bearer, "tools/list", nil)
	if status != http.StatusOK || resp.Error != nil {
		t.Fatalf("tools/list status = %d, error = %+v", status, resp.Error)
	}
	var payload struct {
		Tools []toolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	out := make(map[string]toolInfo, len(payload.Tools))
	for _, tool := range payload.Tools {
		out[tool.Name] = tool
	}
	return out
}

// callTool invokes a tool and returns its result. A transport-level or protocol
// error fails the test; a tool-level refusal comes back with IsError set, which
// is what the caller inspects.
func (e *env) callTool(t *testing.T, bearer, name string, args map[string]any) callResult {
	t.Helper()
	status, resp := e.rpc(t, bearer, "tools/call", map[string]any{"name": name, "arguments": args})
	if status != http.StatusOK {
		t.Fatalf("tools/call %s status = %d", name, status)
	}
	if resp.Error != nil {
		// An unknown tool is reported at the protocol level; surface it as a
		// refusal so the callers can assert on it uniformly.
		return callResult{IsError: true, Content: []struct {
			Text string `json:"text"`
		}{{Text: resp.Error.Message}}}
	}
	var out callResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("decode %s result: %v (%s)", name, err, resp.Result)
	}
	return out
}

// mustCall invokes a tool and fails the test if it refused.
func (e *env) mustCall(t *testing.T, bearer, name string, args map[string]any) callResult {
	t.Helper()
	res := e.callTool(t, bearer, name, args)
	if res.IsError {
		t.Fatalf("%s refused: %s", name, res.text())
	}
	return res
}

// text joins a result's content, for assertions and failure messages.
func (r callResult) text() string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// decode unmarshals a result's structured content.
func (r callResult) decode(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.StructuredContent, dst); err != nil {
		t.Fatalf("decode structured content: %v (%s)", err, r.StructuredContent)
	}
}

// TestMCPDisabledRouteIsNotMounted pins the config switch end to end: with the
// key off the path does not exist, rather than existing and refusing.
//
// The 404 is chi's, because this router mounts no SPA fallback; the full server
// does, so there the same path falls through to index.html like any unknown URL.
// Either way nothing of the MCP server is on the router — which is the claim.
func TestMCPDisabledRouteIsNotMounted(t *testing.T) {
	e := newEnvWith(t, false)
	bearer := e.token(t, "agent-off", auth.RoleAI)

	status, _ := e.rpc(t, bearer, "tools/list", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d with mcp disabled, want 404 (the route must not be mounted at all)", status)
	}
}

// TestMCPRequiresAuthentication checks the endpoint adds no bypass of its own.
func TestMCPRequiresAuthentication(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct{ name, bearer string }{
		{name: "no token", bearer: ""},
		{name: "bogus token", bearer: "kkt_nope_nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := e.rpc(t, tc.bearer, "tools/list", nil)
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
		})
	}
}

// TestMCPInitializeHandshake checks a real client's opening exchange works and
// that the server announces its tools capability.
func TestMCPInitializeHandshake(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-init", auth.RoleAI)

	status, resp := e.rpc(t, bearer, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1"},
	})
	if status != http.StatusOK || resp.Error != nil {
		t.Fatalf("initialize status = %d, error = %+v", status, resp.Error)
	}
	var payload struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools *struct{} `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if payload.Capabilities.Tools == nil {
		t.Error("initialize did not announce the tools capability")
	}
	if payload.ServerInfo.Name != "kukatko" {
		t.Errorf("serverInfo.name = %q, want kukatko", payload.ServerInfo.Name)
	}
}

// TestMCPViewerSeesOnlyReadTools checks a read-only token is not even shown the
// write tools, and that the read tools it does get are all there.
func TestMCPViewerSeesOnlyReadTools(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-viewer", auth.RoleViewer)
	tools := e.listTools(t, bearer)

	for _, want := range []string{
		"search_photos", "get_photo", "find_similar_photos", "library_stats",
		"list_albums", "get_album", "list_labels", "get_label", "list_subjects", "get_subject",
	} {
		if _, ok := tools[want]; !ok {
			t.Errorf("viewer cannot see the read tool %s", want)
		}
	}
	for _, unwanted := range []string{
		"create_album", "add_photos_to_album", "remove_photos_from_album",
		"create_label", "attach_label", "detach_label",
		"set_photo_metadata", "set_photo_rating", "bulk_edit_photos",
	} {
		if _, ok := tools[unwanted]; ok {
			t.Errorf("viewer can see the write tool %s", unwanted)
		}
	}
}

// TestMCPViewerIsRefusedOnEveryWrite is the spec's boundary: a viewer token must
// be refused by every write tool, not merely hidden from it.
func TestMCPViewerIsRefusedOnEveryWrite(t *testing.T) {
	e := newEnv(t)
	viewer := e.token(t, "agent-ro", auth.RoleViewer)
	photoUID := e.seedPhoto(t, "aa11")

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{tool: "create_album", args: map[string]any{"title": "Nope"}},
		{tool: "add_photos_to_album", args: map[string]any{"album_uid": "a1", "photo_uids": []string{photoUID}}},
		{tool: "remove_photos_from_album", args: map[string]any{"album_uid": "a1", "photo_uids": []string{photoUID}}},
		{tool: "create_label", args: map[string]any{"name": "nope"}},
		{tool: "attach_label", args: map[string]any{"photo_uid": photoUID, "label_uid": "l1"}},
		{tool: "detach_label", args: map[string]any{"photo_uid": photoUID, "label_uid": "l1"}},
		{tool: "set_photo_metadata", args: map[string]any{"uid": photoUID, "title": "nope"}},
		{tool: "set_photo_rating", args: map[string]any{"uid": photoUID, "rating": 5}},
		{tool: "bulk_edit_photos", args: map[string]any{"photo_uids": []string{photoUID}, "title": "nope"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			if res := e.callTool(t, viewer, tc.tool, tc.args); !res.IsError {
				t.Fatalf("%s succeeded for a viewer token", tc.tool)
			}
		})
	}

	// The refusals must have changed nothing.
	photo, err := e.photos.GetByUID(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Title != "" {
		t.Errorf("a viewer's refused write still changed the photo: title = %q", photo.Title)
	}
	albums, err := e.organize.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(albums) != 0 {
		t.Errorf("a viewer's refused write created %d album(s)", len(albums))
	}
}

// TestMCPDestructiveToolsAreNotExposed pins the deliberate omission. It checks by
// intent, not by name list, so a future tool called "empty_trash" or
// "purge_photo" trips it.
func TestMCPDestructiveToolsAreNotExposed(t *testing.T) {
	e := newEnv(t)
	// The most privileged role there is: if a tool is hidden even here, it is
	// not exposed at all.
	admin := e.token(t, "agent-admin", auth.RoleAdmin)
	tools := e.listTools(t, admin)

	banned := []string{"delete", "purge", "trash", "archive", "restore", "backup", "user", "empty"}
	for name := range tools {
		for _, word := range banned {
			if strings.Contains(name, word) {
				t.Errorf("tool %q looks destructive or administrative; it must not be exposed", name)
			}
		}
	}
	// And calling one by name must fail rather than find something.
	for _, name := range []string{"delete_photo", "purge_photo", "empty_trash", "restore_backup", "create_user"} {
		if res := e.callTool(t, admin, name, map[string]any{}); !res.IsError {
			t.Errorf("tool %s exists and succeeded; nothing destructive may be reachable", name)
		}
	}
}

// TestMCPSearchReturnsCompactShape checks a search answers with the minimum
// useful fields, the paging counters, and no EXIF blob.
func TestMCPSearchReturnsCompactShape(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-search", auth.RoleViewer)

	// Every photo carries an EXIF blob, not just one: the page below returns only
	// the configured 2 of 4, so a single tagged photo could sit outside it and
	// make the leak assertion pass by luck.
	for _, hash := range []string{"bb01", "bb02", "bb03", "bb04"} {
		uid := e.seedPhoto(t, hash)
		_, err := e.photos.UpdateMetadata(t.Context(), uid, photos.MetadataUpdate{Title: "babička v zahradě"})
		if err != nil {
			t.Fatalf("UpdateMetadata: %v", err)
		}
		_, err = e.db.Pool().Exec(t.Context(),
			`UPDATE photos SET exif = $1, camera_model = 'Zorki' WHERE uid = $2`,
			`{"Make":"Zorki","Secret":"do-not-leak"}`, uid)
		if err != nil {
			t.Fatalf("seeding exif: %v", err)
		}
	}

	res := e.mustCall(t, bearer, "search_photos", map[string]any{})
	raw := string(res.StructuredContent)
	for _, leak := range []string{"exif", "do-not-leak", "file_hash", "file_path"} {
		if strings.Contains(raw, leak) {
			t.Errorf("search leaked %q: %s", leak, raw)
		}
	}

	var got struct {
		Photos []struct {
			UID     string `json:"uid"`
			Title   string `json:"title"`
			TakenAt string `json:"taken_at"`
		} `json:"photos"`
		Total     int `json:"total"`
		Offset    int `json:"offset"`
		Remaining int `json:"remaining"`
	}
	res.decode(t, &got)

	// PageSize is 2 in this env, so the page must be capped and say what is left.
	if len(got.Photos) != 2 {
		t.Fatalf("page carried %d photos, want the configured default of 2", len(got.Photos))
	}
	if got.Total != 4 {
		t.Errorf("Total = %d, want 4", got.Total)
	}
	if got.Remaining != 2 {
		t.Errorf("Remaining = %d, want 2 (the tool must say how many more there are)", got.Remaining)
	}
	if got.Photos[0].UID == "" {
		t.Error("a summary carried no uid, so the agent cannot follow it up")
	}
}

// TestMCPSearchUsesTheQueryLanguage checks the search tool is wired to the real
// query language rather than to free text alone.
func TestMCPSearchUsesTheQueryLanguage(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-q", auth.RoleViewer)

	old := e.seedPhoto(t, "cc01")
	recent := e.seedPhoto(t, "cc02")
	setTaken(t, e, old, time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC))
	setTaken(t, e, recent, time.Date(2019, 6, 1, 12, 0, 0, 0, time.UTC))

	res := e.mustCall(t, bearer, "search_photos", map[string]any{"query": "year:1965"})
	var got struct {
		Photos []struct {
			UID string `json:"uid"`
		} `json:"photos"`
		Total int `json:"total"`
	}
	res.decode(t, &got)
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != old {
		t.Fatalf("year:1965 matched %+v, want only the 1965 photo %s", got, old)
	}
}

// setTaken stamps a capture time on a photo.
func setTaken(t *testing.T, e *env, uid string, at time.Time) {
	t.Helper()
	if _, err := e.photos.UpdateMetadata(t.Context(), uid, photos.MetadataUpdate{TakenAt: &at}); err != nil {
		t.Fatalf("setting taken_at: %v", err)
	}
}

// TestMCPWriteTokenCanOrganize is the spec's happy path: a write token creates an
// album and attaches a label, and every one of those mutations lands in the audit
// trail attributed to the agent's own user.
func TestMCPWriteTokenCanOrganize(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-rw", auth.RoleAI)
	actor := e.userUID(t, "agent-rw")
	photoUID := e.seedPhoto(t, "dd01")

	// Create an album.
	var album struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	}
	e.mustCall(t, bearer, "create_album", map[string]any{
		"title": "Babička v šedesátých", "description": "z MCP",
	}).decode(t, &album)
	if album.UID == "" || album.Title != "Babička v šedesátých" {
		t.Fatalf("create_album returned %+v", album)
	}

	// Put the photo in it.
	e.mustCall(t, bearer, "add_photos_to_album", map[string]any{
		"album_uid": album.UID, "photo_uids": []string{photoUID},
	})

	// Create a label and attach it.
	var label struct {
		UID string `json:"uid"`
	}
	e.mustCall(t, bearer, "create_label", map[string]any{"name": "babička"}).decode(t, &label)
	if label.UID == "" {
		t.Fatal("create_label returned no uid")
	}
	e.mustCall(t, bearer, "attach_label", map[string]any{
		"photo_uid": photoUID, "label_uid": label.UID,
	})

	// The library actually changed.
	uids, err := e.organize.ListPhotoUIDs(t.Context(), album.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDs: %v", err)
	}
	if len(uids) != 1 || uids[0] != photoUID {
		t.Errorf("album holds %v, want [%s]", uids, photoUID)
	}
	labels, err := e.organize.LabelsForPhoto(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("LabelsForPhoto: %v", err)
	}
	if len(labels) != 1 || labels[0].UID != label.UID {
		t.Errorf("photo carries %+v, want the attached label", labels)
	}

	// And every mutation is traceable to the agent.
	for _, action := range []string{
		audit.ActionAlbumCreate, audit.ActionAlbumAddPhotos,
		audit.ActionLabelCreate, audit.ActionLabelAttach,
	} {
		if n := e.auditCount(t, action, actor); n != 1 {
			t.Errorf("audit rows for %s = %d, want 1", action, n)
		}
	}

	// get_photo reflects the new collections.
	var detail struct {
		Albums []struct {
			UID string `json:"uid"`
		} `json:"albums"`
		Labels []struct {
			UID string `json:"uid"`
		} `json:"labels"`
	}
	e.mustCall(t, bearer, "get_photo", map[string]any{"uid": photoUID}).decode(t, &detail)
	if len(detail.Albums) != 1 || detail.Albums[0].UID != album.UID {
		t.Errorf("get_photo albums = %+v, want the new album", detail.Albums)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].UID != label.UID {
		t.Errorf("get_photo labels = %+v, want the new label", detail.Labels)
	}
}

// TestMCPEveryMutationIsAudited walks the remaining write tools and checks each
// one leaves a row behind — an agent's actions must be as traceable as a human's.
func TestMCPEveryMutationIsAudited(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-audit", auth.RoleEditor)
	actor := e.userUID(t, "agent-audit")
	photoUID := e.seedPhoto(t, "ee01")

	e.mustCall(t, bearer, "set_photo_metadata", map[string]any{
		"uid": photoUID, "title": "Na zahradě", "notes": "z MCP",
	})
	if n := e.auditCount(t, audit.ActionPhotoUpdate, actor); n != 1 {
		t.Errorf("audit rows for %s = %d, want 1", audit.ActionPhotoUpdate, n)
	}

	// The rating tool and the bulk tool both land on the bulk action, because the
	// rating tool reuses the bulk write path to get its audit row.
	e.mustCall(t, bearer, "set_photo_rating", map[string]any{"uid": photoUID, "rating": 4, "favorite": true})
	e.mustCall(t, bearer, "bulk_edit_photos", map[string]any{
		"photo_uids": []string{photoUID}, "description": "hromadně",
	})
	if n := e.auditCount(t, audit.ActionPhotosBulk, actor); n != 2 {
		t.Errorf("audit rows for %s = %d, want 2", audit.ActionPhotosBulk, n)
	}

	// The changes really landed.
	photo, err := e.photos.GetByUID(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Title != "Na zahradě" || photo.Notes != "z MCP" || photo.Description != "hromadně" {
		t.Errorf("photo = %+v, want the edits applied", photo)
	}
	rating, err := e.organize.GetRating(t.Context(), actor, photoUID)
	if err != nil {
		t.Fatalf("GetRating: %v", err)
	}
	if rating.Rating != 4 {
		t.Errorf("rating = %d, want 4", rating.Rating)
	}
	fav, err := e.organize.IsFavorite(t.Context(), actor, photoUID)
	if err != nil {
		t.Fatalf("IsFavorite: %v", err)
	}
	if !fav {
		t.Error("the photo was not favourited")
	}
}

// TestMCPSetMetadataDoesNotBlankOtherFields pins the read-modify-write trap: the
// store's update replaces the whole record, so an agent setting only a title must
// not wipe the description, the notes or the capture date.
func TestMCPSetMetadataDoesNotBlankOtherFields(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-partial", auth.RoleEditor)
	photoUID := e.seedPhoto(t, "ff01")

	taken := time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC)
	lat, lng := 50.08, 14.43
	_, err := e.photos.UpdateMetadata(t.Context(), photoUID, photos.MetadataUpdate{
		Title: "old", Description: "keep me", Notes: "keep me too",
		Keywords: "rodina", TakenAt: &taken, Lat: &lat, Lng: &lng, LocationSource: "manual",
	})
	if err != nil {
		t.Fatalf("seeding metadata: %v", err)
	}

	e.mustCall(t, bearer, "set_photo_metadata", map[string]any{"uid": photoUID, "title": "new"})

	photo, err := e.photos.GetByUID(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Title != "new" {
		t.Errorf("Title = %q, want new", photo.Title)
	}
	if photo.Description != "keep me" || photo.Notes != "keep me too" || photo.Keywords != "rodina" {
		t.Errorf("a partial edit blanked another field: %+v", photo)
	}
	if photo.TakenAt == nil || !photo.TakenAt.Equal(taken) {
		t.Errorf("TakenAt = %v, want it preserved", photo.TakenAt)
	}
	if photo.Lat == nil || photo.Lng == nil || photo.LocationSource != "manual" {
		t.Errorf("the location was dropped: %+v", photo)
	}

	// Clearing is still possible, and distinct from omitting.
	e.mustCall(t, bearer, "set_photo_metadata", map[string]any{"uid": photoUID, "notes": ""})
	photo, err = e.photos.GetByUID(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.Notes != "" {
		t.Errorf("Notes = %q, want an explicit empty string to clear it", photo.Notes)
	}
	if photo.Description != "keep me" {
		t.Errorf("clearing the notes also blanked the description: %q", photo.Description)
	}
}

// TestMCPListAndLookupCollections checks the tools that turn a name a human used
// into the uid the other tools want.
func TestMCPListAndLookupCollections(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-coll", auth.RoleEditor)
	photoUID := e.seedPhoto(t, "gg01")

	album, err := e.organize.CreateAlbum(t.Context(), organize.Album{Title: "Dovolená 2019"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if _, err := e.organize.CreateAlbum(t.Context(), organize.Album{Title: "Vánoce"}); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	subj, err := e.people.CreateSubject(t.Context(), people.Subject{Name: "Babička"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// A name filter narrows the listing.
	var albums struct {
		Albums []struct {
			UID   string `json:"uid"`
			Title string `json:"title"`
		} `json:"albums"`
	}
	e.mustCall(t, bearer, "list_albums", map[string]any{"name": "dovol"}).decode(t, &albums)
	if len(albums.Albums) != 1 || albums.Albums[0].UID != album.UID {
		t.Fatalf("list_albums(name=dovol) = %+v, want only Dovolená 2019", albums.Albums)
	}

	// A lookup works by slug as well as by uid.
	var got struct {
		UID        string `json:"uid"`
		PhotoCount int    `json:"photo_count"`
	}
	e.mustCall(t, bearer, "get_album", map[string]any{"slug": album.Slug}).decode(t, &got)
	if got.UID != album.UID {
		t.Errorf("get_album by slug = %s, want %s", got.UID, album.UID)
	}

	var subjects struct {
		People []struct {
			UID  string `json:"uid"`
			Name string `json:"name"`
		} `json:"people"`
	}
	e.mustCall(t, bearer, "list_subjects", map[string]any{}).decode(t, &subjects)
	if len(subjects.People) != 1 || subjects.People[0].UID != subj.UID {
		t.Fatalf("list_subjects = %+v, want the one subject", subjects.People)
	}

	// A missing thing is a clear refusal, not an empty success.
	if res := e.callTool(t, bearer, "get_album", map[string]any{"uid": "nope"}); !res.IsError {
		t.Error("get_album on a missing uid succeeded")
	}
	if res := e.callTool(t, bearer, "get_album", map[string]any{}); !res.IsError {
		t.Error("get_album with neither uid nor slug succeeded")
	}

	// An album scope reaches the search.
	e.mustCall(t, bearer, "add_photos_to_album", map[string]any{
		"album_uid": album.UID, "photo_uids": []string{photoUID},
	})
	var page struct {
		Total int `json:"total"`
	}
	e.mustCall(t, bearer, "search_photos", map[string]any{"album_uid": album.UID}).decode(t, &page)
	if page.Total != 1 {
		t.Errorf("search_photos(album_uid) total = %d, want 1", page.Total)
	}
}

// TestMCPLibraryStats checks the counting tool, including that favourites are the
// calling user's own rather than a library-wide number.
func TestMCPLibraryStats(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-stats", auth.RoleEditor)
	other := e.token(t, "agent-other", auth.RoleEditor)

	first := e.seedPhoto(t, "hh01")
	e.seedPhoto(t, "hh02")
	if _, err := e.organize.CreateAlbum(t.Context(), organize.Album{Title: "A"}); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	e.mustCall(t, bearer, "set_photo_rating", map[string]any{"uid": first, "favorite": true})

	var stats struct {
		Photos    int `json:"photos"`
		Videos    int `json:"videos"`
		Archived  int `json:"archived"`
		Favorites int `json:"favorites"`
		Albums    int `json:"albums"`
	}
	e.mustCall(t, bearer, "library_stats", map[string]any{}).decode(t, &stats)
	if stats.Photos != 2 || stats.Albums != 1 || stats.Videos != 0 || stats.Archived != 0 {
		t.Errorf("stats = %+v, want 2 photos and 1 album", stats)
	}
	if stats.Favorites != 1 {
		t.Errorf("Favorites = %d, want the caller's 1", stats.Favorites)
	}

	// Another user's stats must not see the first user's favourite.
	e.mustCall(t, other, "library_stats", map[string]any{}).decode(t, &stats)
	if stats.Favorites != 0 {
		t.Errorf("Favorites = %d for a different user, want 0 (favourites are per-user)", stats.Favorites)
	}
}

// TestMCPBulkEditIsAtomic checks the batch lever reports its work and does not
// half-apply a change when one uid is wrong.
func TestMCPBulkEditIsAtomic(t *testing.T) {
	e := newEnv(t)
	bearer := e.token(t, "agent-bulk", auth.RoleEditor)
	first := e.seedPhoto(t, "ii01")
	second := e.seedPhoto(t, "ii02")

	label, err := e.organize.CreateLabel(t.Context(), organize.Label{Name: "výlet"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	var res struct {
		Total, Updated, Errored int
	}
	e.mustCall(t, bearer, "bulk_edit_photos", map[string]any{
		"photo_uids": []string{first, second}, "add_labels": []string{label.UID},
	}).decode(t, &res)
	if res.Total != 2 || res.Updated != 2 || res.Errored != 0 {
		t.Fatalf("bulk result = %+v, want 2 updated", res)
	}

	// A bad label uid must abort the whole batch rather than label half of it.
	out := e.callTool(t, bearer, "bulk_edit_photos", map[string]any{
		"photo_uids": []string{first, second}, "add_labels": []string{"no-such-label"},
	})
	if !out.IsError {
		t.Fatal("bulk_edit_photos with a bad label uid succeeded")
	}
	if !strings.Contains(out.text(), "nothing was changed") {
		t.Errorf("error %q should say the batch was not applied", out.text())
	}
}

// TestMCPToolDescriptionsAreWritten checks the interface an agent actually reads.
// A tool nobody described is a tool the agent will misuse.
func TestMCPToolDescriptionsAreWritten(t *testing.T) {
	e := newEnv(t)
	tools := e.listTools(t, e.token(t, "agent-desc", auth.RoleAI))
	if len(tools) < 15 {
		t.Fatalf("a write token sees %d tools, want the full set", len(tools))
	}
	for name, tool := range tools {
		if len(tool.Description) < 40 {
			t.Errorf("tool %s has a %d-character description; describe what it does, "+
				"what the arguments mean and what comes back", name, len(tool.Description))
		}
	}
}
