//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// brokenVideo is a video row the way a failed probe leaves it: the file is
// catalogued but nothing the container states was ever written.
func brokenVideo(hash string) photos.Photo {
	return photos.Photo{
		FileHash:      hash,
		FilePath:      "2024/05/" + hash + ".mp4",
		FileName:      hash + ".mp4",
		FileMime:      "video/mp4",
		MediaType:     photos.MediaVideo,
		TakenAtSource: photos.TakenAtSourceUnknown,
	}
}

// TestRepairVideoMetadata_writesOnlyWhatItWasGiven checks the statement applies the
// caller's decision and nothing else: the fields in the repair are written, every
// other column — including the ones a repair can carry but this one does not — is
// left exactly as it was.
func TestRepairVideoMetadata_writesOnlyWhatItWasGiven(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	seed := brokenVideo("v100")
	seed.Title = "Zpěv u ohně"
	created, err := store.Create(ctx, seed)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	changed, err := store.RepairVideoMetadata(ctx, created.UID, photos.VideoRepair{
		DurationMs: new(4_200),
		VideoCodec: new("h264"),
	})
	if err != nil {
		t.Fatalf("RepairVideoMetadata: %v", err)
	}
	if !changed {
		t.Fatal("RepairVideoMetadata reported no change, want one")
	}

	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.DurationMs == nil || *got.DurationMs != 4_200 || got.VideoCodec != "h264" {
		t.Errorf("repaired fields = %v / %q, want 4200 / h264", got.DurationMs, got.VideoCodec)
	}
	if got.FileWidth != 0 || got.FileHeight != 0 || got.FPS != nil || got.AudioCodec != "" {
		t.Errorf("a column outside the repair was written: %+v", got)
	}
	if got.TakenAt != nil || got.TakenAtSource != photos.TakenAtSourceUnknown {
		t.Errorf("capture time touched: %v / %q", got.TakenAt, got.TakenAtSource)
	}
	if got.Title != "Zpěv u ohně" {
		t.Errorf("title = %q, want the curated one", got.Title)
	}
	if !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("updated_at = %v, want it moved past %v", got.UpdatedAt, created.UpdatedAt)
	}
}

// TestRepairVideoMetadata_emptyRepairIsANoOp checks a repair with nothing in it
// never reaches the database: re-probing a healthy clip must not bump updated_at
// and reorder every "recently changed" listing in the library.
func TestRepairVideoMetadata_emptyRepairIsANoOp(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, brokenVideo("v101"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	changed, err := store.RepairVideoMetadata(ctx, created.UID, photos.VideoRepair{})
	if err != nil {
		t.Fatalf("RepairVideoMetadata: %v", err)
	}
	if changed {
		t.Error("an empty repair reported a change")
	}
	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("updated_at moved on an empty repair: %v → %v", created.UpdatedAt, got.UpdatedAt)
	}
}

// TestRepairVideoMetadata_refusesAStill checks the media_type guard: this statement
// speaks about a container, so aiming it at a photo is a caller's bug worth failing
// on rather than a row worth writing.
func TestRepairVideoMetadata_refusesAStill(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("v102"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = store.RepairVideoMetadata(ctx, created.UID, photos.VideoRepair{DurationMs: new(1_000)})
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("RepairVideoMetadata over a still = %v, want ErrPhotoNotFound", err)
	}
	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.DurationMs != nil {
		t.Errorf("duration_ms = %v, want none — a still has no container", got.DurationMs)
	}
}

// TestListVideosMissingTechnicalMetadata_picksTheBrokenClips checks the backfill's
// candidate set: a clip with no duration, no codec or no frame size is a candidate,
// a complete clip and a still are not — and a repaired clip drops out, which is what
// makes repeated backfills converge.
func TestListVideosMissingTechnicalMetadata_picksTheBrokenClips(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	broken, err := store.Create(ctx, brokenVideo("v110"))
	if err != nil {
		t.Fatalf("Create broken: %v", err)
	}
	noCodec := brokenVideo("v111")
	noCodec.DurationMs = new(5_000)
	noCodec.FileWidth, noCodec.FileHeight = 1920, 1080
	halfRead, err := store.Create(ctx, noCodec)
	if err != nil {
		t.Fatalf("Create half-read: %v", err)
	}
	complete := brokenVideo("v112")
	complete.DurationMs = new(5_000)
	complete.FileWidth, complete.FileHeight = 1920, 1080
	complete.VideoCodec = "h264"
	if _, err := store.Create(ctx, complete); err != nil {
		t.Fatalf("Create complete: %v", err)
	}
	if _, err := store.Create(ctx, samplePhoto("v113")); err != nil {
		t.Fatalf("Create still: %v", err)
	}

	uids, err := store.ListVideosMissingTechnicalMetadata(ctx, 0)
	if err != nil {
		t.Fatalf("ListVideosMissingTechnicalMetadata: %v", err)
	}
	if len(uids) != 2 || !contains(uids, broken.UID) || !contains(uids, halfRead.UID) {
		t.Fatalf("candidates = %v, want exactly %s and %s", uids, broken.UID, halfRead.UID)
	}

	// Repair one of them; it must drop out of the set.
	if _, err := store.RepairVideoMetadata(ctx, broken.UID, photos.VideoRepair{
		DurationMs: new(3_000), Width: new(320), Height: new(240), VideoCodec: new("h264"),
	}); err != nil {
		t.Fatalf("RepairVideoMetadata: %v", err)
	}
	uids, err = store.ListVideosMissingTechnicalMetadata(ctx, 0)
	if err != nil {
		t.Fatalf("second ListVideosMissingTechnicalMetadata: %v", err)
	}
	if len(uids) != 1 || uids[0] != halfRead.UID {
		t.Errorf("candidates after the repair = %v, want just %s", uids, halfRead.UID)
	}
}
