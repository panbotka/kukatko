package thumbjob

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakeGate answers AwaitingMove with a canned verdict and records the question.
type fakeGate struct {
	awaiting bool
	err      error
	asked    []string
}

// AwaitingMove records the project and key and returns the canned answer.
func (g *fakeGate) AwaitingMove(_ context.Context, projectUID, filePath string) (bool, error) {
	g.asked = append(g.asked, projectUID+"|"+filePath)
	return g.awaiting, g.err
}

// TestHandle_awaitsTheMove verifies the job handler asks the gate about the
// photo's own project and key, defers a photo a running move has yet to take
// without rendering anything, renders one the move is done with, and reports a
// failing gate as an error rather than rendering past it.
func TestHandle_awaitsTheMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		gate         *fakeGate
		wantDeferral bool
		wantErr      bool
		wantRendered int
	}{
		{name: "awaiting the move", gate: &fakeGate{awaiting: true}, wantDeferral: true, wantErr: true},
		{name: "already moved", gate: &fakeGate{}, wantRendered: 1},
		{name: "gate fails", gate: &fakeGate{err: errors.New("db down")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakePhotos{
				photo:    photos.Photo{ProjectID: "pj1", FilePath: "2024/05/a.jpg", Blurhash: "x"},
				hasPhash: true,
			}
			th := &fakeThumbs{}
			svc := New(Config{Photos: p, Thumbnailer: th, Decoder: &fakeDecoder{}, Moves: tt.gate})

			err := svc.Handle(t.Context(), jobs.Job{Payload: payload(t, "ph1")})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Handle = %v, want error %v", err, tt.wantErr)
			}
			if got := worker.IsDeferral(err) && errors.Is(err, ErrAwaitingMove); got != tt.wantDeferral {
				t.Errorf("Handle = %v, deferral awaiting the move = %v, want %v", err, got, tt.wantDeferral)
			}
			if th.calls != tt.wantRendered {
				t.Errorf("GenerateAll calls = %d, want %d", th.calls, tt.wantRendered)
			}
			if want := []string{"pj1|2024/05/a.jpg"}; !slices.Equal(tt.gate.asked, want) {
				t.Errorf("gate asked %v, want %v", tt.gate.asked, want)
			}
		})
	}
}

// TestHandle_defersAPublishTheMoveOvertook verifies a generation the
// thumbnailer refused to publish because the photo's prefix changed mid-job (a
// storage move committed it) defers the job — it re-runs from the moved row —
// for the repair and the forced rebuild alike, while any other failure stays an
// ordinary one.
func TestHandle_defersAPublishTheMoveOvertook(t *testing.T) {
	t.Parallel()

	moved := fmt.Errorf("thumb: generate sizes: %w", thumb.ErrPrefixMoved)
	tests := []struct {
		name         string
		force        bool
		err          error
		wantDeferral bool
	}{
		{name: "repair overtaken", err: moved, wantDeferral: true},
		{name: "rebuild overtaken", force: true, err: moved, wantDeferral: true},
		{name: "other failure", err: errors.New("disk full")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			th := &fakeThumbs{err: tt.err, regenErr: tt.err}
			svc := newService(&fakePhotos{hasPhash: true}, th, &fakeDecoder{})
			raw := payload(t, "ph1")
			if tt.force {
				raw = []byte(`{"photo_uid":"ph1","force":true}`)
			}

			err := svc.Handle(t.Context(), jobs.Job{Payload: raw})
			if err == nil {
				t.Fatal("Handle succeeded over a failing thumbnailer")
			}
			if got := worker.IsDeferral(err); got != tt.wantDeferral {
				t.Errorf("Handle = %v, deferral = %v, want %v", err, got, tt.wantDeferral)
			}
			if !errors.Is(err, tt.err) {
				t.Errorf("Handle = %v, want it to wrap %v", err, tt.err)
			}
		})
	}
}
