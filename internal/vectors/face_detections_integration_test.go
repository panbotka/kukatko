//go:build integration

package vectors_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestRecordFaceDetection_storesFacesAndMarksProcessed records faces and marks
// the photo detected in one call, then verifies both the faces and the detection
// flag are persisted.
func TestRecordFaceDetection_storesFacesAndMarksProcessed(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "fd_store")

	faces := []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),
		sampleFace(1, faceVec(map[int]float32{1: 1})),
	}
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Faces: faces, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}

	got, err := store.ListFaces(ctx, uid)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListFaces = %d faces, %v; want 2", len(got), err)
	}
	detected, err := store.FacesDetected(ctx, uid)
	if err != nil || !detected {
		t.Fatalf("FacesDetected = %v, %v; want true, nil", detected, err)
	}
}

// TestRecordFaceDetection_persistsTheFrame verifies the detection frame survives a
// round trip and that an unmeasurable frame is stored as NULL rather than zero: a
// zero would read like a measurement and would not be distinguishable from the
// detections written before the columns existed.
func TestRecordFaceDetection_persistsTheFrame(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()

	measured := makePhoto(t, photoStore, "fd_frame")
	if err := store.RecordFaceDetection(ctx, measured, vectors.Detection{
		Model: "buffalo_l", FrameWidth: 3000, FrameHeight: 4000,
	}); err != nil {
		t.Fatalf("RecordFaceDetection measured: %v", err)
	}
	var width, height *int
	row := db.Pool().QueryRow(ctx,
		"SELECT detect_width, detect_height FROM face_detections WHERE photo_uid = $1", measured)
	if err := row.Scan(&width, &height); err != nil {
		t.Fatalf("reading the recorded frame: %v", err)
	}
	if width == nil || height == nil || *width != 3000 || *height != 4000 {
		t.Errorf("recorded frame = %v x %v, want 3000x4000", width, height)
	}

	unmeasured := makePhoto(t, photoStore, "fd_noframe")
	if err := store.RecordFaceDetection(ctx, unmeasured, vectors.Detection{Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection unmeasured: %v", err)
	}
	row = db.Pool().QueryRow(ctx,
		"SELECT detect_width, detect_height FROM face_detections WHERE photo_uid = $1", unmeasured)
	if err := row.Scan(&width, &height); err != nil {
		t.Fatalf("reading the unmeasured frame: %v", err)
	}
	if width != nil || height != nil {
		t.Errorf("unmeasured frame = %v x %v, want NULL x NULL", width, height)
	}
}

// TestRecordFaceDetection_zeroFaces marks a photo with no faces as processed, so
// it is distinguishable from a never-processed photo.
func TestRecordFaceDetection_zeroFaces(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "fd_zero")

	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if got, _ := store.ListFaces(ctx, uid); len(got) != 0 {
		t.Fatalf("ListFaces = %d, want 0", len(got))
	}
	detected, err := store.FacesDetected(ctx, uid)
	if err != nil || !detected {
		t.Fatalf("FacesDetected = %v, %v; want true, nil (zero-face photo is processed)", detected, err)
	}
}

// TestRecordFaceDetection_idempotentReplace re-runs detection and verifies the
// faces are replaced rather than appended, and the photo stays processed.
func TestRecordFaceDetection_idempotentReplace(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "fd_replace")

	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Faces: []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),
		sampleFace(1, faceVec(map[int]float32{1: 1})),
	}, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection first: %v", err)
	}
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Faces: []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{2: 1})),
	}, Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection second: %v", err)
	}
	if got, _ := store.ListFaces(ctx, uid); len(got) != 1 {
		t.Fatalf("ListFaces after replace = %d, want 1", len(got))
	}
	if detected, _ := store.FacesDetected(ctx, uid); !detected {
		t.Error("photo not marked detected after re-detection")
	}
}

// TestFacesDetected_unprocessed reports false for a photo that never ran detection.
func TestFacesDetected_unprocessed(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "fd_none")

	detected, err := store.FacesDetected(ctx, uid)
	if err != nil || detected {
		t.Fatalf("FacesDetected = %v, %v; want false, nil", detected, err)
	}
}

// TestListPhotosMissingFaces returns only photos without a detection record and
// excludes archived ones.
func TestListPhotosMissingFaces(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	processed := makePhoto(t, photoStore, "fd_processed")
	missing := makePhoto(t, photoStore, "fd_missing")
	archived := makePhoto(t, photoStore, "fd_archived")

	if err := store.RecordFaceDetection(ctx, processed, vectors.Detection{Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if _, err := photoStore.Archive(ctx, archived); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	uids, err := store.ListPhotosMissingFaces(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingFaces: %v", err)
	}
	if len(uids) != 1 || uids[0] != missing {
		t.Fatalf("ListPhotosMissingFaces = %v, want only %s", uids, missing)
	}
}

// TestFaceDetectionCascadeDelete checks that deleting a photo removes its
// detection record (no orphans).
func TestFaceDetectionCascadeDelete(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "fd_cascade")

	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{Model: "buffalo_l"}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if err := photoStore.Delete(ctx, uid); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	if detected, _ := store.FacesDetected(ctx, uid); detected {
		t.Error("face detection record survived photo delete")
	}
}
