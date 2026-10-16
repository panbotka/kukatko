package mailer

import (
	"context"
	"net/mail"
	"net/textproto"
	"slices"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Sender that records what it was asked to send instead of
// delivering it. It lives in the package rather than in a _test.go file because
// every caller of Sender wants it in its own tests; it opens no socket, needs no
// server and is safe for concurrent use.
//
// It applies the same recipient guard as the real sender, so a test that mails a
// placeholder address fails there rather than in production, and it builds the
// same headers the SMTP sender writes (Message-ID included), so a test can read
// them through SentHeaders without a socket.
type Fake struct {
	mu       sync.Mutex
	from     mail.Address
	idDomain string
	sent     []Message
	headers  []mail.Header
	err      error
}

// fakeFromAddress is the sender NewFake writes as: a placeholder in the reserved
// .invalid domain, since a fake delivers nothing.
const fakeFromAddress = "kukatko@" + FallbackMessageIDDomain

// NewFake returns an empty Fake that accepts every valid recipient and writes
// its headers as a placeholder sender.
func NewFake() *Fake {
	return NewFakeFrom(fakeFromAddress, "")
}

// NewFakeFrom returns an empty Fake whose headers are written as fromAddress,
// with the Message-ID domain derived exactly as the SMTP sender derives it from
// fromAddress and baseURL (MessageIDDomain).
func NewFakeFrom(fromAddress, baseURL string) *Fake {
	return &Fake{
		from:     mail.Address{Address: strings.TrimSpace(fromAddress)},
		idDomain: MessageIDDomain(fromAddress, baseURL),
	}
}

// Send records msg and the headers it would have been written with and returns
// nil, unless a recipient guard rejects it (nothing is recorded then) or FailWith
// installed an error to return.
func (f *Fake) Send(_ context.Context, msg Message) error {
	if err := ValidateAddress(msg.To); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	header := mail.Header{}
	for _, field := range messageHeaders(f.from, msg, time.Now(), NewMessageID(f.idDomain)) {
		// Canonical keys, so mail.Header.Get finds "Message-ID" under "Message-Id".
		key := textproto.CanonicalMIMEHeaderKey(field[0])
		header[key] = append(header[key], field[1])
	}
	f.sent = append(f.sent, msg)
	f.headers = append(f.headers, header)
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

// SentHeaders returns the headers of the messages recorded so far, oldest first
// and index-aligned with Sent. The values are as written on the wire, so the
// subject is still RFC 2047 encoded.
func (f *Fake) SentHeaders() []mail.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.headers)
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
	f.headers = nil
}

// Compile-time proof that both non-SMTP senders satisfy the interface.
var (
	_ Sender = Noop{}
	_ Sender = (*Fake)(nil)
)
