package mailer

import (
	"context"
	"slices"
	"sync"
)

// Fake is an in-memory Sender that records what it was asked to send instead of
// delivering it. It lives in the package rather than in a _test.go file because
// every caller of Sender wants it in its own tests; it opens no socket, needs no
// server and is safe for concurrent use.
//
// It applies the same recipient guard as the real sender, so a test that mails a
// placeholder address fails there rather than in production.
type Fake struct {
	mu   sync.Mutex
	sent []Message
	err  error
}

// NewFake returns an empty Fake that accepts every valid recipient.
func NewFake() *Fake {
	return &Fake{}
}

// Send records msg and returns nil, unless a recipient guard rejects it (nothing
// is recorded then) or FailWith installed an error to return.
func (f *Fake) Send(_ context.Context, msg Message) error {
	if err := ValidateAddress(msg.To); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg)
	return nil
}

// FailWith makes every subsequent Send return err without recording anything;
// passing nil restores normal recording. Use it to exercise a caller's failure
// path, typically with ErrSendFailed.
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Sent returns a copy of the messages recorded so far, oldest first.
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

// Last returns the most recently recorded message; ok is false when nothing has
// been sent.
func (f *Fake) Last() (msg Message, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return Message{}, false
	}
	return f.sent[len(f.sent)-1], true
}

// Reset forgets every recorded message, leaving any installed failure in place.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

// Compile-time proof that both non-SMTP senders satisfy the interface.
var (
	_ Sender = Noop{}
	_ Sender = (*Fake)(nil)
)
