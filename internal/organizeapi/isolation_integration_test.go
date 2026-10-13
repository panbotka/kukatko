//go:build integration

package organizeapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// isolationServer mounts the album and label routes under the project prefix
// over the seeded database, the way cmd/kukatko does.
func isolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	store := organize.NewStore(pool)
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:              store,
		Labels:              store,
		Featured:            store,
		FeaturedDefaultDays: 61,
		RequireAuth:         authAPI.RequireAuth,
		RequireCurator:      authAPI.RequireCurator,
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

// TestAlbumAndLabelRoutes_foreignIsMissing reads A's album and label from inside
// B: the detail is the very 404 a missing uid gets, and B's listings — albums,
// labels and the featured strip, with A's album featured — never name A's rows.
func TestAlbumAndLabelRoutes_foreignIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := isolationServer(t, pool)
	clientA := f.ClientFor(t, server, projecttest.SideA)
	client := f.ClientFor(t, server, projecttest.SideB)

	featured := f.Send(t, clientA, http.MethodPut, f.PathFor(projecttest.SideA, "albums/"+f.A.AlbumUID+"/featured"),
		map[string]any{})
	if featured.StatusCode != http.StatusOK {
		t.Fatalf("featuring A's album as A = %d, want 200", featured.StatusCode)
	}

	for _, c := range []struct{ kind, uid, prefix string }{
		{"albums", f.A.AlbumUID, "al"},
		{"labels", f.A.LabelUID, "lb"},
	} {
		path := f.PathFor(projecttest.SideB, c.kind+"/"+c.uid)
		f.AssertInvisible(t, client, path)
		projecttest.AssertSameShape(t, f.Get(t, client, path),
			f.Get(t, client, f.PathFor(projecttest.SideB, c.kind+"/"+projecttest.MissingUID(c.prefix))))
	}
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "albums"), f.A.AlbumUID, f.A.PhotoUID)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "labels"), f.A.LabelUID, f.A.PhotoUID)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "albums/featured"), f.A.AlbumUID, f.A.PhotoUID)
}

// TestAlbumWrites_foreignTargetIsMissing aims B's writes at A: adding A's photo
// to B's album (alone, or in a batch with one of B's own photos) is the 404 a
// missing photo gets and leaves the album unchanged; editing, deleting or
// featuring A's album and attaching A's label are the 404 a missing album or
// label gets, and A's rows survive.
func TestAlbumWrites_foreignTargetIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := isolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	extraB := insertPhoto(t, pool, f.B.ProjectUID)
	add := f.PathFor(projecttest.SideB, "albums/"+f.B.AlbumUID+"/photos")

	projecttest.AssertSameShape(t,
		f.Send(t, client, http.MethodPost, add, map[string]any{"photo_uids": []string{extraB, f.A.PhotoUID}}),
		f.Send(t, client, http.MethodPost, add,
			map[string]any{"photo_uids": []string{extraB, projecttest.MissingUID("ph")}}))
	refused := f.Send(t, client, http.MethodPost, add, map[string]any{"photo_uids": []string{f.A.PhotoUID}})
	_ = refused.Body.Close()
	if refused.StatusCode != http.StatusNotFound {
		t.Errorf("adding A's photo to B's album = %d, want 404", refused.StatusCode)
	}
	members, err := organize.NewStore(pool).ListPhotoUIDs(t.Context(), f.B.ProjectUID, f.B.AlbumUID)
	if err != nil || len(members) != 1 || members[0] != f.B.PhotoUID {
		t.Errorf("B's album after the refused adds = %v, %v; want only %s", members, err, f.B.PhotoUID)
	}

	missingAlbum := projecttest.MissingUID("al")
	for _, c := range []struct {
		method, suffix string
		body           any
	}{
		{http.MethodPatch, "", map[string]any{"title": "Cizí"}},
		{http.MethodDelete, "", nil},
		{http.MethodPut, "/featured", map[string]any{}},
		{http.MethodPost, "/photos", map[string]any{"photo_uids": []string{f.B.PhotoUID}}},
	} {
		projecttest.AssertSameShape(t,
			f.Send(t, client, c.method, f.PathFor(projecttest.SideB, "albums/"+f.A.AlbumUID+c.suffix), c.body),
			f.Send(t, client, c.method, f.PathFor(projecttest.SideB, "albums/"+missingAlbum+c.suffix), c.body))
	}
	projecttest.AssertSameShape(t,
		f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "labels/"+f.A.LabelUID+"/photos"),
			map[string]any{"photo_uid": f.B.PhotoUID}),
		f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "labels/"+projecttest.MissingUID("lb")+"/photos"),
			map[string]any{"photo_uid": f.B.PhotoUID}))

	ctx := t.Context()
	store := organize.NewStore(pool)
	if album, err := store.GetAlbumByUID(ctx, f.A.ProjectUID, f.A.AlbumUID); err != nil ||
		album.Title != "Album "+f.A.Token || album.FeaturedUntil != nil {
		t.Errorf("A's album after B's writes = %+v, %v; want untouched", album, err)
	}
	if members, err := store.ListPhotoUIDsByLabel(ctx, f.A.ProjectUID, f.A.LabelUID); err != nil || len(members) != 1 {
		t.Errorf("A's label after B's attach = %v, %v; want only A's photo", members, err)
	}
}

// insertPhoto inserts one more photo into the project and returns its uid.
func insertPhoto(t *testing.T, pool *pgxpool.Pool, projectUID string) string {
	t.Helper()
	uid := projecttest.MissingUID("ph")
	sum := sha256.Sum256([]byte(uid))
	if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, file_mime, file_width, file_height, taken_at)
		VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
		uid, projectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
		t.Fatalf("inserting a photo into %s: %v", projectUID, err)
	}
	return uid
}
