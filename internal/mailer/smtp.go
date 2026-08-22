package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Encryption modes for the SMTP connection.
const (
	// EncryptionSTARTTLS connects in the clear on the submission port and upgrades
	// with STARTTLS. It is the default and what port 587 expects.
	EncryptionSTARTTLS = "starttls"
	// EncryptionTLS wraps the connection in TLS from the first byte (port 465).
	EncryptionTLS = "tls"
	// EncryptionNone sends unencrypted. It exists for a relay on localhost; the
	// standard library additionally refuses password authentication over such a
	// connection unless the server is the local host.
	EncryptionNone = "none"
)

// Defaults applied by NewSMTP to a configuration that leaves them out.
const (
	// DefaultPort is the SMTP submission port.
	DefaultPort = 587
	// DefaultTimeout bounds one whole delivery — connect, negotiate and send.
	DefaultTimeout = 15 * time.Second
)

// SMTPConfig describes the server SMTPSender talks to and the address it sends
// as. Username and Password are optional: a relay on localhost usually wants no
// authentication at all, and an empty username skips the AUTH command entirely.
type SMTPConfig struct {
	// Host is the SMTP server's hostname; it also becomes the TLS server name.
	Host string
	// Port is the server's port; 0 means DefaultPort.
	Port int
	// Username and Password authenticate with PLAIN. Both empty = no AUTH.
	Username string
	Password string
	// Encryption is EncryptionSTARTTLS (default), EncryptionTLS or EncryptionNone.
	Encryption string
	// FromAddress is the envelope and header sender; FromName is its optional
	// display name.
	FromAddress string
	FromName    string
	// Timeout bounds one delivery; 0 means DefaultTimeout.
	Timeout time.Duration
}

// SMTPSender delivers messages over SMTP using only the standard library, so the
// binary keeps building with CGO_ENABLED=0. One delivery is one connection: the
// volumes here are a few messages a day, and a pooled connection would only add
// a way to hold a socket open to a server that has since forgotten about it.
type SMTPSender struct {
	host       string
	port       int
	username   string
	password   string
	encryption string
	from       mail.Address
	timeout    time.Duration
	// now supplies the Date header; it is a field so a test can pin it.
	now func() time.Time
}

// NewSMTP validates cfg and returns a sender for it. It returns
// ErrIncompleteConfig — naming what is missing — when the host or the sender
// address is empty or the encryption mode is unknown; a missing port or timeout
// is filled in from the defaults rather than refused.
func NewSMTP(cfg SMTPConfig) (*SMTPSender, error) {
	encryption, err := normalizeEncryption(cfg.Encryption)
	if err != nil {
		return nil, err
	}
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, fmt.Errorf("%w: the host is empty", ErrIncompleteConfig)
	}
	from := strings.TrimSpace(cfg.FromAddress)
	if from == "" {
		return nil, fmt.Errorf("%w: the sender address is empty", ErrIncompleteConfig)
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("%w: the sender address %q is not an address: %w", ErrIncompleteConfig, from, err)
	}
	sender := &SMTPSender{
		host:       host,
		port:       cfg.Port,
		username:   cfg.Username,
		password:   cfg.Password,
		encryption: encryption,
		from:       mail.Address{Name: strings.TrimSpace(cfg.FromName), Address: from},
		timeout:    cfg.Timeout,
		now:        time.Now,
	}
	if sender.port <= 0 {
		sender.port = DefaultPort
	}
	if sender.timeout <= 0 {
		sender.timeout = DefaultTimeout
	}
	return sender, nil
}

// normalizeEncryption maps an encryption setting onto one of the three supported
// modes, treating the empty string as the STARTTLS default and rejecting anything
// else with ErrIncompleteConfig so a typo cannot silently downgrade to plaintext.
func normalizeEncryption(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", EncryptionSTARTTLS:
		return EncryptionSTARTTLS, nil
	case EncryptionTLS:
		return EncryptionTLS, nil
	case EncryptionNone:
		return EncryptionNone, nil
	default:
		return "", fmt.Errorf("%w: unknown encryption %q (want starttls, tls or none)", ErrIncompleteConfig, mode)
	}
}

// Send delivers msg over a fresh connection. A recipient that must never be
// written to is refused before anything is dialled (ErrInvalidAddress /
// ErrPlaceholderAddress); everything that goes wrong on the wire — connecting,
// STARTTLS, authentication, a rejected recipient — comes back as ErrSendFailed
// wrapping the server's own words.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	recipient, err := parseRecipient(msg.To)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := s.negotiate(client); err != nil {
		return err
	}
	return s.deliver(client, recipient, msg)
}

// connect dials the server, wraps the connection in TLS for the implicit-TLS
// mode, and reads the SMTP greeting. The context's deadline becomes the socket's
// deadline, so a server that accepts the connection and then goes quiet cannot
// hold the delivery open past the configured timeout.
func (s *SMTPSender) connect(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: connecting to %s: %w", ErrSendFailed, addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if s.encryption == EncryptionTLS {
		conn = tls.Client(conn, s.tlsConfig())
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: greeting from %s: %w", ErrSendFailed, addr, err)
	}
	return client, nil
}

// negotiate upgrades the connection with STARTTLS when that mode is configured
// and authenticates when a username is set. An empty username skips AUTH, which
// is what an unauthenticated local relay needs.
func (s *SMTPSender) negotiate(client *smtp.Client) error {
	if s.encryption == EncryptionSTARTTLS {
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("%w: starttls on %s: %w", ErrSendFailed, s.host, err)
		}
	}
	if s.username == "" {
		return nil
	}
	if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return fmt.Errorf("%w: authenticating as %q: %w", ErrSendFailed, s.username, err)
	}
	return nil
}

// deliver runs the MAIL/RCPT/DATA exchange for one message and closes the session
// politely with QUIT, whose error still counts: a server that refuses the QUIT
// may not have committed the message.
func (s *SMTPSender) deliver(client *smtp.Client, recipient string, msg Message) error {
	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("%w: MAIL FROM %q: %w", ErrSendFailed, s.from.Address, err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("%w: RCPT TO %q: %w", ErrSendFailed, recipient, err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("%w: DATA: %w", ErrSendFailed, err)
	}
	if _, err := writer.Write(renderMessage(s.from, msg, s.now())); err != nil {
		return fmt.Errorf("%w: writing the message: %w", ErrSendFailed, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("%w: finishing the message: %w", ErrSendFailed, err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("%w: QUIT: %w", ErrSendFailed, err)
	}
	return nil
}

// tlsConfig is the TLS configuration for both the implicit and the STARTTLS
// mode: the configured host is the expected certificate name, and nothing below
// TLS 1.2 is accepted.
func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
}

// Compile-time proof that the SMTP sender satisfies the interface.
var _ Sender = (*SMTPSender)(nil)
