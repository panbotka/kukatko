package facejob

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakePhotoStore is an in-memory PhotoStore for unit tests.
type fakePhotoStore struct {
	photos map[string]photos.Photo
}

// GetByUID returns the stored photo or photos.ErrPhotoNotFound.
func (f *fakePhotoStore) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	p, ok := f.photos[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return p, nil
}

// recordedDetection captures one RecordFaceDetection call.
type recordedDetection struct {
	photoUID string
	faces    []vectors.Face
	model    string
}

// fakeVectorStore is an in-memory VectorStore for unit tests.
type fakeVectorStore struct {
	detected  map[string]bool
	missing   []string
	recorded  []recordedDetection
	recordErr error
}

// FacesDetected reports whether the photo is marked detected.
func (f *fakeVectorStore) FacesDetected(_ context.Context, uid string) (bool, error) {
	return f.detected[uid], nil
}

// RecordFaceDetection records the call and marks the photo detected.
func (f *fakeVectorStore) RecordFaceDetection(
	_ context.Context, uid string, faces []vectors.Face, model string,
) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, recordedDetection{photoUID: uid, faces: faces, model: model})
	if f.detected == nil {
		f.detected = make(map[string]bool)
	}
	f.detected[uid] = true
	return nil
}

// ListPhotosMissingFaces returns the canned missing uids.
func (f *fakeVectorStore) ListPhotosMissingFaces(_ context.Context, _ int) ([]string, error) {
	return append([]string(nil), f.missing...), nil
}

// fakeSource is an ImageSource serving a fixed payload.
type fakeSource struct {
	openErr error
	opened  int
}

// OpenDecodable returns a fixed in-memory reader or openErr.
func (f *fakeSource) OpenDecodable(_ context.Context, _ photos.Photo) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	f.opened++
	return io.NopCloser(strings.NewReader("jpeg-bytes")), nil
}

// fakeClient is an embedding.Client returning canned face detections.
type fakeClient struct {
	faces []embedding.Face
	model string
	err   error
	calls int
}

// ImageEmbedding is unused in these tests.
func (f *fakeClient) ImageEmbedding(_ context.Context, _ io.Reader) ([]float32, string, string, error) {
	return nil, "", "", nil
}

// TextEmbedding is unused in these tests.
func (f *fakeClient) TextEmbedding(_ context.Context, _ string) ([]float32, string, string, error) {
	return nil, "", "", nil
}

// FaceEmbeddings records the call and returns the canned faces or error.
func (f *fakeClient) FaceEmbeddings(_ context.Context, _ io.Reader) ([]embedding.Face, string, error) {
	f.calls++
	if f.err != nil {
		return nil, "", f.err
	}
	return f.faces, f.model, nil
}

// Healthy is unused in these tests.
func (f *fakeClient) Healthy(_ context.Context) bool { return true }

// fakeEnqueuer records enqueued uids.
type fakeEnqueuer struct {
	enqueued []string
	err      error
}

// EnqueueFaceDetect records uid and returns the canned error.
func (f *fakeEnqueuer) EnqueueFaceDetect(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, uid)
	return nil
}

// faceVec builds a FaceDim vector with index 0 set to one.
func faceVec() []float32 {
	v := make([]float32, vectors.FaceDim)
	v[0] = 1
	return v
}

// detection builds an embedding.Face with the given score and box.
func detection(score float64, box [4]float64) embedding.Face {
	return embedding.Face{Dim: vectors.FaceDim, Embedding: faceVec(), BBox: box, DetScore: score}
}

// newService wires a Service over the given fakes with a 0.5 score floor.
func newService(
	t *testing.T, ps PhotoStore, vs VectorStore, c embedding.Client, src ImageSource, e Enqueuer,
) *Service {
	t.Helper()
	return New(Config{Photos: ps, Vectors: vs, Client: c, Source: src, Enqueuer: e, MinDetScore: 0.5})
}

// TestDetect_success stores the detected faces with normalized boxes and model.
func TestDetect_success(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"ph1": {UID: "ph1", FileWidth: 1000, FileHeight: 500, FileOrientation: 1},
	}}
	vs := &fakeVectorStore{}
	client := &fakeClient{model: "buffalo_l", faces: []embedding.Face{
		detection(0.99, [4]float64{100, 50, 300, 150}),
	}}
	src := &fakeSource{}
	svc := newService(t, ps, vs, client, src, &fakeEnqueuer{})

	if err := svc.Detect(context.Background(), "ph1"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(vs.recorded) != 1 {
		t.Fatalf("recorded %d detections, want 1", len(vs.recorded))
	}
	rec := vs.recorded[0]
	if rec.photoUID != "ph1" || rec.model != "buffalo_l" || len(rec.faces) != 1 {
		t.Fatalf("recorded = %+v, want ph1/buffalo_l/1 face", rec)
	}
	face := rec.faces[0]
	if face.FaceIndex != 0 || face.DetScore != 0.99 || face.Model != "buffalo_l" {
		t.Errorf("face metadata = %+v, want index 0 / score 0.99 / buffalo_l", face)
	}
	if want := [4]float64{0.1, 0.1, 0.2, 0.2}; !bboxClose(face.BBox, want) {
		t.Errorf("normalized bbox = %v, want %v", face.BBox, want)
	}
	if face.PhotoWidth != 1000 || face.PhotoHeight != 500 || face.Orientation != 1 {
		t.Errorf("cached dims = %dx%d o%d, want 1000x500 o1",
			face.PhotoWidth, face.PhotoHeight, face.Orientation)
	}
}

// TestDetect_idempotentSkip skips the sidecar when detection is already recorded.
func TestDetect_idempotentSkip(t *testing.T) {
	t.Parallel()

	vs := &fakeVectorStore{detected: map[string]bool{"ph1": true}}
	client := &fakeClient{}
	src := &fakeSource{}
	svc := newService(t, &fakePhotoStore{}, vs, client, src, &fakeEnqueuer{})

	if err := svc.Detect(context.Background(), "ph1"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("sidecar called %d times, want 0 (idempotent skip)", client.calls)
	}
	if src.opened != 0 {
		t.Errorf("image opened %d times, want 0", src.opened)
	}
	if len(vs.recorded) != 0 {
		t.Errorf("recorded %d detections, want 0", len(vs.recorded))
	}
}

// TestDetect_filtersLowScore drops faces below the configured det_score floor and
// reindexes the survivors contiguously.
func TestDetect_filtersLowScore(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"ph1": {UID: "ph1", FileWidth: 100, FileHeight: 100, FileOrientation: 1},
	}}
	vs := &fakeVectorStore{}
	client := &fakeClient{model: "buffalo_l", faces: []embedding.Face{
		detection(0.95, [4]float64{0, 0, 10, 10}), // keep
		detection(0.10, [4]float64{0, 0, 10, 10}), // drop (below 0.5)
		detection(0.80, [4]float64{0, 0, 10, 10}), // keep
		detection(0.49, [4]float64{0, 0, 10, 10}), // drop (below 0.5)
	}}
	svc := newService(t, ps, vs, client, &fakeSource{}, &fakeEnqueuer{})

	if err := svc.Detect(context.Background(), "ph1"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	faces := vs.recorded[0].faces
	if len(faces) != 2 {
		t.Fatalf("stored %d faces, want 2 after filtering", len(faces))
	}
	if faces[0].DetScore != 0.95 || faces[1].DetScore != 0.80 {
		t.Errorf("kept scores = %v/%v, want 0.95/0.80", faces[0].DetScore, faces[1].DetScore)
	}
	if faces[0].FaceIndex != 0 || faces[1].FaceIndex != 1 {
		t.Errorf("reindexed faces = %d/%d, want 0/1", faces[0].FaceIndex, faces[1].FaceIndex)
	}
}

// TestDetect_disabledFilterKeepsAll stores every detection when MinDetScore <= 0.
func TestDetect_disabledFilterKeepsAll(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{
		"ph1": {UID: "ph1", FileWidth: 100, FileHeight: 100, FileOrientation: 1},
	}}
	vs := &fakeVectorStore{}
	client := &fakeClient{faces: []embedding.Face{
		detection(0.95, [4]float64{0, 0, 10, 10}),
		detection(0.01, [4]float64{0, 0, 10, 10}),
	}}
	svc := New(Config{
		Photos: ps, Vectors: vs, Client: client, Source: &fakeSource{},
		Enqueuer: &fakeEnqueuer{}, MinDetScore: -1,
	})

	if err := svc.Detect(context.Background(), "ph1"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got := len(vs.recorded[0].faces); got != 2 {
		t.Errorf("stored %d faces, want 2 (filter disabled)", got)
	}
}

// TestDetect_noFacesStillRecords records a zero-face detection so the photo is
// marked processed and not re-enqueued by the backfill.
func TestDetect_noFacesStillRecords(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1"}}}
	vs := &fakeVectorStore{}
	client := &fakeClient{model: "buffalo_l"}
	svc := newService(t, ps, vs, client, &fakeSource{}, &fakeEnqueuer{})

	if err := svc.Detect(context.Background(), "ph1"); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(vs.recorded) != 1 || len(vs.recorded[0].faces) != 0 {
		t.Fatalf("recorded = %+v, want one zero-face detection", vs.recorded)
	}
	if !vs.detected["ph1"] {
		t.Error("photo not marked detected after a zero-face run")
	}
}

// TestDetect_offlineDefers maps an unavailable sidecar to a worker.RetryAfter.
func TestDetect_offlineDefers(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1"}}}
	client := &fakeClient{err: embedding.ErrUnavailable}
	svc := New(Config{
		Photos: ps, Vectors: &fakeVectorStore{}, Client: client,
		Source: &fakeSource{}, Enqueuer: &fakeEnqueuer{},
		OfflineRetryDelay: 90 * time.Second, MinDetScore: 0.5,
	})

	err := svc.Detect(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("Detect offline = %v, want RetryAfterError", err)
	}
	if ra.Delay != 90*time.Second {
		t.Errorf("retry delay = %v, want 90s", ra.Delay)
	}
	if !embedding.IsUnavailable(err) {
		t.Errorf("error %v should still report IsUnavailable", err)
	}
}

// TestDetect_badResponseFails returns a plain (non-defer) error for a bad sidecar
// response so the job follows the normal retry/dead-letter path.
func TestDetect_badResponseFails(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1"}}}
	client := &fakeClient{err: embedding.ErrBadResponse}
	svc := newService(t, ps, &fakeVectorStore{}, client, &fakeSource{}, &fakeEnqueuer{})

	err := svc.Detect(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if errors.As(err, &ra) {
		t.Fatalf("Detect bad response = %v, want a plain error not RetryAfter", err)
	}
	if !errors.Is(err, embedding.ErrBadResponse) {
		t.Errorf("Detect error = %v, want ErrBadResponse", err)
	}
}

// TestDetect_missingPhotoFails returns an error when the photo is gone.
func TestDetect_missingPhotoFails(t *testing.T) {
	t.Parallel()

	svc := newService(t, &fakePhotoStore{}, &fakeVectorStore{}, &fakeClient{}, &fakeSource{}, &fakeEnqueuer{})
	if err := svc.Detect(context.Background(), "gone"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("Detect missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestHandle_payload covers payload decoding and dispatch to Detect.
func TestHandle_payload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantErr error
	}{
		{name: "valid", payload: `{"photo_uid":"ph1"}`, wantErr: nil},
		{name: "missing uid", payload: `{}`, wantErr: ErrMissingPhotoUID},
		{name: "bad json", payload: `{not json`, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1"}}}
			svc := newService(t, ps, &fakeVectorStore{}, &fakeClient{}, &fakeSource{}, &fakeEnqueuer{})

			err := svc.Handle(context.Background(), jobs.Job{Type: jobs.TypeFaceDetect, Payload: []byte(tt.payload)})
			switch {
			case tt.name == "bad json":
				if err == nil {
					t.Error("Handle bad json = nil, want an error")
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Handle error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Errorf("Handle error = %v, want nil", err)
				}
			}
		})
	}
}

// TestBackfillFaces enqueues one job per unprocessed photo and counts them.
func TestBackfillFaces(t *testing.T) {
	t.Parallel()

	vs := &fakeVectorStore{missing: []string{"ph1", "ph2", "ph3"}}
	enq := &fakeEnqueuer{}
	svc := newService(t, &fakePhotoStore{}, vs, &fakeClient{}, &fakeSource{}, enq)

	n, err := svc.BackfillFaces(context.Background())
	if err != nil {
		t.Fatalf("BackfillFaces: %v", err)
	}
	if n != 3 {
		t.Errorf("enqueued count = %d, want 3", n)
	}
	if strings.Join(enq.enqueued, ",") != "ph1,ph2,ph3" {
		t.Errorf("enqueued = %v, want [ph1 ph2 ph3]", enq.enqueued)
	}
}

// TestNew_panicsWithoutDeps verifies New rejects a missing collaborator.
func TestNew_panicsWithoutDeps(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New did not panic on missing dependency")
		}
	}()
	New(Config{Photos: &fakePhotoStore{}})
}
