//go:build integration

package people_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// coverTracer counts the statements a pool issues, so the batch cover lookup can
// be held to its promise: one query for a whole page of people, not one per row.
type coverTracer struct {
	queries atomic.Int64
}

// TraceQueryStart counts one statement and passes the context through.
func (c *coverTracer) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData,
) context.Context {
	c.queries.Add(1)
	return ctx
}

// TraceQueryEnd is required by pgx.QueryTracer and has nothing to record.
func (c *coverTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedStore returns a second people.Store over a pool of its own that counts
// the statements it runs, for the N+1 assertion below.
func tracedStore(t *testing.T) (*people.Store, *coverTracer) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv(dbtest.EnvTestDatabaseURL))
	if err != nil {
		t.Fatalf("parsing test DSN: %v", err)
	}
	tracer := &coverTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("opening traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return people.NewStore(pool), tracer
}

// june returns noon on the given day of June 2024, so a test can order the
// photos a "newest photo" rule has to choose between.
func june(day int) time.Time {
	return time.Date(2024, time.June, day, 12, 0, 0, 0, time.UTC)
}

// markPerson puts a valid face marker for subjectUID on photoUID.
func markPerson(t *testing.T, store *people.Store, photoUID, subjectUID string) {
	t.Helper()
	if _, err := store.CreateMarker(context.Background(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	}); err != nil {
		t.Fatalf("CreateMarker on %s: %v", photoUID, err)
	}
}

// TestSubjectCovers verifies a person's cover is the newest visible photo they
// appear on, that a cover chosen by hand wins over it, and that a person on no
// photo is absent from the map rather than present with an empty cover.
func TestSubjectCovers(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	older := makeDatedPhoto(t, photoStore, "su-old", june(1))
	newer := makeDatedPhoto(t, photoStore, "su-new", june(2))
	seen, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markPerson(t, store, older, seen.UID)
	markPerson(t, store, newer, seen.UID)
	unseen, err := store.CreateSubject(ctx, people.Subject{Name: "Nobody"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	picked, err := store.CreateSubject(ctx, people.Subject{Name: "Chosen", CoverPhotoUID: &older})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markPerson(t, store, newer, picked.UID)

	covers, err := store.SubjectCovers(ctx, []string{seen.UID, unseen.UID, picked.UID, "su-nothing"})
	if err != nil {
		t.Fatalf("SubjectCovers: %v", err)
	}
	if got := covers[seen.UID]; got.PhotoUID != newer || got.FileHash != "su-new" {
		t.Errorf("cover = %+v, want the newest photo %s / su-new", got, newer)
	}
	if got := covers[picked.UID].PhotoUID; got != older {
		t.Errorf("chosen cover = %q, want the hand-picked %s", got, older)
	}
	if _, ok := covers[unseen.UID]; ok {
		t.Errorf("person on no photo has a cover %+v, want none", covers[unseen.UID])
	}
	if _, ok := covers["su-nothing"]; ok {
		t.Error("a uid naming no subject came back with a cover")
	}
}

// TestSubjectCovers_skipsRejectedAndHidden verifies a cover never comes from a
// marker the user rejected, nor from a photo the person's own gallery would not
// show.
func TestSubjectCovers_skipsRejectedAndHidden(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := context.Background()

	kept := makeDatedPhoto(t, photoStore, "sk-keep", june(1))
	rejected := makeDatedPhoto(t, photoStore, "sk-bad", june(2))
	hidden := makeDatedPhoto(t, photoStore, "sk-hid", june(3))
	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markPerson(t, store, kept, subject.UID)
	markPerson(t, store, hidden, subject.UID)
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET hidden_from_library = true WHERE uid = $1", hidden); err != nil {
		t.Fatalf("hiding %s: %v", hidden, err)
	}
	marker, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: rejected, SubjectUID: &subject.UID,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	if _, err := store.SetMarkerInvalid(ctx, marker.UID, true); err != nil {
		t.Fatalf("SetMarkerInvalid: %v", err)
	}

	covers, err := store.SubjectCovers(ctx, []string{subject.UID})
	if err != nil {
		t.Fatalf("SubjectCovers: %v", err)
	}
	if got := covers[subject.UID].PhotoUID; got != kept {
		t.Errorf("cover = %q, want the only usable photo %s", got, kept)
	}
}

// TestSubjectCovers_oneQueryPerBatch verifies a page of people costs one query,
// not one per person — the reason the method takes a slice at all.
func TestSubjectCovers_oneQueryPerBatch(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	photoUID := makeDatedPhoto(t, photoStore, "nq-one", june(1))
	uids := make([]string, 0, 5)
	for i := range 5 {
		subject, err := store.CreateSubject(ctx, people.Subject{Name: "Person " + string(rune('a'+i))})
		if err != nil {
			t.Fatalf("CreateSubject: %v", err)
		}
		markPerson(t, store, photoUID, subject.UID)
		uids = append(uids, subject.UID)
	}

	traced, tracer := tracedStore(t)
	// Run once to warm the connection — pgx loads its type information lazily,
	// and those statements are not the ones under test — then count the second.
	if _, err := traced.SubjectCovers(ctx, uids); err != nil {
		t.Fatalf("SubjectCovers warm-up: %v", err)
	}
	tracer.queries.Store(0)
	if _, err := traced.SubjectCovers(ctx, uids); err != nil {
		t.Fatalf("SubjectCovers: %v", err)
	}
	if got := tracer.queries.Load(); got != 1 {
		t.Errorf("covers for 5 people ran %d queries, want 1", got)
	}
}
