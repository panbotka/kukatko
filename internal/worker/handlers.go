package worker

import (
	"context"

	"github.com/panbotka/kukatko/internal/jobs"
)

// TypeNoop is a trivial built-in job type whose handler does nothing and always
// succeeds. It exists only for worker sanity checks and integration tests; real
// job types (image_embed, face_detect, …) register their handlers in later
// milestones.
const TypeNoop = "noop"

// NoopHandler is the handler for TypeNoop jobs. It performs no work and returns
// nil, so claiming a noop job exercises the full claim → dispatch → Complete
// path end to end.
func NoopHandler(_ context.Context, _ jobs.Job) error {
	return nil
}

// RegisterBuiltins registers the worker's built-in handlers (currently only the
// noop handler) on r. The serve command calls it so a fresh worker can always
// run TypeNoop without any later milestone wired up.
func RegisterBuiltins(r *Registry) {
	r.Register(TypeNoop, NoopHandler)
}
