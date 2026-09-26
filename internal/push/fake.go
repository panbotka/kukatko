package push

import (
	"context"
	"slices"
	"sync"
)

// Delivery is one send a Fake recorded: the subscription it was addressed to
// and the notification.
type Delivery struct {
	// Subscription is the subscription Send was called with.
	Subscription Subscription
	// Notification is the notification Send was called with.
	Notification Notification
}

// Fake is an in-memory Sender that records what it was asked to send instead of
// delivering it. It lives in the package rather than in a _test.go file because
// every caller of Sender wants it in its own tests; it opens no socket, needs no
// keys and is safe for concurrent use.
//
// It applies the same guards as the real sender — the subscription must be one
// that could be sent to and the notification must encode within MaxPayloadSize
// — so a test that sends something the real sender would refuse fails there
// rather than in production.
type Fake struct {
	mu         sync.Mutex
	sent       []Delivery
	err        error
	byEndpoint map[string]error
}

// NewFake returns an empty Fake that accepts every valid send.
func NewFake() *Fake {
	return &Fake{byEndpoint: make(map[string]error)}
}

// Send records the delivery and returns nil, unless a guard rejects it or an
// installed failure applies (nothing is recorded then). An endpoint-specific
// failure (FailEndpoint) wins over the global one (FailWith).
func (f *Fake) Send(_ context.Context, sub Subscription, n Notification) error {
	if err := ValidateSubscription(sub); err != nil {
		return err
	}
	if _, err := n.Encode(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.byEndpoint[sub.Endpoint]; ok {
		return err
	}
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, Delivery{Subscription: sub, Notification: n})
	return nil
}

// FailWith makes every subsequent Send return err without recording anything;
// passing nil restores normal recording. Use it to exercise a caller's failure
// path, typically with ErrRetryable.
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// FailEndpoint makes every subsequent Send to endpoint return err without
// recording anything, while other endpoints keep working — the shape of one
// dead device among live ones, typically with ErrGone. Passing nil removes the
// endpoint's failure.
func (f *Fake) FailEndpoint(endpoint string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.byEndpoint, endpoint)
		return
	}
	f.byEndpoint[endpoint] = err
}

// Sent returns a copy of the deliveries recorded so far, oldest first.
func (f *Fake) Sent() []Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

// Last returns the most recently recorded delivery; ok is false when nothing
// has been sent.
func (f *Fake) Last() (delivery Delivery, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return Delivery{}, false
	}
	return f.sent[len(f.sent)-1], true
}

// Reset forgets every recorded delivery, leaving any installed failures in place.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

// Compile-time proof that every sender satisfies the interface.
var (
	_ Sender = Noop{}
	_ Sender = (*Fake)(nil)
	_ Sender = (*VAPIDSender)(nil)
)
