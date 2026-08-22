package mailjob

import (
	"context"
	"encoding/json"
	"errors"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/worker"
)

// jobFor builds a `mail_send` job carrying the payload m encodes to, the way
// Enqueue would have written it.
func jobFor(t *testing.T, m Mail) jobs.Job {
	t.Helper()
	raw, err := encodePayload(m)
	if err != nil {
		t.Fatalf("encodePayload(%v): %v", m, err)
	}
	return jobs.Job{ID: 1, Type: jobs.TypeMailSend, Payload: raw}
}

// TestHandle_rendersAndSends verifies the happy path against the fake mailer: the
// template named in the payload is rendered with its data and the message goes to
// the payload's recipient.
func TestHandle_rendersAndSends(t *testing.T) {
	t.Parallel()

	fake := mailer.NewFake()
	svc := NewService(ServiceConfig{Sender: fake})
	mail := AccountApproved("jan@example.com", mailer.AccountApprovedData{
		DisplayName: "Jan Novák",
		SignInURL:   "https://kukatko.example/prihlaseni",
	})

	if err := svc.Handle(context.Background(), jobFor(t, mail)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	sent, ok := fake.Last()
	if !ok {
		t.Fatal("nothing was sent")
	}
	if sent.To != "jan@example.com" {
		t.Errorf("To = %q, want jan@example.com", sent.To)
	}
	want := mailer.RenderAccountApproved(mailer.AccountApprovedData{
		DisplayName: "Jan Novák",
		SignInURL:   "https://kukatko.example/prihlaseni",
	})
	if sent.Subject != want.Subject || sent.Body != want.Body {
		t.Errorf("message = %q/%q, want %q/%q", sent.Subject, sent.Body, want.Subject, want.Body)
	}
}

// TestHandle_rendersEveryTemplate verifies each template a job may name survives
// the round trip through JSON: the data comes back out on the other side, so a
// message rendered after a restart says what it was scheduled to say.
func TestHandle_rendersEveryTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mail     Mail
		wantBody string
	}{
		{
			name: "registration received",
			mail: RegistrationReceived("jan@example.com", mailer.RegistrationReceivedData{
				DisplayName: "Jan", Username: "jan",
			}),
			wantBody: "Účet „jan“",
		},
		{
			name: "account approved",
			mail: AccountApproved("jan@example.com", mailer.AccountApprovedData{
				SignInURL: "https://kukatko.example/",
			}),
			wantBody: "https://kukatko.example/",
		},
		{
			name: "new registration pending",
			mail: NewRegistrationPending("admin@example.com", mailer.NewRegistrationPendingData{
				Username: "jan", Email: "jan@example.com",
			}),
			wantBody: "Uživatelské jméno: jan",
		},
		{
			name: "password reset",
			mail: PasswordReset("jan@example.com", mailer.PasswordResetData{
				ResetURL: "https://kukatko.example/reset?t=abc", ValidFor: time.Hour,
			}),
			wantBody: "Odkaz platí jednu hodinu.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := mailer.NewFake()
			svc := NewService(ServiceConfig{Sender: fake})
			if err := svc.Handle(context.Background(), jobFor(t, tt.mail)); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			sent, ok := fake.Last()
			if !ok {
				t.Fatal("nothing was sent")
			}
			if !strings.Contains(sent.Body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", sent.Body, tt.wantBody)
			}
		})
	}
}

// TestHandle_retryableFailure verifies a delivery that merely did not go through
// this time comes back as an ordinary error — so the queue retries it with its
// own backoff — and is not mistaken for a permanent one.
func TestHandle_retryableFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "the server was unreachable",
			err:  errors.New("mailer: sending the message failed: connecting to smtp:587: dial tcp: refused"),
		},
		{
			name: "the server asked us to come back later",
			err:  &textproto.Error{Code: 451, Msg: "4.7.1 Greylisted, try again later"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := mailer.NewFake()
			fake.FailWith(tt.err)
			svc := NewService(ServiceConfig{Sender: fake})

			err := svc.Handle(context.Background(), jobFor(t, AccountApproved(
				"jan@example.com", mailer.AccountApprovedData{})))
			if err == nil {
				t.Fatal("Handle returned nil, want a retryable error")
			}
			var terminal *worker.TerminalError
			if errors.As(err, &terminal) {
				t.Errorf("Handle returned a terminal failure for %v, want a retryable one", tt.err)
			}
			if !errors.Is(err, tt.err) {
				t.Errorf("Handle error = %v, want it to wrap %v", err, tt.err)
			}
		})
	}
}

// TestHandle_terminalFailure verifies the failures no retry can fix are marked
// terminal, so the queue parks the job instead of knocking on the server's door
// until the attempts run out.
func TestHandle_terminalFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		job  jobs.Job
		want error
	}{
		{
			name: "the server rejected the recipient for good",
			job: jobs.Job{ID: 2, Type: jobs.TypeMailSend, Payload: mustPayload(t, payload{
				Template: mailer.TemplateAccountApproved, To: "gone@example.com",
			})},
			want: &textproto.Error{Code: 550, Msg: "5.1.1 No such user here"},
		},
		{
			name: "the payload is not JSON",
			job:  jobs.Job{ID: 3, Type: jobs.TypeMailSend, Payload: json.RawMessage("not json")},
		},
		{
			name: "the payload names no recipient",
			job: jobs.Job{ID: 4, Type: jobs.TypeMailSend, Payload: mustPayload(t, payload{
				Template: mailer.TemplateAccountApproved,
			})},
			want: ErrMissingRecipient,
		},
		{
			name: "the payload names a template this binary does not have",
			job: jobs.Job{ID: 5, Type: jobs.TypeMailSend, Payload: mustPayload(t, payload{
				Template: "invoice_overdue", To: "jan@example.com",
			})},
			want: ErrUnknownTemplate,
		},
		{
			name: "the data does not fit its template",
			job: jobs.Job{ID: 6, Type: jobs.TypeMailSend, Payload: mustPayload(t, payload{
				Template: mailer.TemplateAccountApproved, To: "jan@example.com",
				Data: json.RawMessage(`["not", "an", "object"]`),
			})},
		},
		{
			name: "the recipient is not an address at all",
			job: jobs.Job{ID: 7, Type: jobs.TypeMailSend, Payload: mustPayload(t, payload{
				Template: mailer.TemplateAccountApproved, To: "not an address",
			})},
			want: mailer.ErrInvalidAddress,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := mailer.NewFake()
			if tt.want != nil {
				fake.FailWith(tt.want)
			}
			svc := NewService(ServiceConfig{Sender: fake})

			err := svc.Handle(context.Background(), tt.job)
			var terminal *worker.TerminalError
			if !errors.As(err, &terminal) {
				t.Fatalf("Handle error = %v, want a worker.TerminalError", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("Handle error = %v, want it to wrap %v", err, tt.want)
			}
		})
	}
}

// TestNewService_requiresASender verifies the wiring bug surfaces at startup
// rather than as a dead letter per message.
func TestNewService_requiresASender(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewService(nil sender) did not panic")
		}
	}()
	_ = NewService(ServiceConfig{})
}

// mustPayload encodes p as a job payload, failing the test if it cannot.
func mustPayload(t *testing.T, p payload) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}
	return raw
}
