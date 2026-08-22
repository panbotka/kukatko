// Package mailjob delivers Kukátko's transactional e-mail through the persistent
// job queue instead of from the request that caused it.
//
// Sending straight from an HTTP handler has two faults: the request waits on a
// remote host, and a mail server that is briefly unreachable loses the message
// for good. Both disappear once the queue is the only delivery path. A caller
// schedules a `mail_send` job — cheaply, and inside its own transaction, so a
// registration that rolls back sends nothing — and the worker renders and
// delivers it later, retrying with the queue's own backoff for as long as the
// server stays unreachable.
//
// The payload names a template and carries its data rather than a finished
// message, so nothing in it depends on the process that scheduled it: it is
// rendered when it is sent, which is what lets a mail survive a restart. The
// templates themselves and the SMTP client live in internal/mailer; this package
// only decides what is scheduled, when it is refused and which failures are worth
// retrying.
//
// Two refusals happen before anything is queued. With mail disabled in the
// configuration nothing is enqueued at all — an instance nobody gave an SMTP
// server to must not grow a queue of jobs that can only fail — and a recipient in
// the reserved .invalid domain is skipped, because those are the placeholder
// addresses of accounts imported without a real one.
package mailjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/textproto"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/worker"
)

// Sentinel errors this package returns so callers and tests can branch with
// errors.Is.
var (
	// ErrUnknownTemplate indicates a job (or an Enqueue call) named a template
	// this package cannot render. It is permanent: no later attempt will teach a
	// running binary a template it does not have.
	ErrUnknownTemplate = errors.New("mailjob: unknown mail template")
	// ErrMissingRecipient indicates a `mail_send` payload carried no recipient. It
	// is permanent for the same reason — the payload is already written and will
	// not gain an address.
	ErrMissingRecipient = errors.New("mailjob: payload has no recipient")
)

// firstPermanentSMTPCode is the lowest SMTP reply code that means "permanent
// failure" (RFC 5321 §4.2.1): a 5yz reply will be repeated verbatim however often
// the same message is offered, while a 4yz reply invites a retry.
const firstPermanentSMTPCode = 500

// payload is the JSON shape of a `mail_send` job: which template to render, whom
// to send it to and the template's own data. It deliberately holds no rendered
// text and no live object — everything in it is plain JSON that means the same
// thing after a restart or an upgrade.
type payload struct {
	// Template is one of the mailer.TemplateXxx names.
	Template string `json:"template"`
	// To is the recipient address.
	To string `json:"to"`
	// Data is the template's data struct, encoded as JSON.
	Data json.RawMessage `json:"data,omitempty"`
}

// ServiceConfig bundles the handler's collaborators. Sender is required.
type ServiceConfig struct {
	// Sender delivers the rendered message. Production wires mailer.SMTPSender;
	// tests wire mailer.Fake.
	Sender mailer.Sender
	// Logger records what was delivered; nil uses slog.Default().
	Logger *slog.Logger
}

// Service is the worker handler for `mail_send` jobs: it renders the template
// named in the payload and hands the message to the sender.
type Service struct {
	sender mailer.Sender
	log    *slog.Logger
}

// NewService builds a Service from cfg. It panics if Sender is nil, since a mail
// handler without a sender is a wiring bug that should surface at startup rather
// than dead-letter every message.
func NewService(cfg ServiceConfig) *Service {
	if cfg.Sender == nil {
		panic("mailjob: NewService requires a Sender")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{sender: cfg.Sender, log: log}
}

// Handle is the worker.HandlerFunc for `mail_send` jobs. It decodes the payload,
// renders its template and delivers the message.
//
// Failures are classified, because the queue treats them differently: anything
// the payload itself makes impossible (it does not decode, it names no recipient,
// it names a template this binary does not have) and any permanent rejection by
// the server — a malformed or placeholder address, an SMTP 5yz reply — is a
// worker.Terminal failure that is never retried. Everything else (the host is
// down, the greeting timed out, the server said "try again later") is returned as
// an ordinary error, so the queue retries it with its own backoff and the message
// arrives once the server comes back.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		// Terminal is our own worker control-flow signal, not a foreign error to
		// annotate; wrapping it would obscure the type the worker matches with
		// errors.As.
		return worker.Terminal(fmt.Errorf("mailjob: decoding payload: %w", err)) //nolint:wrapcheck
	}
	if p.To == "" {
		return worker.Terminal(ErrMissingRecipient) //nolint:wrapcheck
	}
	rendered, err := render(p)
	if err != nil {
		return worker.Terminal(err) //nolint:wrapcheck
	}
	if err := s.sender.Send(ctx, rendered.Message(p.To)); err != nil {
		if isPermanent(err) {
			return worker.Terminal( //nolint:wrapcheck
				fmt.Errorf("mailjob: %s is permanently undeliverable: %w", p.Template, err))
		}
		return fmt.Errorf("mailjob: sending %s: %w", p.Template, err)
	}
	s.log.InfoContext(ctx, "mail delivered",
		slog.String("template", p.Template), slog.Int64("job_id", job.ID))
	return nil
}

// render turns a payload into the message its template describes, decoding the
// template's own data on the way. It returns ErrUnknownTemplate for a name this
// binary cannot render, and a wrapped decoding error when the data does not fit
// the template it is addressed to; both are permanent.
func render(p payload) (mailer.Rendered, error) {
	switch p.Template {
	case mailer.TemplateRegistrationReceived:
		return renderWith(p, mailer.RenderRegistrationReceived)
	case mailer.TemplateAccountApproved:
		return renderWith(p, mailer.RenderAccountApproved)
	case mailer.TemplateNewRegistrationPending:
		return renderWith(p, mailer.RenderNewRegistrationPending)
	case mailer.TemplatePasswordReset:
		return renderWith(p, mailer.RenderPasswordReset)
	default:
		return mailer.Rendered{}, fmt.Errorf("%w: %q", ErrUnknownTemplate, p.Template)
	}
}

// renderWith decodes the payload's data into the type its template expects and
// renders it. The type parameter is what keeps a template and its data struct
// paired by the compiler rather than by convention.
func renderWith[T any](p payload, render func(T) mailer.Rendered) (mailer.Rendered, error) {
	var data T
	if len(p.Data) > 0 {
		if err := json.Unmarshal(p.Data, &data); err != nil {
			return mailer.Rendered{}, fmt.Errorf("mailjob: decoding data of %s: %w", p.Template, err)
		}
	}
	return render(data), nil
}

// isPermanent reports whether err means the message can never be delivered, so
// retrying it would only knock on the server's door forever.
//
// Two things qualify: a recipient the mailer itself refuses (unparseable, or a
// placeholder in the reserved .invalid domain), and an SMTP reply in the 5yz
// range — "no such user", "mailbox disabled", a message the server will not take
// from us. A 4yz reply is explicitly temporary and a transport error (no route,
// refused connection, timeout) says nothing about the address, so both stay
// retryable.
func isPermanent(err error) bool {
	if errors.Is(err, mailer.ErrInvalidAddress) || errors.Is(err, mailer.ErrPlaceholderAddress) {
		return true
	}
	var reply *textproto.Error
	return errors.As(err, &reply) && reply.Code >= firstPermanentSMTPCode
}
