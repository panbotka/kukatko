//go:build integration

package comments_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
)

// TestLatestAmong verifies the bulk "newest comment per photo" read: the newest
// live comment wins, a soft-deleted one is invisible even when it is the newest,
// a photo without a live comment is absent from the map, and the author's name
// rides along so a ledger row needs no second lookup.
func TestLatestAmong(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	bob := f.makeUser(t, "us_bob", "bob", "")
	first := f.makePhoto(t, "one")
	second := f.makePhoto(t, "two")
	third := f.makePhoto(t, "three")
	onlyDeleted := f.makePhoto(t, "four")

	f.mustCreate(t, first.UID, alice, "older")
	newest := f.mustCreate(t, first.UID, bob, "newest\nsecond line")
	gone := f.mustCreate(t, first.UID, alice, "deleted after the newest")
	f.mustCreate(t, second.UID, alice, "only one")
	lone := f.mustCreate(t, onlyDeleted.UID, alice, "will be deleted")
	for _, c := range []comments.Comment{gone, lone} {
		if err := f.comments.Delete(context.Background(), c.UID,
			entry(audit.ActionCommentDelete, alice, c.PhotoUID)); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}

	latest, err := f.comments.LatestAmong(context.Background(), comments.SubjectPhoto,
		[]string{first.UID, second.UID, third.UID, onlyDeleted.UID, "ph_missing"})
	if err != nil {
		t.Fatalf("LatestAmong: %v", err)
	}
	if got := latest[first.UID]; got.UID != newest.UID || got.Body != "newest\nsecond line" {
		t.Errorf("latest(%s) = %+v, want the newest live comment %s", first.UID, got, newest.UID)
	}
	if got := latest[first.UID]; got.AuthorUID != bob || got.AuthorName != "bob" {
		t.Errorf("latest(%s) author = %q/%q, want bob resolved to its username", first.UID,
			got.AuthorUID, got.AuthorName)
	}
	if got := latest[second.UID]; got.Body != "only one" || got.AuthorName != "Alice A." {
		t.Errorf("latest(%s) = %+v, want the single comment by Alice A.", second.UID, got)
	}
	for _, uid := range []string{third.UID, onlyDeleted.UID, "ph_missing"} {
		if _, ok := latest[uid]; ok {
			t.Errorf("%s is present in the map, want it absent (no live comment)", uid)
		}
	}
	if len(latest) != 2 {
		t.Errorf("latest = %v, want only the two commented photos", latest)
	}

	empty, err := f.comments.LatestAmong(context.Background(), comments.SubjectPhoto, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("LatestAmong(nil) = %v, %v, want an empty map and no error", empty, err)
	}
	if _, err := f.comments.LatestAmong(context.Background(), comments.SubjectKind("nope"),
		[]string{first.UID}); err == nil {
		t.Error("LatestAmong(unknown kind) = nil error, want ErrInvalidSubject")
	}
}

// TestLatestAmong_isIndexBacked verifies the grouped read is served by the
// partial (photo_uid, created_at) index rather than a scan of the whole table.
// The seed is deliberately shaped like a large library and analysed: tens of
// thousands of photographs with a few comments each, of which one page asks
// about a hundred. On a small table the planner prefers a sequential scan on
// its merits — a hundred index probes cost more than reading a few hundred
// pages — and the assertion would prove nothing; the point is that the index
// is there and usable once the thread table has grown past that.
func TestLatestAmong_isIndexBacked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pool := f.db.Pool()
	const seedPhotos = `
		INSERT INTO photos (uid, file_hash, file_path, file_name, file_mime, taken_at_source)
		SELECT 'lat' || lpad(i::text, 29, '0'), 'lat-hash-' || lpad(i::text, 55, '0'),
		       'p/' || i || '.jpg', i || '.jpg', 'image/jpeg', 'exif'
		FROM generate_series(1, 20000) i`
	const seedComments = `
		INSERT INTO comments (uid, photo_uid, body, created_at)
		SELECT 'cm' || lpad(i::text, 30, '0'), 'lat' || lpad(((i % 20000) + 1)::text, 29, '0'),
		       'note ' || i, timestamptz '2026-01-01' + (i || ' minutes')::interval
		FROM generate_series(1, 60000) i`
	for _, stmt := range []string{seedPhotos, seedComments, "ANALYZE photos", "ANALYZE comments"} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	page := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		page = append(page, fmt.Sprintf("lat%029d", i))
	}

	latest, err := f.comments.LatestAmong(ctx, comments.SubjectPhoto, page)
	if err != nil {
		t.Fatalf("LatestAmong: %v", err)
	}
	if len(latest) != 100 {
		t.Fatalf("latest holds %d photos, want all 100 of the page", len(latest))
	}
	// Comment i lands on photo (i mod 20000) + 1, so the first photo received
	// comments 20000, 40000 and 60000; the newest is the last of them.
	if got := latest[page[0]].Body; got != "note 60000" {
		t.Errorf("newest comment of the first photo = %q, want note 60000", got)
	}

	plan := explainLatest(t, f, page)
	if !strings.Contains(plan, "idx_comments_photo") {
		t.Errorf("the latest-comment read does not use idx_comments_photo:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on comments") {
		t.Errorf("the latest-comment read scans the whole comments table:\n%s", plan)
	}
}

// explainLatest returns the planner's text for the statement LatestAmong runs,
// reproduced here verbatim because the store keeps its SQL private.
func explainLatest(t *testing.T, f *fixture, uids []string) string {
	t.Helper()
	const query = `EXPLAIN SELECT DISTINCT ON (c.photo_uid) c.uid, c.body
		FROM comments c LEFT JOIN users u ON u.uid = c.author_uid
		WHERE c.photo_uid = ANY($1) AND c.deleted_at IS NULL
		ORDER BY c.photo_uid, c.created_at DESC, c.uid DESC`
	rows, err := f.db.Pool().Query(context.Background(), query, uids)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating plan: %v", err)
	}
	return b.String()
}
