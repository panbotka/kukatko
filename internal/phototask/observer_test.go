package phototask

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// recordingObserver remembers every assignment it hears and fails on demand.
type recordingObserver struct {
	heard []Assignment
	err   error
}

// Assigned records a or returns the configured error.
func (r *recordingObserver) Assigned(_ context.Context, _ pgx.Tx, a Assignment) error {
	if r.err != nil {
		return r.err
	}
	r.heard = append(r.heard, a)
	return nil
}

// TestWithAssignObserver_copies checks the hook lands on a copy: the original
// store keeps hearing nothing, the copy reports to the observer.
func TestWithAssignObserver_copies(t *testing.T) {
	t.Parallel()

	base := NewStore(nil)
	obs := &recordingObserver{}
	hooked := base.WithAssignObserver(obs)
	if base.assigns != nil {
		t.Fatal("WithAssignObserver changed the original store")
	}
	if hooked.assigns != obs {
		t.Fatal("the copy does not hold the observer")
	}
}

// TestAssigned_rules checks when the store speaks to its observer: only for a
// row the assignment inserted, only with an observer, and an observer's error
// fails the call.
func TestAssigned_rules(t *testing.T) {
	t.Parallel()

	want := Assignment{TaskUID: "pt1", UserUID: "us-asked", ActorUID: "us-actor"}
	boom := errors.New("boom")
	tests := []struct {
		name     string
		observer *recordingObserver
		inserted bool
		wantErr  error
		heard    int
	}{
		{name: "fresh row is reported", observer: &recordingObserver{}, inserted: true, heard: 1},
		{name: "upgraded row is silent", observer: &recordingObserver{}, inserted: false},
		{name: "no observer hears nothing", observer: nil, inserted: true},
		{name: "observer failure fails the call", observer: &recordingObserver{err: boom}, inserted: true, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewStore(nil)
			if tt.observer != nil {
				store = store.WithAssignObserver(tt.observer)
			}
			err := store.assigned(context.Background(), nil, tt.inserted, want)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("assigned() error = %v, want %v", err, tt.wantErr)
			}
			if tt.observer == nil {
				return
			}
			if got := len(tt.observer.heard); got != tt.heard {
				t.Fatalf("observer heard %d assignments, want %d", got, tt.heard)
			}
			if tt.heard == 1 && tt.observer.heard[0] != want {
				t.Errorf("observer heard %+v, want %+v", tt.observer.heard[0], want)
			}
		})
	}
}
