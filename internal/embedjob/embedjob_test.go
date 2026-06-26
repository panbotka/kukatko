package embedjob

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

// fakeVectorStore is an in-memory VectorStore for unit tests.
type fakeVectorStore struct {
	embeddings map[string]vectors.Embedding
	missing    []string
	saved      []vectors.Embedding
	similar    []vectors.Match
}

// GetEmbedding returns the stored embedding or vectors.ErrEmbeddingNotFound.
func (f *fakeVectorStore) GetEmbedding(_ context.Context, uid string) (vectors.Embedding, error) {
	emb, ok := f.embeddings[uid]
	if !ok {
		return vectors.Embedding{}, vectors.ErrEmbeddingNotFound
	}
	return emb, nil
}

// SaveEmbedding records the embedding and stores it for later reads.
func (f *fakeVectorStore) SaveEmbedding(_ context.Context, emb vectors.Embedding) (vectors.Embedding, error) {
	f.saved = append(f.saved, emb)
	if f.embeddings == nil {
		f.embeddings = make(map[string]vectors.Embedding)
	}
	f.embeddings[emb.PhotoUID] = emb
	return emb, nil
}

// FindSimilar returns the canned similar matches.
func (f *fakeVectorStore) FindSimilar(
	_ context.Context, _ []float32, _ int, _ float64,
) ([]vectors.Match, error) {
	return append([]vectors.Match(nil), f.similar...), nil
}

// ListPhotosMissingEmbedding returns the canned missing uids.
func (f *fakeVectorStore) ListPhotosMissingEmbedding(_ context.Context, _ int) ([]string, error) {
	return append([]string(nil), f.missing...), nil
}

// fakePreviewer is a Previewer that serves a fixed preview payload.
type fakePreviewer struct {
	generateErr error
	openErr     error
}

// Generate records nothing and returns generateErr.
func (f *fakePreviewer) Generate(
	_ context.Context, _ photos.Photo, _ ...string,
) (map[string]string, error) {
	return map[string]string{}, f.generateErr
}

// Open returns a fixed in-memory preview reader or openErr.
func (f *fakePreviewer) Open(_, _ string) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return io.NopCloser(strings.NewReader("jpeg-bytes")), nil
}

// fakeClient is an embedding.Client returning canned image embeddings.
type fakeClient struct {
	vec        []float32
	model      string
	pretrained string
	err        error
	calls      int
}

// ImageEmbedding records the call and returns the canned vector or error.
func (f *fakeClient) ImageEmbedding(_ context.Context, _ io.Reader) ([]float32, string, string, error) {
	f.calls++
	if f.err != nil {
		return nil, "", "", f.err
	}
	return f.vec, f.model, f.pretrained, nil
}

// TextEmbedding is unused in these tests.
func (f *fakeClient) TextEmbedding(_ context.Context, _ string) ([]float32, string, string, error) {
	return nil, "", "", nil
}

// FaceEmbeddings is unused in these tests.
func (f *fakeClient) FaceEmbeddings(_ context.Context, _ io.Reader) ([]embedding.Face, string, error) {
	return nil, "", nil
}

// Healthy is unused in these tests.
func (f *fakeClient) Healthy(_ context.Context) bool { return true }

// fakeEnqueuer records enqueued uids.
type fakeEnqueuer struct {
	enqueued []string
	err      error
}

// EnqueueImageEmbed records uid and returns the canned error.
func (f *fakeEnqueuer) EnqueueImageEmbed(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, uid)
	return nil
}

// imageVec builds an ImageDim vector with index 0 set to one.
func imageVec() []float32 {
	v := make([]float32, vectors.ImageDim)
	v[0] = 1
	return v
}

// newService wires a Service over the given fakes with sane defaults.
func newService(t *testing.T, ps PhotoStore, vs VectorStore, c embedding.Client, p Previewer, e Enqueuer) *Service {
	t.Helper()
	return New(Config{
		Photos: ps, Vectors: vs, Client: c, Previewer: p, Enqueuer: e,
		DuplicateMaxDist: 0.05,
	})
}

// TestEmbed_success stores the embedding returned by the sidecar.
func TestEmbed_success(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1", FileHash: "abc"}}}
	vs := &fakeVectorStore{}
	client := &fakeClient{vec: imageVec(), model: "ViT-B-32", pretrained: "laion2b"}
	svc := newService(t, ps, vs, client, &fakePreviewer{}, &fakeEnqueuer{})

	if err := svc.Embed(context.Background(), "ph1"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vs.saved) != 1 {
		t.Fatalf("saved %d embeddings, want 1", len(vs.saved))
	}
	got := vs.saved[0]
	if got.PhotoUID != "ph1" || got.Model != "ViT-B-32" || got.Pretrained != "laion2b" {
		t.Errorf("saved embedding = %+v, want ph1/ViT-B-32/laion2b", got)
	}
	if len(got.Vector) != vectors.ImageDim {
		t.Errorf("saved vector len = %d, want %d", len(got.Vector), vectors.ImageDim)
	}
}

// TestEmbed_idempotent skips the sidecar when an embedding already exists.
func TestEmbed_idempotent(t *testing.T) {
	t.Parallel()

	vs := &fakeVectorStore{embeddings: map[string]vectors.Embedding{"ph1": {PhotoUID: "ph1", Vector: imageVec()}}}
	client := &fakeClient{vec: imageVec()}
	svc := newService(t, &fakePhotoStore{}, vs, client, &fakePreviewer{}, &fakeEnqueuer{})

	if err := svc.Embed(context.Background(), "ph1"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if client.calls != 0 {
		t.Errorf("sidecar called %d times, want 0 (idempotent skip)", client.calls)
	}
	if len(vs.saved) != 0 {
		t.Errorf("saved %d embeddings, want 0", len(vs.saved))
	}
}

// TestEmbed_offlineDefers maps an unavailable sidecar to a worker.RetryAfter.
func TestEmbed_offlineDefers(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1", FileHash: "abc"}}}
	client := &fakeClient{err: embedding.ErrUnavailable}
	svc := New(Config{
		Photos: ps, Vectors: &fakeVectorStore{}, Client: client,
		Previewer: &fakePreviewer{}, Enqueuer: &fakeEnqueuer{},
		OfflineRetryDelay: 90 * time.Second,
	})

	err := svc.Embed(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("Embed offline = %v, want RetryAfterError", err)
	}
	if ra.Delay != 90*time.Second {
		t.Errorf("retry delay = %v, want 90s", ra.Delay)
	}
	if !embedding.IsUnavailable(err) {
		t.Errorf("error %v should still report IsUnavailable", err)
	}
}

// TestEmbed_badResponseFails returns a plain (non-defer) error for a bad sidecar
// response so the job follows the normal retry/dead-letter path.
func TestEmbed_badResponseFails(t *testing.T) {
	t.Parallel()

	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1", FileHash: "abc"}}}
	client := &fakeClient{err: embedding.ErrBadResponse}
	svc := newService(t, ps, &fakeVectorStore{}, client, &fakePreviewer{}, &fakeEnqueuer{})

	err := svc.Embed(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if errors.As(err, &ra) {
		t.Fatalf("Embed bad response = %v, want a plain error not RetryAfter", err)
	}
	if !errors.Is(err, embedding.ErrBadResponse) {
		t.Errorf("Embed error = %v, want ErrBadResponse", err)
	}
}

// TestEmbed_missingPhotoFails returns an error when the photo is gone.
func TestEmbed_missingPhotoFails(t *testing.T) {
	t.Parallel()

	svc := newService(t, &fakePhotoStore{}, &fakeVectorStore{}, &fakeClient{}, &fakePreviewer{}, &fakeEnqueuer{})
	if err := svc.Embed(context.Background(), "gone"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Fatalf("Embed missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestHandle_payload covers payload decoding and dispatch to Embed.
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
			ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1", FileHash: "abc"}}}
			svc := newService(t, ps, &fakeVectorStore{}, &fakeClient{vec: imageVec()}, &fakePreviewer{}, &fakeEnqueuer{})

			err := svc.Handle(context.Background(), jobs.Job{Type: jobs.TypeImageEmbed, Payload: []byte(tt.payload)})
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

// TestBackfillEmbeddings enqueues one job per missing photo and counts them.
func TestBackfillEmbeddings(t *testing.T) {
	t.Parallel()

	vs := &fakeVectorStore{missing: []string{"ph1", "ph2", "ph3"}}
	enq := &fakeEnqueuer{}
	svc := newService(t, &fakePhotoStore{}, vs, &fakeClient{}, &fakePreviewer{}, enq)

	n, err := svc.BackfillEmbeddings(context.Background())
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	if n != 3 {
		t.Errorf("enqueued count = %d, want 3", n)
	}
	if strings.Join(enq.enqueued, ",") != "ph1,ph2,ph3" {
		t.Errorf("enqueued = %v, want [ph1 ph2 ph3]", enq.enqueued)
	}
}

// TestDuplicates excludes the source photo and respects the disabled threshold.
func TestDuplicates(t *testing.T) {
	t.Parallel()

	vs := &fakeVectorStore{
		embeddings: map[string]vectors.Embedding{"ph1": {PhotoUID: "ph1", Vector: imageVec()}},
		similar: []vectors.Match{
			{PhotoUID: "ph1", Distance: 0},
			{PhotoUID: "ph2", Distance: 0.01},
		},
	}
	svc := newService(t, &fakePhotoStore{}, vs, &fakeClient{}, &fakePreviewer{}, &fakeEnqueuer{})

	dups, err := svc.Duplicates(context.Background(), "ph1")
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(dups) != 1 || dups[0].PhotoUID != "ph2" {
		t.Errorf("Duplicates = %+v, want only ph2", dups)
	}

	// No embedding yet → no duplicates, no error.
	none, err := svc.Duplicates(context.Background(), "missing")
	if err != nil || none != nil {
		t.Errorf("Duplicates(missing) = %v, %v; want nil, nil", none, err)
	}

	// Disabled threshold → no duplicates regardless of matches.
	disabled := New(Config{
		Photos: &fakePhotoStore{}, Vectors: vs, Client: &fakeClient{},
		Previewer: &fakePreviewer{}, Enqueuer: &fakeEnqueuer{}, DuplicateMaxDist: 0,
	})
	if got, err := disabled.Duplicates(context.Background(), "ph1"); err != nil || got != nil {
		t.Errorf("disabled Duplicates = %v, %v; want nil, nil", got, err)
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
