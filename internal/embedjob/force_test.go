package embedjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/embedding"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// forcedFixture wires a service over a photo that already has an embedding, the
// state that tells the repair and the rebuild apart.
func forcedFixture(t *testing.T) (*Service, *fakeVectorStore, *fakeClient) {
	t.Helper()
	ps := &fakePhotoStore{photos: map[string]photos.Photo{"ph1": {UID: "ph1", FileHash: "abc"}}}
	vs := &fakeVectorStore{embeddings: map[string]vectors.Embedding{
		"ph1": {PhotoUID: "ph1", Vector: imageVec(), Model: "old", Pretrained: "old"},
	}}
	client := &fakeClient{vec: imageVec(), model: "ViT-B-32", pretrained: "laion2b"}
	return newService(t, ps, vs, client, &fakePreviewer{}, &fakeEnqueuer{}), vs, client
}

// TestForceEmbed_recomputesWhereEmbedSkips is the whole point of the forced path:
// on a photo that already has an embedding the repair calls no sidecar and writes
// nothing, while the rebuild recomputes the vector and replaces the stored one.
func TestForceEmbed_recomputesWhereEmbedSkips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		run       func(*Service) error
		wantCalls int
		wantModel string
	}{
		{
			name:      "repair skips a photo that already has an embedding",
			run:       func(s *Service) error { return s.Embed(context.Background(), "ph1") },
			wantCalls: 0,
			wantModel: "old",
		},
		{
			name:      "rebuild recomputes and replaces it",
			run:       func(s *Service) error { return s.ForceEmbed(context.Background(), "ph1") },
			wantCalls: 1,
			wantModel: "ViT-B-32",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, vs, client := forcedFixture(t)
			if err := tt.run(svc); err != nil {
				t.Fatalf("run: %v", err)
			}
			if client.calls != tt.wantCalls {
				t.Errorf("sidecar calls = %d, want %d", client.calls, tt.wantCalls)
			}
			if got := vs.embeddings["ph1"].Model; got != tt.wantModel {
				t.Errorf("stored model = %q, want %q", got, tt.wantModel)
			}
			if len(vs.saved) != tt.wantCalls {
				t.Errorf("saved %d embeddings, want %d", len(vs.saved), tt.wantCalls)
			}
		})
	}
}

// TestHandle_forcePayloadRebuilds proves the queued job honours the payload's
// force flag: the same job type on the same photo either skips or recomputes,
// which is what lets the forced enqueue keep dedup keyed on type + photo uid.
func TestHandle_forcePayloadRebuilds(t *testing.T) {
	t.Parallel()

	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain payload skips", true: "forced payload recomputes"}[force],
			func(t *testing.T) {
				t.Parallel()
				svc, _, client := forcedFixture(t)
				payload, err := json.Marshal(map[string]any{"photo_uid": "ph1", "force": force})
				if err != nil {
					t.Fatalf("marshal payload: %v", err)
				}
				if err := svc.Handle(context.Background(), jobs.Job{
					Type: jobs.TypeImageEmbed, Payload: payload,
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

// TestForceEmbed_offlineDefers confirms the rebuild answers an offline box the
// way the repair does: a worker deferral, so the forced job waits in the queue
// instead of burning its retries — and so the on-demand caller can recognise the
// outage and queue the work rather than failing the request.
func TestForceEmbed_offlineDefers(t *testing.T) {
	t.Parallel()

	svc, vs, client := forcedFixture(t)
	client.err = embedding.ErrUnavailable

	err := svc.ForceEmbed(context.Background(), "ph1")
	if !worker.IsDeferral(err) {
		t.Fatalf("ForceEmbed with the box offline = %v, want a worker deferral", err)
	}
	if got := vs.embeddings["ph1"].Model; got != "old" {
		t.Errorf("stored model = %q, want the previous %q left untouched", got, "old")
	}
}

// TestForceEmbed_missingPhoto keeps the rebuild's error surface the same as the
// repair's, so the HTTP layer can answer 404 from the same sentinel.
func TestForceEmbed_missingPhoto(t *testing.T) {
	t.Parallel()

	svc, _, _ := forcedFixture(t)
	if err := svc.ForceEmbed(context.Background(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("ForceEmbed on an unknown photo = %v, want photos.ErrPhotoNotFound", err)
	}
}
