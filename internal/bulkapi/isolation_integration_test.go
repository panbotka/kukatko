//go:build integration

package bulkapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/bulkapi"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// newIsolationServer mounts the real auth API and the bulk routes over the real
// bulk service, under /api/v1/p/{project} the way the server does.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := bulkapi.NewAPI(bulkapi.Config{
		Service: bulk.NewService(pool, 100), RequireWrite: authAPI.RequireWrite, RequireCurator: authAPI.RequireCurator,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// TestBulk_foreignPhotoRefusesTheWholeBatch sends, as B's member, a batch of
// B's own photo plus A's: the batch is refused with the very 404 a batch with a
// missing uid gets — naming neither uid — and neither photo changes.
func TestBulk_foreignPhotoRefusesTheWholeBatch(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	client := f.ClientFor(t, newIsolationServer(t, pool), projecttest.SideB)
	path := f.PathFor(projecttest.SideB, "photos/bulk")
	batch := func(other string) map[string]any {
		return map[string]any{
			"photo_uids": []string{f.B.PhotoUID, other},
			"operations": map[string]any{"archive": true, "set_caption": "written from B"},
		}
	}

	foreign := f.Send(t, client, http.MethodPost, path, batch(f.A.PhotoUID))
	if foreign.StatusCode != http.StatusNotFound {
		t.Errorf("bulk with A's photo from B = %d, want 404", foreign.StatusCode)
	}
	missing := f.Send(t, client, http.MethodPost, path, batch(projecttest.MissingUID("ph")))
	projecttest.AssertSameShape(t, foreign, missing)

	store := photos.NewStore(pool)
	for _, tn := range []*projecttest.Tenant{&f.A, &f.B} {
		p, err := store.GetByUIDInProject(t.Context(), tn.ProjectUID, tn.PhotoUID)
		if err != nil {
			t.Fatalf("reloading %s: %v", tn.PhotoUID, err)
		}
		if p.ArchivedAt != nil || p.Title == "written from B" {
			t.Errorf("photo %s was changed by a refused batch: %+v", tn.PhotoUID, p)
		}
	}
	var audited int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action = 'photos.bulk'`).
		Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 0 {
		t.Errorf("a refused batch left %d audit row(s)", audited)
	}

	// A's album is no target for B's photo either.
	addAlbum := f.Send(t, client, http.MethodPost, path, map[string]any{
		"photo_uids": []string{f.B.PhotoUID}, "operations": map[string]any{"add_to_albums": []string{f.A.AlbumUID}},
	})
	if addAlbum.StatusCode != http.StatusBadRequest {
		t.Errorf("filing B's photo into A's album = %d, want 400 (album not found)", addAlbum.StatusCode)
	}
}

// TestBulk_summariesCountOnlyTheProject asks B's summaries about a selection of
// A's photo and B's: only B's photo counts, and A's album and label are never
// named.
func TestBulk_summariesCountOnlyTheProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	client := f.ClientFor(t, newIsolationServer(t, pool), projecttest.SideB)
	selection := map[string]any{"photo_uids": []string{f.A.PhotoUID, f.B.PhotoUID}}

	for _, route := range []string{"photos/bulk/location-summary", "photos/bulk/membership-summary"} {
		resp := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, route), selection)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading %s: %v", route, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d, want 200; body %s", route, resp.StatusCode, body)
		}
		var summary struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(body, &summary); err != nil {
			t.Fatalf("decoding %s: %v", route, err)
		}
		if summary.Total != 1 {
			t.Errorf("%s total = %d, want 1 (B's photo only); body %s", route, summary.Total, body)
		}
		for _, uid := range []string{f.A.AlbumUID, f.A.LabelUID, f.A.PhotoUID} {
			if bytes.Contains(body, []byte(uid)) {
				t.Errorf("%s names A's %s; body %s", route, uid, body)
			}
		}
	}
}
