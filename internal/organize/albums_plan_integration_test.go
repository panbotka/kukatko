//go:build integration

package organize_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
)

// The album index is the one listing whose cost is easy to get wrong: it derives
// a per-album value (the fallback cover) over a table that dwarfs the albums. A
// handful of development photos cannot show the difference between a per-album
// probe and a single pass — both answer instantly — so this fixture reproduces
// the production shape instead: ~20 000 photos, 437 albums and ~40 000
// memberships, each album a contiguous slice of the timeline. That skew is what
// matters. An album full of 2011 photos is the slow case for a correlated
// "newest first, LIMIT 1" probe, because the planner walks the global capture-time
// order from the newest photo in the library down to that album's first hit.
const (
	// planPhotos is how many photos the fixture catalogues.
	planPhotos = 20000
	// planAlbums is how many albums it creates.
	planAlbums = 437
	// planAlbumSize is how many photos each album holds (~93, the production
	// average of 40 459 memberships over 437 albums).
	planAlbumSize = 93
	// planAlbumStride is how far the next album's slice starts into the timeline.
	// It is smaller than planAlbumSize, so consecutive albums overlap the way real
	// ones do, and small enough that the last album's slice stays inside the
	// catalogue.
	planAlbumStride = 44
	// planBufferBudget bounds the plan's buffer hits as a multiple of the heap
	// pages of the tables it must read. Reading photos, album_photos and albums
	// once each is the honest cost of the answer; the multiple leaves room for
	// index pages and a different-but-sane plan. A per-album probe overshoots this
	// by three orders of magnitude, which is the regression being fenced off.
	planBufferBudget = 8
	// planReferenceSample is how many albums are checked against the reference
	// cover definition. Every album would mean 437 correlated probes, which is the
	// slow shape this test exists to keep out.
	planReferenceSample = 5
)

// seedPlanFixtureSQL builds the production-shaped library in three set-based
// statements. Photos are dated seven hours apart from 2010 onwards, so a photo's
// ordinal is also its position in the timeline; every 97th has no capture time,
// every 50th is archived and every 60th is a non-primary stack member, so the
// listing's visibility predicate has something to exclude in every album.
const seedPlanFixtureSQL = `
INSERT INTO photos (uid, file_hash, file_path, file_name, taken_at, archived_at,
                    stack_uid, stack_primary)
SELECT
    'ph' || lpad(i::text, 10, '0'),
    lpad(i::text, 64, '0'),
    '2024/01/' || i || '.jpg',
    i || '.jpg',
    CASE WHEN i %% 97 = 0 THEN NULL
         ELSE TIMESTAMPTZ '2010-01-01 00:00:00+00' + (i * INTERVAL '7 hours') END,
    CASE WHEN i %% 50 = 0 THEN now() ELSE NULL END,
    CASE WHEN i %% 60 = 0 THEN 'st' || lpad(i::text, 10, '0') ELSE NULL END,
    false
FROM generate_series(1, %d) AS i;

INSERT INTO albums (uid, slug, title)
SELECT 'al' || lpad(j::text, 10, '0'), 'album-' || j, 'Album ' || j
FROM generate_series(1, %d) AS j;

INSERT INTO album_photos (album_uid, photo_uid)
SELECT 'al' || lpad(j::text, 10, '0'), 'ph' || lpad(((j - 1) * %d + k)::text, 10, '0')
FROM generate_series(1, %d) AS j, generate_series(1, %d) AS k;

ANALYZE photos;
ANALYZE albums;
ANALYZE album_photos;`

// referenceCoverSQL is the album's fallback cover written the obvious way: its
// newest visible photo, an unknown capture time last, uid breaking ties. It is
// the contract in one statement, and it is deliberately the shape ListAlbums must
// NOT use for every album at once — for a single album it is cheap and it makes
// an independent oracle the listing is checked against.
const referenceCoverSQL = `
SELECT ap.photo_uid
FROM album_photos ap
JOIN photos p ON p.uid = ap.photo_uid
WHERE ap.album_uid = $1 AND p.archived_at IS NULL
  AND (p.stack_uid IS NULL OR p.stack_primary)
ORDER BY p.taken_at DESC NULLS LAST, ap.photo_uid
LIMIT 1`

// heapPagesSQL sums the heap pages of the three tables the album index reads.
const heapPagesSQL = `
SELECT (pg_relation_size('photos') + pg_relation_size('album_photos')
        + pg_relation_size('albums')) / current_setting('block_size')::bigint`

// explainNode is the subset of one EXPLAIN (FORMAT JSON) plan node this test
// reads. The block counters are cumulative over a node's subtree, so the root
// node carries the whole statement's totals.
type explainNode struct {
	NodeType         string `json:"Node Type"`
	SharedHitBlocks  int64  `json:"Shared Hit Blocks"`
	SharedReadBlocks int64  `json:"Shared Read Blocks"`
}

// explainResult is one entry of the EXPLAIN (FORMAT JSON) array.
type explainResult struct {
	Plan          explainNode `json:"Plan"`
	ExecutionTime float64     `json:"Execution Time"`
}

// TestAlbumListPlanStaysProportionalToMemberships is the regression test for the
// album index's cost. On a production-shaped library it asserts that the whole
// statement reads no more than a small multiple of the heap it must cover, which
// is the property that separates a single pass over the memberships from a
// per-album walk of the library — and it checks, on the same data, that the cover
// the listing returns is still the album's newest visible photo.
//
// The assertion is a plan property on purpose. A wall-clock threshold would be
// flaky on a shared machine, and it would not say what actually went wrong; buffer
// hits are deterministic for a given plan and are exactly what blew up in
// production (17.3M of them to produce 437 rows).
func TestAlbumListPlanStaysProportionalToMemberships(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	seedPlanFixture(t, db)

	list, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(list) != planAlbums {
		t.Fatalf("ListAlbums len = %d, want %d", len(list), planAlbums)
	}

	assertCoversMatchReference(t, db, list)
	assertPlanWithinBudget(t, db)

	dbtest.TruncateAll(t, db)
}

// seedPlanFixture writes the production-shaped library and analyses it, so the
// planner works from real statistics rather than from the defaults it assumes for
// a never-analysed table.
func seedPlanFixture(t *testing.T, db *database.DB) {
	t.Helper()
	stmt := fmt.Sprintf(seedPlanFixtureSQL,
		planPhotos, planAlbums, planAlbumStride, planAlbums, planAlbumSize)
	if _, err := db.Pool().Exec(t.Context(), stmt); err != nil {
		t.Fatalf("seeding the plan fixture: %v", err)
	}
}

// assertCoversMatchReference checks a sample of the listed albums against the
// reference definition of the fallback cover, so a cheaper plan cannot quietly
// return a different photo. The sample is spread across the listing rather than
// taken from its head: the albums at the end are the ones whose newest photo is
// oldest, which is where an ordering mistake would hide.
func assertCoversMatchReference(t *testing.T, db *database.DB, list []organize.AlbumSummary) {
	t.Helper()
	step := max(len(list)/planReferenceSample, 1)
	for i := 0; i < len(list); i += step {
		album := list[i]
		want := referenceCover(t, db, album.UID)
		got := ""
		if album.CoverUID != nil {
			got = *album.CoverUID
		}
		if got != want {
			t.Errorf("album %s (position %d) cover = %q, want %q", album.UID, i, got, want)
		}
	}
}

// referenceCover returns the album's fallback cover computed the obvious way, or
// "" when the album has no visible photo.
func referenceCover(t *testing.T, db *database.DB, albumUID string) string {
	t.Helper()
	var uid string
	err := db.Pool().QueryRow(t.Context(), referenceCoverSQL, albumUID).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("reference cover for %s: %v", albumUID, err)
	}
	return uid
}

// assertPlanWithinBudget runs the real album-index statement under EXPLAIN
// (ANALYZE, BUFFERS) and fails when it reads more than planBufferBudget times the
// heap pages of the tables involved. It logs the measured figures either way, so a
// run of this test also answers "what does the album index cost now".
func assertPlanWithinBudget(t *testing.T, db *database.DB) {
	t.Helper()
	pages := heapPages(t, db)
	plan := explainAlbumIndex(t, db)
	blocks := plan.Plan.SharedHitBlocks + plan.Plan.SharedReadBlocks
	budget := int64(planBufferBudget) * pages

	t.Logf("album index over %d photos / %d albums / ~%d memberships: %.0f ms, "+
		"%d shared blocks (%d heap pages, budget %d), root node %s",
		planPhotos, planAlbums, planAlbums*planAlbumSize,
		plan.ExecutionTime, blocks, pages, budget, plan.Plan.NodeType)

	if blocks > budget {
		t.Errorf("album index read %d shared blocks over %d heap pages (budget %d): "+
			"the plan is no longer proportional to the memberships — has a per-album "+
			"lookup crept back in?", blocks, pages, budget)
	}
}

// heapPages returns the combined heap size, in pages, of the tables the album
// index reads. It is the yardstick the buffer budget is expressed in, so the
// budget follows the fixture instead of hard-coding a number that silently stops
// meaning anything when the fixture changes.
func heapPages(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var pages int64
	if err := db.Pool().QueryRow(t.Context(), heapPagesSQL).Scan(&pages); err != nil {
		t.Fatalf("measuring heap pages: %v", err)
	}
	if pages == 0 {
		t.Fatal("heap pages = 0; the fixture did not land")
	}
	return pages
}

// explainAlbumIndex runs the album-index statement under EXPLAIN (ANALYZE,
// BUFFERS, FORMAT JSON) and returns the parsed plan.
func explainAlbumIndex(t *testing.T, db *database.DB) explainResult {
	t.Helper()
	var raw []byte
	err := db.Pool().QueryRow(t.Context(),
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+organize.ListAlbumsSQL,
		organize.CoverCandidates).Scan(&raw)
	if err != nil {
		t.Fatalf("explaining the album index: %v", err)
	}
	var results []explainResult
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("parsing the plan: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("plan has %d entries, want 1", len(results))
	}
	return results[0]
}
