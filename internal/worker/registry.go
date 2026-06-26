// Package worker is Kukátko's in-process background worker runtime: it drains
// the persistent jobs queue (internal/jobs) with bounded concurrency, dispatches
// each claimed job to a handler registered for its type, and records the outcome
// (Complete or Fail) back on the queue. It owns the execution loop and graceful
// lifecycle; the queue owns durability, retry/backoff, dead-lettering and
// stale-lock recovery.
//
// Handlers for the real job types (image_embed, face_detect, …) are contributed
// by later milestones; this package ships only the runtime, the handler registry
// and a trivial built-in noop handler used for sanity checks and tests.
package worker

import (
	"context"
	"sync"

	"github.com/panbotka/kukatko/internal/jobs"
)

// HandlerFunc processes a single claimed job. It receives the worker's context
// (cancelled when the worker is shutting down) and the job whose Payload holds
// the type-specific arguments. Returning nil marks the job done; returning an
// error fails the job, letting the queue retry it with backoff or dead-letter it
// once attempts are exhausted. Handlers should honour ctx cancellation so a
// shutdown can interrupt long-running work.
type HandlerFunc func(ctx context.Context, job jobs.Job) error

// Registry maps a job type to the handler that processes it. It is safe for
// concurrent use: registration happens at startup, lookups happen on every
// worker goroutine.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]HandlerFunc)}
}

// Register associates fn with jobType. It panics if jobType is empty, fn is nil,
// or a handler is already registered for jobType: all three are programming
// errors that should surface at startup rather than silently mis-route jobs.
func (r *Registry) Register(jobType string, fn HandlerFunc) {
	if jobType == "" {
		panic("worker: Register called with empty job type")
	}
	if fn == nil {
		panic("worker: Register called with nil handler for " + jobType)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[jobType]; exists {
		panic("worker: handler already registered for " + jobType)
	}
	r.handlers[jobType] = fn
}

// Handler returns the handler registered for jobType and whether one exists.
func (r *Registry) Handler(jobType string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.handlers[jobType]
	return fn, ok
}

// Types returns the job types that have a registered handler, in unspecified
// order. It is used to restrict the worker's Claim calls to handleable types.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.handlers))
	for jobType := range r.handlers {
		types = append(types, jobType)
	}
	return types
}
