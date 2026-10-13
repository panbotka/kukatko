//go:build integration

package placesapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/placesapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// newIsolationServer mounts the real auth API and the places route under
// /api/v1/p/{project} the way the server does.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := placesapi.NewAPI(placesapi.Config{Store: photos.NewStore(pool), RequireAuth: authAPI.RequireAuth})
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

// cityOf names each side's city; both sides share the country, so only the
// project predicate can keep the cities apart.
var cityOf = map[projecttest.Side]string{projecttest.SideA: "Veselice", projecttest.SideB: "Kotrzina"}

// TestPlaces_treeNeverCrossesProjects caches a place for each side's photo in
// the same country and lists B's tree: B's country counts B's photo only and
// names B's city, never A's. The backfill source behind it — the geotagged
// photos still missing a place — is scoped the same way.
func TestPlaces_treeNeverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	placeStore := places.NewStore(pool)
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		if _, err := placeStore.SavePlace(t.Context(), places.Place{
			PhotoUID: f.Tenant(side).PhotoUID, Country: "Česko", City: cityOf[side],
		}); err != nil {
			t.Fatalf("caching %s's place: %v", side, err)
		}
	}

	server := newIsolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	resp := f.Get(t, client, f.PathFor(projecttest.SideB, "places"))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET places = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Places []photos.CountryPlaces `json:"places"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Places) != 1 || body.Places[0].Count != 1 || len(body.Places[0].Cities) != 1 ||
		body.Places[0].Cities[0].City != cityOf[projecttest.SideB] || body.Places[0].CoverUID != f.B.PhotoUID {
		t.Errorf("B's place tree = %+v, want one country holding only %s with B's photo",
			body.Places, cityOf[projecttest.SideB])
	}

	store := photos.NewStore(pool)
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		tree, err := store.AggregatePlaces(t.Context(), projectUID, "")
		var covers []string
		for _, country := range tree {
			covers = append(covers, country.CoverUID)
		}
		return covers, err
	})

	// The backfill source: give both photos coordinates and drop the cached
	// places, and each project's listing names its own photo only.
	if _, err := pool.Exec(t.Context(),
		`UPDATE photos SET lat = 49.4, lng = 16.1 WHERE uid = ANY($1)`, []string{f.A.PhotoUID, f.B.PhotoUID}); err != nil {
		t.Fatalf("geotagging the photos: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM photo_places`); err != nil {
		t.Fatalf("dropping the cached places: %v", err)
	}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		return placeStore.ListPhotosMissingPlaces(t.Context(), projectUID, 0)
	})
}
