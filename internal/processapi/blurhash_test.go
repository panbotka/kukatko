package processapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeBlurhashBackfiller models the placeholder backfill over an in-memory queue,
// exactly as fakeThumbnailBackfiller models the thumbnail one: it "enqueues" a job
// per candidate uid, deduping against jobs already pending, and records what it
// was asked.
type fakeBlurhashBackfiller struct {
	missing []string
	active  []string
	pending map[string]bool
	calls   int
	counts  int
	lastAll bool
	err     error
}

// newFakeBlurhashBackfiller returns a fake seeded with the candidate uids of each
// predicate.
func newFakeBlurhashBackfiller(missing, active []string) *fakeBlurhashBackfiller {
	return &fakeBlurhashBackfiller{missing: missing, active: active, pending: map[string]bool{}}
}

// BackfillBlurhash schedules the appropriate candidate set and returns how many
// candidates it iterated.
func (f *fakeBlurhashBackfiller) BackfillBlurhash(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	candidates := f.candidates(all)
	for _, uid := range candidates {
		f.pending[uid] = true
	}
	return len(candidates), nil
}

// CountBackfillBlurhash reports how many candidates the same call would cover,
// scheduling nothing.
func (f *fakeBlurhashBackfiller) CountBackfillBlurhash(_ context.Context, all bool) (int, error) {
	f.counts++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	return len(f.candidates(all)), nil
}

// candidates returns the uid set the given flag selects.
func (f *fakeBlurhashBackfiller) candidates(all bool) []string {
	if all {
		return f.active
	}
	return f.missing
}

// newServerWithBlurhash mounts the API with the given placeholder backfiller (the
// others are stubbed) behind the given admin guard.
func newServerWithBlurhash(
	t *testing.T, bb BlurhashBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		BlurhashBackfiller: bb, RequireMaintainer: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestBackfillBlurhash_ok schedules the photos with no placeholder and reports
// both what it scheduled and how many matched.
func TestBackfillBlurhash_ok(t *testing.T) {
	t.Parallel()

	bb := newFakeBlurhashBackfiller([]string{"p1", "p2"}, []string{"p1", "p2", "p3"})
	srv := newServerWithBlurhash(t, bb, passthrough)

	resp := postProcess(t, srv.URL+"/process/blurhash")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body countedBackfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 2 || body.Pending != 2 || body.DryRun {
		t.Errorf("body = %+v, want enqueued 2, pending 2, dry_run false", body)
	}
	if bb.calls != 1 || bb.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", bb.calls, bb.lastAll)
	}
}

// TestBackfillBlurhash_all verifies ?all=true switches to the forced full re-run,
// which re-encodes placeholders the library already has.
func TestBackfillBlurhash_all(t *testing.T) {
	t.Parallel()

	bb := newFakeBlurhashBackfiller([]string{"p1"}, []string{"p1", "p2", "p3"})
	srv := newServerWithBlurhash(t, bb, passthrough)

	resp := postProcess(t, srv.URL+"/process/blurhash?all=true")
	defer func() { _ = resp.Body.Close() }()
	var body countedBackfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 3 || body.Pending != 3 {
		t.Errorf("body = %+v, want the active listing (3)", body)
	}
	if !bb.lastAll {
		t.Error("the all flag did not reach the backfiller")
	}
}

// TestBackfillBlurhash_dryRun verifies ?dry_run=true reports the size of the run
// without starting it.
func TestBackfillBlurhash_dryRun(t *testing.T) {
	t.Parallel()

	bb := newFakeBlurhashBackfiller([]string{"p1", "p2", "p3"}, nil)
	srv := newServerWithBlurhash(t, bb, passthrough)

	resp := postProcess(t, srv.URL+"/process/blurhash?dry_run=true")
	defer func() { _ = resp.Body.Close() }()
	var body countedBackfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Pending != 3 || body.Enqueued != 0 || !body.DryRun {
		t.Errorf("body = %+v, want pending 3, enqueued 0, dry_run true", body)
	}
	if bb.calls != 0 {
		t.Errorf("a dry run scheduled %d backfill call(s), want none", bb.calls)
	}
	if len(bb.pending) != 0 {
		t.Errorf("a dry run queued %d job(s), want none", len(bb.pending))
	}
}

// TestBackfillBlurhash_unavailable verifies the endpoint answers 503 rather than
// panicking when no placeholder backfiller is wired.
func TestBackfillBlurhash_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithBlurhash(t, nil, passthrough)
	resp := postProcess(t, srv.URL+"/process/blurhash")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillBlurhash_requiresMaintainer verifies the route sits behind the
// maintainer guard like every other processing endpoint.
func TestBackfillBlurhash_requiresMaintainer(t *testing.T) {
	t.Parallel()

	bb := newFakeBlurhashBackfiller([]string{"p1"}, nil)
	srv := newServerWithBlurhash(t, bb, forbid)
	resp := postProcess(t, srv.URL+"/process/blurhash")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if bb.calls != 0 || bb.counts != 0 {
		t.Errorf("a forbidden call reached the backfiller (%d runs, %d counts)", bb.calls, bb.counts)
	}
}
