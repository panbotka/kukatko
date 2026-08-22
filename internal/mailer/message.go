package mailer

import (
	"bytes"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"time"
)

// charsetUTF8 is the charset every message is written in; it appears both in the
// Content-Type header and inside the RFC 2047 encoded subject.
const charsetUTF8 = "utf-8"

// renderMessage builds the RFC 5322 bytes handed to the SMTP DATA command:
// headers terminated by CRLF, a blank line, then the body.
//
// The subject goes through RFC 2047 Q-encoding and the sender's display name
// through net/mail's own encoder, so "Účet schválen" and "Kukátko" survive a
// server that only accepts ASCII headers. The body is quoted-printable rather
// than raw 8-bit: it needs no 8BITMIME support from the server and it folds the
// long lines a 998-octet line limit would otherwise break.
func renderMessage(from mail.Address, msg Message, sentAt time.Time) []byte {
	var buf bytes.Buffer
	headers := [][2]string{
		{"From", from.String()},
		{"To", msg.To},
		{"Subject", mime.QEncoding.Encode(charsetUTF8, msg.Subject)},
		{"Date", sentAt.Format(time.RFC1123Z)},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=" + charsetUTF8},
		{"Content-Transfer-Encoding", "quoted-printable"},
	}
	for _, header := range headers {
		buf.WriteString(header[0])
		buf.WriteString(": ")
		buf.WriteString(header[1])
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")

	// A bytes.Buffer never fails a write, so neither can the encoder wrapping it.
	writer := quotedprintable.NewWriter(&buf)
	_, _ = writer.Write([]byte(msg.Body))
	_ = writer.Close()
	return buf.Bytes()
}
