package push

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestFake_records covers recording, Last, Sent's copy semantics and Reset.
func TestFake_records(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	ctx := context.Background()
	if _, ok := fake.Last(); ok {
		t.Fatal("Last() on an empty fake reported a delivery")
	}
	phone := newTestBrowser(t, "https://push.example.org/phone").sub
	laptop := newTestBrowser(t, "https://push.example.org/laptop").sub
	if err := fake.Send(ctx, phone, Notification{Title: "one"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := fake.Send(ctx, laptop, Notification{Title: "two"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := fake.Sent()
	if len(sent) != 2 || sent[0].Notification.Title != "one" || sent[1].Subscription.Endpoint != laptop.Endpoint {
		t.Fatalf("Sent() = %+v", sent)
	}
	sent[0].Notification.Title = "mutated"
	if fake.Sent()[0].Notification.Title != "one" {
		t.Fatal("Sent() returned the fake's own slice")
	}
	last, ok := fake.Last()
	if !ok || last.Notification.Title != "two" {
		t.Fatalf("Last() = %+v, %v", last, ok)
	}
	fake.Reset()
	if len(fake.Sent()) != 0 {
		t.Fatal("Reset() kept deliveries")
	}
}

// TestFake_guards proves the fake refuses what the real sender refuses.
func TestFake_guards(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	ctx := context.Background()
	good := newTestBrowser(t, "https://push.example.org/x").sub
	bad := good
	bad.Endpoint = "http://push.example.org/x"

	if err := fake.Send(ctx, bad, Notification{Title: "T"}); !errors.Is(err, ErrInvalidSubscription) {
		t.Errorf("bad subscription: error = %v", err)
	}
	if err := fake.Send(ctx, good, Notification{}); !errors.Is(err, ErrInvalidNotification) {
		t.Errorf("no title: error = %v", err)
	}
	big := Notification{Title: "T", Body: strings.Repeat("x", MaxPayloadSize)}
	if err := fake.Send(ctx, good, big); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("oversized: error = %v", err)
	}
	if n := len(fake.Sent()); n != 0 {
		t.Fatalf("refused sends were recorded: %d", n)
	}
}

// TestFake_failures covers the global and the per-endpoint failure switches and
// that the per-endpoint one wins.
func TestFake_failures(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	ctx := context.Background()
	dead := newTestBrowser(t, "https://push.example.org/dead").sub
	live := newTestBrowser(t, "https://push.example.org/live").sub
	n := Notification{Title: "T"}

	fake.FailEndpoint(dead.Endpoint, ErrGone)
	if err := fake.Send(ctx, dead, n); !errors.Is(err, ErrGone) {
		t.Fatalf("dead endpoint: error = %v, want ErrGone", err)
	}
	if err := fake.Send(ctx, live, n); err != nil {
		t.Fatalf("live endpoint: error = %v", err)
	}

	fake.FailWith(ErrRetryable)
	if err := fake.Send(ctx, live, n); !errors.Is(err, ErrRetryable) {
		t.Fatalf("global failure: error = %v, want ErrRetryable", err)
	}
	if err := fake.Send(ctx, dead, n); !errors.Is(err, ErrGone) {
		t.Fatalf("endpoint failure should win over the global one: error = %v", err)
	}

	fake.FailWith(nil)
	fake.FailEndpoint(dead.Endpoint, nil)
	if err := fake.Send(ctx, dead, n); err != nil {
		t.Fatalf("after clearing: error = %v", err)
	}
	if got := len(fake.Sent()); got != 2 {
		t.Fatalf("recorded %d deliveries, want 2 (only the successful ones)", got)
	}
}

// TestFake_concurrent exercises the fake from many goroutines; run under -race
// it proves the locking.
func TestFake_concurrent(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	sub := newTestBrowser(t, "https://push.example.org/x").sub
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_ = fake.Send(context.Background(), sub, Notification{Title: "T"}) // counted below
		})
	}
	wg.Wait()
	if got := len(fake.Sent()); got != 20 {
		t.Fatalf("recorded %d deliveries, want 20", got)
	}
}
