//go:build integration

package stacks_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/stacks"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they do not run in parallel.

// ptr returns a pointer to v, for the many optional pointer fields.
func ptr[T any](v T) *T { return &v }

// newStores returns the three stores over a freshly truncated integration
// database plus the db handle.
func newStores(t *testing.T) (*photos.Store, *organize.Store, *people.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return photos.NewStore(db.Pool()), organize.NewStore(db.Pool()), people.NewStore(db.Pool()), db
}

// makePhoto inserts a photo of the given name/mime/dimensions, dated 2021 and
// geotagged so it participates in the year facet and place counts, and returns it.
func makePhoto(t *testing.T, store *photos.Store, hash, name, mime string, w, h int) photos.Photo {
	t.Helper()
	taken := time.Date(2021, 7, 4, 10, 0, 0, 0, time.UTC)
	created, err := store.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2021/07/" + hash, FileName: name, FileSize: int64(w * h),
		FileMime: mime, FileWidth: w, FileHeight: h, MediaType: photos.MediaImage,
		TakenAt: &taken, TakenAtSource: "exif", Title: "Title " + hash,
		Lat: ptr(50.08), Lng: ptr(14.42),
	})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return created
}

// detector builds a Service with only the safe base-name rule enabled.
func detector(store *photos.Store) *stacks.Service {
	return stacks.New(store, stacks.Config{Enabled: true, Rules: stacks.RuleSet{BaseName: true}})
}

// listUIDs returns the uids the default (visible-only) list returns.
func listUIDs(t *testing.T, store *photos.Store) map[string]bool {
	t.Helper()
	list, err := store.List(t.Context(), photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	out := make(map[string]bool, len(list))
	for _, p := range list {
		out[p.UID] = true
	}
	return out
}

func TestIntegration_DetectStacksBackfill(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()
	raw := makePhoto(t, store, "raw1", "IMG_1.CR2", "image/x-canon-cr2", 6000, 4000)
	jpg := makePhoto(t, store, "jpg1", "IMG_1.jpg", "image/jpeg", 6000, 4000)
	other := makePhoto(t, store, "oth1", "OTHER.jpg", "image/jpeg", 4000, 3000)

	svc := detector(store)
	created, err := svc.DetectStacks(ctx)
	if err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	if created != 1 {
		t.Fatalf("DetectStacks created %d stacks, want 1", created)
	}

	// The grid returns one row where there were two: the JPEG primary and the
	// unrelated photo, but not the hidden RAW.
	visible := listUIDs(t, store)
	if !visible[jpg.UID] || !visible[other.UID] || visible[raw.UID] {
		t.Errorf("visible list = %v, want jpg+other without raw", visible)
	}
	if n, err := store.Count(ctx, photos.ListParams{}); err != nil || n != 2 {
		t.Errorf("Count = %d (err %v), want 2", n, err)
	}

	// The JPEG is the primary (rendered beats RAW); both share one stack_uid.
	rawGot, _ := store.GetByUID(ctx, raw.UID)
	jpgGot, _ := store.GetByUID(ctx, jpg.UID)
	if rawGot.StackUID == nil || jpgGot.StackUID == nil || *rawGot.StackUID != *jpgGot.StackUID {
		t.Fatalf("members do not share a stack_uid: raw=%v jpg=%v", rawGot.StackUID, jpgGot.StackUID)
	}
	if !jpgGot.StackPrimary || rawGot.StackPrimary {
		t.Errorf("primary flags wrong: jpg.primary=%v raw.primary=%v", jpgGot.StackPrimary, rawGot.StackPrimary)
	}

	// Re-running over the settled library changes nothing.
	if again, err := svc.DetectStacks(ctx); err != nil || again != 0 {
		t.Errorf("re-run created %d stacks (err %v), want 0", again, err)
	}
}

func TestIntegration_CountsDropAndUnstackRestores(t *testing.T) {
	store, org, ppl, _ := newStores(t)
	ctx := t.Context()
	raw := makePhoto(t, store, "raw2", "IMG_2.CR2", "image/x-canon-cr2", 6000, 4000)
	jpg := makePhoto(t, store, "jpg2", "IMG_2.jpg", "image/jpeg", 6000, 4000)

	album, _ := org.CreateAlbum(ctx, organize.Album{Title: "Trip"})
	label, _ := org.CreateLabel(ctx, organize.Label{Name: "beach"})
	subject, _ := ppl.CreateSubject(ctx, people.Subject{Name: "Alice", Type: people.SubjectPerson})
	for _, uid := range []string{raw.UID, jpg.UID} {
		if err := org.AddPhoto(ctx, album.UID, uid); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
		if err := org.AttachLabel(ctx, uid, label.UID, organize.SourceManual, 0); err != nil {
			t.Fatalf("AttachLabel: %v", err)
		}
		mustMarker(t, ppl, uid, subject.UID)
		if err := store.SetPhash(ctx, photos.Phash{PhotoUID: uid, Phash: 1, Dhash: 2}); err != nil {
			t.Fatalf("SetPhash: %v", err)
		}
	}

	assertCounts(t, store, org, ppl, subject.UID, album.UID, label.UID, 2)

	if _, err := detector(store).DetectStacks(ctx); err != nil {
		t.Fatalf("DetectStacks: %v", err)
	}
	// Every count drops to one; only the JPEG primary appears in the gallery.
	assertCounts(t, store, org, ppl, subject.UID, album.UID, label.UID, 1)
	if gallery, _ := ppl.ListPhotoUIDsBySubject(ctx, subject.UID); len(gallery) != 1 || gallery[0] != jpg.UID {
		t.Errorf("subject gallery = %v, want [%s]", gallery, jpg.UID)
	}
	// The duplicates node universe drops the hidden RAW, so the same-stack pair
	// can never be offered as a near-duplicate.
	if phashes, _ := store.ListActivePhashes(ctx); len(phashes) != 1 || phashes[0].PhotoUID != jpg.UID {
		t.Errorf("active phashes = %v, want only %s", phashes, jpg.UID)
	}

	// Unstacking the whole stack restores both rows fully.
	if _, err := detector(store).UnstackWhole(ctx, jpg.UID); err != nil {
		t.Fatalf("UnstackWhole: %v", err)
	}
	assertCounts(t, store, org, ppl, subject.UID, album.UID, label.UID, 2)
	rawGot, _ := store.GetByUID(ctx, raw.UID)
	if rawGot.StackUID != nil || rawGot.StackPrimary {
		t.Errorf("raw still stacked after unstack: %+v", rawGot.StackUID)
	}
	if rawGot.Title != "Title raw2" || rawGot.FileWidth != 6000 {
		t.Errorf("raw metadata not intact after unstack: %+v", rawGot)
	}
	if markers := markerCount(t, ppl, subject.UID); markers != 2 {
		t.Errorf("markers after unstack = %d, want 2 (faces intact)", markers)
	}
}

func TestIntegration_ManualStackingLifecycle(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()
	a := makePhoto(t, store, "man_a", "A.jpg", "image/jpeg", 4000, 3000)
	b := makePhoto(t, store, "man_b", "B.jpg", "image/jpeg", 6000, 4000) // highest resolution
	c := makePhoto(t, store, "man_c", "C.jpg", "image/jpeg", 3000, 2000)

	svc := stacks.New(store, stacks.Config{Enabled: true})
	stackUID, err := svc.StackSelection(ctx, []string{a.UID, b.UID, c.UID})
	if err != nil || stackUID == "" {
		t.Fatalf("StackSelection: %q err %v", stackUID, err)
	}
	// One tile remains: the highest-resolution member is the primary.
	if visible := listUIDs(t, store); len(visible) != 1 || !visible[b.UID] {
		t.Fatalf("after manual stack visible = %v, want only %s", visible, b.UID)
	}

	// Set-primary moves the primary to A.
	if _, err := svc.SetPrimary(ctx, a.UID); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	if visible := listUIDs(t, store); len(visible) != 1 || !visible[a.UID] {
		t.Fatalf("after set-primary visible = %v, want only %s", visible, a.UID)
	}

	// Unstacking C returns it to standalone; A and B stay stacked.
	if _, err := svc.Unstack(ctx, c.UID); err != nil {
		t.Fatalf("Unstack: %v", err)
	}
	if visible := listUIDs(t, store); len(visible) != 2 || !visible[a.UID] || !visible[c.UID] {
		t.Fatalf("after unstacking C visible = %v, want A+C", visible)
	}

	// Removing B leaves A alone: a one-member remnant dissolves, so all stand alone.
	if _, err := svc.Unstack(ctx, b.UID); err != nil {
		t.Fatalf("Unstack B: %v", err)
	}
	aGot, _ := store.GetByUID(ctx, a.UID)
	if aGot.StackUID != nil {
		t.Errorf("A still stacked after its stack dissolved: %v", aGot.StackUID)
	}
	if visible := listUIDs(t, store); len(visible) != 3 {
		t.Errorf("after dissolve visible = %v, want all three standalone", visible)
	}
}

// mustMarker attaches a valid non-invalid marker for subject on photo.
func mustMarker(t *testing.T, ppl *people.Store, photoUID, subjectUID string) {
	t.Helper()
	if _, err := ppl.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 90,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
}

// assertCounts checks that the album, label and subject all report want photos.
func assertCounts(
	t *testing.T, store *photos.Store, org *organize.Store, ppl *people.Store,
	subjectUID, albumUID, labelUID string, want int,
) {
	t.Helper()
	if got := albumCount(t, org, albumUID); got != want {
		t.Errorf("album count = %d, want %d", got, want)
	}
	if got := labelCount(t, org, labelUID); got != want {
		t.Errorf("label count = %d, want %d", got, want)
	}
	if got := markerCount(t, ppl, subjectUID); got != want {
		t.Errorf("subject marker count = %d, want %d", got, want)
	}
	if years, err := store.YearBuckets(t.Context(), photos.ListParams{}); err != nil || years.Total != want {
		t.Errorf("year-facet total = %d (err %v), want %d", years.Total, err, want)
	}
}

func albumCount(t *testing.T, org *organize.Store, albumUID string) int {
	t.Helper()
	list, err := org.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	for _, a := range list {
		if a.UID == albumUID {
			return a.PhotoCount
		}
	}
	t.Fatalf("album %s not found", albumUID)
	return -1
}

func labelCount(t *testing.T, org *organize.Store, labelUID string) int {
	t.Helper()
	list, err := org.ListLabels(t.Context())
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	for _, l := range list {
		if l.UID == labelUID {
			return l.PhotoCount
		}
	}
	t.Fatalf("label %s not found", labelUID)
	return -1
}

func markerCount(t *testing.T, ppl *people.Store, subjectUID string) int {
	t.Helper()
	list, err := ppl.ListSubjects(t.Context())
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	for _, s := range list {
		if s.UID == subjectUID {
			return s.MarkerCount
		}
	}
	t.Fatalf("subject %s not found", subjectUID)
	return -1
}
