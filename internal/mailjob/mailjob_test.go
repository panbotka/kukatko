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
	"github.com/panbotka/kukatko/internal/notifystanding"
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
	svc := NewService(ServiceConfig{Sender: fake, Standing: inStanding()})
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
				Username: "jan", Email: "jan@example.com", ApprovalURL: "https://kukatko.example/users",
			}),
			wantBody: "Schválit nebo zamítnout: https://kukatko.example/users",
		},
		{
			name: "password reset",
			mail: PasswordReset("jan@example.com", mailer.PasswordResetData{
				ResetURL: "https://kukatko.example/reset?t=abc", ValidFor: time.Hour,
			}),
			wantBody: "Odkaz platí jednu hodinu.",
		},
		{
			name: "tasks waiting digest",
			mail: TasksWaitingDigest("jan@example.com", mailer.TasksWaitingDigestData{
				Total: 1, Tasks: []mailer.DigestTask{{Title: "Rok svatby", State: "question",
					URL: "https://kukatko.example/tasks/tk-1"}},
				QueueURL: "https://kukatko.example/tasks?waiting=1",
			}),
			wantBody: "- Rok svatby (čeká na odpověď)\n  https://kukatko.example/tasks/tk-1",
		},
		{
			name: "username changed",
			mail: UsernameChanged("jan@example.com", mailer.UsernameChangedData{
				Username: "jan.novak", SignInURL: "https://kukatko.example/login",
			}),
			wantBody: "jménem „jan.novak“",
		},
		{
			name:     "tagged",
			mail:     Tagged("jan@example.com", mailer.TaggedData{Count: 3, URL: "https://kukatko.example/n/nt1"}),
			wantBody: "označil na 3 fotkách.\n\nPodívat se na ně můžeš tady:\n\nhttps://kukatko.example/n/nt1",
		},
		{
			name: "generic notification",
			mail: Notification("jan@example.com", mailer.NotificationData{
				Title: "Nové album", Body: "Přibylo album Pouť.", URL: "https://kukatko.example/albums/a1",
			}),
			wantBody: "Přibylo album Pouť.\n\nhttps://kukatko.example/albums/a1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := mailer.NewFake()
			svc := NewService(ServiceConfig{Sender: fake, Standing: inStanding()})
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
			svc := NewService(ServiceConfig{Sender: fake, Standing: inStanding()})

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
			svc := NewService(ServiceConfig{Sender: fake, Standing: inStanding()})

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

// TestNewService_requiresCollaborators verifies the wiring bug surfaces at
// startup rather than as a dead letter (or a mis-delivery) per message.
func TestNewService_requiresCollaborators(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]ServiceConfig{
		"no sender":   {Standing: inStanding()},
		"no standing": {Sender: mailer.NewFake()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("NewService did not panic")
				}
			}()
			_ = NewService(cfg)
		})
	}
}

// stubStanding is a Standing answering every question the same way and
// recording whom it was asked about.
type stubStanding struct {
	ok    bool
	err   error
	asked []notifystanding.Addressee
}

// InStanding returns the configured answer.
func (s *stubStanding) InStanding(_ context.Context, a notifystanding.Addressee) (bool, error) {
	s.asked = append(s.asked, a)
	return s.ok, s.err
}

// inStanding is a Standing that lets every account through.
func inStanding() *stubStanding { return &stubStanding{ok: true} }

// TestHandle_addressedMail verifies a notification mail is checked against its
// addressee when it is sent — delivered while the account is in standing,
// dropped (completed, counted) when it is not, retried when the question fails
// — and that an account's own mail, which names no addressee, is never asked
// about.
func TestHandle_addressedMail(t *testing.T) {
	t.Parallel()

	to := notifystanding.Addressee{UserUID: "us1", ProjectUID: "pj1", Kind: "album_featured"}
	notice := AlbumFeatured("anna@example.org", mailer.AlbumFeaturedData{Title: "Pouť", URL: "https://k.example/a"})
	own := AccountApproved("anna@example.org", mailer.AccountApprovedData{SignInURL: "https://k.example"})
	tests := []struct {
		name      string
		mail      Mail
		standing  *stubStanding
		wantSent  bool
		wantErr   bool
		wantAsked bool
		wantDrop  bool
	}{
		{name: "in standing", mail: notice.Addressed(to), standing: inStanding(), wantSent: true, wantAsked: true},
		{name: "out of standing", mail: notice.Addressed(to), standing: &stubStanding{}, wantAsked: true, wantDrop: true},
		{
			name: "question fails", mail: notice.Addressed(to), standing: &stubStanding{err: errors.New("db down")},
			wantErr: true, wantAsked: true,
		},
		{name: "own mail", mail: own, standing: &stubStanding{}, wantSent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := mailer.NewFake()
			svc := NewService(ServiceConfig{Sender: fake, Standing: tt.standing})
			before := mailDrops()
			err := svc.Handle(context.Background(), jobFor(t, tt.mail))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Handle = %v, want error %v", err, tt.wantErr)
			}
			if _, sent := fake.Last(); sent != tt.wantSent {
				t.Errorf("sent = %v, want %v", sent, tt.wantSent)
			}
			if asked := len(tt.standing.asked) == 1 && tt.standing.asked[0] == to; asked != tt.wantAsked {
				t.Errorf("asked about %+v, want asked = %v", tt.standing.asked, tt.wantAsked)
			}
			if tt.wantDrop && mailDrops() <= before {
				t.Errorf("mail drops did not grow from %d", before)
			}
		})
	}
}

// mailDrops reads the process-wide count of mails dropped for standing.
func mailDrops() uint64 {
	for _, d := range notifystanding.Drops() {
		if d.Stage == notifystanding.StageMail && d.Reason == notifystanding.ReasonStanding {
			return d.Count
		}
	}
	return 0
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
