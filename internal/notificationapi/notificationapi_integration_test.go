//go:build integration

package notificationapi_test

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notificationapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth API and the notification API behind an httptest server over
// the integration database.
type env struct {
	server  *httptest.Server
	db      *database.DB
	authSvc *auth.Service
}

// session is a signed-in account: its uid and the cookies that carry it.
type session struct {
	uid     string
	cookies []*http.Cookie
}

// envOptions varies the wiring between cases.
type envOptions struct {
	pushOff  bool
	throttle func(http.Handler) http.Handler
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T, opts envOptions) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authSvc := auth.NewService(auth.NewStore(db.Pool()), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := notificationapi.NewAPI(notificationapi.Config{
		Subscriptions:     push.NewStore(db.Pool()),
		Notifications:     notification.NewStore(db.Pool()),
		Photos:            photos.NewStore(db.Pool()),
		Push:              notificationapi.PushSettings{Enabled: !opts.pushOff, PublicKey: "public-key"},
		RequireAuth:       authAPI.RequireAuth,
		SubscribeThrottle: opts.throttle,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, db: db, authSvc: authSvc}
}

// login creates an account with the given role and signs it in.
func (e *env) login(t *testing.T, username string, role auth.Role) session {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	status, _, cookies := e.send(t, session{}, http.MethodPost, "/api/v1/auth/login", string(body))
	if status != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login %s = %d with %d cookies, want 200 with a session", username, status, len(cookies))
	}
	return session{uid: user.UID, cookies: cookies}
}

// send issues a request as s and returns the status, the body and any cookies set.
func (e *env) send(t *testing.T, s session, method, path, body string) (int, string, []*http.Cookie) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Integration Browser")
	for _, c := range s.cookies {
		req.AddCookie(c)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, path, err)
	}
	return resp.StatusCode, string(raw), resp.Cookies()
}

// do is send without the cookies.
func (e *env) do(t *testing.T, s session, method, path, body string) (int, string) {
	t.Helper()
	status, raw, _ := e.send(t, s, method, path, body)
	return status, raw
}

// exec runs one statement against the test database.
func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// decode unmarshals body into dst, failing the test on malformed JSON.
func decode(t *testing.T, body string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), dst); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

// browserKeys mints the client keys a browser would report for a subscription.
func browserKeys(t *testing.T) (string, string) {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(secret)
}

// subscribeBody is the browser's PushSubscription.toJSON() for endpoint.
func subscribeBody(endpoint, p256dh, authKey string) string {
	body, _ := json.Marshal(map[string]any{
		"endpoint": endpoint, "expirationTime": nil,
		"keys": map[string]string{"p256dh": p256dh, "auth": authKey},
	})
	return string(body)
}

// subscriptionView mirrors the JSON shape of one subscription.
type subscriptionView struct {
	ID         string     `json:"id"`
	Endpoint   string     `json:"endpoint"`
	UserAgent  string     `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// listSubscriptions reads s's subscriptions and returns them with the raw body.
func (e *env) listSubscriptions(t *testing.T, s session) ([]subscriptionView, string) {
	t.Helper()
	status, body := e.do(t, s, http.MethodGet, "/api/v1/push/subscriptions", "")
	if status != http.StatusOK {
		t.Fatalf("list subscriptions = %d: %s", status, body)
	}
	var out struct {
		Subscriptions []subscriptionView `json:"subscriptions"`
	}
	decode(t, body, &out)
	return out.Subscriptions, body
}

// TestSubscriptions_lifecycle registers, refreshes, lists and removes a
// subscription as a viewer, and checks another account can neither see nor
// remove it and the client keys never come back.
func TestSubscriptions_lifecycle(t *testing.T) {
	e := newEnv(t, envOptions{})
	viewer := e.login(t, "vera", auth.RoleViewer)
	other := e.login(t, "otto", auth.RoleEditor)
	p256dh, authKey := browserKeys(t)
	const endpoint = "https://push.example/v1/abc"

	status, body := e.do(t, viewer, http.MethodPost, "/api/v1/push/subscriptions",
		subscribeBody(endpoint, p256dh, authKey))
	if status != http.StatusOK {
		t.Fatalf("subscribe = %d, want 200: %s", status, body)
	}
	var stored subscriptionView
	decode(t, body, &stored)
	if stored.Endpoint != endpoint || stored.UserAgent != "Integration Browser" || stored.ID == "" {
		t.Errorf("subscribe = %+v, want the endpoint with the request's user agent", stored)
	}

	// Refreshing the same browser keeps one row, now with a new key pair.
	p256dh2, authKey2 := browserKeys(t)
	if status, body := e.do(t, viewer, http.MethodPost, "/api/v1/push/subscriptions",
		subscribeBody(endpoint, p256dh2, authKey2)); status != http.StatusOK {
		t.Fatalf("re-subscribe = %d: %s", status, body)
	}
	subs, raw := e.listSubscriptions(t, viewer)
	if len(subs) != 1 || subs[0].ID != stored.ID {
		t.Fatalf("subscriptions = %+v, want the one refreshed row %s", subs, stored.ID)
	}
	for _, secret := range []string{p256dh, authKey, p256dh2, authKey2, "p256dh", `"auth"`} {
		if strings.Contains(raw, secret) {
			t.Errorf("the subscription list carries a client key (%q): %s", secret, raw)
		}
	}

	if others, _ := e.listSubscriptions(t, other); len(others) != 0 {
		t.Errorf("another account sees %+v, want none", others)
	}
	path := "/api/v1/push/subscriptions?endpoint=" + endpoint
	if status, body := e.do(t, other, http.MethodDelete, path, ""); status != http.StatusNotFound {
		t.Errorf("foreign delete = %d, want 404: %s", status, body)
	}
	if status, body := e.do(t, viewer, http.MethodDelete, path, ""); status != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204: %s", status, body)
	}
	if subs, _ := e.listSubscriptions(t, viewer); len(subs) != 0 {
		t.Errorf("subscriptions after delete = %+v, want none", subs)
	}
	if status, _ := e.do(t, viewer, http.MethodDelete, path, ""); status != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", status)
	}
}

// TestSubscribe_refusesUnusable checks a subscription push could never deliver
// to is a 400 and stores nothing.
func TestSubscribe_refusesUnusable(t *testing.T) {
	e := newEnv(t, envOptions{})
	user := e.login(t, "vera", auth.RoleViewer)
	p256dh, authKey := browserKeys(t)
	bodies := []string{
		subscribeBody("http://push.example/plain", p256dh, authKey),
		subscribeBody("https://push.example/a", "not-a-key", authKey),
		subscribeBody("https://push.example/a", p256dh, "c2hvcnQ"),
		`{"endpoint":"https://push.example/a"}`,
	}
	for _, body := range bodies {
		if status, resp := e.do(t, user, http.MethodPost, "/api/v1/push/subscriptions", body); status != 400 {
			t.Errorf("subscribe %s = %d, want 400: %s", body, status, resp)
		}
	}
	if subs, _ := e.listSubscriptions(t, user); len(subs) != 0 {
		t.Errorf("subscriptions = %+v, want none stored", subs)
	}
}

// TestPushDisabled_saysSoAndStoresNothing checks the capability reports push off
// and the subscription write answers 503 without storing a row.
func TestPushDisabled_saysSoAndStoresNothing(t *testing.T) {
	e := newEnv(t, envOptions{pushOff: true})
	user := e.login(t, "vera", auth.RoleViewer)

	status, body := e.do(t, user, http.MethodGet, "/api/v1/push/config", "")
	if status != http.StatusOK || !strings.Contains(body, `"enabled":false`) {
		t.Errorf("config = %d %s, want 200 with enabled false", status, body)
	}
	p256dh, authKey := browserKeys(t)
	status, body = e.do(t, user, http.MethodPost, "/api/v1/push/subscriptions",
		subscribeBody("https://push.example/a", p256dh, authKey))
	if status != http.StatusServiceUnavailable {
		t.Errorf("subscribe with push off = %d, want 503: %s", status, body)
	}
	if subs, _ := e.listSubscriptions(t, user); len(subs) != 0 {
		t.Errorf("subscriptions = %+v, want none", subs)
	}
}

// TestSubscribe_isRateLimited checks the throttle mounted on the write answers
// 429 once the caller's bucket is empty.
func TestSubscribe_isRateLimited(t *testing.T) {
	limiter := ratelimit.New(0.001, 2)
	keyFn := func(r *http.Request) string {
		user, _ := auth.UserFromContext(r.Context())
		return user.UID
	}
	e := newEnv(t, envOptions{throttle: limiter.KeyedMiddleware(keyFn)})
	user := e.login(t, "vera", auth.RoleViewer)
	p256dh, authKey := browserKeys(t)
	body := subscribeBody("https://push.example/a", p256dh, authKey)

	var statuses []int
	for range 3 {
		status, _ := e.do(t, user, http.MethodPost, "/api/v1/push/subscriptions", body)
		statuses = append(statuses, status)
	}
	if statuses[0] != 200 || statuses[1] != 200 || statuses[2] != http.StatusTooManyRequests {
		t.Errorf("statuses = %v, want 200, 200, 429", statuses)
	}
}

// preferences mirrors the preference envelope.
type preferences struct {
	Preferences []notification.Preference `json:"preferences"`
}

// TestPreferences_readAndReplace checks the read reports the defaults, a replace
// stores and reports the choice (audited), a kind left out returns to its
// default, and an unknown or doubled kind is a 400 that changes nothing.
func TestPreferences_readAndReplace(t *testing.T) {
	e := newEnv(t, envOptions{})
	user := e.login(t, "vera", auth.RoleViewer)

	status, body := e.do(t, user, http.MethodGet, "/api/v1/notifications/preferences", "")
	var got preferences
	decode(t, body, &got)
	if status != http.StatusOK || len(got.Preferences) != len(notification.Kinds()) {
		t.Fatalf("read = %d %s, want one entry per kind", status, body)
	}
	for _, pref := range got.Preferences {
		if !pref.IsDefault || pref.Enabled != pref.Kind.Default() {
			t.Errorf("fresh preference %+v, want the kind's default", pref)
		}
	}

	status, body = e.do(t, user, http.MethodPut, "/api/v1/notifications/preferences",
		`{"preferences":[{"kind":"tagged","enabled":false}]}`)
	decode(t, body, &got)
	if status != http.StatusOK || got.Preferences[0].Kind != notification.KindTagged ||
		got.Preferences[0].Enabled || got.Preferences[0].IsDefault {
		t.Fatalf("replace = %d %s, want tagged stored off", status, body)
	}
	var audited int
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM audit_log WHERE actor_uid = $1 AND target_uid = $1", user.uid).Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 1 {
		t.Errorf("audit rows for the replace = %d, want 1", audited)
	}

	for _, bad := range []string{
		`{"preferences":[{"kind":"nope","enabled":true}]}`,
		`{"preferences":[{"kind":"tagged","enabled":true},{"kind":"tagged","enabled":false}]}`,
		`{"preferences":[{"kind":"tagged"}]}`,
	} {
		if status, resp := e.do(t, user, http.MethodPut, "/api/v1/notifications/preferences", bad); status != 400 {
			t.Errorf("replace %s = %d, want 400: %s", bad, status, resp)
		}
	}
	_, body = e.do(t, user, http.MethodGet, "/api/v1/notifications/preferences", "")
	decode(t, body, &got)
	if got.Preferences[0].Enabled || got.Preferences[0].IsDefault {
		t.Errorf("after refused replaces %s, want tagged still stored off", body)
	}

	_, body = e.do(t, user, http.MethodPut, "/api/v1/notifications/preferences", `{"preferences":[]}`)
	decode(t, body, &got)
	if !got.Preferences[0].IsDefault || !got.Preferences[0].Enabled {
		t.Errorf("after an empty replace %s, want tagged back to its default", body)
	}
}

// detail mirrors the notification detail response.
type detail struct {
	UID    string     `json:"uid"`
	Kind   string     `json:"kind"`
	Title  string     `json:"title"`
	ReadAt *time.Time `json:"read_at"`
	Photos []struct {
		UID         string `json:"uid"`
		ThumbURL    string `json:"thumb_url"`
		DownloadURL string `json:"download_url"`
	} `json:"photos"`
	TotalCount   int `json:"total_count"`
	DroppedCount int `json:"dropped_count"`
}

// seedPhotos creates n photos and returns their uids in creation order.
func (e *env) seedPhotos(t *testing.T, n int) []string {
	t.Helper()
	store := photos.NewStore(e.db.Pool())
	uids := make([]string, 0, n)
	for i := range n {
		name := "ntf-" + string(rune('a'+i))
		p, err := store.Create(t.Context(), photos.Photo{
			Title: name, FileHash: name + "-hash", FilePath: "2026/09/" + name + ".jpg", FileName: name + ".jpg",
		})
		if err != nil {
			t.Fatalf("Create photo %s: %v", name, err)
		}
		uids = append(uids, p.UID)
	}
	return uids
}

// TestNotification_detailFiltersByVisibilityNow checks the frozen set is read in
// its stored order, filtered by what is visible now — archived, hidden and
// private photographs drop out and are counted — with media URLs stamped on.
func TestNotification_detailFiltersByVisibilityNow(t *testing.T) {
	e := newEnv(t, envOptions{})
	owner := e.login(t, "vera", auth.RoleViewer)
	p := e.seedPhotos(t, 5)
	set := []string{p[4], p[0], p[2], p[1], p[3]}
	n, err := notification.NewStore(e.db.Pool()).Create(t.Context(), notification.New{
		UserUID: owner.uid, Kind: notification.KindTagged, Title: "You were tagged in 5 photos",
		Link: "/notifications/x", PhotoUIDs: set,
	})
	if err != nil {
		t.Fatalf("Create notification: %v", err)
	}
	// After the notification was made: one archived, one hidden, one private.
	e.exec(t, "UPDATE photos SET archived_at = now() WHERE uid = $1", p[0])
	e.exec(t, "UPDATE photos SET hidden_from_library = true WHERE uid = $1", p[1])
	e.exec(t, "UPDATE photos SET private = true WHERE uid = $1", p[3])

	status, body := e.do(t, owner, http.MethodGet, "/api/v1/notifications/"+n.UID, "")
	if status != http.StatusOK {
		t.Fatalf("detail = %d: %s", status, body)
	}
	var got detail
	decode(t, body, &got)
	if got.UID != n.UID || got.Kind != "tagged" || got.Title != n.Title || got.ReadAt != nil {
		t.Errorf("detail = %+v, want the unread notification %s", got, n.UID)
	}
	if len(got.Photos) != 2 || got.Photos[0].UID != p[4] || got.Photos[1].UID != p[2] {
		t.Fatalf("photos = %+v, want %s then %s (frozen order, invisible dropped)", got.Photos, p[4], p[2])
	}
	if got.TotalCount != 5 || got.DroppedCount != 3 {
		t.Errorf("counts = total %d dropped %d, want 5 and 3", got.TotalCount, got.DroppedCount)
	}
	if want := "/api/v1/photos/" + p[4] + "/thumb/"; !strings.HasPrefix(got.Photos[0].ThumbURL, want) ||
		got.Photos[0].DownloadURL == "" {
		t.Errorf("media urls = %q, %q, want the app's own routes", got.Photos[0].ThumbURL, got.Photos[0].DownloadURL)
	}
	if strings.Contains(body, owner.uid) {
		t.Errorf("the detail carries the owner: %s", body)
	}

	// Un-archiving brings the photograph back: visibility is read now.
	e.exec(t, "UPDATE photos SET archived_at = NULL WHERE uid = $1", p[0])
	_, body = e.do(t, owner, http.MethodGet, "/api/v1/notifications/"+n.UID, "")
	decode(t, body, &got)
	if len(got.Photos) != 3 || got.Photos[1].UID != p[0] || got.DroppedCount != 2 {
		t.Errorf("after un-archiving = %s, want %s back in second place and 2 dropped", body, p[0])
	}
}

// TestNotification_scopedToOwner checks a foreign or unknown uid is a 404 for
// both the read and the mark-read — never a 403 — and the mark-read is
// idempotent for the owner.
func TestNotification_scopedToOwner(t *testing.T) {
	e := newEnv(t, envOptions{})
	owner := e.login(t, "vera", auth.RoleViewer)
	admin := e.login(t, "ada", auth.RoleAdmin)
	n, err := notification.NewStore(e.db.Pool()).Create(t.Context(), notification.New{
		UserUID: owner.uid, Kind: notification.KindRegistrationPending, Title: "Someone registered",
	})
	if err != nil {
		t.Fatalf("Create notification: %v", err)
	}

	for _, path := range []string{"/api/v1/notifications/" + n.UID, "/api/v1/notifications/ntunknown"} {
		if status, body := e.do(t, admin, http.MethodGet, path, ""); status != http.StatusNotFound {
			t.Errorf("GET %s as another account = %d, want 404: %s", path, status, body)
		}
		if status, body := e.do(t, admin, http.MethodPost, path+"/read", ""); status != http.StatusNotFound {
			t.Errorf("POST %s/read as another account = %d, want 404: %s", path, status, body)
		}
	}

	status, body := e.do(t, owner, http.MethodGet, "/api/v1/notifications/"+n.UID, "")
	var got detail
	decode(t, body, &got)
	if status != http.StatusOK || got.Photos == nil || len(got.Photos) != 0 || got.ReadAt != nil {
		t.Fatalf("detail of a photo-less notification = %d %s, want 200, photos [], unread", status, body)
	}

	status, body = e.do(t, owner, http.MethodPost, "/api/v1/notifications/"+n.UID+"/read", "")
	var first detail
	decode(t, body, &first)
	if status != http.StatusOK || first.ReadAt == nil {
		t.Fatalf("mark read = %d %s, want 200 with read_at", status, body)
	}
	_, body = e.do(t, owner, http.MethodPost, "/api/v1/notifications/"+n.UID+"/read", "")
	var second detail
	decode(t, body, &second)
	if second.ReadAt == nil || !second.ReadAt.Equal(*first.ReadAt) {
		t.Errorf("second mark read = %v, want the first reading %v kept", second.ReadAt, first.ReadAt)
	}
	status, body = e.do(t, admin, http.MethodGet, "/api/v1/notifications/"+n.UID, "")
	if status != http.StatusNotFound || bytes.Contains([]byte(body), []byte(n.Title)) {
		t.Errorf("foreign read after marking = %d %s, want a bare 404", status, body)
	}
}

// TestRoutes_requireAuth checks an anonymous request is refused on every route.
func TestRoutes_requireAuth(t *testing.T) {
	e := newEnv(t, envOptions{})
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/push/config"},
		{http.MethodGet, "/api/v1/push/subscriptions"},
		{http.MethodPost, "/api/v1/push/subscriptions"},
		{http.MethodDelete, "/api/v1/push/subscriptions?endpoint=x"},
		{http.MethodGet, "/api/v1/notifications/preferences"},
		{http.MethodPut, "/api/v1/notifications/preferences"},
		{http.MethodGet, "/api/v1/notifications/nt1"},
		{http.MethodPost, "/api/v1/notifications/nt1/read"},
	} {
		if status, _ := e.do(t, session{}, route.method, route.path, ""); status != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", route.method, route.path, status)
		}
	}
}
