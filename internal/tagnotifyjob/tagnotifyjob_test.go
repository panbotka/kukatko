package tagnotifyjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/worker"
)

// TestPayloadRoundTrip checks the payload names the account under the key the
// dedup index reads, and decodes back to it.
func TestPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	raw, err := encodePayload("us-anna")
	if err != nil {
		t.Fatalf("encodePayload: %v", err)
	}
	if string(raw) != `{"user_uid":"us-anna"}` {
		t.Fatalf("payload = %s, want the user_uid key", raw)
	}
	got, err := decodePayload(raw)
	if err != nil || got != "us-anna" {
		t.Fatalf("decodePayload = %q, %v, want us-anna", got, err)
	}
}

// TestDecodePayload_refusals checks a payload that cannot name an account is an
// error.
func TestDecodePayload_refusals(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"not json":      `{`,
		"no account":    `{}`,
		"blank account": `{"user_uid":"  "}`,
		"wrong type":    `{"user_uid":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodePayload(json.RawMessage(raw)); err == nil {
				t.Fatalf("decodePayload(%s) succeeded, want an error", raw)
			}
		})
	}
}

// panicDB fails the test if the handler ever opens a transaction.
type panicDB struct{ t *testing.T }

// Begin fails the test: nothing should reach the database.
func (d panicDB) Begin(context.Context) (pgx.Tx, error) {
	d.t.Error("Begin called")
	return nil, errors.New("unexpected")
}

// fakeDeliver is a Deliverer and an Audience that must not deliver anything.
type fakeDeliver struct{}

// Deliver delivers nothing.
func (fakeDeliver) Deliver(
	context.Context, pgx.Tx, string, notifydeliver.Message,
) (notifydeliver.Result, error) {
	return notifydeliver.Result{}, nil
}

// ChannelsTx wants every channel.
func (fakeDeliver) ChannelsTx(context.Context, pgx.Tx, string, notification.Kind) (notifydeliver.Channels, error) {
	return notifydeliver.Channels{InApp: true, Push: true, Email: true}, nil
}

// TestHandle_malformedPayloadIsTerminal checks a payload no retry can fix
// fails terminally without touching the database.
func TestHandle_malformedPayloadIsTerminal(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Deliver: fakeDeliver{}})
	err := svc.Handle(t.Context(), jobs.Job{Type: jobs.TypeTagNotify, Payload: json.RawMessage(`{}`)})
	var terminal *worker.TerminalError
	if !errors.As(err, &terminal) || !errors.Is(err, errMissingUser) {
		t.Fatalf("Handle = %v, want a terminal errMissingUser", err)
	}
}

// TestNew_defaultsAndRequirements checks the language defaults to Czech and a
// missing dependency is refused at construction.
func TestNew_defaultsAndRequirements(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Deliver: fakeDeliver{}})
	if svc.lang != notification.LanguageCzech || svc.log == nil {
		t.Fatalf("defaults: lang %q, logger set %v", svc.lang, svc.log != nil)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("New without Deliver did not panic")
		}
	}()
	New(Config{DB: panicDB{t: t}})
}

// TestNewRecorder_defaults checks a non-positive window falls back to the
// default, a set one is kept, and Audience is required.
func TestNewRecorder_defaults(t *testing.T) {
	t.Parallel()

	if r := NewRecorder(RecorderConfig{Audience: fakeDeliver{}}); r.window != DefaultWindow || r.now == nil {
		t.Fatalf("zero config: window %s, clock set %v", r.window, r.now != nil)
	}
	if r := NewRecorder(RecorderConfig{Audience: fakeDeliver{}, Window: time.Minute}); r.window != time.Minute {
		t.Fatalf("window = %s, want 1m", r.window)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewRecorder without Audience did not panic")
		}
	}()
	NewRecorder(RecorderConfig{})
}

// TestTaggedMail checks the tagged kind's e-mail carries the count, the
// recipient's name and the link, addressed to the recipient.
func TestTaggedMail(t *testing.T) {
	t.Parallel()

	mail := taggedMail(3)(notifydeliver.Recipient{Email: "anna@example.org", DisplayName: "Anna"},
		notification.Notification{}, "https://k.example/n/nt1")
	if mail.Template != mailer.TemplateTagged || mail.To != "anna@example.org" {
		t.Fatalf("mail = %+v, want the tagged template to anna", mail)
	}
	want := mailer.TaggedData{DisplayName: "Anna", Count: 3, URL: "https://k.example/n/nt1"}
	if mail.Data != want {
		t.Fatalf("data = %+v, want %+v", mail.Data, want)
	}
}
