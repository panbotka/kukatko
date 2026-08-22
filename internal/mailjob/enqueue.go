package mailjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
)

// MaxAttempts is how often a `mail_send` job is retried before it is
// dead-lettered. It is higher than the queue's default because the failure it
// exists for is a mail server that is temporarily away: with the queue's
// exponential backoff (30 s doubling to an hourly cap) ten attempts span roughly
// three hours, so an outage of that length still delivers the message rather than
// discarding it. A permanent rejection never gets that far — it is a terminal
// failure on the first attempt.
const MaxAttempts = 10

// Mail is one message to schedule: which template renders it, whom it goes to and
// the data that template needs. Build it with one of the constructors below,
// which pair each template with the data struct it expects.
type Mail struct {
	// Template is one of the mailer.TemplateXxx names.
	Template string
	// To is the recipient address, either bare or as a "Name <addr>" pair.
	To string
	// Data is the template's data struct; it is stored as JSON in the payload, so
	// it must contain nothing that only makes sense inside this process.
	Data any
}

// RegistrationReceived is the mail confirming somebody's registration was
// received and is waiting for an administrator.
func RegistrationReceived(to string, data mailer.RegistrationReceivedData) Mail {
	return Mail{Template: mailer.TemplateRegistrationReceived, To: to, Data: data}
}

// AccountApproved is the mail telling somebody their account is now usable.
func AccountApproved(to string, data mailer.AccountApprovedData) Mail {
	return Mail{Template: mailer.TemplateAccountApproved, To: to, Data: data}
}

// NewRegistrationPending is the mail asking an administrator to approve somebody.
func NewRegistrationPending(to string, data mailer.NewRegistrationPendingData) Mail {
	return Mail{Template: mailer.TemplateNewRegistrationPending, To: to, Data: data}
}

// PasswordReset is the mail carrying the link to choose a new password.
func PasswordReset(to string, data mailer.PasswordResetData) Mail {
	return Mail{Template: mailer.TemplatePasswordReset, To: to, Data: data}
}

// Scheduler inserts a job into the persistent queue through a caller-supplied
// executor. It is satisfied by jobs.Enqueue, which is what production wires; a
// test substitutes its own function and needs no database.
type Scheduler func(
	ctx context.Context, exec jobs.Execer, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions,
) (jobs.Job, error)

// EnqueuerConfig bundles what the Enqueuer needs.
type EnqueuerConfig struct {
	// Enabled mirrors mail.enabled: with mail switched off nothing is enqueued.
	Enabled bool
	// Schedule inserts the job; nil uses jobs.Enqueue.
	Schedule Scheduler
	// Logger records the messages that were not enqueued; nil uses slog.Default().
	Logger *slog.Logger
}

// Enqueuer schedules mail on the persistent queue. It is what callers use instead
// of touching the queue — or the mailer — themselves, so the two refusals (mail
// is off, the recipient is a placeholder) are made in exactly one place.
type Enqueuer struct {
	schedule Scheduler
	enabled  bool
	log      *slog.Logger
}

// NewEnqueuer builds an Enqueuer from cfg, defaulting Schedule to jobs.Enqueue
// and Logger to slog.Default().
func NewEnqueuer(cfg EnqueuerConfig) *Enqueuer {
	schedule := cfg.Schedule
	if schedule == nil {
		schedule = jobs.Enqueue
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Enqueuer{schedule: schedule, enabled: cfg.Enabled, log: log}
}

// Enabled reports whether this instance sends mail at all, for a caller that
// wants to skip preparing a message nobody will receive (minting a password-reset
// token, say).
func (e *Enqueuer) Enabled() bool {
	return e.enabled
}

// Enqueue schedules m for delivery, using exec — a pool or an open transaction —
// to insert the job. Passing the transaction of the mutation that caused the mail
// is the intended use: the message is scheduled if and only if that mutation
// commits, so a rolled-back registration sends nothing.
//
// Two cases return nil without enqueuing anything, both recorded in the log
// because "no mail was sent" is worth being able to explain: mail is disabled in
// the configuration, and the recipient is a placeholder in the reserved .invalid
// domain. A recipient that is not an address at all, and a template this binary
// cannot render, are refused with an error instead — both are caller bugs, and
// enqueuing them would only produce a job that is dead on arrival.
func (e *Enqueuer) Enqueue(ctx context.Context, exec jobs.Execer, m Mail) error {
	if !e.enabled {
		e.log.InfoContext(ctx, "mail not scheduled: mail is disabled",
			slog.String("template", m.Template))
		return nil
	}
	skip, err := e.checkRecipient(ctx, m)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	raw, err := encodePayload(m)
	if err != nil {
		return err
	}

	if _, err := e.schedule(ctx, exec, jobs.TypeMailSend, raw, jobs.EnqueueOptions{
		MaxAttempts: MaxAttempts,
	}); err != nil {
		return fmt.Errorf("mailjob: enqueuing %s: %w", m.Template, err)
	}
	return nil
}

// checkRecipient validates m's recipient. It reports skip=true for a placeholder
// in the reserved .invalid domain — those accounts exist and mailing them is a
// no-op, not a failure — after recording the skip, and returns an error for a
// recipient that is not an address at all.
func (e *Enqueuer) checkRecipient(ctx context.Context, m Mail) (skip bool, err error) {
	err = mailer.ValidateAddress(m.To)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, mailer.ErrPlaceholderAddress):
		e.log.DebugContext(ctx, "mail not scheduled: placeholder recipient",
			slog.String("template", m.Template))
		return true, nil
	default:
		return false, fmt.Errorf("mailjob: refusing to enqueue %s: %w", m.Template, err)
	}
}

// encodePayload turns m into the job's JSON payload, refusing a template this
// binary cannot render before the job is written rather than after it is claimed.
func encodePayload(m Mail) (json.RawMessage, error) {
	if !knownTemplate(m.Template) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTemplate, m.Template)
	}
	data, err := json.Marshal(m.Data)
	if err != nil {
		return nil, fmt.Errorf("mailjob: encoding data of %s: %w", m.Template, err)
	}
	raw, err := json.Marshal(payload{Template: m.Template, To: m.To, Data: data})
	if err != nil {
		return nil, fmt.Errorf("mailjob: encoding payload of %s: %w", m.Template, err)
	}
	return raw, nil
}

// knownTemplate reports whether name is a template this package can render.
func knownTemplate(name string) bool {
	switch name {
	case mailer.TemplateRegistrationReceived, mailer.TemplateAccountApproved,
		mailer.TemplateNewRegistrationPending, mailer.TemplatePasswordReset:
		return true
	default:
		return false
	}
}
