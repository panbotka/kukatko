package mailer

import (
	"bufio"
	"context"
	"net"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// messageIDPattern is the shape every Message-ID must have: angle brackets
// around 26 base32 characters (crypto/rand.Text, 130 bits), "@" and a domain.
var messageIDPattern = regexp.MustCompile(`^<[A-Z2-7]{26}@([a-z0-9.-]+)>$`)

// assertMessageID fails the test unless id is a well-formed Message-ID on
// wantDomain, and returns it for a uniqueness comparison.
func assertMessageID(t *testing.T, id, wantDomain string) string {
	t.Helper()
	match := messageIDPattern.FindStringSubmatch(id)
	if match == nil {
		t.Fatalf("Message-ID %q is not <26 base32 chars@domain>", id)
	}
	if match[1] != wantDomain {
		t.Errorf("Message-ID %q has domain %q, want %q", id, match[1], wantDomain)
	}
	return id
}

// TestMessageIDDomain covers the fallback chain: the sender's domain, then the
// base URL's host, then the reserved placeholder.
func TestMessageIDDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, from, baseURL, want string
	}{
		{name: "from address wins", from: "kukatko@Example.COM", baseURL: "https://photos.example.org", want: "example.com"},
		{name: "named from address", from: "Kukátko <kukatko@example.com>", want: "example.com"},
		{name: "base url host", from: "", baseURL: "https://photos.example.org:8443/app", want: "photos.example.org"},
		{name: "unparseable from", from: "not an address", baseURL: "https://photos.example.org", want: "photos.example.org"},
		{name: "ip base url", baseURL: "http://192.0.2.10:6480", want: FallbackMessageIDDomain},
		{name: "nothing", want: FallbackMessageIDDomain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MessageIDDomain(tt.from, tt.baseURL); got != tt.want {
				t.Errorf("MessageIDDomain(%q, %q) = %q, want %q", tt.from, tt.baseURL, got, tt.want)
			}
		})
	}
}

// TestNewMessageID verifies the identifier is well formed and never repeats.
func TestNewMessageID(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for range 1000 {
		id := assertMessageID(t, NewMessageID("example.com"), "example.com")
		if seen[id] {
			t.Fatalf("Message-ID %q was minted twice", id)
		}
		seen[id] = true
	}
}

// TestFake_headers verifies the fake records the headers the SMTP sender would
// write: a well-formed Message-ID on the sender's domain, different for every
// message, and the RFC 3834 Auto-Submitted marker.
func TestFake_headers(t *testing.T) {
	t.Parallel()

	fake := NewFakeFrom("kukatko@example.com", "https://photos.example.org")
	for _, to := range []string{"jan@example.com", "eva@example.com"} {
		if err := fake.Send(context.Background(), Message{To: to, Subject: "Ahoj", Body: "tělo\n"}); err != nil {
			t.Fatalf("Send(%q) returned error: %v", to, err)
		}
	}

	headers := fake.SentHeaders()
	if len(headers) != 2 || len(fake.Sent()) != 2 {
		t.Fatalf("recorded %d headers for %d messages, want 2 and 2", len(headers), len(fake.Sent()))
	}
	first := assertMessageID(t, headers[0].Get("Message-ID"), "example.com")
	second := assertMessageID(t, headers[1].Get("Message-ID"), "example.com")
	if first == second {
		t.Errorf("both messages carry Message-ID %q", first)
	}
	for i, header := range headers {
		if got := header.Get("Auto-Submitted"); got != "auto-generated" {
			t.Errorf("message %d Auto-Submitted = %q, want auto-generated", i, got)
		}
		if got := header.Get("To"); got != fake.Sent()[i].To {
			t.Errorf("message %d To = %q, want %q", i, got, fake.Sent()[i].To)
		}
	}

	fake.Reset()
	if got := fake.SentHeaders(); len(got) != 0 {
		t.Errorf("SentHeaders after Reset = %d entries, want none", len(got))
	}
}

// TestNewFake_placeholderDomain verifies a fake built without a sender writes its
// Message-IDs on the reserved placeholder domain.
func TestNewFake_placeholderDomain(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	if err := fake.Send(context.Background(), Message{To: "jan@example.com", Subject: "Test"}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	assertMessageID(t, fake.SentHeaders()[0].Get("Message-ID"), FallbackMessageIDDomain)
}

// TestSMTPSender_Send_writesMessageID drives the real sender against a minimal
// in-process SMTP server and parses what reached DATA: two deliveries carry two
// different, well-formed Message-IDs on the sender's domain.
func TestSMTPSender_Send_writesMessageID(t *testing.T) {
	t.Parallel()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	received := make(chan string, 2)
	go serveSMTP(listener, received)

	sender, err := NewSMTP(SMTPConfig{
		Host:        "127.0.0.1",
		Port:        listener.Addr().(*net.TCPAddr).Port,
		Encryption:  EncryptionNone,
		FromAddress: "kukatko@example.com",
		BaseURL:     "https://photos.example.org",
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSMTP returned error: %v", err)
	}

	ids := make([]string, 0, 2)
	for range 2 {
		if err := sender.Send(context.Background(), Message{To: "jan@example.com", Subject: "Test", Body: "tělo\n"}); err != nil {
			t.Fatalf("Send returned error: %v", err)
		}
		parsed, err := mail.ReadMessage(strings.NewReader(<-received))
		if err != nil {
			t.Fatalf("parsing the delivered message: %v", err)
		}
		ids = append(ids, assertMessageID(t, parsed.Header.Get("Message-ID"), "example.com"))
		if got := parsed.Header.Get("Auto-Submitted"); got != "auto-generated" {
			t.Errorf("Auto-Submitted = %q, want auto-generated", got)
		}
	}
	if ids[0] == ids[1] {
		t.Errorf("both deliveries carry Message-ID %q", ids[0])
	}
}

// serveSMTP accepts connections until the listener closes and answers each with
// just enough SMTP for net/smtp's client, sending every DATA payload (dot
// unstuffed, CRLF kept) to received.
func serveSMTP(listener net.Listener, received chan<- string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serveSMTPConn(conn, received)
	}
}

// serveSMTPConn runs one SMTP session on conn.
func serveSMTPConn(conn net.Conn, received chan<- string) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	reply := func(code int, text string) { _, _ = conn.Write([]byte(strconv.Itoa(code) + " " + text + "\r\n")) }
	reply(220, "test ready")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		switch verb {
		case "EHLO", "HELO", "MAIL", "RCPT", "RSET", "NOOP":
			reply(250, "ok")
		case "DATA":
			reply(354, "go ahead")
			var data strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(dataLine, "."))
			}
			received <- data.String()
			reply(250, "queued")
		case "QUIT":
			reply(221, "bye")
			return
		default:
			reply(502, "not implemented")
		}
	}
}
