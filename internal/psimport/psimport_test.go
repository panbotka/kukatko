package psimport

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
)

// onePhotoSource builds a fakeSource with a single fully-populated photo and the
// matching on-disk file bytes.
func onePhotoSource() (*fakeSource, map[string][]byte) {
	src := newFakeSource()
	src.photos = []photosorter.Photo{{
		UID: "ps1", FileHash: "abc", FilePath: "/orig/a.jpg", FileName: "a.jpg",
		Title: "Beach", UpdatedAt: time.Unix(100, 0).UTC(),
	}}
	src.subjects = []photosorter.Subject{{UID: "su_ps1", Slug: "alice", Name: "Alice", Type: "person"}}
	src.albums = []photosorter.Album{{UID: "al_ps1", Slug: "trip", Title: "Trip"}}
	src.labels = []photosorter.Label{{UID: "lb_ps1", Slug: "beach", Name: "Beach"}}
	src.embed["ps1"] = photosorter.Embedding{PhotoUID: "ps1", Vector: []float32{0.1, 0.2}, Model: "clip", Pretrained: "openai"}
	subj := "su_ps1"
	markerUID := "mk_ps1"
	src.faces["ps1"] = []photosorter.Face{{
		PhotoUID: "ps1", FaceIndex: 0, Vector: []float32{0.3}, BBox: [4]float64{0.1, 0.1, 0.2, 0.2},
		DetScore: 0.9, Model: "buffalo_l", SubjectUID: &subj, SubjectName: "Alice", MarkerUID: &markerUID,
	}}
	src.processed["ps1"] = 1
	src.phash["ps1"] = photosorter.Phash{PhotoUID: "ps1", Phash: 11, Dhash: 22}
	src.markers["ps1"] = []photosorter.Marker{{
		UID: "mk_ps1", PhotoUID: "ps1", SubjectUID: &subj, Type: "face",
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Reviewed: true,
	}}
	src.albumMem["ps1"] = []photosorter.AlbumPhoto{{AlbumUID: "al_ps1", PhotoUID: "ps1", SortOrder: 0}}
	src.labelMem["ps1"] = []photosorter.PhotoLabel{{PhotoUID: "ps1", LabelUID: "lb_ps1", Source: "import"}}
	return src, map[string][]byte{"/orig/a.jpg": []byte("jpeg-bytes")}
}

// TestMigrate_happyPath creates a photo and transfers all of its satellites,
// mapping the subject, album and label across.
func TestMigrate_happyPath(t *testing.T) {
	t.Parallel()
	src, files := onePhotoSource()
	h := newHarness(src, files)

	result, err := h.svc.Migrate(t.Context())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if result.Counts.Imported != 1 || result.Counts.Failed != 0 {
		t.Fatalf("counts = %+v, want 1 imported 0 failed", result.Counts)
	}
	if h.photos.creates != 1 {
		t.Fatalf("creates = %d, want 1", h.photos.creates)
	}
	kkUID := h.photos.byPS["ps1"]
	if kkUID == "" {
		t.Fatal("photosorter_uid not mapped")
	}
	if _, ok := h.vec.embeddings[kkUID]; !ok {
		t.Error("embedding not transferred")
	}
	faces := h.vec.faces[kkUID]
	if len(faces) != 1 || faces[0].SubjectUID == nil {
		t.Fatalf("faces = %+v, want 1 face with remapped subject", faces)
	}
	subjUID := h.people.bySlug["alice"].UID
	if *faces[0].SubjectUID != subjUID {
		t.Errorf("face subject = %v, want remapped %s", *faces[0].SubjectUID, subjUID)
	}
	if faces[0].MarkerUID == nil || *faces[0].MarkerUID != "mk_ps1" {
		t.Errorf("face marker = %v, want preserved mk_ps1", faces[0].MarkerUID)
	}
	if _, ok := h.people.markers["mk_ps1"]; !ok {
		t.Error("marker not migrated with preserved uid")
	}
	albumUID := h.albums.bySlug["trip"].UID
	if got := h.albums.members[albumUID]; len(got) != 1 || got[0] != kkUID {
		t.Errorf("album members = %v, want [%s]", got, kkUID)
	}
	labelUID := h.labels.byName["Beach"].UID
	if got := h.labels.attached[labelUID]; len(got) != 1 || got[0] != kkUID {
		t.Errorf("label attached = %v, want [%s]", got, kkUID)
	}
	if h.runs.completed[result.RunID] == nil {
		t.Error("run not completed with a watermark")
	}
}

// TestMigrate_matchByFileHash attaches to an existing photo (e.g. from PhotoPrism)
// without copying the original, backfilling photosorter_uid.
func TestMigrate_matchByFileHash(t *testing.T) {
	t.Parallel()
	src, files := onePhotoSource()
	h := newHarness(src, files)
	// Pre-seed an existing photo with the same content hash as ps1.
	h.photos.byUID["ph_existing"] = photos.Photo{UID: "ph_existing", FileHash: "abc"}
	h.photos.byHash["abc"] = "ph_existing"

	result, err := h.svc.Migrate(t.Context())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if h.photos.creates != 0 {
		t.Errorf("creates = %d, want 0 (matched existing)", h.photos.creates)
	}
	if result.Counts.Skipped != 1 {
		t.Errorf("counts = %+v, want 1 skipped", result.Counts)
	}
	if h.photos.byPS["ps1"] != "ph_existing" {
		t.Errorf("photosorter_uid not backfilled onto existing photo")
	}
	if _, ok := h.vec.embeddings["ph_existing"]; !ok {
		t.Error("embedding not attached to existing photo")
	}
}

// TestMigrate_idempotent re-runs the migration and verifies nothing is duplicated.
func TestMigrate_idempotent(t *testing.T) {
	t.Parallel()
	src, files := onePhotoSource()
	h := newHarness(src, files)

	if _, err := h.svc.Migrate(t.Context()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if _, err := h.svc.Migrate(t.Context()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if h.photos.creates != 1 {
		t.Errorf("creates = %d, want 1 after re-run (no duplicate)", h.photos.creates)
	}
	if h.people.nextSub != 1 {
		t.Errorf("subjects created = %d, want 1 after re-run", h.people.nextSub)
	}
	if len(h.people.markers) != 1 {
		t.Errorf("markers = %d, want 1 after re-run", len(h.people.markers))
	}
}

// TestMigrate_perPhotoFailure records a failed photo (its original cannot be read)
// without aborting the run.
func TestMigrate_perPhotoFailure(t *testing.T) {
	t.Parallel()
	src, _ := onePhotoSource()
	h := newHarness(src, map[string][]byte{}) // no files -> open fails

	result, err := h.svc.Migrate(t.Context())
	if err != nil {
		t.Fatalf("Migrate returned error, want run to continue: %v", err)
	}
	if result.Counts.Failed != 1 || result.Counts.Imported != 0 {
		t.Errorf("counts = %+v, want 1 failed", result.Counts)
	}
	if h.runs.completed[result.RunID] == nil && h.runs.failed[result.RunID] != "" {
		t.Error("run was failed; a per-photo failure must not fail the run")
	}
}

// TestMigrate_missingVectorsEnqueued enqueues Kukátko's own jobs for a photo
// photo-sorter never embedded or detected.
func TestMigrate_missingVectorsEnqueued(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	src.photos = []photosorter.Photo{{
		UID: "ps2", FileHash: "xyz", FilePath: "/orig/b.jpg", FileName: "b.jpg",
		UpdatedAt: time.Unix(50, 0).UTC(),
	}}
	h := newHarness(src, map[string][]byte{"/orig/b.jpg": []byte("bytes")})

	if _, err := h.svc.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	kkUID := h.photos.byPS["ps2"]
	if len(h.enq.embeds) != 1 || h.enq.embeds[0] != kkUID {
		t.Errorf("embed enqueued = %v, want [%s]", h.enq.embeds, kkUID)
	}
	if len(h.enq.faces) != 1 || h.enq.faces[0] != kkUID {
		t.Errorf("face detect enqueued = %v, want [%s]", h.enq.faces, kkUID)
	}
}
