package mailer

import (
	"bytes"
	"mime/quotedprintable"
	"net/mail"
	"time"
)

// charsetUTF8 is the charset every message is written in; it appears both in the
// Content-Type header and inside the RFC 2047 encoded subject.
const charsetUTF8 = "utf-8"

// renderMessage builds the RFC 5322 bytes handed to the SMTP DATA command:
// the headers of messageHeaders terminated by CRLF, a blank line, then the body.
//
// The body is quoted-printable rather than raw 8-bit: it needs no 8BITMIME
// support from the server and it folds the long lines a 998-octet line limit
// would otherwise break.
func renderMessage(from mail.Address, msg Message, sentAt time.Time, messageID string) []byte {
	var buf bytes.Buffer
	for _, header := range messageHeaders(from, msg, sentAt, messageID) {
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
