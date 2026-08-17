//go:build integration

package vectors_test

import (
	"context"
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The geometry of the photo the bug report measured: `phqale6fftf3a3v5tn17vtfd3d`
// is stored 3000 × 4000 with EXIF orientation 6 while its original is really
// 4000 × 3000, so the display frame is 3000 × 4000 and the raw one 4000 × 3000.
// The photos half of `repair --dimensions` has already written the raw pair onto
// the photo; the face rows still cache the display pair, which is the fingerprint
// the faces half finds them by.
const (
	rawWidth       = 4000
	rawHeight      = 3000
	rawOrientation = 6
)

// rawSpaceBBox is the detection as the catalogue holds it: a correct box in the
// RAW frame, which is a quarter turn away from where the face is displayed.
var rawSpaceBBox = [4]float64{0.21085, 0.68267, 0.21971, 0.14213}

// displaySpaceBBox is a detection on the same photo that is already right — only
// its cached frame is transposed. It sits far enough from the first that neither
// row's candidate transforms reach the other's marker.
var displaySpaceBBox = [4]float64{0.60, 0.70, 0.10, 0.10}

// makeRotatedPhoto inserts a photo with the reported geometry and returns its uid.
func makeRotatedPhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash:        hash,
		FilePath:        "2024/01/" + hash + ".jpg",
		FileName:        hash + ".jpg",
		FileWidth:       rawWidth,
		FileHeight:      rawHeight,
		FileOrientation: rawOrientation,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// transposedFace builds a face row as the defect left it: the cached frame is the
// DISPLAYED pair (the photo's stored pair swapped) whatever space the box is in.
func transposedFace(index int, bbox [4]float64) vectors.Face {
	return vectors.Face{
		FaceIndex:   index,
		Vector:      faceVec(map[int]float32{index: 1}),
		BBox:        bbox,
		Model:       "buffalo_l",
		PhotoWidth:  rawHeight,
		PhotoHeight: rawWidth,
		Orientation: rawOrientation,
	}
}

// addMarker inserts a valid face marker centred on the given box's centre — the
// region a person has seen sit on the face in the displayed image, which is the
// evidence the repair reconciles a detection against.
func addMarker(t *testing.T, db *database.DB, uid, markerUID string, on [4]float64) {
	t.Helper()
	const stmt = `INSERT INTO markers (uid, photo_uid, type, x, y, w, h)
	              VALUES ($1, $2, 'face', $3, $4, 0.12, 0.16)`
	x := on[0] + on[2]/2 - 0.06
	y := on[1] + on[3]/2 - 0.08
	if _, err := db.Pool().Exec(context.Background(), stmt, markerUID, uid, x, y); err != nil {
		t.Fatalf("inserting marker %s: %v", markerUID, err)
	}
}

// faceByIndex returns one face of a photo by its per-photo slot.
func faceByIndex(t *testing.T, store *vectors.Store, uid string, index int) vectors.Face {
	t.Helper()
	faces, err := store.ListFaces(context.Background(), uid)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	for _, face := range faces {
		if face.FaceIndex == index {
			return face
		}
	}
	t.Fatalf("photo %s has no face %d (got %d faces)", uid, index, len(faces))
	return vectors.Face{}
}

// TestFaceBoxRepair_turnsARawSpaceBoxOntoTheFace is the reported case end to end:
// a quarter-turned photo carrying one detection recorded in the raw frame, one
// already in display space and one row that was never wrong. The repair turns the
// first onto its marker, corrects only the cached frame of the second, and does
// not even look at the third.
func TestFaceBoxRepair_turnsARawSpaceBoxOntoTheFace(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()
	uid := makeRotatedPhoto(t, photoStore, "rot_repair")

	// The third row is already right: its cached frame is the photo's stored pair,
	// so it is not a candidate at all.
	correctRow := transposedFace(2, [4]float64{0.30, 0.30, 0.10, 0.10})
	correctRow.PhotoWidth, correctRow.PhotoHeight = rawWidth, rawHeight
	faces := []vectors.Face{
		transposedFace(0, rawSpaceBBox),
		transposedFace(1, displaySpaceBBox),
		correctRow,
	}
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Faces: faces, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	turned := vectors.RotateRawBBox(rawSpaceBBox, rawOrientation)
	addMarker(t, db, uid, "mk_turned", turned)
	addMarker(t, db, uid, "mk_correct", displaySpaceBBox)

	plans, err := store.PlanFaceBoxRepair(ctx)
	if err != nil {
		t.Fatalf("PlanFaceBoxRepair: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("planned %d rows, want 2 (the already-correct row is not a candidate): %+v", len(plans), plans)
	}
	if plans[0].Transform != vectors.TransformRotate || plans[1].Transform != vectors.TransformFrame {
		t.Fatalf("transforms = %d/%d, want rotate/frame", plans[0].Transform, plans[1].Transform)
	}

	applied, err := store.ApplyFaceBoxRepair(ctx, plans)
	if err != nil {
		t.Fatalf("ApplyFaceBoxRepair: %v", err)
	}
	if applied != 2 {
		t.Fatalf("applied %d rows, want 2", applied)
	}

	// The raw-space box ended up on the face: turned into display space, its centre
	// within a fifth of a face box of the marker a person put there.
	got := faceByIndex(t, store, uid, 0)
	for i := range got.BBox {
		if math.Abs(got.BBox[i]-turned[i]) > 1e-9 {
			t.Errorf("bbox[%d] = %v, want the turned box %v", i, got.BBox[i], turned[i])
		}
	}
	if got.PhotoWidth != rawWidth || got.PhotoHeight != rawHeight {
		t.Errorf("frame = %d×%d, want %d×%d", got.PhotoWidth, got.PhotoHeight, rawWidth, rawHeight)
	}

	// The already-correct box was not moved; only its cached frame was corrected.
	kept := faceByIndex(t, store, uid, 1)
	if kept.BBox != displaySpaceBBox {
		t.Errorf("bbox = %v, want it untouched %v", kept.BBox, displaySpaceBBox)
	}
	if kept.PhotoWidth != rawWidth || kept.PhotoHeight != rawHeight {
		t.Errorf("frame = %d×%d, want %d×%d", kept.PhotoWidth, kept.PhotoHeight, rawWidth, rawHeight)
	}

	// The row that was never wrong is untouched in every column.
	untouched := faceByIndex(t, store, uid, 2)
	if untouched.BBox != correctRow.BBox ||
		untouched.PhotoWidth != rawWidth || untouched.PhotoHeight != rawHeight {
		t.Errorf("already-correct row changed: %+v", untouched)
	}

	// Idempotent: the fingerprint no longer matches, so a second run plans nothing.
	again, err := store.PlanFaceBoxRepair(ctx)
	if err != nil || len(again) != 0 {
		t.Errorf("second plan = (%+v, %v), want empty", again, err)
	}
}

// TestFaceBoxRepair_leavesRowsWithoutEvidenceForALaterRun verifies the promise the
// skip rests on: a photo with no marker to reconcile against is planned as a skip,
// nothing is written for it, and it is still a candidate afterwards — so once a
// marker does exist, a later run picks it up.
func TestFaceBoxRepair_leavesRowsWithoutEvidenceForALaterRun(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()
	uid := makeRotatedPhoto(t, photoStore, "rot_blind")

	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{transposedFace(0, rawSpaceBBox)}, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}

	plans, err := store.PlanFaceBoxRepair(ctx)
	if err != nil {
		t.Fatalf("PlanFaceBoxRepair: %v", err)
	}
	if len(plans) != 1 || plans[0].Transform != vectors.TransformSkip {
		t.Fatalf("plans = %+v, want one skip", plans)
	}
	applied, err := store.ApplyFaceBoxRepair(ctx, plans)
	if err != nil || applied != 0 {
		t.Fatalf("ApplyFaceBoxRepair = (%d, %v), want (0, nil)", applied, err)
	}
	before := faceByIndex(t, store, uid, 0)
	if before.BBox != rawSpaceBBox || before.PhotoWidth != rawHeight || before.PhotoHeight != rawWidth {
		t.Errorf("skipped row was written: %+v", before)
	}

	// The marker arrives (somebody tagged the face) — now the same row decides.
	addMarker(t, db, uid, "mk_late", vectors.RotateRawBBox(rawSpaceBBox, rawOrientation))
	plans, err = store.PlanFaceBoxRepair(ctx)
	if err != nil {
		t.Fatalf("PlanFaceBoxRepair after the marker: %v", err)
	}
	if len(plans) != 1 || plans[0].Transform != vectors.TransformRotate {
		t.Fatalf("plans = %+v, want one rotate", plans)
	}
	if applied, err = store.ApplyFaceBoxRepair(ctx, plans); err != nil || applied != 1 {
		t.Fatalf("ApplyFaceBoxRepair = (%d, %v), want (1, nil)", applied, err)
	}
}

// TestFaceBoxRepair_ignoresUnrotatedPhotos verifies the candidate set is the
// orientation defect and nothing else: an upright photo's faces are never planned,
// however their cached frame happens to compare.
func TestFaceBoxRepair_ignoresUnrotatedPhotos(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()
	created, err := photoStore.Create(ctx, photos.Photo{
		FileHash:        "upright",
		FilePath:        "2024/01/upright.jpg",
		FileName:        "upright.jpg",
		FileWidth:       rawWidth,
		FileHeight:      rawHeight,
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("creating photo: %v", err)
	}
	face := transposedFace(0, rawSpaceBBox)
	face.Orientation = 1
	if err := store.RecordFaceDetection(ctx, created.UID,
		vectors.Detection{Faces: []vectors.Face{face}, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	addMarker(t, db, created.UID, "mk_upright", rawSpaceBBox)

	plans, err := store.PlanFaceBoxRepair(ctx)
	if err != nil {
		t.Fatalf("PlanFaceBoxRepair: %v", err)
	}
	if len(plans) != 0 {
		t.Errorf("planned %+v, want nothing for an upright photo", plans)
	}
}
