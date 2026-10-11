package phototask

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Assignment is one person newly put on a task by somebody else's decision:
// the task, the person asked and the account that asked them.
type Assignment struct {
	// TaskUID is the task the person was put on.
	TaskUID string
	// UserUID is the account that was asked.
	UserUID string
	// ActorUID is the account that asked, or empty for an internal path with
	// nobody acting (such a row carries a NULL added_by and counts as joined by
	// acting, not as asked).
	ActorUID string
}

// AssignObserver hears that somebody was freshly put on a task — asked while
// they were not on it — inside the transaction that put them there, so
// whatever it records commits with the assignment or not at all. Re-asking a
// person already on the task (asked or joined by acting) is not reported: they
// already know, and nobody should hear about the same task twice. Joining by
// acting is never reported either — nobody asked. It is how the "task assigned
// to me" notification (internal/taskassignnotify) learns about a hand-over
// without the HTTP handler having to remember it.
//
// An error it returns fails the assignment.
type AssignObserver interface {
	// Assigned hears that a.UserUID is now on a.TaskUID at a.ActorUID's asking.
	Assigned(ctx context.Context, tx pgx.Tx, a Assignment) error
}

// WithAssignObserver returns a copy of s that reports every fresh assignment
// to observer, within the transaction of the change. The copy shares the pool;
// s itself is unchanged.
func (s *Store) WithAssignObserver(observer AssignObserver) *Store {
	out := *s
	out.assigns = observer
	return &out
}

// assigned reports a to the observer, if there is one, when inserted says the
// assignment created the participant row rather than upgrading an existing
// one.
func (s *Store) assigned(ctx context.Context, tx pgx.Tx, inserted bool, a Assignment) error {
	if !inserted || s.assigns == nil {
		return nil
	}
	if err := s.assigns.Assigned(ctx, tx, a); err != nil {
		return fmt.Errorf("assign observer: %w", err)
	}
	return nil
}
