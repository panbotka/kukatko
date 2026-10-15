//go:build integration

package notificationapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notificationapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/push"
)

// unreadByProjectPath is the instance-level per-project count.
const unreadByProjectPath = auth.APIBasePath + "/notifications/unread-by-project"

// TestUnreadByProject_onlyProjectsInStanding gives A's member unread
// notifications in A and in B — in B before they are its member — and walks
// the membership in B through every state. The count names B only while the
// membership is approved, not withheld and B active; otherwise B's uid appears
// nowhere in the answer. Read notifications and records outside the inbox are
// never counted, and nobody sees another account's counts.
func TestUnreadByProject_onlyProjectsInStanding(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()
	projects := project.NewStore(pool)
	store := notification.NewStore(pool)

	seed := func(tenant projecttest.Tenant, userUID string, inApp, read bool) {
		t.Helper()
		n, err := store.Create(ctx, notification.New{
			ProjectUID: tenant.ProjectUID, ProjectSlug: tenant.Slug, UserUID: userUID,
			Kind: notification.KindTagged, Title: "Označen", SelfLink: true,
			PhotoUIDs: []string{tenant.PhotoUID}, InApp: inApp,
		})
		if err != nil {
			t.Fatalf("Create in %s: %v", tenant.Slug, err)
		}
		if read {
			if _, err := store.MarkRead(ctx, tenant.ProjectUID, userUID, n.UID); err != nil {
				t.Fatalf("MarkRead: %v", err)
			}
		}
	}
	// A's member: two unread in A plus one read and one outside the inbox;
	// three unread in B, a project they are not (yet) a member of.
	seed(f.A, f.A.UserUID, true, false)
	seed(f.A, f.A.UserUID, true, false)
	seed(f.A, f.A.UserUID, true, true)
	seed(f.A, f.A.UserUID, false, false)
	for range 3 {
		seed(f.B, f.A.UserUID, true, false)
	}
	// B's member: one unread in B, never in A's answer.
	seed(f.B, f.B.UserUID, true, false)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := notificationapi.NewAPI(notificationapi.Config{
		Subscriptions: push.NewStore(pool), Notifications: store, Photos: photos.NewStore(pool),
		RequireAuth: authAPI.RequireAuth, RequireSignedIn: authAPI.RequireSignedIn,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterInstanceRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	clientA := f.ClientFor(t, server, projecttest.SideA)
	clientB := f.ClientFor(t, server, projecttest.SideB)

	type count struct {
		Project string `json:"project"`
		Unread  int    `json:"unread"`
	}
	read := func(client *http.Client) ([]count, string) {
		t.Helper()
		resp := f.Get(t, client, unreadByProjectPath)
		defer func() { _ = resp.Body.Close() }()
		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			t.Fatalf("decoding the per-project count: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d %s, want 200", unreadByProjectPath, resp.StatusCode, raw)
		}
		var body struct {
			Projects []count `json:"projects"`
		}
		if err := json.Unmarshal(raw, &body); err != nil || body.Projects == nil {
			t.Fatalf("decoding %s: %v, want {projects: [...]}", raw, err)
		}
		return body.Projects, string(raw)
	}
	onlyA := []count{{Project: f.A.ProjectUID, Unread: 2}}
	expectOnlyA := func(state string) {
		t.Helper()
		got, raw := read(clientA)
		if !reflect.DeepEqual(got, onlyA) {
			t.Fatalf("%s: counts = %+v, want %+v", state, got, onlyA)
		}
		if strings.Contains(raw, f.B.ProjectUID) {
			t.Fatalf("%s: the answer names project B: %s", state, raw)
		}
	}
	both := []count{{Project: f.A.ProjectUID, Unread: 2}, {Project: f.B.ProjectUID, Unread: 3}}
	if both[1].Project < both[0].Project {
		both[0], both[1] = both[1], both[0]
	}
	expectBoth := func(state string) {
		t.Helper()
		if got, _ := read(clientA); !reflect.DeepEqual(got, both) {
			t.Fatalf("%s: counts = %+v, want %+v", state, got, both)
		}
	}

	expectOnlyA("no membership in B")
	if got, raw := read(clientB); !reflect.DeepEqual(got, []count{{Project: f.B.ProjectUID, Unread: 1}}) ||
		strings.Contains(raw, f.A.ProjectUID) {
		t.Fatalf("B's member: counts = %s, want only B's one", raw)
	}

	if _, err := projects.AddMember(ctx, audit.Meta{}, f.B.ProjectUID, f.A.UserUID, project.RoleViewer, false); err != nil {
		t.Fatalf("adding A's member to B, pending: %v", err)
	}
	expectOnlyA("pending membership in B")

	if _, err := projects.Approve(ctx, audit.Meta{}, f.B.ProjectUID, f.A.UserUID); err != nil {
		t.Fatalf("approving A's member in B: %v", err)
	}
	expectBoth("approved membership in B")

	if err := projects.RemoveMember(ctx, audit.Meta{}, f.B.ProjectUID, f.A.UserUID); err != nil {
		t.Fatalf("removing A's member from B: %v", err)
	}
	expectOnlyA("removed membership in B")

	outcome, err := projects.AddByIdentifier(ctx, audit.Meta{}, f.B.ProjectUID, f.A.Username, project.RoleViewer)
	if err != nil || outcome != project.OutcomeAdded {
		t.Fatalf("exact-match add of A's member to B = %q, %v; want added", outcome, err)
	}
	expectOnlyA("withheld membership in B")

	if err := projects.RevealIdentity(ctx, f.B.ProjectUID, f.A.UserUID); err != nil {
		t.Fatalf("revealing A's member in B: %v", err)
	}
	expectBoth("entered membership in B")

	off := false
	if _, err := projects.Update(ctx, audit.Meta{}, f.B.ProjectUID, project.UpdatePatch{Active: &off}); err != nil {
		t.Fatalf("deactivating B: %v", err)
	}
	expectOnlyA("deactivated B")
}

// TestUnreadByProject_signedOut is a 401: the count is the account's own.
func TestUnreadByProject_signedOut(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := notificationapi.NewAPI(notificationapi.Config{
		Subscriptions: push.NewStore(pool), Notifications: notification.NewStore(pool),
		Photos: photos.NewStore(pool), RequireAuth: authAPI.RequireAuth, RequireSignedIn: authAPI.RequireSignedIn,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, api.RegisterInstanceRoutes)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+unreadByProjectPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: %d, want 401", resp.StatusCode)
	}
}
