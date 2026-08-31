package facejob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// forcedFixture wires a service over a photo whose detection is already
// recorded — the state that tells the repair and the rebuild apart — with a
// sidecar that would find two faces.
func forcedFixture(t *testing.T) (*Service, *fakeVectorStore, *fakeClient) {
	t.Helper()
	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"ph1": {UID: "ph1", FileWidth: 1000, FileHeight: 500, FileOrientation: 1},
	}}
	vs := &fakeVectorStore{detected: map[string]bool{"ph1": true}}
	client := &fakeClient{model: "buffalo_l", faces: []embedding.Face{
		detection(0.9, [4]float64{0, 0, 100, 100}),
		detection(0.8, [4]float64{200, 100, 100, 100}),
	}}
	src := &fakeSource{width: 1000, height: 500, orientation: 1}
	return newService(t, ps, vs, client, src, &fakeEnqueuer{}), vs, client
}

// TestForceDetect_recomputesWhereDetectSkips is the whole point of the forced
// path: on a photo whose detection is recorded the repair calls no sidecar and
// stores nothing, while the rebuild runs the detector again and records what it
// finds, reporting how many faces the photo has afterwards.
func TestForceDetect_recomputesWhereDetectSkips(t *testing.T) {
	t.Parallel()

	t.Run("repair skips a photo already detected", func(t *testing.T) {
		t.Parallel()
		svc, vs, client := forcedFixture(t)
		if err := svc.Detect(context.Background(), "ph1"); err != nil {
			t.Fatalf("Detect: %v", err)
		}
		if client.calls != 0 {
			t.Errorf("sidecar calls = %d, want 0", client.calls)
		}
		if len(vs.recorded) != 0 {
			t.Errorf("recorded %d detections, want 0", len(vs.recorded))
		}
	})

	t.Run("rebuild re-detects and reports the count", func(t *testing.T) {
		t.Parallel()
		svc, vs, client := forcedFixture(t)
		count, err := svc.ForceDetect(context.Background(), "ph1")
		if err != nil {
			t.Fatalf("ForceDetect: %v", err)
		}
		if client.calls != 1 {
			t.Errorf("sidecar calls = %d, want 1", client.calls)
		}
		if count != 2 {
			t.Errorf("ForceDetect count = %d, want 2", count)
		}
		if len(vs.recorded) != 1 {
			t.Fatalf("recorded %d detections, want 1", len(vs.recorded))
		}
		if got := len(vs.recorded[0].det.Faces); got != 2 {
			t.Errorf("recorded %d faces, want 2", got)
		}
	})
}

// TestForceDetect_reportsZeroForAnEmptyPhoto keeps "looked and found nobody" a
// result rather than a failure: the count is zero, the detection is recorded, and
// the previous faces are gone — which is exactly what a rebuild of a photo whose
// old detections were spurious must do.
func TestForceDetect_reportsZeroForAnEmptyPhoto(t *testing.T) {
	t.Parallel()

	svc, vs, client := forcedFixture(t)
	client.faces = nil

	count, err := svc.ForceDetect(context.Background(), "ph1")
	if err != nil {
		t.Fatalf("ForceDetect: %v", err)
	}
	if count != 0 {
		t.Errorf("ForceDetect count = %d, want 0", count)
	}
	if len(vs.recorded) != 1 || len(vs.recorded[0].det.Faces) != 0 {
		t.Errorf("recorded = %+v, want one detection with no faces", vs.recorded)
	}
}

// TestHandle_forcePayloadRedetects proves the queued job honours the payload's
// force flag, which is what lets the forced enqueue keep dedup keyed on type +
// photo uid rather than needing a job type of its own.
func TestHandle_forcePayloadRedetects(t *testing.T) {
	t.Parallel()

	for _, force := range []bool{false, true} {
		name := "plain payload skips"
		if force {
			name = "forced payload re-detects"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, _, client := forcedFixture(t)
			payload, err := json.Marshal(map[string]any{"photo_uid": "ph1", "force": force})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			if err := svc.Handle(context.Background(), jobs.Job{
				Type: jobs.TypeFaceDetect, Payload: payload,
			}); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			want := 0
			if force {
				want = 1
			}
			if client.calls != want {
				t.Errorf("sidecar calls = %d, want %d", client.calls, want)
			}
		})
	}
}

// TestForceDetect_offlineDefers confirms the rebuild answers an offline box the
// way the repair does — a worker deferral — so the forced job waits in the queue
// and the on-demand caller can queue the work instead of failing the request.
func TestForceDetect_offlineDefers(t *testing.T) {
	t.Parallel()

	svc, vs, client := forcedFixture(t)
	client.err = embedding.ErrUnavailable

	if _, err := svc.ForceDetect(context.Background(), "ph1"); !worker.IsDeferral(err) {
		t.Fatalf("ForceDetect with the box offline = %v, want a worker deferral", err)
	}
	if len(vs.recorded) != 0 {
		t.Errorf("recorded %d detections, want none — the old faces must survive", len(vs.recorded))
	}
}

// TestForceDetect_missingPhoto keeps the rebuild's error surface the same as the
// repair's, so the HTTP layer can answer 404 from the same sentinel.
func TestForceDetect_missingPhoto(t *testing.T) {
	t.Parallel()

	svc, _, _ := forcedFixture(t)
	if _, err := svc.ForceDetect(context.Background(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("ForceDetect on an unknown photo = %v, want photos.ErrPhotoNotFound", err)
	}
}
