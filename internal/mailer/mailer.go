// Package mailer sends the handful of transactional e-mails Kukátko needs
// around accounts — a registration was received, an account was approved, an
// administrator has somebody to approve, somebody forgot their password.
//
// Everything goes through the Sender interface, so a caller never depends on
// SMTP: production wires SMTPSender, an instance with mail turned off wires
// Noop, and tests wire Fake, which records what it was asked to send and never
// opens a socket. Messages are plain text in UTF-8 — no HTML alternative — and
// their subjects are RFC 2047 encoded so Czech diacritics survive a server that
// only speaks ASCII headers.
//
// Two guard rails matter to callers. The user table holds placeholder addresses
// in the reserved .invalid domain (RFC 6761), and sending to one is refused with
// ErrPlaceholderAddress before anything is dialled. A delivery that genuinely
// failed is ErrSendFailed, which is what tells "this address is unusable" apart
// from "the mail server did not take it this time" — the first is permanent, the
// second is worth retrying.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Sentinel errors returned by the senders in this package so callers can match
// them with errors.Is.
var (
	// ErrInvalidAddress indicates the recipient is empty or is not a parseable
	// e-mail address. Nothing was sent and retrying is pointless.
	ErrInvalidAddress = errors.New("mailer: invalid recipient address")
	// ErrPlaceholderAddress indicates the recipient sits in the reserved .invalid
	// domain, which by RFC 6761 can never resolve. Accounts imported without a
	// real address carry one, and mail to them is refused rather than attempted.
	ErrPlaceholderAddress = errors.New("mailer: recipient is a placeholder address")
	// ErrSendFailed indicates the message could not be delivered: the server was
	// unreachable, refused the credentials, or rejected the message. The address
	// itself may well be fine, so the caller may retry later.
	ErrSendFailed = errors.New("mailer: sending the message failed")
	// ErrIncompleteConfig indicates NewSMTP was given a configuration it cannot
	// send with — a missing host or sender address, or an unknown encryption mode.
	ErrIncompleteConfig = errors.New("mailer: incomplete SMTP configuration")
)

// invalidTLD is the reserved top-level domain (RFC 6761) used by placeholder
// addresses in the user table. Mail to it is refused, never attempted.
const invalidTLD = "invalid"

// Message is one outbound e-mail: a single recipient, a subject, and a plain-text
// body in UTF-8. There is deliberately no HTML alternative, no attachments and no
// carbon copies — nothing Kukátko sends needs them.
type Message struct {
	// To is the recipient, either a bare address or a "Name <addr>" pair.
	To string
	// Subject is the unencoded subject line; the sender RFC 2047 encodes it.
	Subject string
	// Body is the plain-text body in UTF-8, with \n line breaks.
	Body string
}

// Sender sends one message. Every caller depends on this interface rather than
// on SMTP, so mail can be turned off (Noop) or captured (Fake) without the
// caller knowing.
type Sender interface {
	// Send delivers msg, or returns ErrInvalidAddress / ErrPlaceholderAddress for
	// a recipient that must never be written to, or ErrSendFailed for a delivery
	// that did not go through. The context bounds the attempt.
	Send(ctx context.Context, msg Message) error
}

// Noop is the Sender used when mail is disabled. It accepts every message and
// does nothing with it — including for a placeholder address, because there is
// no attempt to refuse: an instance with mail switched off must never fail a
// registration just because nobody has configured an SMTP server.
type Noop struct{}

// Send discards msg and returns nil.
func (Noop) Send(_ context.Context, _ Message) error {
	return nil
}

// ValidateAddress reports whether addr may be written to at all. It returns
// ErrInvalidAddress for an empty or unparseable address and ErrPlaceholderAddress
// for anything in the reserved .invalid domain, wrapping the offending value so
// the message is actionable.
func ValidateAddress(addr string) error {
	_, err := parseRecipient(addr)
	return err
}

// parseRecipient validates addr and returns the bare address inside it, so a
// "Jméno <adresa>" recipient reaches the SMTP RCPT command as just the address.
// It returns the same errors as ValidateAddress.
func parseRecipient(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", fmt.Errorf("%w: the address is empty", ErrInvalidAddress)
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidAddress, addr, err)
	}
	if IsPlaceholder(parsed.Address) {
		return "", fmt.Errorf("%w: %q", ErrPlaceholderAddress, parsed.Address)
	}
	return parsed.Address, nil
}

// IsPlaceholder reports whether addr sits in the reserved .invalid domain — the
// shape the user table uses for an account with no real e-mail. The comparison
// is case-insensitive and covers both "someone@invalid" and
// "someone@example.invalid".
func IsPlaceholder(addr string) bool {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(strings.TrimSuffix(addr[at+1:], "."))
	return domain == invalidTLD || strings.HasSuffix(domain, "."+invalidTLD)
}
