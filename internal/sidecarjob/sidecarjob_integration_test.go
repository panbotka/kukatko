//go:build integration

package sidecarjob_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/sidecarjob"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They exercise the whole export end to end — real
// stores, real curation, real storage backend — because the unit tests fake the
// collaborators, and the queries that gather a photo's curation are exactly the
// half that fakes cannot prove.
//
// They share one database and truncate between cases, so they do not run in
// parallel.

// exportFixture is one integration case's world: the stores, the storage root and
// the service under test.
type exportFixture struct {
	svc      *sidecarjob.Service
	photos   *photos.Store
	organize *organize.Store
	people   *people.Store
	db       *database.DB
	root     string
}

// newExportFixture builds a Service over a freshly truncated database and a real
// filesystem storage backend rooted in a temp dir.
func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()

	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS returned error: %v", err)
	}
	photoStore := photos.NewStore(db.Pool())
	return &exportFixture{
		svc: sidecarjob.New(sidecarjob.Config{
			Photos:   photoStore,
			Organize: organize.NewStore(db.Pool()),
			People:   people.NewStore(db.Pool()),
			Places:   places.NewStore(db.Pool()),
			Users:    auth.NewStore(db.Pool()),
			Writer:   sidecarexport.NewWriter(fs),
			Lister:   photoStore,
			Enqueuer: jobs.NewEnqueuer(jobs.NewStore(db.Pool())),
		}),
		photos:   photoStore,
		organize: organize.NewStore(db.Pool()),
		people:   people.NewStore(db.Pool()),
		db:       db,
		root:     root,
	}
}

// readSidecar reads and parses the sidecar of the original at fileKey.
func (f *exportFixture) readSidecar(t *testing.T, fileKey string) sidecarexport.Document {
	t.Helper()

	key, err := sidecarexport.KeyFor(fileKey)
	if err != nil {
		t.Fatalf("KeyFor(%q) returned error: %v", fileKey, err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatalf("reading sidecar at %s: %v", key, err)
	}
	doc, err := sidecarexport.Unmarshal(data)
	if err != nil {
		t.Fatalf("parsing sidecar at %s: %v", key, err)
	}
	return doc
}

// makePhoto inserts a minimal photo and returns it.
func makePhoto(t *testing.T, store *photos.Store, hash string) photos.Photo {
	t.Helper()

	created, err := store.Create(context.Background(), photos.Photo{
		FileHash: hash,
		FilePath: "2024/01/" + hash + ".jpg",
		FileName: hash + ".jpg",
		FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created
}

// TestExport_writesRealCurationToRealStorage is the end-to-end case: a photo with
// an album, a label, a named face, a favorite and a rating is exported, and every
// one of those reaches the file on disk.
//
// It is the test that proves the queries added for the export actually return the
// curation, which is the half the unit tests fake away.
func TestExport_writesRealCurationToRealStorage(t *testing.T) {
	fix := newExportFixture(t)
	ctx := t.Context()

	photo := makePhoto(t, fix.photos, "e2ecuration01")
	userUID := makeUser(t, auth.NewStore(fix.db.Pool()), "usr0000000000001", "pan.botka")

	album, err := fix.organize.CreateAlbum(ctx, organize.Album{Title: "Svatba"})
	if err != nil {
		t.Fatalf("CreateAlbum returned error: %v", err)
	}
	if err := fix.organize.AddPhoto(ctx, album.UID, photo.UID); err != nil {
		t.Fatalf("AddPhoto returned error: %v", err)
	}
	label, err := fix.organize.CreateLabel(ctx, organize.Label{Name: "Portrét", Priority: 3})
	if err != nil {
		t.Fatalf("CreateLabel returned error: %v", err)
	}
	if err := fix.organize.AttachLabel(ctx, photo.UID, label.UID, organize.SourceAI, 12); err != nil {
		t.Fatalf("AttachLabel returned error: %v", err)
	}
	if err := fix.organize.AddFavorite(ctx, userUID, photo.UID); err != nil {
		t.Fatalf("AddFavorite returned error: %v", err)
	}
	if err := fix.organize.SetRating(ctx, userUID, photo.UID, 4); err != nil {
		t.Fatalf("SetRating returned error: %v", err)
	}
	subject, err := fix.people.CreateSubject(ctx, people.Subject{Name: "Jana Nováková", Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("CreateSubject returned error: %v", err)
	}
	marker, err := fix.people.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, Type: people.MarkerFace,
		X: 0.25, Y: 0.1, W: 0.2, H: 0.3, Score: 88,
	})
	if err != nil {
		t.Fatalf("CreateMarker returned error: %v", err)
	}
	if _, err := fix.people.AssignSubject(ctx, marker.UID, subject.UID); err != nil {
		t.Fatalf("AssignSubject returned error: %v", err)
	}

	if err := fix.svc.Export(ctx, photo.UID); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	doc := fix.readSidecar(t, photo.FilePath)

	if doc.Version != sidecarexport.Version {
		t.Errorf("version = %d, want %d", doc.Version, sidecarexport.Version)
	}
	if doc.Identity.SHA256 != "e2ecuration01" {
		t.Errorf("sha256 = %q, want the photo's file hash", doc.Identity.SHA256)
	}
	if len(doc.Curation.Albums) != 1 || doc.Curation.Albums[0].Title != "Svatba" {
		t.Errorf("albums = %+v, want the one album", doc.Curation.Albums)
	}
	if len(doc.Curation.Labels) != 1 {
		t.Fatalf("labels = %+v, want the one label", doc.Curation.Labels)
	}
	if got := doc.Curation.Labels[0]; got.Name != "Portrét" || got.Source != "ai" || got.Uncertainty != 12 {
		t.Errorf("label = %+v, want Portrét attached by ai with uncertainty 12", got)
	}
	if len(doc.Curation.People) != 1 {
		t.Fatalf("people = %+v, want the one marker", doc.Curation.People)
	}
	person := doc.Curation.People[0]
	if person.Name != "Jana Nováková" || person.SubjectType != "person" {
		t.Errorf("person = %+v, want the named subject — a box without a name is a rectangle", person)
	}
	if want := (sidecarexport.Box{X: 0.25, Y: 0.1, W: 0.2, H: 0.3}); person.Box != want {
		t.Errorf("box = %+v, want %+v — a marker without its box cannot be rebuilt", person.Box, want)
	}
	if len(doc.Curation.Favorites) != 1 || doc.Curation.Favorites[0].User != "pan.botka" {
		t.Errorf("favorites = %+v, want pan.botka's favorite by username", doc.Curation.Favorites)
	}
	if len(doc.Curation.Ratings) != 1 || doc.Curation.Ratings[0].Stars != 4 {
		t.Errorf("ratings = %+v, want the 4-star rating", doc.Curation.Ratings)
	}
}

// TestExport_stampsMarkerSoBackfillDrains asserts the export clears the photo
// from the backfill's pending set, over the real predicate.
func TestExport_stampsMarkerSoBackfillDrains(t *testing.T) {
	fix := newExportFixture(t)
	ctx := t.Context()

	photo := makePhoto(t, fix.photos, "e2emarker01")
	if err := fix.svc.Export(ctx, photo.UID); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	pending, err := fix.photos.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	for _, uid := range pending {
		if uid == photo.UID {
			t.Fatal("the exported photo is still pending; the backfill would never drain")
		}
	}
}

// TestBackfill_isIdempotent asserts the backfill schedules the library once and
// then, over a drained library, schedules nothing — the property that makes it
// safe from cron and before every risky operation.
func TestBackfill_isIdempotent(t *testing.T) {
	fix := newExportFixture(t)
	ctx := t.Context()

	first := makePhoto(t, fix.photos, "e2ebackfill01")
	second := makePhoto(t, fix.photos, "e2ebackfill02")

	enqueued, err := fix.svc.BackfillSidecars(ctx, false)
	if err != nil {
		t.Fatalf("BackfillSidecars returned error: %v", err)
	}
	if enqueued != 2 {
		t.Errorf("first run enqueued %d, want 2", enqueued)
	}

	// A second run while the jobs are still queued must add nothing: the queue's
	// per-photo dedup index swallows the repeat, which is what makes a re-run cheap
	// rather than a duplicate write per photo.
	again, err := fix.svc.BackfillSidecars(ctx, false)
	if err != nil {
		t.Fatalf("second BackfillSidecars returned error: %v", err)
	}
	if again != 2 {
		t.Errorf("second run enqueued %d, want 2 (the dedup makes each a no-op, not an error)", again)
	}
	assertQueuedSidecarJobs(t, fix.db, 2)

	// Once both are exported, the pending predicate is empty and a run schedules
	// nothing at all.
	for _, uid := range []string{first.UID, second.UID} {
		if err := fix.svc.Export(ctx, uid); err != nil {
			t.Fatalf("Export returned error: %v", err)
		}
	}
	drained, err := fix.svc.BackfillSidecars(ctx, true)
	if err != nil {
		t.Fatalf("BackfillSidecars(all) returned error: %v", err)
	}
	if drained != 2 {
		t.Errorf("forced full run enqueued %d, want 2 (all=true ignores the marker)", drained)
	}
	pending, err := fix.svc.BackfillSidecars(ctx, false)
	if err != nil {
		t.Fatalf("BackfillSidecars returned error: %v", err)
	}
	if pending != 0 {
		t.Errorf("run over a drained library enqueued %d, want 0", pending)
	}
}

// assertQueuedSidecarJobs asserts the queue holds exactly want sidecar jobs,
// proving the dedup index collapsed the repeats rather than the count merely
// looking right.
func assertQueuedSidecarJobs(t *testing.T, db *database.DB, want int) {
	t.Helper()

	var got int
	err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM jobs WHERE type = $1 AND state = 'queued'`, jobs.TypeSidecar).Scan(&got)
	if err != nil {
		t.Fatalf("counting queued sidecar jobs: %v", err)
	}
	if got != want {
		t.Errorf("queued sidecar jobs = %d, want %d — the dedup index should collapse repeats", got, want)
	}
}

// TestExport_reflectsAnEditRatherThanTheStateThatTriggeredIt asserts the handler
// writes the photo as it is *now*. It is what makes a debounced or late job safe:
// the job carries no payload but a uid, so a coalesced burst of edits still ends
// with the final value on disk.
func TestExport_reflectsAnEditRatherThanTheStateThatTriggeredIt(t *testing.T) {
	fix := newExportFixture(t)
	ctx := t.Context()

	photo := makePhoto(t, fix.photos, "e2elate01")
	if _, err := fix.photos.UpdateMetadata(ctx, photo.UID, photos.MetadataUpdate{Title: "První"}); err != nil {
		t.Fatalf("UpdateMetadata returned error: %v", err)
	}
	if _, err := fix.photos.UpdateMetadata(ctx, photo.UID, photos.MetadataUpdate{Title: "Poslední"}); err != nil {
		t.Fatalf("UpdateMetadata returned error: %v", err)
	}
	// One export, standing in for the single debounced job the two edits collapse
	// into.
	if err := fix.svc.Export(ctx, photo.UID); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if got := fix.readSidecar(t, photo.FilePath).Descriptive.Title; got != "Poslední" {
		t.Errorf("title = %q, want Poslední — the job must write the current state", got)
	}
}

// TestRemove_deletesTheFile covers the purge path against real storage.
func TestRemove_deletesTheFile(t *testing.T) {
	fix := newExportFixture(t)
	ctx := t.Context()

	photo := makePhoto(t, fix.photos, "e2epurge01")
	if err := fix.svc.Export(ctx, photo.UID); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	key, err := sidecarexport.KeyFor(photo.FilePath)
	if err != nil {
		t.Fatalf("KeyFor returned error: %v", err)
	}
	if err := fix.svc.Remove(ctx, photo.FilePath); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fix.root, filepath.FromSlash(key))); !os.IsNotExist(err) {
		t.Errorf("sidecar survived the purge (stat err = %v); a rebuild would resurrect the photo", err)
	}
}

// makeUser inserts a viewer account and returns its uid.
func makeUser(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()

	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}
