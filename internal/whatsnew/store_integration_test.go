//go:build integration

package whatsnew_test

import (
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/whatsnew"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

// testGap is the inactivity threshold the test store runs on. A test cannot wait
// six hours for a visit to rotate, so the store is built with a one-minute gap
// and the clock is moved by hand instead, which reproduces the production
// transition exactly without changing its logic.
const testGap = time.Minute

// env is a Store over the integration database plus the hand-moved clock the
// visit bookkeeping reads as "now".
type env struct {
	store *whatsnew.Store
	db    *database.DB
	now   time.Time
}

// newEnv builds a Store over a freshly truncated database, with the short test
// gap and the clock parked at a fixed instant.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &env{
		store: whatsnew.NewStore(db.Pool()).WithGap(testGap),
		db:    db,
		now:   time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC),
	}
}

// exec runs a statement against the test database, failing the test on error.
func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// advance moves the clock forward by d.
func (e *env) advance(d time.Duration) {
	e.now = e.now.Add(d)
}

// summary reads the digest for a user at the current clock, failing the test on
// error.
func (e *env) summary(t *testing.T, userUID string) whatsnew.Summary {
	t.Helper()
	got, err := e.store.Summary(t.Context(), userUID, e.now)
	if err != nil {
		t.Fatalf("Summary(%s): %v", userUID, err)
	}
	return got
}

// addUser creates an account whose uid is also its username.
func (e *env) addUser(t *testing.T, uid string) {
	t.Helper()
	e.exec(t, `INSERT INTO users (uid, username, email, password_hash, role)
	           VALUES ($1, $2, $2||'@example.test', 'x', 'editor')`, uid, uid)
}

// addPhoto inserts a live library photo uploaded by uploader (empty = nobody, as
// after the uploader's account was deleted).
func (e *env) addPhoto(t *testing.T, uid, uploader string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO photos (uid, file_hash, file_path, uploaded_by, created_at, updated_at)
	           VALUES ($1, $2, $3, nullif($4, ''), $5, $5)`, uid, uid, uid+".jpg", uploader, createdAt)
}

// addComment inserts a live comment on a photo, written by author (empty = the
// author's account is gone, since author_uid is ON DELETE SET NULL).
func (e *env) addComment(t *testing.T, uid, photoUID, author string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO photo_comments (uid, photo_uid, author_uid, body, created_at)
	           VALUES ($1, $2, nullif($3, ''), 'kdo je to?', $4)`, uid, photoUID, author, createdAt)
}

// addAlbum inserts a hand-curated album created by creator (empty = nobody).
func (e *env) addAlbum(t *testing.T, uid, title, creator string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO albums (uid, slug, title, type, created_by, created_at, updated_at)
	           VALUES ($1, $2, $3, 'album', nullif($4, ''), $5, $5)`, uid, uid, title, creator, createdAt)
}

// addSubject inserts a named person. Subjects record no creator, which is why
// they are news to everybody.
func (e *env) addSubject(t *testing.T, uid, name string, createdAt time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO subjects (uid, slug, name, type, created_at, updated_at)
	           VALUES ($1, $2, $3, 'person', $4, $4)`, uid, uid, name, createdAt)
}

// openVisits gives every named account a reference point (the first read has
// none) and then moves the clock past the gap, so that whatever the test does
// next falls inside the reported window.
func (e *env) openVisits(t *testing.T, userUIDs ...string) {
	t.Helper()
	for _, uid := range userUIDs {
		if got := e.summary(t, uid); got.HasNews {
			t.Fatalf("first read for %s = %+v, want has_news false", uid, got)
		}
	}
	e.advance(2 * testGap)
}

// TestOwnWorkIsNotNews: what user A wrote, curated and uploaded is news to B and
// silence to A — and because A did nothing else, A gets no panel at all, which is
// the reported bug ("1 nový komentář" for one's own comment).
func TestOwnWorkIsNotNews(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")
	env.addUser(t, "usr-b")
	env.openVisits(t, "usr-a", "usr-b")

	at := env.now.Add(-time.Second)
	env.addPhoto(t, "ph-a", "usr-a", at)
	env.addComment(t, "cm-a", "ph-a", "usr-a", at)
	env.addAlbum(t, "al-a", "Léto 2026", "usr-a", at)

	mine := env.summary(t, "usr-a")
	if mine.HasNews {
		t.Errorf("author's own digest = %+v, want has_news false — own work is not news", mine)
	}

	other := env.summary(t, "usr-b")
	if !other.HasNews {
		t.Fatalf("other reader's digest = %+v, want has_news true", other)
	}
	if other.Photos != 1 || other.Comments != 1 || other.AlbumCount != 1 {
		t.Errorf("other reader's photos/comments/albums = %d/%d/%d, want 1/1/1",
			other.Photos, other.Comments, other.AlbumCount)
	}
}

// TestDeletedActorIsNewsToEverybody: the actor columns are ON DELETE SET NULL, so
// work whose author is gone belongs to nobody — every reader is told about it,
// including the one who happens to hold the uid it once had.
func TestDeletedActorIsNewsToEverybody(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")
	env.addUser(t, "usr-b")
	env.openVisits(t, "usr-a", "usr-b")

	at := env.now.Add(-time.Second)
	env.addPhoto(t, "ph-orphan", "", at)
	env.addComment(t, "cm-orphan", "ph-orphan", "", at)
	env.addAlbum(t, "al-orphan", "Po babičce", "", at)

	for _, uid := range []string{"usr-a", "usr-b"} {
		got := env.summary(t, uid)
		if !got.HasNews {
			t.Fatalf("%s digest = %+v, want has_news true", uid, got)
		}
		if got.Photos != 1 || got.Comments != 1 || got.AlbumCount != 1 {
			t.Errorf("%s photos/comments/albums = %d/%d/%d, want 1/1/1",
				uid, got.Photos, got.Comments, got.AlbumCount)
		}
	}
}

// TestAlbumListMatchesTheCount: the "Podrobnosti" list is filtered exactly like
// the count it belongs to, so the panel never names an album it did not count.
func TestAlbumListMatchesTheCount(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")
	env.addUser(t, "usr-b")
	env.openVisits(t, "usr-a", "usr-b")

	at := env.now.Add(-time.Second)
	env.addAlbum(t, "al-a", "Moje", "usr-a", at)
	env.addAlbum(t, "al-b", "Cizí", "usr-b", at)

	got := env.summary(t, "usr-a")
	if got.AlbumCount != 1 || len(got.Albums) != 1 {
		t.Fatalf("album_count/len(albums) = %d/%d, want 1/1", got.AlbumCount, len(got.Albums))
	}
	if got.Albums[0].UID != "al-b" {
		t.Errorf("named album = %q, want the album the reader did not create", got.Albums[0].UID)
	}
}

// TestNamedPeopleAreNewsToEverybody: a subject records no creator, so the person
// who named a face is told about it like everybody else — the deliberate
// exception to "own work is not news".
func TestNamedPeopleAreNewsToEverybody(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")
	env.openVisits(t, "usr-a")

	env.addSubject(t, "su-1", "Anna", env.now.Add(-time.Second))

	got := env.summary(t, "usr-a")
	if !got.HasNews || got.PersonCount != 1 || len(got.People) != 1 {
		t.Fatalf("digest = %+v, want one newly named person", got)
	}
}

// TestMinePhotosExcludesOwnUploads: "new photos of you" is a subset of the new
// photos, so a self-portrait the reader uploaded themselves drops out of both
// rather than leaving the line larger than the total it belongs to.
func TestMinePhotosExcludesOwnUploads(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")
	env.addUser(t, "usr-b")
	env.addSubject(t, "su-me", "Anna", env.now.Add(-time.Hour))
	env.exec(t, `UPDATE users SET subject_uid = 'su-me' WHERE uid = 'usr-a'`)
	env.openVisits(t, "usr-a")

	at := env.now.Add(-time.Second)
	env.addPhoto(t, "ph-selfie", "usr-a", at)
	env.addPhoto(t, "ph-theirs", "usr-b", at)
	for _, photo := range []string{"ph-selfie", "ph-theirs"} {
		env.exec(t, `INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
		             VALUES ($1, $2, 'su-me', 'face', 0.1, 0.1, 0.2, 0.2)`, "mk-"+photo, photo)
	}

	got := env.summary(t, "usr-a")
	if got.Photos != 1 {
		t.Fatalf("photos = %d, want 1 — the reader's own upload is not news", got.Photos)
	}
	if got.MinePhotos != 1 {
		t.Errorf("mine_photos = %d, want 1 — only the photo somebody else uploaded", got.MinePhotos)
	}
}

// TestCountsStayIndexBacked: the actor exclusion is a filter on the existing
// created_at ranges, not a new access path. The plan for a library with many old
// rows and a handful of new ones must still reach them through the range
// indexes (0053_user_visits, idx_photos_live_created_at from 0015); a sequential
// scan here would make the panel cost grow with the library.
func TestCountsStayIndexBacked(t *testing.T) {
	env := newEnv(t)
	env.addUser(t, "usr-a")

	old := env.now.Add(-30 * 24 * time.Hour)
	env.exec(t, `INSERT INTO photos (uid, file_hash, file_path, created_at, updated_at)
	             SELECT 'ph'||i, 'h'||i, 'p'||i||'.jpg', $1, $1 FROM generate_series(1, 4000) i`, old)
	env.exec(t, `INSERT INTO photo_comments (uid, photo_uid, body, created_at)
	             SELECT 'cm'||i, 'ph'||i, 'x', $1 FROM generate_series(1, 4000) i`, old)
	env.exec(t, `INSERT INTO albums (uid, slug, title, type, created_at, updated_at)
	             SELECT 'al'||i, 'al'||i, 'a'||i, 'album', $1, $1 FROM generate_series(1, 4000) i`, old)
	env.exec(t, `INSERT INTO subjects (uid, slug, name, type, created_at, updated_at)
	             SELECT 'su'||i, 'su'||i, 's'||i, 'person', $1, $1 FROM generate_series(1, 4000) i`, old)
	env.exec(t, `ANALYZE photos, photo_comments, albums, subjects`)

	for _, table := range []string{"photos", "photo_comments", "albums", "subjects"} {
		if plan := env.plan(t, table); !strings.Contains(plan, "Index") {
			t.Errorf("plan for %s is not index-backed:\n%s", table, plan)
		}
	}
}

// countPlans mirrors the four subqueries of the store's countsSQL, one per table,
// so the plan test asks the planner exactly what production asks it.
var countPlans = map[string]string{
	"photos": `SELECT count(*) FROM photos
        WHERE created_at > $1 AND archived_at IS NULL
          AND (stack_uid IS NULL OR stack_primary)
          AND NOT hidden_from_library
          AND uploaded_by IS DISTINCT FROM $2`,
	"photo_comments": `SELECT count(*) FROM photo_comments
        WHERE created_at > $1 AND deleted_at IS NULL
          AND author_uid IS DISTINCT FROM $2`,
	"albums": `SELECT count(*) FROM albums
        WHERE created_at > $1 AND type = 'album'
          AND created_by IS DISTINCT FROM $2`,
	"subjects": `SELECT count(*) FROM subjects
        WHERE created_at > $1 AND name <> ''`,
}

// plan returns the query plan of one count subquery over a window that holds
// almost nothing, which is the shape the digest runs in.
func (e *env) plan(t *testing.T, table string) string {
	t.Helper()
	sql := countPlans[table]
	// The subjects subquery names no reader — subjects have no creator — so it
	// takes the range parameter alone.
	args := []any{e.now.Add(-time.Hour)}
	if strings.Contains(sql, "$2") {
		args = append(args, "usr-a")
	}
	rows, err := e.db.Pool().Query(t.Context(), "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", table, err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scanning plan line: %v", err)
		}
		b.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading plan: %v", err)
	}
	return b.String()
}
