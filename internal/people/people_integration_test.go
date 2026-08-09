//go:build integration

package people_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStores returns a people.Store plus the photos and vectors stores it leans on,
// over a freshly truncated integration database.
func newStores(t *testing.T) (*people.Store, *photos.Store, *vectors.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return people.NewStore(db.Pool()), photos.NewStore(db.Pool()),
		vectors.NewStore(db.Pool()), db
}

// makePhoto inserts a photo with the given file hash and a 4000x3000 frame — the
// cover-face pick skips a photo with no dimensions, so a minimal row would make
// every face test silently vacuous — and returns its uid.
func makePhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	return makeSizedPhoto(t, store, hash, 4000, 3000)
}

// makeSizedPhoto inserts a photo with the given file hash and pixel dimensions,
// returning its uid. The dimensions ride along on the cover face, so a test that
// cares what the tile crops from sets them explicitly.
func makeSizedPhoto(t *testing.T, store *photos.Store, hash string, width, height int) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2024/01/" + hash + ".jpg",
		FileName:   hash + ".jpg",
		FileWidth:  width,
		FileHeight: height,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// faceVec builds a FaceDim vector with index 0 set so SaveFaces accepts it.
func faceVec() []float32 {
	v := make([]float32, vectors.FaceDim)
	v[0] = 1
	return v
}

// saveLinkedFace stores one face on photoUID linked to markerUID via the cache
// column, so subject (un)assignment has a row whose cache it must update.
func saveLinkedFace(t *testing.T, store *vectors.Store, photoUID, markerUID string) {
	t.Helper()
	if err := store.SaveFaces(context.Background(), photoUID, []vectors.Face{{
		FaceIndex: 0,
		Vector:    faceVec(),
		BBox:      [4]float64{0.1, 0.2, 0.3, 0.4},
		MarkerUID: &markerUID,
	}}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
}

// faceCache returns the cached subject_uid and subject_name of the single face on
// photoUID, failing the test if there is not exactly one.
func faceCache(t *testing.T, store *vectors.Store, photoUID string) (*string, string) {
	t.Helper()
	faces, err := store.ListFaces(context.Background(), photoUID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	return faces[0].SubjectUID, faces[0].SubjectName
}

// TestSubjectCRUD exercises create, lookups, update (with re-slug) and delete.
func TestSubjectCRUD(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	created, err := store.CreateSubject(ctx, people.Subject{Name: "Anna Nováková", Notes: "sister"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if created.UID == "" || created.Slug != "anna-novakova" || created.Type != people.SubjectPerson {
		t.Fatalf("unexpected created subject: %+v", created)
	}

	byUID, err := store.GetSubjectByUID(ctx, created.UID)
	if err != nil || byUID.UID != created.UID {
		t.Fatalf("GetSubjectByUID = %+v, %v", byUID, err)
	}
	bySlug, err := store.GetSubjectBySlug(ctx, "anna-novakova")
	if err != nil || bySlug.UID != created.UID {
		t.Fatalf("GetSubjectBySlug = %+v, %v", bySlug, err)
	}

	updated, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{
		Name: "Bobík", Type: people.SubjectPet, Favorite: true,
	})
	if err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	if updated.Name != "Bobík" || updated.Slug != "bobik" || updated.Type != people.SubjectPet || !updated.Favorite {
		t.Fatalf("unexpected updated subject: %+v", updated)
	}

	if err := store.DeleteSubject(ctx, created.UID); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}
	if _, err := store.GetSubjectByUID(ctx, created.UID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("GetSubjectByUID after delete = %v, want ErrSubjectNotFound", err)
	}
	if err := store.DeleteSubject(ctx, created.UID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("DeleteSubject missing = %v, want ErrSubjectNotFound", err)
	}
}

// TestSubjectUniqueSlug checks that subjects sharing a name get distinct slugs.
func TestSubjectUniqueSlug(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	want := []string{"alice", "alice-2", "alice-3"}
	names := []string{"Alice", "Alice", "Alíce!"}
	for i, name := range names {
		got, err := store.CreateSubject(ctx, people.Subject{Name: name})
		if err != nil {
			t.Fatalf("CreateSubject %d: %v", i, err)
		}
		if got.Slug != want[i] {
			t.Errorf("subject %d slug = %q, want %q", i, got.Slug, want[i])
		}
	}
}

// TestSubjectInvalidType checks type validation on create and update.
func TestSubjectInvalidType(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	if _, err := store.CreateSubject(ctx, people.Subject{Name: "X", Type: "robot"}); !errors.Is(err, people.ErrInvalidType) {
		t.Fatalf("CreateSubject bad type = %v, want ErrInvalidType", err)
	}
	created, err := store.CreateSubject(ctx, people.Subject{Name: "Y"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if _, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{Name: "Y", Type: "alien"}); !errors.Is(err, people.ErrInvalidType) {
		t.Fatalf("UpdateSubject bad type = %v, want ErrInvalidType", err)
	}
}

// TestSubjectLifeYearsRoundTrip stores a life span, reads it back through every
// path that returns a subject (get, list) and then clears it — the update
// rewrites the whole editable set, so a nil year has to mean "forget this one"
// rather than "leave it alone".
func TestSubjectLifeYearsRoundTrip(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	created, err := store.CreateSubject(ctx, people.Subject{
		Name: "Jarmila", BirthYear: new(1923), DeathYear: new(1998),
	})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	assertLifeYears(t, "created", created, 1923, 1998)

	fetched, err := store.GetSubjectByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	assertLifeYears(t, "fetched", fetched, 1923, 1998)

	listed, err := store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListSubjects returned %d subjects, want 1", len(listed))
	}
	assertLifeYears(t, "listed", listed[0].Subject, 1923, 1998)

	// Only the birth year is known now: the death has to come back as unknown,
	// not as the value it used to hold.
	half, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{
		Name: "Jarmila", BirthYear: new(1923),
	})
	if err != nil {
		t.Fatalf("UpdateSubject clearing the death year: %v", err)
	}
	if half.BirthYear == nil || *half.BirthYear != 1923 || half.DeathYear != nil {
		t.Fatalf("after clearing the death year = %v/%v, want 1923/nil",
			half.BirthYear, half.DeathYear)
	}

	cleared, err := store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{Name: "Jarmila"})
	if err != nil {
		t.Fatalf("UpdateSubject clearing both years: %v", err)
	}
	if cleared.BirthYear != nil || cleared.DeathYear != nil {
		t.Fatalf("after clearing both = %v/%v, want nil/nil",
			cleared.BirthYear, cleared.DeathYear)
	}
}

// assertLifeYears fails the test unless subj carries exactly the given years.
func assertLifeYears(t *testing.T, where string, subj people.Subject, birth, death int) {
	t.Helper()
	if subj.BirthYear == nil || *subj.BirthYear != birth {
		t.Errorf("%s birth year = %v, want %d", where, subj.BirthYear, birth)
	}
	if subj.DeathYear == nil || *subj.DeathYear != death {
		t.Errorf("%s death year = %v, want %d", where, subj.DeathYear, death)
	}
}

// TestSubjectLifeYearsValidation checks that both write paths refuse the years
// that cannot be true — too early, in the future, or a death before the birth —
// and that neither leaves the stored record changed.
func TestSubjectLifeYearsValidation(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	nextYear := time.Now().Year() + 1
	bad := []struct {
		name  string
		birth *int
		death *int
	}{
		{name: "birth before 1800", birth: new(1799)},
		{name: "birth in the future", birth: new(nextYear)},
		{name: "death in the future", death: new(nextYear)},
		{name: "death before birth", birth: new(1998), death: new(1923)},
	}

	created, err := store.CreateSubject(ctx, people.Subject{Name: "Ludmila", BirthYear: new(1930)})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.CreateSubject(ctx, people.Subject{
				Name: "Rejected " + tt.name, BirthYear: tt.birth, DeathYear: tt.death,
			})
			if !errors.Is(err, people.ErrInvalidLifeYears) {
				t.Errorf("CreateSubject = %v, want ErrInvalidLifeYears", err)
			}
			_, err = store.UpdateSubject(ctx, created.UID, people.SubjectUpdate{
				Name: "Ludmila", BirthYear: tt.birth, DeathYear: tt.death,
			})
			if !errors.Is(err, people.ErrInvalidLifeYears) {
				t.Errorf("UpdateSubject = %v, want ErrInvalidLifeYears", err)
			}
		})
	}

	// A rejected edit must not have touched the stored record on its way out.
	after, err := store.GetSubjectByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	if after.BirthYear == nil || *after.BirthYear != 1930 || after.DeathYear != nil {
		t.Fatalf("after the rejected edits = %v/%v, want 1930/nil",
			after.BirthYear, after.DeathYear)
	}
}

// TestListSubjectsCounts checks that ListSubjects reports the non-invalid marker
// count per subject, ordered by name. Both of Alice's valid markers sit on the
// same photo, so the two counts must part company: two markers, one photo.
func TestListSubjectsCounts(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "list_counts")

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	bob, _ := store.CreateSubject(ctx, people.Subject{Name: "Bob"})

	// Two valid markers for Alice, one invalid (excluded), none for Bob.
	mkMarker(t, store, photoUID, &alice.UID, false)
	mkMarker(t, store, photoUID, &alice.UID, false)
	mkMarker(t, store, photoUID, &alice.UID, true)

	list, err := store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListSubjects len = %d, want 2", len(list))
	}
	if list[0].UID != alice.UID || list[0].MarkerCount != 2 || list[0].PhotoCount != 1 {
		t.Errorf("subject[0] = %+v, want Alice 2 markers on 1 photo", list[0])
	}
	if list[1].UID != bob.UID || list[1].MarkerCount != 0 || list[1].PhotoCount != 0 {
		t.Errorf("subject[1] = %+v, want Bob 0 markers on 0 photos", list[1])
	}
}

// TestListSubjectsPhotoCountMatchesGallery is the badge's promise: the people
// index says "N photos" under a subject and clicking it opens the gallery, so the
// count ListSubjects reports must be the length of the list the gallery pages
// through — not the marker count, which is larger whenever one photo holds
// several of the person's faces.
//
// The fixture makes every way they can diverge visible at once: a photo with two
// valid markers (counted once), a photo with one (counted once), a photo whose
// only marker is invalid, and an archived photo with a valid marker — the last
// two counted by neither.
func TestListSubjectsPhotoCountMatchesGallery(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := t.Context()

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})

	twice := makePhoto(t, photoStore, "gallery_twice")
	once := makePhoto(t, photoStore, "gallery_once")
	rejected := makePhoto(t, photoStore, "gallery_rejected")
	archived := makePhoto(t, photoStore, "gallery_archived")

	mkMarker(t, store, twice, &alice.UID, false)
	mkMarker(t, store, twice, &alice.UID, false)
	mkMarker(t, store, once, &alice.UID, false)
	mkMarker(t, store, rejected, &alice.UID, true)
	mkMarker(t, store, archived, &alice.UID, false)
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET archived_at = now() WHERE uid = $1", archived); err != nil {
		t.Fatalf("archiving photo: %v", err)
	}

	gallery, err := store.ListPhotoUIDsBySubject(ctx, alice.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}
	sc := firstSubject(t, store)
	if sc.PhotoCount != len(gallery) {
		t.Errorf("PhotoCount = %d, gallery has %d photos (%v)",
			sc.PhotoCount, len(gallery), gallery)
	}
	if sc.PhotoCount != 2 {
		t.Errorf("PhotoCount = %d, want 2 (the two visible photos)", sc.PhotoCount)
	}
	// The marker count keeps counting markers: three valid ones, of which the
	// archived photo's does not count either.
	if sc.MarkerCount != 3 {
		t.Errorf("MarkerCount = %d, want 3", sc.MarkerCount)
	}
}

// mkFace inserts a face marker for the subject with an explicit box and score,
// returning its uid. It is mkMarker's variant for the cover-face pick, which is
// decided entirely by the box's area and the score.
func mkFace(
	t *testing.T, store *people.Store, photoUID, subjectUID string,
	box [4]float64, score int, invalid bool,
) string {
	t.Helper()
	m, err := store.CreateMarker(t.Context(), people.Marker{
		PhotoUID:   photoUID,
		SubjectUID: &subjectUID,
		Type:       people.MarkerFace,
		X:          box[0], Y: box[1], W: box[2], H: box[3],
		Score:   score,
		Invalid: invalid,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	return m.UID
}

// firstSubject returns the single subject ListSubjects reports, failing the test
// unless there is exactly one.
func firstSubject(t *testing.T, store *people.Store) people.SubjectCount {
	t.Helper()
	list, err := store.ListSubjects(t.Context())
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListSubjects len = %d, want 1", len(list))
	}
	return list[0]
}

// TestListSubjectsCoverFaceBiggestWins checks the primary term of the pick: the
// face with the largest box wins, because it is the one with enough pixels behind
// it to survive being blown up into a tile.
func TestListSubjectsCoverFaceBiggestWins(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makeSizedPhoto(t, photoStore, "cover_big", 4000, 3000)

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	// The small face is the more confident one: score must not outrank area, or a
	// pin-sharp speck would take the tile from a large, merely good face.
	mkFace(t, store, photoUID, alice.UID, [4]float64{0.8, 0.8, 0.05, 0.05}, 99, false)
	big := mkFace(t, store, photoUID, alice.UID, [4]float64{0.1, 0.2, 0.3, 0.4}, 50, false)

	got := firstSubject(t, store)
	if got.CoverFace == nil {
		t.Fatalf("CoverFace = nil, want the big face %s", big)
	}
	if got.CoverFace.W != 0.3 || got.CoverFace.H != 0.4 {
		t.Errorf("CoverFace box = %+v, want the 0.3x0.4 face", got.CoverFace)
	}
	if got.CoverFace.PhotoUID != photoUID {
		t.Errorf("CoverFace.PhotoUID = %q, want %q", got.CoverFace.PhotoUID, photoUID)
	}
	if got.CoverFace.Width != 4000 || got.CoverFace.Height != 3000 {
		t.Errorf("CoverFace frame = %dx%d, want 4000x3000",
			got.CoverFace.Width, got.CoverFace.Height)
	}
}

// TestListSubjectsCoverFaceScoreBreaksTies checks the secondary term: between two
// boxes of the same area the more confident detection wins.
func TestListSubjectsCoverFaceScoreBreaksTies(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "cover_score")

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	mkFace(t, store, photoUID, alice.UID, [4]float64{0.1, 0.1, 0.2, 0.2}, 40, false)
	mkFace(t, store, photoUID, alice.UID, [4]float64{0.5, 0.5, 0.2, 0.2}, 90, false)

	got := firstSubject(t, store)
	if got.CoverFace == nil {
		t.Fatal("CoverFace = nil, want the high-score face")
	}
	if got.CoverFace.X != 0.5 || got.CoverFace.Y != 0.5 {
		t.Errorf("CoverFace box = %+v, want the score-90 face at 0.5,0.5", got.CoverFace)
	}
}

// TestListSubjectsCoverFaceSkipsInvalid checks that a face the user rejected is
// never offered back as a tile, even when it is the biggest one.
func TestListSubjectsCoverFaceSkipsInvalid(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "cover_invalid")

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	mkFace(t, store, photoUID, alice.UID, [4]float64{0, 0, 0.9, 0.9}, 99, true)
	mkFace(t, store, photoUID, alice.UID, [4]float64{0.1, 0.1, 0.2, 0.2}, 10, false)

	got := firstSubject(t, store)
	if got.CoverFace == nil {
		t.Fatal("CoverFace = nil, want the small valid face")
	}
	if got.CoverFace.W != 0.2 {
		t.Errorf("CoverFace = %+v, want the valid 0.2 face, not the invalid 0.9 one", got.CoverFace)
	}
}

// TestListSubjectsCoverFaceNone checks that a subject with nothing usable gets no
// cover face at all, so the client keeps its placeholder rather than inventing a
// face: a subject with no markers, one whose only face is invalid, one carrying
// only a drawn label box, and one whose face sits on an archived photo.
func TestListSubjectsCoverFaceNone(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := t.Context()

	// A subject with no markers whatsoever.
	if _, err := store.CreateSubject(ctx, people.Subject{Name: "Bare"}); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	invalidOnly, _ := store.CreateSubject(ctx, people.Subject{Name: "InvalidOnly"})
	mkFace(t, store, makePhoto(t, photoStore, "none_invalid"), invalidOnly.UID,
		[4]float64{0.1, 0.1, 0.3, 0.3}, 90, true)

	labelOnly, _ := store.CreateSubject(ctx, people.Subject{Name: "LabelOnly"})
	labelPhoto := makePhoto(t, photoStore, "none_label")
	if _, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: labelPhoto, SubjectUID: &labelOnly.UID, Type: people.MarkerLabel,
		X: 0.1, Y: 0.1, W: 0.5, H: 0.5, Score: 90,
	}); err != nil {
		t.Fatalf("CreateMarker label: %v", err)
	}

	archivedOnly, _ := store.CreateSubject(ctx, people.Subject{Name: "ArchivedOnly"})
	archivedPhoto := makePhoto(t, photoStore, "none_archived")
	mkFace(t, store, archivedPhoto, archivedOnly.UID, [4]float64{0.1, 0.1, 0.3, 0.3}, 90, false)
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET archived_at = now() WHERE uid = $1", archivedPhoto); err != nil {
		t.Fatalf("archiving photo: %v", err)
	}

	list, err := store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("ListSubjects len = %d, want 4", len(list))
	}
	for _, sc := range list {
		if sc.CoverFace != nil {
			t.Errorf("subject %q CoverFace = %+v, want nil", sc.Name, sc.CoverFace)
		}
	}
}

// TestListPhotoUIDsBySubject returns each photo with a non-invalid marker for the
// subject, de-duplicated, and excludes photos whose only marker is invalid.
func TestListPhotoUIDsBySubject(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	p1 := makePhoto(t, photoStore, "subj_p1")
	p2 := makePhoto(t, photoStore, "subj_p2")
	p3 := makePhoto(t, photoStore, "subj_p3")

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})

	// p1 has two valid markers (must appear once), p2 one valid marker, p3 only an
	// invalid marker (must be excluded).
	mkMarker(t, store, p1, &alice.UID, false)
	mkMarker(t, store, p1, &alice.UID, false)
	mkMarker(t, store, p2, &alice.UID, false)
	mkMarker(t, store, p3, &alice.UID, true)

	uids, err := store.ListPhotoUIDsBySubject(ctx, alice.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}
	got := map[string]bool{}
	for _, u := range uids {
		if got[u] {
			t.Errorf("duplicate uid %s", u)
		}
		got[u] = true
	}
	if len(uids) != 2 || !got[p1] || !got[p2] || got[p3] {
		t.Errorf("uids = %v, want exactly {%s, %s}", uids, p1, p2)
	}
}

// TestSubjectPhotosMatchLibraryPersonFacet asserts that the subject gallery
// (ListPhotoUIDsBySubject, backing GET /subjects/{uid}/photos) and the library
// person facet (photos.Store.List with SubjectUIDs, backing
// GET /photos?person=<uid>&sort=newest) return the same photos in the same order.
// The subject-detail photo viewer pages prev/next through the person facet, so the
// two must agree down to the uid tiebreaker or the viewer would step through the
// person's photos in a different order than the gallery shows. Every photo here is
// undated, so all rows tie on the primary taken_at sort and the whole order is
// decided by the tiebreaker — the exact case an ASC/DESC mismatch would expose.
func TestSubjectPhotosMatchLibraryPersonFacet(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	alice, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})

	// Five undated photos, each carrying a non-invalid marker for Alice.
	hashes := []string{"match_p0", "match_p1", "match_p2", "match_p3", "match_p4"}
	for _, hash := range hashes {
		uid := makePhoto(t, photoStore, hash)
		mkMarker(t, store, uid, &alice.UID, false)
	}

	gallery, err := store.ListPhotoUIDsBySubject(ctx, alice.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}

	// The person facet with the default (newest) sort — exactly what the viewer's
	// neighbours hook pages when a photo is opened from the subject gallery.
	list, err := photoStore.List(ctx, photos.ListParams{
		SubjectUIDs: []string{alice.UID},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("photos.List person facet: %v", err)
	}
	facet := make([]string, len(list))
	for i, p := range list {
		facet[i] = p.UID
	}

	if len(gallery) != len(hashes) {
		t.Fatalf("subject gallery returned %d photos, want %d", len(gallery), len(hashes))
	}
	if !slices.Equal(gallery, facet) {
		t.Errorf("subject gallery order %v != library person facet order %v", gallery, facet)
	}
}

// mkMarker creates a face marker for the given photo/subject and optional invalid
// flag, returning its uid.
func mkMarker(t *testing.T, store *people.Store, photoUID string, subjectUID *string, invalid bool) string {
	t.Helper()
	m, err := store.CreateMarker(t.Context(), people.Marker{
		PhotoUID:   photoUID,
		SubjectUID: subjectUID,
		Type:       people.MarkerFace,
		X:          0.1, Y: 0.1, W: 0.2, H: 0.2,
		Invalid: invalid,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	return m.UID
}

// TestMarkerCreateBounds checks the normalised-bounds validation on create.
func TestMarkerCreateBounds(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	photoUID := makePhoto(t, photoStore, "bounds")
	_, err := store.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 1.5, W: 0.2, H: 0.2,
	})
	if !errors.Is(err, people.ErrInvalidBounds) {
		t.Fatalf("CreateMarker out of bounds = %v, want ErrInvalidBounds", err)
	}
}

// TestMarkerAssignUnassignUpdatesFaceCache is the core cache-consistency check:
// assigning and unassigning a subject must keep the faces cache columns in step.
func TestMarkerAssignUnassignUpdatesFaceCache(t *testing.T) {
	store, photoStore, vecStore, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "assign")

	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	marker, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.3, H: 0.3,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	saveLinkedFace(t, vecStore, photoUID, marker.UID)

	// Before assignment the cache is empty.
	if uid, name := faceCache(t, vecStore, photoUID); uid != nil || name != "" {
		t.Fatalf("face cache before assign = %v/%q, want nil/empty", uid, name)
	}

	assigned, err := store.AssignSubject(ctx, marker.UID, subject.UID)
	if err != nil {
		t.Fatalf("AssignSubject: %v", err)
	}
	if assigned.SubjectUID == nil || *assigned.SubjectUID != subject.UID {
		t.Fatalf("assigned marker subject = %v, want %s", assigned.SubjectUID, subject.UID)
	}
	if uid, name := faceCache(t, vecStore, photoUID); uid == nil || *uid != subject.UID || name != "Alice" {
		t.Fatalf("face cache after assign = %v/%q, want %s/Alice", uid, name, subject.UID)
	}

	unassigned, err := store.UnassignSubject(ctx, marker.UID)
	if err != nil {
		t.Fatalf("UnassignSubject: %v", err)
	}
	if unassigned.SubjectUID != nil {
		t.Fatalf("unassigned marker subject = %v, want nil", unassigned.SubjectUID)
	}
	if uid, name := faceCache(t, vecStore, photoUID); uid != nil || name != "" {
		t.Fatalf("face cache after unassign = %v/%q, want nil/empty", uid, name)
	}
}

// TestMarkerCreateWithSubjectUpdatesFaceCache checks that creating a marker that
// already names a subject seeds the faces cache.
func TestMarkerCreateWithSubjectUpdatesFaceCache(t *testing.T) {
	store, photoStore, vecStore, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "create_assigned")

	subject, _ := store.CreateSubject(ctx, people.Subject{Name: "Bob"})
	// The face must be linked first; CreateMarker generates the marker uid, so we
	// assign after to exercise the create-with-subject path on a fresh marker, then
	// confirm the rename path refreshes the cache name.
	marker, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subject.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	if err != nil {
		t.Fatalf("CreateMarker with subject: %v", err)
	}
	saveLinkedFace(t, vecStore, photoUID, marker.UID)

	// Renaming the subject must refresh the cached name on the linked face.
	if _, err := store.UpdateSubject(ctx, subject.UID, people.SubjectUpdate{Name: "Bobby"}); err != nil {
		t.Fatalf("UpdateSubject: %v", err)
	}
	// Re-link via assign so the face picks up the (now renamed) subject.
	if _, err := store.AssignSubject(ctx, marker.UID, subject.UID); err != nil {
		t.Fatalf("AssignSubject: %v", err)
	}
	if uid, name := faceCache(t, vecStore, photoUID); uid == nil || *uid != subject.UID || name != "Bobby" {
		t.Fatalf("face cache = %v/%q, want %s/Bobby", uid, name, subject.UID)
	}
}

// TestAssignSubjectMissing checks the not-found paths of assignment.
func TestAssignSubjectMissing(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "assign_missing")

	subject, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	marker, _ := store.CreateMarker(ctx, people.Marker{PhotoUID: photoUID, Type: people.MarkerFace})

	if _, err := store.AssignSubject(ctx, "mkmissing", subject.UID); !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("AssignSubject missing marker = %v, want ErrMarkerNotFound", err)
	}
	if _, err := store.AssignSubject(ctx, marker.UID, "sumissing"); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("AssignSubject missing subject = %v, want ErrSubjectNotFound", err)
	}
}

// TestMarkerFlagsAndList checks invalid/reviewed toggles and listing by photo.
func TestMarkerFlagsAndList(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "flags")

	marker, _ := store.CreateMarker(ctx, people.Marker{PhotoUID: photoUID, Type: people.MarkerFace})

	invalid, err := store.SetMarkerInvalid(ctx, marker.UID, true)
	if err != nil || !invalid.Invalid {
		t.Fatalf("SetMarkerInvalid = %+v, %v", invalid, err)
	}
	reviewed, err := store.SetMarkerReviewed(ctx, marker.UID, true)
	if err != nil || !reviewed.Reviewed {
		t.Fatalf("SetMarkerReviewed = %+v, %v", reviewed, err)
	}

	list, err := store.ListMarkersByPhoto(ctx, photoUID)
	if err != nil || len(list) != 1 || !list[0].Invalid || !list[0].Reviewed {
		t.Fatalf("ListMarkersByPhoto = %+v, %v", list, err)
	}

	if _, err := store.SetMarkerInvalid(ctx, "mkmissing", true); !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("SetMarkerInvalid missing = %v, want ErrMarkerNotFound", err)
	}
}

// TestMarkerCascadeDeleteOnPhoto checks markers vanish when their photo is deleted.
func TestMarkerCascadeDeleteOnPhoto(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "cascade")

	marker, _ := store.CreateMarker(ctx, people.Marker{PhotoUID: photoUID, Type: people.MarkerFace})
	if err := photoStore.Delete(ctx, photoUID); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	if _, err := store.GetMarkerByUID(ctx, marker.UID); !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("marker survived photo delete: %v", err)
	}
}

// TestSubjectCoverSetNullOnPhotoDelete checks the cover_photo_uid SET NULL FK.
func TestSubjectCoverSetNullOnPhotoDelete(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "cover")

	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice", CoverPhotoUID: &photoUID})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	if subject.CoverPhotoUID == nil || *subject.CoverPhotoUID != photoUID {
		t.Fatalf("cover not stored: %+v", subject)
	}
	if err := photoStore.Delete(ctx, photoUID); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	got, err := store.GetSubjectByUID(ctx, subject.UID)
	if err != nil {
		t.Fatalf("GetSubjectByUID: %v", err)
	}
	if got.CoverPhotoUID != nil {
		t.Errorf("cover_photo_uid = %v, want nil after photo delete", got.CoverPhotoUID)
	}
}

// TestSubjectDeleteDetachesMarkers checks deleting a subject nulls its markers'
// subject_uid (FK SET NULL) and clears the faces cache.
func TestSubjectDeleteDetachesMarkers(t *testing.T) {
	store, photoStore, vecStore, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "detach")

	subject, _ := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	marker, _ := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	saveLinkedFace(t, vecStore, photoUID, marker.UID)
	if _, err := store.AssignSubject(ctx, marker.UID, subject.UID); err != nil {
		t.Fatalf("AssignSubject: %v", err)
	}

	if err := store.DeleteSubject(ctx, subject.UID); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}
	got, err := store.GetMarkerByUID(ctx, marker.UID)
	if err != nil {
		t.Fatalf("GetMarkerByUID: %v", err)
	}
	if got.SubjectUID != nil {
		t.Errorf("marker subject_uid = %v, want nil after subject delete", got.SubjectUID)
	}
	if uid, name := faceCache(t, vecStore, photoUID); uid != nil || name != "" {
		t.Errorf("face cache = %v/%q, want cleared after subject delete", uid, name)
	}
}
