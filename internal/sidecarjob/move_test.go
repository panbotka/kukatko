package sidecarjob

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// movedKey is where the storage move puts the test photo's original.
const movedKey = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/2024/05/a.jpg"

// movingPhotos is a PhotoStore whose row a storage move rewrites mid-job: every
// GetForJob call after the first moveAfter answers with movedKey. With flip set
// it alternates between the two keys on every call instead.
type movingPhotos struct {
	fakePhotos
	moveAfter int
	flip      bool
}

// GetForJob returns the row as the move has left it by this call.
func (m *movingPhotos) GetForJob(ctx context.Context, uid string) (photos.Photo, error) {
	row, err := m.fakePhotos.GetForJob(ctx, uid)
	if err != nil {
		return row, err
	}
	if (m.flip && m.getCalls%2 == 0) || (!m.flip && m.getCalls > m.moveAfter) {
		row.FilePath = movedKey
	}
	return row, nil
}

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

// TestExport_followsTheMove verifies the sidecar is written next to the key the
// photo's row holds when it is written: a move that commits the photo between
// the read the sidecar is built from and the write sends the sidecar to the new
// key, never the one the photo left, and the document describes the new key.
func TestExport_followsTheMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moveAfter int
		wantKey   string
	}{
		{name: "moved before the job", moveAfter: 0, wantKey: movedKey},
		{name: "moved mid-export", moveAfter: 1, wantKey: movedKey},
		{name: "not moved", moveAfter: 100, wantKey: "2024/05/a.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := &movingPhotos{
				fakePhotos: fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}},
				moveAfter:  tt.moveAfter,
			}
			writer := newFakeWriter()
			svc := newTestService(t, Config{Photos: cat, Writer: writer})

			if err := svc.Export(t.Context(), "pht1"); err != nil {
				t.Fatalf("Export: %v", err)
			}
			keys := make([]string, 0, len(writer.written))
			for key := range writer.written {
				keys = append(keys, key)
			}
			if want := []string{tt.wantKey}; !slices.Equal(keys, want) {
				t.Fatalf("sidecars written next to %v, want %v", keys, want)
			}
			if !slices.Equal(cat.marked, []string{"pht1"}) {
				t.Errorf("marked %v, want the photo once", cat.marked)
			}
		})
	}
}

// TestExport_defersAPhotoThatKeepsMoving verifies a photo whose key changes on
// every re-read is not chased for ever: the job is deferred with ErrPathMoved
// and nothing is written.
func TestExport_defersAPhotoThatKeepsMoving(t *testing.T) {
	t.Parallel()

	cat := &movingPhotos{
		fakePhotos: fakePhotos{photo: photos.Photo{UID: "pht1", FilePath: "2024/05/a.jpg"}},
		flip:       true,
	}
	writer := newFakeWriter()
	svc := newTestService(t, Config{Photos: cat, Writer: writer})

	err := svc.Export(t.Context(), "pht1")
	if !errors.Is(err, ErrPathMoved) || !worker.IsDeferral(err) {
		t.Fatalf("Export = %v, want a deferral wrapping ErrPathMoved", err)
	}
	if len(writer.written) != 0 {
		t.Errorf("sidecars written: %d, want none", len(writer.written))
	}
}

// TestHandle_awaitsTheMove verifies the job handler asks the gate about the
// photo's own project and key, defers a photo a running move has yet to take
// without writing anything, writes for a photo the move is done with, and
// reports a failing gate as an error.
func TestHandle_awaitsTheMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		gate         *fakeGate
		wantDeferral bool
		wantErr      bool
		wantWritten  int
	}{
		{name: "awaiting the move", gate: &fakeGate{awaiting: true}, wantDeferral: true, wantErr: true},
		{name: "already moved", gate: &fakeGate{}, wantWritten: 1},
		{name: "gate fails", gate: &fakeGate{err: errors.New("db down")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := &fakePhotos{photo: photos.Photo{UID: "pht1", ProjectID: "pj1", FilePath: "2024/05/a.jpg"}}
			writer := newFakeWriter()
			svc := newTestService(t, Config{Photos: cat, Writer: writer, Moves: tt.gate})

			err := svc.Handle(t.Context(), jobs.Job{Payload: []byte(`{"photo_uid":"pht1"}`)})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Handle = %v, want error %v", err, tt.wantErr)
			}
			if got := worker.IsDeferral(err) && errors.Is(err, ErrAwaitingMove); got != tt.wantDeferral {
				t.Errorf("Handle = %v, deferral awaiting the move = %v, want %v", err, got, tt.wantDeferral)
			}
			if len(writer.written) != tt.wantWritten {
				t.Errorf("sidecars written = %d, want %d", len(writer.written), tt.wantWritten)
			}
			if want := []string{"pj1|2024/05/a.jpg"}; !slices.Equal(tt.gate.asked, want) {
				t.Errorf("gate asked %v, want %v", tt.gate.asked, want)
			}
		})
	}
}
