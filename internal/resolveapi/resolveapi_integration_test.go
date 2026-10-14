//go:build integration

package resolveapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/resolveapi"
)

// harness is one seeded instance with the resolver mounted.
type harness struct {
	f      *projecttest.Fixture
	server *httptest.Server
	exec   func(sql string, args ...any)
}

// newHarness seeds the two tenants (with opts) and mounts the resolver behind
// the real auth guard, throttled by limit when it is not nil.
func newHarness(t *testing.T, limit *ratelimit.Limiter, opts ...projecttest.Option) *harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, opts...)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: 24 * time.Hour, MaxLifetime: 48 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	cfg := resolveapi.Config{Resolver: resolveapi.NewStore(pool), RequireSignedIn: authAPI.RequireSignedIn}
	if limit != nil {
		cfg.Throttle = limit.KeyedMiddleware(func(r *http.Request) string {
			user, _ := auth.UserFromContext(r.Context())
			return user.UID
		})
	}
	api := resolveapi.NewAPI(cfg)
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
	return &harness{f: f, server: server, exec: exec}
}

// resolvePath is the resolver's address for kind and uid.
func resolvePath(kind, uid string) string {
	return auth.APIBasePath + "/resolve/" + kind + "/" + uid
}

// slugOf asks the resolver as client and returns the slug, failing unless the
// answer is a 200.
func (h *harness) slugOf(t *testing.T, client *http.Client, kind, uid string) string {
	t.Helper()
	resp := h.f.Get(t, client, resolvePath(kind, uid))
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve %s %s = %d %s, want 200", kind, uid, resp.StatusCode, body)
	}
	var got struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return got.Slug
}

// assertRefused checks that asking client about kind/uid is the very same 404
// as asking about a uid nothing holds.
func (h *harness) assertRefused(t *testing.T, client *http.Client, kind, uid string) {
	t.Helper()
	foreign := h.f.Get(t, client, resolvePath(kind, uid))
	defer func() { _ = foreign.Body.Close() }()
	missing := h.f.Get(t, client, resolvePath(kind, projecttest.MissingUID("zz")))
	defer func() { _ = missing.Body.Close() }()
	if foreign.StatusCode != http.StatusNotFound {
		t.Fatalf("resolve %s %s = %d, want 404", kind, uid, foreign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreign, missing)
}

// TestResolve_ownProjectAndForeignIsMissing resolves each kind of A's entities
// for A's member, and refuses B's exactly like a uid nothing holds.
func TestResolve_ownProjectAndForeignIsMissing(t *testing.T) {
	h := newHarness(t, nil)
	f := h.f
	a := f.ClientFor(t, h.server, projecttest.SideA)

	own := map[string]string{
		"photo": f.A.PhotoUID, "album": f.A.AlbumUID, "label": f.A.LabelUID,
		"task": f.A.TaskUID, "subject": f.A.SubjectUID,
	}
	for kind, uid := range own {
		if got := h.slugOf(t, a, kind, uid); got != f.A.Slug {
			t.Errorf("resolve %s = %q, want %q", kind, got, f.A.Slug)
		}
	}
	foreign := map[string]string{
		"photo": f.B.PhotoUID, "album": f.B.AlbumUID, "label": f.B.LabelUID,
		"task": f.B.TaskUID, "subject": f.B.SubjectUID,
	}
	for kind, uid := range foreign {
		h.assertRefused(t, a, kind, uid)
	}
	// A kind the resolver does not know is the same answer, too.
	h.assertRefused(t, a, "saved", f.A.SavedSearchUID)
}

// TestResolve_membershipStates walks A's member through B's membership states:
// none, pending, approved, and approved in a deactivated project. Only the
// approved membership of an active project resolves.
func TestResolve_membershipStates(t *testing.T) {
	h := newHarness(t, nil)
	f := h.f
	a := f.ClientFor(t, h.server, projecttest.SideA)

	h.exec(`INSERT INTO project_members (project_uid, user_uid, role) VALUES ($1, $2, 'viewer')`,
		f.B.ProjectUID, f.A.UserUID)
	h.assertRefused(t, a, "photo", f.B.PhotoUID)

	h.exec(`UPDATE project_members SET approved_at = now() WHERE project_uid = $1 AND user_uid = $2`,
		f.B.ProjectUID, f.A.UserUID)
	if got := h.slugOf(t, a, "photo", f.B.PhotoUID); got != f.B.Slug {
		t.Fatalf("approved member resolves %q, want %q", got, f.B.Slug)
	}

	h.exec(`UPDATE projects SET active = false WHERE uid = $1`, f.B.ProjectUID)
	h.assertRefused(t, a, "photo", f.B.PhotoUID)
}

// TestResolve_maintainerAndNotifications: the maintainer resolves into every
// project, deactivated or not, member or not — but a notification only ever
// resolves for its addressee.
func TestResolve_maintainerAndNotifications(t *testing.T) {
	h := newHarness(t, nil, projecttest.AsMaintainer(projecttest.SideB))
	f := h.f
	a := f.ClientFor(t, h.server, projecttest.SideA)
	b := f.ClientFor(t, h.server, projecttest.SideB)

	if got := h.slugOf(t, b, "photo", f.A.PhotoUID); got != f.A.Slug {
		t.Fatalf("maintainer resolves A's photo to %q, want %q", got, f.A.Slug)
	}
	h.exec(`UPDATE projects SET active = false WHERE uid = $1`, f.A.ProjectUID)
	if got := h.slugOf(t, b, "task", f.A.TaskUID); got != f.A.Slug {
		t.Fatalf("maintainer resolves a deactivated project's task to %q, want %q", got, f.A.Slug)
	}
	h.exec(`UPDATE projects SET active = true WHERE uid = $1`, f.A.ProjectUID)

	const notificationUID = "ntresolvea000000000000001"
	h.exec(`INSERT INTO notifications (uid, project_id, user_uid, kind, title) VALUES ($1, $2, $3, 'tagged', 'x')`,
		notificationUID, f.A.ProjectUID, f.A.UserUID)
	if got := h.slugOf(t, a, "notification", notificationUID); got != f.A.Slug {
		t.Fatalf("addressee resolves the notification to %q, want %q", got, f.A.Slug)
	}
	h.assertRefused(t, b, "notification", notificationUID)
}

// TestResolve_guardedAndThrottled: an anonymous caller is refused before the
// lookup, and a caller past the per-caller budget gets 429.
func TestResolve_guardedAndThrottled(t *testing.T) {
	h := newHarness(t, ratelimit.New(0.001, 2))
	f := h.f

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		h.server.URL+resolvePath("photo", f.A.PhotoUID), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous resolve = %d, want 401", resp.StatusCode)
	}

	a := f.ClientFor(t, h.server, projecttest.SideA)
	h.slugOf(t, a, "photo", f.A.PhotoUID)
	h.slugOf(t, a, "photo", f.A.PhotoUID)
	third := h.f.Get(t, a, resolvePath("photo", f.A.PhotoUID))
	defer func() { _ = third.Body.Close() }()
	if third.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third resolve in the burst = %d, want 429", third.StatusCode)
	}
}
