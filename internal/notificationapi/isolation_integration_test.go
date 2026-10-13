//go:build integration

package notificationapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

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

// TestInbox_perProject makes A's member a member of B and gives them one
// notification in each project. Through each prefix the inbox lists and counts
// only that project's, "read everything" leaves the other project's unread,
// and the other project's notification — its detail and its mark-read — is the
// very 404 a missing one gets.
func TestInbox_perProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()
	projects := project.NewStore(pool)
	if _, err := projects.AddMember(ctx, audit.Meta{}, f.B.ProjectUID, f.A.UserUID, project.RoleViewer, true); err != nil {
		t.Fatalf("adding A's member to B: %v", err)
	}
	store := notification.NewStore(pool)
	inB, err := store.Create(ctx, notification.New{
		ProjectUID: f.B.ProjectUID, ProjectSlug: f.B.Slug, UserUID: f.A.UserUID, Kind: notification.KindTagged,
		Title: "Označen v B", SelfLink: true, PhotoUIDs: []string{f.B.PhotoUID}, InApp: true,
	})
	if err != nil {
		t.Fatalf("Create in B: %v", err)
	}
	inA, err := store.Create(ctx, notification.New{
		ProjectUID: f.A.ProjectUID, ProjectSlug: f.A.Slug, UserUID: f.A.UserUID, Kind: notification.KindTagged,
		Title: "Označen v A", SelfLink: true, PhotoUIDs: []string{f.A.PhotoUID}, InApp: true,
	})
	if err != nil {
		t.Fatalf("Create in A: %v", err)
	}

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := notificationapi.NewAPI(notificationapi.Config{
		Subscriptions: push.NewStore(pool), Notifications: store, Photos: photos.NewStore(pool),
		RequireAuth: authAPI.RequireAuth,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterInboxRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	client := f.ClientFor(t, server, projecttest.SideA)
	pathA := func(suffix string) string { return f.PathFor(projecttest.SideA, suffix) }

	f.AssertListExcludes(t, client, pathA("notifications"), inB.UID, f.B.PhotoUID)
	f.AssertInvisible(t, client, pathA("notifications/"+inB.UID))
	for _, tc := range []struct{ method, sub string }{
		{http.MethodGet, ""}, {http.MethodPost, "/read"},
	} {
		projecttest.AssertSameShape(t,
			f.Send(t, client, tc.method, pathA("notifications/"+inB.UID+tc.sub), nil),
			f.Send(t, client, tc.method, pathA("notifications/"+projecttest.MissingUID("nt")+tc.sub), nil))
	}
	own := f.Get(t, client, pathA("notifications/"+inA.UID))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("A's own notification through A = %d, want 200", own.StatusCode)
	}

	unread := func(which projecttest.Side) int {
		t.Helper()
		resp := f.Get(t, client, f.PathFor(which, "notifications/unread-count"))
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Count *int `json:"unread"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Count == nil {
			t.Fatalf("decoding the unread count through %s: %v", which, err)
		}
		return *body.Count
	}
	if a, b := unread(projecttest.SideA), unread(projecttest.SideB); a != 1 || b != 1 {
		t.Fatalf("unread counts A=%d B=%d, want one each", a, b)
	}
	readAll := f.Send(t, client, http.MethodPost, pathA("notifications/read-all"), nil)
	_ = readAll.Body.Close()
	if a, b := unread(projecttest.SideA), unread(projecttest.SideB); a != 0 || b != 1 {
		t.Fatalf("after read-all through A, unread A=%d B=%d, want 0 and 1", a, b)
	}
}
