// Package reachability caches whether an external dependency — the embeddings
// sidecar that powers semantic search — is currently reachable, so any request
// handler can read the answer without a live network probe. The sidecar box is
// frequently offline by design, so probing it on every request would be both
// slow and wasteful; instead a small background loop (mirroring internal/wake)
// refreshes an atomic flag on an interval and handlers read it in nanoseconds.
//
// The cached flag is purely presentational: it decides whether the UI advertises
// semantic search, not whether search works (the search endpoint degrades to
// full text on its own when the box is offline). A safe default of "unreachable"
// therefore only ever hides an affordance, never breaks one.
package reachability

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// HealthChecker reports whether a dependency is currently reachable via a live
// probe. embedding.Client satisfies it with its Healthy method.
type HealthChecker interface {
	// Healthy reports whether the dependency answered a fresh probe.
	Healthy(ctx context.Context) bool
}

// Checker caches the reachability of a dependency, refreshed by a background loop
// (Run), so callers read the last result via Reachable without blocking. The
// zero flag is a safe "unreachable", so a handler that reads before the first
// probe — or reads a disabled Checker — never falsely advertises the dependency.
type Checker struct {
	health    HealthChecker
	enabled   bool
	logger    *slog.Logger
	reachable atomic.Bool
}

// Config configures a Checker. When Enabled is false (for example the dependency
// URL is unconfigured) the Checker is inert: Run returns immediately and
// Reachable always reports false, so Health may be nil.
type Config struct {
	// Health is the probe backend; required when Enabled, ignored otherwise.
	Health HealthChecker
	// Enabled turns the checker on. A disabled checker never probes and always
	// reports unreachable.
	Enabled bool
	// Logger receives reachability transitions; nil uses slog.Default.
	Logger *slog.Logger
}

// New builds a Checker from cfg. A disabled Checker is inert and always reports
// false; an enabled one reports false until Run performs its first probe.
func New(cfg Config) *Checker {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{
		health:  cfg.Health,
		enabled: cfg.Enabled,
		logger:  logger,
	}
}

// Reachable reports the result of the most recent probe. It never blocks and is
// safe for concurrent use. It is false before the first probe and for a disabled
// Checker.
func (c *Checker) Reachable() bool {
	return c.reachable.Load()
}

// Run probes the dependency once immediately and then on every tick of interval
// until ctx is cancelled, caching each result. A disabled Checker logs once and
// returns immediately, so it is always safe to start in a goroutine.
func (c *Checker) Run(ctx context.Context, interval time.Duration) {
	if !c.enabled {
		c.logger.Info("reachability: checker disabled; semantic search reported unavailable")
		return
	}
	c.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Tick(ctx)
		}
	}
}

// Tick performs one probe and caches the result, logging any change from the
// previous state. It is a no-op for a disabled Checker, so it never touches a
// nil Health backend.
func (c *Checker) Tick(ctx context.Context) {
	if !c.enabled {
		return
	}
	now := c.health.Healthy(ctx)
	if c.reachable.Swap(now) != now {
		c.logger.Info("reachability: embeddings sidecar state changed", "reachable", now)
	}
}
