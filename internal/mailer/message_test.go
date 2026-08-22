package mailer

import (
	"net/mail"
	"strings"
	"testing"
	"time"
)

// TestRenderMessage verifies the wire format: CRLF headers, a blank line, a
// quoted-printable body, and an RFC 2047 encoded subject and sender name so
// Czech diacritics survive an ASCII-only header.
func TestRenderMessage(t *testing.T) {
	t.Parallel()

	from := mail.Address{Name: "Kukátko", Address: "kukatko@example.com"}
	sentAt := time.Date(2026, time.August, 22, 9, 30, 0, 0, time.UTC)
	msg := Message{
		To:      "jan@example.com",
		Subject: "Váš účet v Kukátku byl schválen",
		Body:    "Dobrý den,\n\nhotovo.\n",
	}

	got := string(renderMessage(from, msg, sentAt))
	head, bodyPart, found := strings.Cut(got, "\r\n\r\n")
	if !found {
		t.Fatalf("rendered message has no header/body separator:\n%s", got)
	}

	wantHeaders := []string{
		"From: =?utf-8?q?Kuk=C3=A1tko?= <kukatko@example.com>",
		"To: jan@example.com",
		"Subject: =?utf-8?q?V=C3=A1=C5=A1_=C3=BA=C4=8Det_v_Kuk=C3=A1tku_byl_schv=C3=A1len?=",
		"Date: Sat, 22 Aug 2026 09:30:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
	}
	if head != strings.Join(wantHeaders, "\r\n") {
		t.Errorf("headers =\n%q\nwant\n%q", head, strings.Join(wantHeaders, "\r\n"))
	}
	if want := "Dobr=C3=BD den,\r\n\r\nhotovo.\r\n"; bodyPart != want {
		t.Errorf("body = %q, want %q", bodyPart, want)
	}
}

// TestRenderMessage_lineEndings verifies every line break on the wire is CRLF —
// a bare LF would be an SMTP protocol violation.
func TestRenderMessage_lineEndings(t *testing.T) {
	t.Parallel()

	from := mail.Address{Address: "kukatko@example.com"}
	msg := Message{To: "jan@example.com", Subject: "Test", Body: "první\ndruhý\n"}
	got := string(renderMessage(from, msg, time.Unix(0, 0).UTC()))

	for i := range len(got) {
		if got[i] == '\n' && (i == 0 || got[i-1] != '\r') {
			t.Fatalf("bare LF at offset %d in:\n%q", i, got)
		}
	}
	// With no display name net/mail writes a bare angle-addr, which is what a
	// From header without a name looks like in RFC 5322.
	if !strings.HasPrefix(got, "From: <kukatko@example.com>\r\n") {
		t.Errorf("From header = %q, want a bare angle-addr", got[:40])
	}
}

// TestRenderMessage_longLineIsFolded verifies the quoted-printable encoder folds
// a body line that would otherwise exceed the SMTP line limit.
func TestRenderMessage_longLineIsFolded(t *testing.T) {
	t.Parallel()

	from := mail.Address{Address: "kukatko@example.com"}
	msg := Message{To: "jan@example.com", Subject: "Test", Body: strings.Repeat("a", 500) + "\n"}
	got := string(renderMessage(from, msg, time.Unix(0, 0).UTC()))

	for line := range strings.SplitSeq(got, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line of %d octets exceeds the RFC 5322 limit", len(line))
		}
	}
	if !strings.Contains(got, "=\r\n") {
		t.Error("expected a soft line break in the encoded body")
	}
}
