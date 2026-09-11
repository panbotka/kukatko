package facejob

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/photos"
)

// videoService wires a Service over a library holding one clip and one
// photograph, and hands back the fakes the assertions read.
func videoService(t *testing.T) (*Service, *fakeVectorStore, *fakeClient) {
	t.Helper()
	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"clip": {UID: "clip", MediaType: photos.MediaVideo, FileWidth: 1920, FileHeight: 1080},
		"live": {UID: "live", MediaType: photos.MediaLive, FileWidth: 1000, FileHeight: 500},
	}}
	vs := &fakeVectorStore{}
	client := &fakeClient{model: "buffalo_l", faces: []embedding.Face{
		detection(0.99, [4]float64{100, 50, 300, 150}),
	}}
	src := &fakeSource{width: 1000, height: 500, orientation: 1}
	return newService(t, ps, vs, client, src, &fakeEnqueuer{}), vs, client
}

// TestDetect_skipsVideo verifies a clip is passed over without calling the
// sidecar and without writing anything — neither faces nor the record that the
// detector looked, which would report the step as done for work that no longer
// happens. The job succeeds: failing it would dead-letter work that was never
// meant to run.
func TestDetect_skipsVideo(t *testing.T) {
	t.Parallel()

	svc, vs, client := videoService(t)
	if err := svc.Detect(context.Background(), "clip"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("sidecar calls = %d, want 0", client.calls)
	}
	if len(vs.recorded) != 0 {
		t.Errorf("recorded %d detections, want none", len(vs.recorded))
	}
}

// TestForceDetect_skipsVideo verifies the rebuild is no different: forcing does
// not buy a clip a detection that no longer exists, and it reports no faces
// rather than an error.
func TestForceDetect_skipsVideo(t *testing.T) {
	t.Parallel()

	svc, vs, client := videoService(t)
	count, err := svc.ForceDetect(context.Background(), "clip")
	if err != nil {
		t.Fatalf("ForceDetect: %v", err)
	}
	if count != 0 {
		t.Errorf("faces = %d, want 0", count)
	}
	if client.calls != 0 || len(vs.recorded) != 0 {
		t.Errorf("calls = %d, recorded = %d, want 0/0", client.calls, len(vs.recorded))
	}
}

// TestDetect_livePhotoIsStillDetected pins down the boundary: a live photo is a
// photograph that happens to carry a motion clip beside it, the detector reads
// the photograph, and it always did.
func TestDetect_livePhotoIsStillDetected(t *testing.T) {
	t.Parallel()

	svc, vs, client := videoService(t)
	if err := svc.Detect(context.Background(), "live"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if client.calls != 1 {
		t.Errorf("sidecar calls = %d, want 1", client.calls)
	}
	if len(vs.recorded) != 1 {
		t.Fatalf("recorded %d detections, want 1", len(vs.recorded))
	}
}
