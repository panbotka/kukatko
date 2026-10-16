package mailer

import (
	"crypto/rand"
	"mime"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// FallbackMessageIDDomain is the right-hand side of a Message-ID when neither the
// sender address nor the instance's base URL names a domain. It sits in the
// reserved .invalid TLD (RFC 6761), so it can never collide with somebody's real
// domain.
const FallbackMessageIDDomain = "kukatko.invalid"

// MessageIDDomain picks the domain written after the "@" of every Message-ID:
// the domain of fromAddress, falling back to the host of baseURL (without a
// port), falling back to FallbackMessageIDDomain. Using the sender's own domain
// is what RFC 5322 §3.6.4 suggests to keep the identifier globally unique.
func MessageIDDomain(fromAddress, baseURL string) string {
	if parsed, err := mail.ParseAddress(strings.TrimSpace(fromAddress)); err == nil {
		if at := strings.LastIndex(parsed.Address, "@"); at >= 0 {
			if domain := strings.ToLower(strings.TrimSuffix(parsed.Address[at+1:], ".")); domain != "" {
				return domain
			}
		}
	}
	if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err == nil {
		if host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")); host != "" && net.ParseIP(host) == nil {
			return host
		}
	}
	return FallbackMessageIDDomain
}

// NewMessageID returns a fresh "<random@domain>" identifier. The random part is
// crypto/rand.Text — 26 base32 characters, 130 bits of entropy — so two messages
// never share one even across instances that send from the same domain.
//
// It is minted when a message is built for sending, so a retried `mail_send` job
// carries a new identifier. That is deliberate: the queue's job id is a
// sequential number with no entropy, and a retry only happens when the previous
// attempt was not accepted, so a receiver has nothing to deduplicate against.
func NewMessageID(domain string) string {
	return "<" + rand.Text() + "@" + domain + ">"
}

// messageHeaders lists the headers of one message in the order they are written
// on the wire. messageID is a complete "<id@domain>" value.
//
// The subject goes through RFC 2047 Q-encoding and the sender's display name
// through net/mail's own encoder, so "Účet schválen" and "Kukátko" survive a
// server that only accepts ASCII headers. Auto-Submitted (RFC 3834) tells an
// auto-responder — a vacation reply, an out-of-office — not to answer: every mail
// Kukátko sends is machine-generated and nobody reads the replies.
func messageHeaders(from mail.Address, msg Message, sentAt time.Time, messageID string) [][2]string {
	return [][2]string{
		{"From", from.String()},
		{"To", msg.To},
		{"Subject", mime.QEncoding.Encode(charsetUTF8, msg.Subject)},
		{"Date", sentAt.Format(time.RFC1123Z)},
		{"Message-ID", messageID},
		{"Auto-Submitted", "auto-generated"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=" + charsetUTF8},
		{"Content-Transfer-Encoding", "quoted-printable"},
	}
}
