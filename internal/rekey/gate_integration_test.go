//go:build integration

package rekey

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
)

// awaiting asks the gate about one key of the bootstrap project.
func (h *harness) awaiting(gate *Gate, filePath string) bool {
	h.t.Helper()
	got, err := gate.AwaitingMove(h.t.Context(), project.BootstrapUID, filePath)
	if err != nil {
		h.t.Fatalf("AwaitingMove(%s): %v", filePath, err)
	}
	return got
}

// activeStates returns the states of the instance's active runs.
func (h *harness) activeStates() []State {
	h.t.Helper()
	runs, err := h.svc.ActiveRuns(h.t.Context())
	if err != nil {
		h.t.Fatalf("ActiveRuns: %v", err)
	}
	states := make([]State, 0, len(runs))
	for _, run := range runs {
		states = append(states, run.State)
	}
	return states
}

// TestGate_awaitsOnlyARunningMoveOfThePhoto verifies the gate the publishing
// jobs ask: a photo still in the old layout of a project whose move is running
// is awaited; once the move has taken it (its key is under the target prefix),
// while the move is paused, for another project, and after the move is done,
// nothing is. ActiveRuns follows the same run through running and paused, and
// forgets it once it is done.
func TestGate_awaitsOnlyARunningMoveOfThePhoto(t *testing.T) {
	h := newHarness(t)
	gate := NewGate(h.db.Pool())
	photo := h.seedPhoto("ph000000000000000000000001", "2023/01/a.jpg", true)

	if h.awaiting(gate, photo.filePath) {
		t.Fatal("awaited a photo with no move at all")
	}
	if got := h.activeStates(); len(got) != 0 {
		t.Fatalf("active runs before any start = %v", got)
	}
	run := h.start()
	if !h.awaiting(gate, photo.filePath) {
		t.Error("a photo in the old layout of a running move is not awaited")
	}
	if h.awaiting(gate, run.TargetPrefix+photo.filePath) {
		t.Error("a photo already under the target prefix is awaited")
	}
	other, err := gate.AwaitingMove(t.Context(), "pjbbbbbbbbbbbbbbbbbbbbbbbb", photo.filePath)
	if err != nil || other {
		t.Errorf("AwaitingMove for another project = %v, %v; want false", other, err)
	}
	if got := h.activeStates(); len(got) != 1 || got[0] != StateRunning {
		t.Errorf("active runs while running = %v, want [running]", got)
	}

	if _, err := h.svc.Pause(t.Context(), audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if h.awaiting(gate, photo.filePath) {
		t.Error("a paused move is awaited; it takes nothing")
	}
	if got := h.activeStates(); len(got) != 1 || got[0] != StatePaused {
		t.Errorf("active runs while paused = %v, want [paused]", got)
	}

	if _, err := h.svc.Resume(t.Context(), audit.Meta{}, project.BootstrapUID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := h.stepToEnd(run, 10); err != nil {
		t.Fatalf("stepping the move: %v", err)
	}
	if _, err := h.svc.Finish(t.Context(), run); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if h.awaiting(gate, photo.filePath) || h.awaiting(gate, run.TargetPrefix+photo.filePath) {
		t.Error("a finished move is still awaited")
	}
	if got := h.activeStates(); len(got) != 0 {
		t.Errorf("active runs after the move = %v, want none", got)
	}
}
