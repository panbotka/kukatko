package mailjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
)

// fakeExec stands in for the pool or transaction an enqueue runs on. It is never
// used — the fake scheduler below does not touch the database — but the Enqueuer
// must hand exactly the executor it was given to the scheduler, which is what
// makes "enqueue inside the caller's transaction" work.
type fakeExec struct{ name string }

// QueryRow is never called in these tests; it exists to satisfy jobs.Execer.
func (fakeExec) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("fakeExec.QueryRow must not be called")
}

// scheduled is one recorded call to the fake scheduler.
type scheduled struct {
	exec    jobs.Execer
	jobType string
	payload payload
	opts    jobs.EnqueueOptions
}

// recorder is a fake Scheduler that records what it was asked to enqueue and can
// fail on demand.
type recorder struct {
	calls []scheduled
	err   error
}

// schedule is the Scheduler the Enqueuer under test is built with.
func (r *recorder) schedule(
	_ context.Context, exec jobs.Execer, jobType string, raw json.RawMessage, opts jobs.EnqueueOptions,
) (jobs.Job, error) {
	if r.err != nil {
		return jobs.Job{}, r.err
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return jobs.Job{}, err
	}
	r.calls = append(r.calls, scheduled{exec: exec, jobType: jobType, payload: p, opts: opts})
	return jobs.Job{ID: int64(len(r.calls)), Type: jobType}, nil
}

// newEnqueuer returns an Enqueuer over a fresh recorder.
func newEnqueuer(t *testing.T, enabled bool) (*Enqueuer, *recorder) {
	t.Helper()
	rec := &recorder{}
	return NewEnqueuer(EnqueuerConfig{Enabled: enabled, Schedule: rec.schedule}), rec
}

// TestEnqueue_schedulesTheJob verifies the payload written to the queue names the
// template, carries its data and the recipient, and that it rides on the executor
// the caller passed — the transaction of the mutation that caused the mail.
func TestEnqueue_schedulesTheJob(t *testing.T) {
	t.Parallel()

	enq, rec := newEnqueuer(t, true)
	tx := fakeExec{name: "the caller's transaction"}

	err := enq.Enqueue(context.Background(), tx, RegistrationReceived(
		"jan@example.com", mailer.RegistrationReceivedData{DisplayName: "Jan", Username: "jan"}))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("scheduled %d jobs, want 1", len(rec.calls))
	}
	call := rec.calls[0]
	if call.jobType != jobs.TypeMailSend {
		t.Errorf("job type = %q, want %q", call.jobType, jobs.TypeMailSend)
	}
	if call.exec != jobs.Execer(tx) {
		t.Errorf("exec = %v, want the caller's executor", call.exec)
	}
	if call.opts.MaxAttempts != MaxAttempts {
		t.Errorf("MaxAttempts = %d, want %d", call.opts.MaxAttempts, MaxAttempts)
	}
	if call.payload.Template != mailer.TemplateRegistrationReceived {
		t.Errorf("template = %q, want %q", call.payload.Template, mailer.TemplateRegistrationReceived)
	}
	if call.payload.To != "jan@example.com" {
		t.Errorf("to = %q, want jan@example.com", call.payload.To)
	}
	// The template's data struct carries no json tags, so its fields land under
	// their Go names; reading them back as a plain map keeps the assertion on what
	// the payload actually contains.
	var data map[string]any
	if err := json.Unmarshal(call.payload.Data, &data); err != nil {
		t.Fatalf("decoding payload data: %v", err)
	}
	if data["Username"] != "jan" || data["DisplayName"] != "Jan" {
		t.Errorf("data = %+v, want the data it was enqueued with", data)
	}
}

// TestEnqueue_disabledMailSchedulesNothing verifies an instance without SMTP does
// not grow a queue of jobs nothing can deliver: the call succeeds and enqueues
// nothing, so the mutation that caused it is not failed either.
func TestEnqueue_disabledMailSchedulesNothing(t *testing.T) {
	t.Parallel()

	enq, rec := newEnqueuer(t, false)
	if enq.Enabled() {
		t.Error("Enabled() = true for a disabled enqueuer")
	}

	err := enq.Enqueue(context.Background(), fakeExec{}, AccountApproved(
		"jan@example.com", mailer.AccountApprovedData{SignInURL: "https://kukatko.example/"}))
	if err != nil {
		t.Fatalf("Enqueue with mail disabled = %v, want nil", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("scheduled %d jobs with mail disabled, want 0", len(rec.calls))
	}
}

// TestEnqueue_placeholderRecipientIsSkipped verifies an address in the reserved
// .invalid domain — what an account imported without a real e-mail carries — is
// never enqueued, and that skipping one is not an error.
func TestEnqueue_placeholderRecipientIsSkipped(t *testing.T) {
	t.Parallel()

	for _, to := range []string{"jan@invalid", "jan@kukatko.invalid", "Jan <jan@x.INVALID>"} {
		t.Run(to, func(t *testing.T) {
			t.Parallel()
			enq, rec := newEnqueuer(t, true)
			err := enq.Enqueue(context.Background(), fakeExec{}, PasswordReset(
				to, mailer.PasswordResetData{ResetURL: "https://kukatko.example/reset"}))
			if err != nil {
				t.Fatalf("Enqueue(%q) = %v, want nil", to, err)
			}
			if len(rec.calls) != 0 {
				t.Errorf("scheduled %d jobs for %q, want 0", len(rec.calls), to)
			}
		})
	}
}

// TestEnqueue_refusesWhatCanNeverBeDelivered verifies the two caller bugs are
// refused before a job exists, rather than dead-lettering later: a recipient that
// is not an address, and a template this binary cannot render.
func TestEnqueue_refusesWhatCanNeverBeDelivered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mail    Mail
		wantErr error
	}{
		{
			name:    "not an address",
			mail:    AccountApproved("not an address", mailer.AccountApprovedData{}),
			wantErr: mailer.ErrInvalidAddress,
		},
		{
			name:    "empty address",
			mail:    AccountApproved("", mailer.AccountApprovedData{}),
			wantErr: mailer.ErrInvalidAddress,
		},
		{
			name:    "unknown template",
			mail:    Mail{Template: "invoice_overdue", To: "jan@example.com"},
			wantErr: ErrUnknownTemplate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq, rec := newEnqueuer(t, true)
			err := enq.Enqueue(context.Background(), fakeExec{}, tt.mail)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Enqueue error = %v, want %v", err, tt.wantErr)
			}
			if len(rec.calls) != 0 {
				t.Errorf("scheduled %d jobs, want 0", len(rec.calls))
			}
		})
	}
}

// TestEnqueue_reportsAQueueFailure verifies a failing insert is reported rather
// than swallowed: the caller is inside a transaction and must be able to roll it
// back.
func TestEnqueue_reportsAQueueFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	rec := &recorder{err: boom}
	enq := NewEnqueuer(EnqueuerConfig{Enabled: true, Schedule: rec.schedule})

	err := enq.Enqueue(context.Background(), fakeExec{}, AccountApproved(
		"jan@example.com", mailer.AccountApprovedData{}))
	if !errors.Is(err, boom) {
		t.Fatalf("Enqueue error = %v, want it to wrap %v", err, boom)
	}
}

// TestNewEnqueuer_defaultsToTheQueue verifies the zero configuration wires the
// real scheduler, so a caller that omits it does not end up with a silent no-op.
func TestNewEnqueuer_defaultsToTheQueue(t *testing.T) {
	t.Parallel()

	if enq := NewEnqueuer(EnqueuerConfig{Enabled: true}); enq.schedule == nil || enq.log == nil {
		t.Errorf("NewEnqueuer left a nil collaborator: schedule=%v log=%v", enq.schedule, enq.log)
	}
}

// TestMailConstructors_pairATemplateWithItsData verifies every constructor names
// a template this package can render — the pairing the payload depends on.
func TestMailConstructors_pairATemplateWithItsData(t *testing.T) {
	t.Parallel()

	mails := []Mail{
		RegistrationReceived("jan@example.com", mailer.RegistrationReceivedData{}),
		AccountApproved("jan@example.com", mailer.AccountApprovedData{}),
		NewRegistrationPending("admin@example.com", mailer.NewRegistrationPendingData{}),
		PasswordReset("jan@example.com", mailer.PasswordResetData{}),
	}
	for _, m := range mails {
		if !knownTemplate(m.Template) {
			t.Errorf("template %q is not renderable", m.Template)
		}
		if _, err := encodePayload(m); err != nil {
			t.Errorf("encodePayload(%q): %v", m.Template, err)
		}
	}
}
