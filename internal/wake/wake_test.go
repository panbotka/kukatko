package wake

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeSender records magic-packet sends instead of touching the network.
type fakeSender struct {
	mu    sync.Mutex
	calls int
	last  net.HardwareAddr
	err   error
}

// Send records the call (and the targeted MAC) and returns the configured error.
func (f *fakeSender) Send(_ context.Context, mac net.HardwareAddr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = mac
	return f.err
}

// count returns how many times Send was called.
func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeQueue returns a fixed pending count (and optional error).
type fakeQueue struct {
	pending int
	err     error
}

// PendingEmbeddingJobs returns the configured pending count or error.
func (q fakeQueue) PendingEmbeddingJobs(context.Context) (int, error) {
	return q.pending, q.err
}

// fakeHealth reports sidecar health from a function so tests can vary the answer
// across successive probes.
type fakeHealth struct {
	fn func() bool
}

// Healthy invokes the configured function.
func (h fakeHealth) Healthy(context.Context) bool { return h.fn() }

// fixedHealth builds a fakeHealth that always returns healthy.
func fixedHealth(healthy bool) fakeHealth {
	return fakeHealth{fn: func() bool { return healthy }}
}

// quietLogger discards log output during tests.
func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// newTestService builds an enabled Service wired to the supplied fakes with a
// negligible grace period and a controllable clock seeded at clock().
func newTestService(t *testing.T, sender Sender, q QueueDepth, h HealthChecker, clock func() time.Time) *Service {
	t.Helper()
	svc, err := New(Config{
		Enabled:     true,
		MAC:         "aa:bb:cc:dd:ee:ff",
		MinQueue:    2,
		Cooldown:    5 * time.Minute,
		GracePeriod: time.Millisecond,
		Queue:       q,
		Health:      h,
		Sender:      sender,
		Logger:      quietLogger(),
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

// TestTick_sendsWhenConditionsMet verifies a packet is sent when enabled, the
// pending count meets the minimum, the cooldown has elapsed (never sent), and
// the sidecar is offline.
func TestTick_sendsWhenConditionsMet(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	svc := newTestService(t, sender, fakeQueue{pending: 3}, fixedHealth(false), time.Now)
	svc.Tick(context.Background())

	if sender.count() != 1 {
		t.Fatalf("send count = %d, want 1", sender.count())
	}
	if sender.last.String() != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("target MAC = %s, want aa:bb:cc:dd:ee:ff", sender.last)
	}
}

// TestTick_skipsBelowMinQueue verifies no packet is sent when fewer than
// MinQueue jobs are pending, even with the box offline.
func TestTick_skipsBelowMinQueue(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	svc := newTestService(t, sender, fakeQueue{pending: 1}, fixedHealth(false), time.Now)
	svc.Tick(context.Background())

	if sender.count() != 0 {
		t.Fatalf("send count = %d, want 0", sender.count())
	}
}

// TestTick_skipsWhenHealthy verifies no packet is sent when the sidecar is
// already reachable, regardless of the queue depth.
func TestTick_skipsWhenHealthy(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	svc := newTestService(t, sender, fakeQueue{pending: 9}, fixedHealth(true), time.Now)
	svc.Tick(context.Background())

	if sender.count() != 0 {
		t.Fatalf("send count = %d, want 0", sender.count())
	}
}

// TestTick_respectsCooldown verifies a second packet is suppressed until the
// cooldown elapses, then allowed again.
func TestTick_respectsCooldown(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	sender := &fakeSender{}
	svc := newTestService(t, sender, fakeQueue{pending: 5}, fixedHealth(false), clock)

	svc.Tick(context.Background())
	if sender.count() != 1 {
		t.Fatalf("after first tick: send count = %d, want 1", sender.count())
	}

	now = now.Add(svc.cooldown - time.Second) // still within cooldown
	svc.Tick(context.Background())
	if sender.count() != 1 {
		t.Fatalf("within cooldown: send count = %d, want 1", sender.count())
	}

	now = now.Add(2 * time.Second) // now past the cooldown
	svc.Tick(context.Background())
	if sender.count() != 2 {
		t.Fatalf("after cooldown: send count = %d, want 2", sender.count())
	}
}

// TestTick_sendErrorStartsCooldown verifies a failed send still arms the cooldown
// so a broken sender is not retried on every tick.
func TestTick_sendErrorStartsCooldown(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	sender := &fakeSender{err: errors.New("boom")}
	svc := newTestService(t, sender, fakeQueue{pending: 5}, fixedHealth(false), clock)

	svc.Tick(context.Background())
	now = now.Add(time.Second)
	svc.Tick(context.Background())

	if sender.count() != 1 {
		t.Fatalf("send count = %d, want 1 (cooldown should suppress retry)", sender.count())
	}
}

// TestTick_queueErrorNoSend verifies a queue-count failure is swallowed (logged)
// and never triggers a wake.
func TestTick_queueErrorNoSend(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	svc := newTestService(t, sender, fakeQueue{err: errors.New("db down")}, fixedHealth(false), time.Now)
	svc.Tick(context.Background())

	if sender.count() != 0 {
		t.Fatalf("send count = %d, want 0", sender.count())
	}
}

// TestDisabled_neverSends verifies a disabled Service is fully inert: even wired
// to a sender with offline health and a full queue, Run returns immediately and
// no packet is ever sent.
func TestDisabled_neverSends(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	svc, err := New(Config{
		Enabled: false,
		MAC:     "aa:bb:cc:dd:ee:ff",
		Queue:   fakeQueue{pending: 100},
		Health:  fixedHealth(false),
		Sender:  sender,
		Logger:  quietLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	svc.Tick(context.Background())                  // direct tick is a no-op
	svc.Run(context.Background(), time.Millisecond) // returns immediately when disabled

	if sender.count() != 0 {
		t.Fatalf("send count = %d, want 0", sender.count())
	}
}

// TestNew_validation covers the enabled-config invariants New enforces.
func TestNew_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid enabled with sender",
			cfg: Config{
				Enabled: true, MAC: "aa:bb:cc:dd:ee:ff",
				Queue: fakeQueue{}, Health: fixedHealth(false), Sender: &fakeSender{},
			},
		},
		{
			name: "invalid mac",
			cfg: Config{
				Enabled: true, MAC: "not-a-mac",
				Queue: fakeQueue{}, Health: fixedHealth(false), Sender: &fakeSender{},
			},
			wantErr: true,
		},
		{
			name: "missing queue and health",
			cfg: Config{
				Enabled: true, MAC: "aa:bb:cc:dd:ee:ff", Sender: &fakeSender{},
			},
			wantErr: true,
		},
		{
			name:    "disabled is always valid",
			cfg:     Config{Enabled: false, MAC: "garbage"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestNew_appliesDefaults verifies the zero-value thresholds fall back to the
// package defaults for an enabled Service.
func TestNew_appliesDefaults(t *testing.T) {
	t.Parallel()

	svc, err := New(Config{
		Enabled: true, MAC: "aa:bb:cc:dd:ee:ff",
		Queue: fakeQueue{}, Health: fixedHealth(false), Sender: &fakeSender{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.minQueue != defaultMinQueue {
		t.Errorf("minQueue = %d, want %d", svc.minQueue, defaultMinQueue)
	}
	if svc.cooldown != defaultCooldown {
		t.Errorf("cooldown = %s, want %s", svc.cooldown, defaultCooldown)
	}
	if svc.grace != defaultGracePeriod {
		t.Errorf("grace = %s, want %s", svc.grace, defaultGracePeriod)
	}
}
