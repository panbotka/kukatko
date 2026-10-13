//go:build integration

package photoapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the HTTP-level tenant-isolation tests of the face routes, over the
// real face-matching service: a member of B can neither draw a marker on A's
// photo nor point a marker of either project at the other project's rows, and
// the suggestions B's faces get never name a person of A, however close A's
// faces are.

// faceEnv is the server the face isolation tests drive, with the stores it is
// built over.
type faceEnv struct {
	server  *httptest.Server
	pool    *pgxpool.Pool
	people  *people.Store
	vectors *vectors.Store
	photos  *photos.Store
}

// newFaceEnv mounts the real auth API and the photo routes, with the real
// face-matching service and people store, under /api/v1/p/{project} the way the
// server does.
func newFaceEnv(t *testing.T) *faceEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	e := &faceEnv{
		pool: pool, people: people.NewStore(pool), vectors: vectors.NewStore(pool), photos: photos.NewStore(pool),
	}
	api := photoapi.NewAPI(photoapi.Config{
		Store:   e.photos,
		Storage: fs,
		Faces: facematch.New(facematch.Config{
			Photos: e.photos, Faces: e.vectors, People: e.people,
		}),
		Attacher:          e.people,
		Similar:           e.vectors,
		RequireAuth:       authAPI.RequireAuth,
		RequireCurator:    authAPI.RequireCurator,
		RequireWrite:      authAPI.RequireWrite,
		RequireAdmin:      authAPI.RequireAdmin,
		RequireMaintainer: authAPI.RequireMaintainer,
		RequireDownload:   authAPI.RequireAuthOrDownloadToken,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
		})
	})
	e.server = httptest.NewServer(r)
	t.Cleanup(e.server.Close)
	return e
}

// TestFaceRoutes_foreignRowsAreMissingAndUntouched aims the face routes at A's
// rows through B's prefix: reading A's faces, drawing a marker on A's photo,
// assigning A's marker and assigning A's subject are each answered exactly like
// the same request naming a row that does not exist, and A's photo still carries
// only its seeded marker, still naming A's subject.
func TestFaceRoutes_foreignRowsAreMissingAndUntouched(t *testing.T) {
	e := newFaceEnv(t)
	f := projecttest.Seed(t, e.pool)
	client := f.ClientFor(t, e.server, projecttest.SideB)
	ctx := t.Context()

	readA := f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+"/faces")
	f.AssertInvisible(t, client, readA)
	projecttest.AssertSameShape(t, f.Get(t, client, readA),
		f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+"/faces")))

	box := []float64{0.6, 0.6, 0.2, 0.2}
	cases := []struct {
		name            string
		photo           string
		body, missingAs map[string]any
		missingPhoto    bool
	}{
		{
			name: "create a marker on A's photo", photo: f.A.PhotoUID, missingPhoto: true,
			body: map[string]any{"action": facematch.ActionCreateMarker, "bbox": box, "subject_uid": f.B.SubjectUID},
		},
		{
			name: "assign A's marker", photo: f.B.PhotoUID,
			body: map[string]any{
				"action": facematch.ActionAssignPerson, "marker_uid": f.A.MarkerUID, "subject_uid": f.B.SubjectUID,
			},
			missingAs: map[string]any{
				"action":     facematch.ActionAssignPerson,
				"marker_uid": projecttest.MissingUID("mk"), "subject_uid": f.B.SubjectUID,
			},
		},
		{
			name: "assign A's subject to B's marker", photo: f.B.PhotoUID,
			body: map[string]any{
				"action": facematch.ActionAssignPerson, "marker_uid": f.B.MarkerUID, "subject_uid": f.A.SubjectUID,
			},
			missingAs: map[string]any{
				"action":     facematch.ActionAssignPerson,
				"marker_uid": f.B.MarkerUID, "subject_uid": projecttest.MissingUID("su"),
			},
		},
		{
			name: "unassign A's marker", photo: f.B.PhotoUID,
			body: map[string]any{"action": facematch.ActionUnassignPerson, "marker_uid": f.A.MarkerUID},
			missingAs: map[string]any{
				"action": facematch.ActionUnassignPerson, "marker_uid": projecttest.MissingUID("mk"),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := f.PathFor(projecttest.SideB, "photos/"+c.photo+"/faces/assign")
			foreign := f.Send(t, client, http.MethodPost, path, c.body)
			if foreign.StatusCode != http.StatusNotFound {
				t.Errorf("%s = %d, want 404", c.name, foreign.StatusCode)
			}
			missingPath, missingBody := path, c.missingAs
			if c.missingPhoto {
				missingPath = f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+"/faces/assign")
				missingBody = c.body
			}
			projecttest.AssertSameShape(t, foreign, f.Send(t, client, http.MethodPost, missingPath, missingBody))
		})
	}

	markers, err := e.people.ListMarkersByPhoto(ctx, f.A.ProjectUID, f.A.PhotoUID)
	if err != nil {
		t.Fatalf("listing A's markers: %v", err)
	}
	if len(markers) != 1 || markers[0].UID != f.A.MarkerUID ||
		markers[0].SubjectUID == nil || *markers[0].SubjectUID != f.A.SubjectUID {
		t.Errorf("A's photo now carries %+v, want only its seeded marker naming %s", markers, f.A.SubjectUID)
	}
	ownB, err := e.people.ListMarkersByPhoto(ctx, f.B.ProjectUID, f.B.PhotoUID)
	if err != nil || len(ownB) != 1 || ownB[0].SubjectUID == nil || *ownB[0].SubjectUID != f.B.SubjectUID {
		t.Errorf("B's photo now carries %+v (%v), want only its seeded marker naming %s", ownB, err, f.B.SubjectUID)
	}
}

// TestFaceSuggestions_neverNameAnotherProjectsPerson gives B's photo an unnamed
// face whose embedding is identical to A's named face. B's faces view must not
// suggest A's person — while an unnamed twin face inside A does get A's person
// suggested, which proves the suggestion path works and the silence in B is the
// project filter.
func TestFaceSuggestions_neverNameAnotherProjectsPerson(t *testing.T) {
	e := newFaceEnv(t)
	f := projecttest.Seed(t, e.pool)
	ctx := t.Context()

	vec := make([]float32, vectors.FaceDim)
	vec[1], vec[9] = 1, 0.5
	named := vectors.Face{
		FaceIndex: 0, Vector: vec, BBox: [4]float64{0.25, 0.25, 0.2, 0.2}, DetScore: 0.9, Model: "buffalo_l",
		MarkerUID: &f.A.MarkerUID, SubjectUID: &f.A.SubjectUID, SubjectName: "Osoba " + f.A.Token,
	}
	if err := e.vectors.SaveFaces(ctx, f.A.PhotoUID, []vectors.Face{named}); err != nil {
		t.Fatalf("saving A's named face: %v", err)
	}
	unnamed := vectors.Face{FaceIndex: 0, Vector: vec, BBox: [4]float64{0.7, 0.7, 0.1, 0.1}, DetScore: 0.9}
	if err := e.vectors.SaveFaces(ctx, f.B.PhotoUID, []vectors.Face{unnamed}); err != nil {
		t.Fatalf("saving B's face: %v", err)
	}
	twin, err := e.photos.Create(ctx, photos.Photo{
		FileHash: "isolation-twin", FilePath: "2026/10/twin.jpg", FileName: "twin.jpg", FileWidth: 640, FileHeight: 480,
	})
	if err != nil {
		t.Fatalf("creating A's twin photo: %v", err)
	}
	if err := e.vectors.SaveFaces(ctx, twin.UID, []vectors.Face{unnamed}); err != nil {
		t.Fatalf("saving the twin face: %v", err)
	}

	clientB := f.ClientFor(t, e.server, projecttest.SideB)
	f.AssertListExcludes(t, clientB, f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID+"/faces"), f.A.SubjectUID)

	clientA := f.ClientFor(t, e.server, projecttest.SideA)
	resp := f.Get(t, clientA, f.PathFor(projecttest.SideA, "photos/"+twin.UID+"/faces"))
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading A's faces: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), f.A.SubjectUID) {
		t.Errorf("A's twin face view = %d %s, want a suggestion of %s", resp.StatusCode, body, f.A.SubjectUID)
	}
}
