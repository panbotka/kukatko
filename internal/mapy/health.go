package mapy

import (
	"context"
	"errors"
	"sync"
	"time"
)

// HealthState classifies the last observed outcome of a mapy.com call, so the
// admin status dashboard can tell "the provider is fine" from "our API key is
// being rejected" without probing the upstream itself.
type HealthState string

// The possible health states of the mapy.com upstream.
const (
	// HealthUnknown means no call has been observed yet since start-up.
	HealthUnknown HealthState = "unknown"
	// HealthOK means the last call succeeded.
	HealthOK HealthState = "ok"
	// HealthKeyRejected means mapy.com answered 401/403: the key is expired,
	// revoked or over quota. This is a server-side configuration problem and
	// needs a human in the mapy.com console.
	HealthKeyRejected HealthState = "key_rejected"
	// HealthRateLimited means mapy.com answered 429: the rate or credit cap was hit.
	HealthRateLimited HealthState = "rate_limited"
	// HealthUnavailable means mapy.com could not be reached at all.
	HealthUnavailable HealthState = "unavailable"
	// HealthError means mapy.com failed in some other way (an unexpected status
	// or an unreadable body).
	HealthError HealthState = "error"
)

// Degraded reports whether the state means map data is currently broken. A
// missing tile (ErrNotFound) is not a degradation — it is a normal answer for a
// coordinate outside the covered area — so it never reaches the tracker as one.
func (s HealthState) Degraded() bool {
	switch s {
	case HealthKeyRejected, HealthRateLimited, HealthUnavailable, HealthError:
		return true
	case HealthUnknown, HealthOK:
		return false
	default:
		return false
	}
}

// HealthStatus is a snapshot of the mapy.com upstream's last observed outcome.
// Detail is derived from the client's sentinel errors, which never carry the API
// key or the User-Agent, so it is safe to surface.
type HealthStatus struct {
	// State is the last observed outcome.
	State HealthState `json:"state"`
	// Detail is a short, sanitised description of the last failure; empty while
	// healthy or unknown.
	Detail string `json:"detail,omitempty"`
	// CheckedAt is when the last outcome was observed; zero when none has been.
	CheckedAt time.Time `json:"checked_at,omitzero"`
}

// Health tracks the last observed outcome of the mapy.com calls made through the
// proxy, so the admin status dashboard can report the map backend as degraded
// (notably when the API key is being rejected) without spending a credit on a
// health probe of its own. It is safe for concurrent use, and every method is
// nil-safe so callers can hold an unconfigured (nil) tracker unconditionally.
type Health struct {
	mu     sync.Mutex
	status HealthStatus
	now    func() time.Time
}

// NewHealth returns a Health tracker in the unknown state.
func NewHealth() *Health {
	return &Health{status: HealthStatus{State: HealthUnknown}, now: time.Now}
}

// Record folds the outcome of one mapy.com call into the tracker: a nil error
// marks the upstream healthy, and a failure is classified by its sentinel. A
// missing tile or an unmatched coordinate (ErrNotFound), a rejected client
// request (ErrInvalidMapset) and a request the browser cancelled say nothing
// about the upstream's health and are ignored. A nil tracker (no key configured)
// ignores everything.
func (h *Health) Record(err error) {
	if h == nil {
		return
	}
	state, detail := classify(err)
	if state == HealthUnknown {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = HealthStatus{State: state, Detail: detail, CheckedAt: h.now()}
}

// Snapshot returns the last observed outcome. A nil tracker reports the unknown
// state.
func (h *Health) Snapshot() HealthStatus {
	if h == nil {
		return HealthStatus{State: HealthUnknown}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

// classify maps a client error onto a health state and a sanitised detail. It
// returns HealthUnknown for outcomes that carry no information about the
// upstream's health, which the caller drops.
func classify(err error) (HealthState, string) {
	switch {
	case err == nil:
		return HealthOK, ""
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrInvalidMapset):
		return HealthUnknown, ""
	case errors.Is(err, context.Canceled):
		// The browser went away mid-request; that says nothing about mapy.com.
		return HealthUnknown, ""
	case errors.Is(err, ErrUnauthorized):
		return HealthKeyRejected, err.Error()
	case errors.Is(err, ErrRateLimited):
		return HealthRateLimited, err.Error()
	case errors.Is(err, ErrUnavailable):
		return HealthUnavailable, err.Error()
	default:
		return HealthError, err.Error()
	}
}
