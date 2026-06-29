package wake

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	// defaultMinQueue is the pending-job threshold used when MinQueue is not set:
	// any single waiting embedding job is enough to attempt a wake.
	defaultMinQueue = 1
	// defaultCooldown is the minimum delay between magic packets used when
	// Cooldown is not set, so a sleeping box is not spammed.
	defaultCooldown = 5 * time.Minute
	// defaultGracePeriod is how long to wait after sending a packet before
	// re-checking sidecar health to report whether the box came up.
	defaultGracePeriod = 30 * time.Second
)

// QueueDepth reports how many embedding jobs (image_embed/face_detect) are
// pending and thus waiting on the box.
type QueueDepth interface {
	// PendingEmbeddingJobs returns the count of queued or running embedding jobs.
	PendingEmbeddingJobs(ctx context.Context) (int, error)
}

// HealthChecker reports whether the embeddings sidecar is currently reachable.
// embedding.Client satisfies it.
type HealthChecker interface {
	// Healthy reports whether the sidecar answered a recent probe.
	Healthy(ctx context.Context) bool
}

// Config wires the auto-wake Service. When Enabled is false the Service is inert
// and the remaining fields are ignored. Sender is optional: when nil and Enabled
// a default network sender is built from BroadcastAddr/Interface.
type Config struct {
	Enabled       bool
	MAC           string
	BroadcastAddr string
	Interface     string
	MinQueue      int
	Cooldown      time.Duration
	// GracePeriod overrides the post-send health re-check delay; <= 0 uses the
	// built-in default. Exposed mainly so tests need not wait seconds.
	GracePeriod time.Duration
	// Queue and Health are required when Enabled.
	Queue  QueueDepth
	Health HealthChecker
	// Sender, when set, replaces the default network sender (used by tests).
	Sender Sender
	// Logger receives wake attempts and outcomes; nil uses the standard logger.
	Logger *log.Logger
	// Clock supplies the current time for cooldown bookkeeping; nil uses
	// time.Now. Exposed for deterministic tests.
	Clock func() time.Time
}

// Service decides when to send a Wake-on-LAN magic packet to the embeddings box
// and runs the periodic check loop. It is safe for the single background
// goroutine that drives it; cooldown state is mutex-guarded.
type Service struct {
	enabled  bool
	mac      net.HardwareAddr
	minQueue int
	cooldown time.Duration
	grace    time.Duration
	queue    QueueDepth
	health   HealthChecker
	sender   Sender
	logger   *log.Logger
	now      func() time.Time

	mu         sync.Mutex
	lastWakeAt time.Time
}

// New builds a Service from cfg. A disabled config yields an inert Service whose
// Run returns immediately and which never sends a packet. When enabled it
// requires a parseable MAC plus a Queue and Health backend, and builds the
// default network sender when none is supplied. It returns an error only for an
// enabled-but-misconfigured Service.
func New(cfg Config) (*Service, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	svc := &Service{
		enabled: cfg.Enabled,
		grace:   orDuration(cfg.GracePeriod, defaultGracePeriod),
		logger:  logger,
		now:     clock,
	}
	if !cfg.Enabled {
		return svc, nil
	}
	if err := configureEnabled(svc, cfg); err != nil {
		return nil, err
	}
	return svc, nil
}

// configureEnabled fills in the fields needed for an enabled Service: the parsed
// MAC, thresholds, the queue and health backends, and the sender (built from the
// network settings when cfg.Sender is nil).
func configureEnabled(svc *Service, cfg Config) error {
	mac, err := net.ParseMAC(cfg.MAC)
	if err != nil {
		return fmt.Errorf("wake: parsing mac %q: %w", cfg.MAC, err)
	}
	if cfg.Queue == nil || cfg.Health == nil {
		return errors.New("wake: enabled service requires a Queue and Health backend")
	}
	sender := cfg.Sender
	if sender == nil {
		if sender, err = newSender(cfg.BroadcastAddr, cfg.Interface); err != nil {
			return err
		}
	}
	svc.mac = mac
	svc.minQueue = orInt(cfg.MinQueue, defaultMinQueue)
	svc.cooldown = orDuration(cfg.Cooldown, defaultCooldown)
	svc.queue = cfg.Queue
	svc.health = cfg.Health
	svc.sender = sender
	return nil
}

// Run drives the auto-wake check on interval until ctx is cancelled. A disabled
// Service logs and returns immediately. The loop never blocks job processing:
// it runs in its own goroutine and only ever sends packets and probes health.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if !s.enabled {
		s.logger.Printf("wake: auto-wake disabled")
		return
	}
	s.logger.Printf("wake: auto-wake enabled (mac=%s, min_queue=%d, cooldown=%s)", s.mac, s.minQueue, s.cooldown)
	s.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick performs one check: if auto-wake is enabled, enough embedding jobs are
// pending, the cooldown has elapsed, and the sidecar is offline, it sends a
// magic packet and re-checks health after the grace period. It is a no-op for a
// disabled Service. Errors are logged, never returned, so a failing tick cannot
// disturb anything else.
func (s *Service) Tick(ctx context.Context) {
	if !s.enabled {
		return
	}
	pending, err := s.queue.PendingEmbeddingJobs(ctx)
	if err != nil {
		s.logger.Printf("wake: counting pending embedding jobs: %v", err)
		return
	}
	if !s.shouldAttempt(pending) {
		return
	}
	if s.health.Healthy(ctx) {
		return // box already awake; nothing to do.
	}
	s.wake(ctx, pending)
}

// shouldAttempt reports whether a wake should be attempted given the pending job
// count: the count must reach the configured minimum and the cooldown since the
// last packet must have elapsed.
func (s *Service) shouldAttempt(pending int) bool {
	if pending < s.minQueue {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastWakeAt.IsZero() || s.now().Sub(s.lastWakeAt) >= s.cooldown
}

// wake records the attempt time (starting the cooldown), sends the magic packet,
// and — on a successful send — re-checks health after the grace period to log
// whether the box came up.
func (s *Service) wake(ctx context.Context, pending int) {
	s.mu.Lock()
	s.lastWakeAt = s.now()
	s.mu.Unlock()

	s.logger.Printf("wake: %d embedding job(s) pending and sidecar offline; sending magic packet to %s",
		pending, s.mac)
	if err := s.sender.Send(ctx, s.mac); err != nil {
		s.logger.Printf("wake: sending magic packet failed: %v", err)
		return
	}
	s.verify(ctx)
}

// verify waits for the grace period (or ctx cancellation) and re-checks sidecar
// health, logging whether the box became reachable or is still offline (and will
// be retried after the cooldown).
func (s *Service) verify(ctx context.Context) {
	timer := time.NewTimer(s.grace)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if s.health.Healthy(ctx) {
		s.logger.Printf("wake: sidecar became reachable after wake")
		return
	}
	s.logger.Printf("wake: sidecar still offline %s after wake; backing off for %s", s.grace, s.cooldown)
}

// orInt returns v when positive, otherwise fallback.
func orInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

// orDuration returns v when positive, otherwise fallback.
func orDuration(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}
