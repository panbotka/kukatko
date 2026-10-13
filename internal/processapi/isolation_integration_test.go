//go:build integration

package processapi_test

import (
	"context"
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
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
)

// newIsolationServer mounts the real auth API and the processing routes under
// /api/v1/p/{project} the way the server does, with the real thumbnail
// backfill over the real catalogue and queue behind /process/thumbnails.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	photoStore := photos.NewStore(pool)
	thumbs := thumbjob.New(thumbjob.Config{
		Photos: photoStore, Thumbnailer: thumb.New(fs, t.TempDir()),
		Decoder: thumbjob.NewStorageDecoder(fs, 0, nil),
		Lister:  photoStore, Enqueuer: jobs.NewEnqueuer(jobs.NewStore(pool)),
	})
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := processapi.NewAPI(processapi.Config{
		Backfiller: noEmbeddings{}, FaceBackfiller: noFaces{},
		ThumbnailBackfiller: thumbs, RequireMaintainer: authAPI.RequireMaintainer,
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

// TestBackfill_schedulesOnlyItsProject runs the thumbnail backfill through B's
// project prefix while both projects' photos lack their thumbnails: the dry
// run counts B's photo only and the real run queues a job for B's photo and
// none for A's.
func TestBackfill_schedulesOnlyItsProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, projecttest.AsMaintainer(projecttest.SideB))
	server := newIsolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)

	dry := backfill(t, f, client, "process/thumbnails?dry_run=true")
	if dry.Pending != 1 || dry.Enqueued != 0 {
		t.Errorf("B's dry run = %+v, want 1 pending (B's photo only) and nothing queued", dry)
	}
	run := backfill(t, f, client, "process/thumbnails")
	if run.Enqueued != 1 {
		t.Errorf("B's backfill queued %d jobs, want 1", run.Enqueued)
	}

	rows, err := pool.Query(t.Context(), `SELECT payload ->> 'photo_uid' FROM jobs WHERE type = $1`, jobs.TypeThumbnail)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	defer rows.Close()
	var queued []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatalf("scanning the queue: %v", err)
		}
		queued = append(queued, uid)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the queue: %v", err)
	}
	if len(queued) != 1 || queued[0] != f.B.PhotoUID {
		t.Errorf("queued thumbnail jobs for %v, want only B's photo %s (never A's %s)",
			queued, f.B.PhotoUID, f.A.PhotoUID)
	}
}

// countedBody is the JSON answer of a counted backfill.
type countedBody struct {
	Enqueued int  `json:"enqueued"`
	Pending  int  `json:"pending"`
	DryRun   bool `json:"dry_run"`
}

// backfill POSTs suffix inside B's project and decodes the 200 answer.
func backfill(t *testing.T, f *projecttest.Fixture, client *http.Client, suffix string) countedBody {
	t.Helper()
	resp := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, suffix), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", suffix, resp.StatusCode)
	}
	var body countedBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	return body
}

// noEmbeddings is a Backfiller this test never calls.
type noEmbeddings struct{}

// BackfillEmbeddings schedules nothing.
func (noEmbeddings) BackfillEmbeddings(_ context.Context, _ string) (int, error) { return 0, nil }

// noFaces is a FaceBackfiller this test never calls.
type noFaces struct{}

// BackfillFaces schedules nothing.
func (noFaces) BackfillFaces(_ context.Context, _ string) (facejob.BackfillResult, error) {
	return facejob.BackfillResult{}, nil
}
