# Message-ID on every mail

`internal/mailer` (message.go / smtp.go) writes From, To, Subject, Date, MIME-Version and Content-Type, but **no `Message-ID`**. A test SMTP catcher showed the only Message-ID in a received mail was the one the catcher added itself. RFC 5322 says a message SHOULD carry one, and large receivers (Gmail among them) score or reject messages without one. Whether the production submission server adds it is outside our control.

## Requirements

- Every message the mailer sends carries a unique `Message-ID: <random@domain>`. The domain part is the domain of `mail.from_address`, falling back to the host of `mail.base_url`, falling back to `kukatko.invalid`. The random part has at least 128 bits of entropy (crypto/rand), base32 or hex encoded.
- The ID is generated when the message is built for sending. A retried `mail_send` job may get a new ID; that is acceptable. If keeping it stable across retries is simple (derive it from the job id), prefer that and document it.
- Add an `Auto-Submitted: auto-generated` header (RFC 3834), so auto-responders do not answer notification mail.
- Tests: the header is present, well-formed, unique across two messages, and uses the from-address domain. The fake sender exposes the headers so this is testable without a socket.
- Docs: `docs/PACKAGES.md` (mailer) and `docs/OPERATIONS.md` (mail section).
