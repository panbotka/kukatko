//go:build integration

package system_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/system"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// seedRendition records that a video was encoded into one streaming quality.
// Every column the table CHECKs is filled with a plausible non-zero value; only
// the row's existence matters to the counts.
func seedRendition(t *testing.T, db *database.DB, photoUID, rendition string) {
	t.Helper()
	const stmt = `
INSERT INTO photo_hls_renditions (
    photo_uid, rendition, playlist, width, height,
    bandwidth, codecs, segment_count, duration_ms)
VALUES ($1, $2, '#EXTM3U', 1920, 1080, 5000000, 'avc1.640028,mp4a.40.2', 3, 12000)`
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, rendition); err != nil {
		t.Fatalf("seed rendition %s/%s: %v", photoUID, rendition, err)
	}
}

// seedEncodeJob inserts one `hls_transcode` queue row for a video in the given
// state, enqueued the given time ago. It writes the payload the real job carries,
// because that is what the aggregation joins on.
func seedEncodeJob(t *testing.T, db *database.DB, photoUID string, state jobs.State, ago time.Duration) {
	t.Helper()
	const stmt = `
INSERT INTO jobs (type, state, payload, created_at)
VALUES ($1, $2, jsonb_build_object('photo_uid', $3::text), now() - $4::interval)`
	_, err := db.Pool().Exec(t.Context(), stmt,
		jobs.TypeHLSTranscode, string(state), photoUID, ago.String())
	if err != nil {
		t.Fatalf("seed %s job for %s: %v", state, photoUID, err)
	}
}

// seedVideoEncoding builds a library that holds one video in every state the
// section reports:
//
//   - v1 is encoded into two qualities (and its finished job is still in the
//     queue table, which must not make it look outstanding);
//   - v2 is waiting, enqueued three hours ago — the oldest wait — and also carries
//     an older dead row, so the precedence between the two is exercised;
//   - v3 is being encoded right now;
//   - v4 was given up on (dead-lettered);
//   - v5 was never enqueued at all;
//   - v6 has a finished job but no rendition, which is the same silent stall as
//     v5: nothing is outstanding and nothing came of it;
//   - v7 is a video in the trash, encoded, and is counted by none of it;
//   - i1 is an image and is not a video at all.
func seedVideoEncoding(t *testing.T, db *database.DB) {
	t.Helper()
	for _, uid := range []string{"v1", "v2", "v3", "v4", "v5", "v6"} {
		seedPhoto(t, db, uid, "video", false)
	}
	seedPhoto(t, db, "v7", "video", true)
	seedPhoto(t, db, "i1", "image", false)

	seedRendition(t, db, "v1", "1080p")
	seedRendition(t, db, "v1", "720p")
	seedRendition(t, db, "v7", "1080p")

	seedEncodeJob(t, db, "v1", jobs.StateDone, 4*time.Hour)
	seedEncodeJob(t, db, "v2", jobs.StateDead, 6*time.Hour)
	seedEncodeJob(t, db, "v2", jobs.StateQueued, 3*time.Hour)
	seedEncodeJob(t, db, "v3", jobs.StateRunning, time.Minute)
	seedEncodeJob(t, db, "v4", jobs.StateDead, 2*time.Hour)
	seedEncodeJob(t, db, "v6", jobs.StateDone, time.Hour)
}

// TestCountDashboard_VideoEncoding asserts the video section against a fixture
// whose every state is known by construction, including that the four states of
// an unencoded video are disjoint and add back up to the missing count.
func TestCountDashboard_VideoEncoding(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedVideoEncoding(t, db)

	got, err := system.NewStore(db.Pool()).CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}

	want := system.Video{
		Videos:        6,
		Streamable:    1,
		Missing:       5,
		EncodeQueued:  1,
		EncodeRunning: 1,
		EncodeFailed:  1,
		NotScheduled:  2,
		// The archived video's rendition counts too: it occupies the store until
		// the trash is purged.
		Renditions: 3,
	}
	video := got.Video
	oldest := video.OldestQueuedAt
	video.OldestQueuedAt = nil
	if video != want {
		t.Errorf("video = %+v, want %+v", video, want)
	}
	if oldest == nil {
		t.Fatal("video.oldest_queued_at = nil, want the wait of the queued encode")
	}
	if waited := time.Since(*oldest); waited < 2*time.Hour || waited > 4*time.Hour {
		t.Errorf("oldest queued encode has waited %s, want about 3 h", waited)
	}
	// The store must never let the card and the backlog list disagree.
	if got.Remaining.VideosWithoutStreaming != want.Missing {
		t.Errorf("remaining.videos_without_streaming = %d, want %d",
			got.Remaining.VideosWithoutStreaming, want.Missing)
	}
	// Streaming being switched on is configuration; the store cannot know it.
	if got.Video.StreamingEnabled {
		t.Error("store reported streaming_enabled = true, want the service to stamp it")
	}
	if sum := video.EncodeQueued + video.EncodeRunning + video.EncodeFailed + video.NotScheduled; sum != video.Missing {
		t.Errorf("the four unencoded states sum to %d, want the missing count %d", sum, video.Missing)
	}
	if video.Streamable+video.Missing != video.Videos {
		t.Errorf("streamable + missing = %d, want the video count %d",
			video.Streamable+video.Missing, video.Videos)
	}
}

// TestCountDashboard_VideoEncodingEmptyLibrary verifies that a library with no
// videos at all reports zeroes and nothing waiting, rather than failing or
// reporting an age for a queue that does not exist.
func TestCountDashboard_VideoEncodingEmptyLibrary(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedPhoto(t, db, "i1", "image", false)

	got, err := system.NewStore(db.Pool()).CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}
	if got.Video != (system.Video{}) {
		t.Errorf("video = %+v, want all zeroes", got.Video)
	}
	if got.Remaining.VideosWithoutStreaming != 0 {
		t.Errorf("remaining.videos_without_streaming = %d, want 0",
			got.Remaining.VideosWithoutStreaming)
	}
}

// offlineEmbeddings is an EmbeddingHealth that reports the sidecar offline, so a
// full Collect needs no network.
type offlineEmbeddings struct{}

// Healthy always reports offline.
func (offlineEmbeddings) Healthy(context.Context) bool { return false }

// collectVideo runs a full Collect over the test database with streaming
// switched on or off, and returns the video section of the snapshot.
func collectVideo(t *testing.T, db *database.DB, streaming bool) system.Video {
	t.Helper()
	pool := db.Pool()
	svc := system.New(system.Config{
		DB:               db,
		Embeddings:       offlineEmbeddings{},
		Jobs:             jobs.NewStore(pool),
		Imports:          importer.NewStore(pool),
		Dashboard:        system.NewStore(pool),
		StreamingEnabled: streaming,
		OriginalsPath:    t.TempDir(),
		CachePath:        t.TempDir(),
	})
	status, err := svc.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return status.Video
}

// TestCollect_VideoStreamingEnabled verifies the snapshot carries the counted
// backlog with streaming switched on.
func TestCollect_VideoStreamingEnabled(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedVideoEncoding(t, db)

	video := collectVideo(t, db, true)
	if !video.StreamingEnabled {
		t.Error("video.streaming_enabled = false, want true")
	}
	if video.Missing != 5 || video.Streamable != 1 {
		t.Errorf("video = %+v, want 1 streamable / 5 missing", video)
	}
}

// TestCollect_VideoStreamingDisabled verifies that with streaming switched off
// the counts are still reported truthfully — five videos really do have no
// streaming version — but the flag says they are not a backlog, which is what
// stops the page showing an alarming number for an instance working exactly as
// configured.
func TestCollect_VideoStreamingDisabled(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedVideoEncoding(t, db)

	video := collectVideo(t, db, false)
	if video.StreamingEnabled {
		t.Error("video.streaming_enabled = true, want false")
	}
	if video.Missing != 5 {
		t.Errorf("video.missing = %d, want the honest count 5", video.Missing)
	}
}

// countingTracer counts the queries a pool executes, so a test can prove how
// many round trips an aggregation costs.
type countingTracer struct {
	queries int
}

// TraceQueryStart counts one query and leaves the context untouched.
func (c *countingTracer) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData,
) context.Context {
	c.queries++
	return ctx
}

// TraceQueryEnd does nothing; the count is taken at the start of each query.
func (c *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedStore returns a store over its own single-connection pool that counts
// every query it runs. The pool is closed when the test ends.
func tracedStore(t *testing.T, tracer *countingTracer) *system.Store {
	t.Helper()
	url := os.Getenv(dbtest.EnvTestDatabaseURL)
	if url == "" {
		t.Skipf("%s not set; skipping integration test", dbtest.EnvTestDatabaseURL)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parsing %s: %v", dbtest.EnvTestDatabaseURL, err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return system.NewStore(pool)
}

// TestCountDashboard_OneRoundTripPerVideoCount is the cost guard the video
// section had to be written against: the dashboard is one memoised round trip,
// and a per-video lookup (a video's renditions, a video's encode job) would turn
// a polled page into a query storm on a library of thousands of clips. It counts
// the queries the aggregation actually issues, first over a handful of videos and
// then over ten times as many: one either way.
func TestCountDashboard_OneRoundTripPerVideoCount(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedVideoEncoding(t, db)

	tracer := &countingTracer{}
	store := tracedStore(t, tracer)

	tracer.queries = 0
	if _, err := store.CountDashboard(t.Context()); err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}
	small := tracer.queries
	if small != 1 {
		t.Errorf("CountDashboard over 6 videos ran %d queries, want 1", small)
	}

	for i := range 60 {
		uid := "vb" + strconv.Itoa(i)
		seedPhoto(t, db, uid, "video", false)
		seedEncodeJob(t, db, uid, jobs.StateQueued, time.Minute)
	}

	tracer.queries = 0
	if _, err := store.CountDashboard(t.Context()); err != nil {
		t.Fatalf("CountDashboard over the larger library: %v", err)
	}
	if tracer.queries != small {
		t.Errorf("CountDashboard over 66 videos ran %d queries, want the same %d",
			tracer.queries, small)
	}
}
