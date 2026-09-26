package tagnotifyjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
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

// fakeNotes is a NotificationRecorder that must not be reached.
type fakeNotes struct{}

// WantsTx wants everything.
func (fakeNotes) WantsTx(context.Context, pgx.Tx, string, notification.Kind) (bool, error) {
	return true, nil
}

// CreateTx records nothing.
func (fakeNotes) CreateTx(context.Context, pgx.Tx, notification.New) (notification.Notification, error) {
	return notification.Notification{}, nil
}

// fakePush is a PushScheduler that queues nothing.
type fakePush struct{}

// Enqueue queues nothing.
func (fakePush) Enqueue(context.Context, pushjob.Execer, string, push.Notification) (int, error) {
	return 0, nil
}

// TestHandle_malformedPayloadIsTerminal checks a payload no retry can fix
// fails terminally without touching the database.
func TestHandle_malformedPayloadIsTerminal(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Notifications: fakeNotes{}, Push: fakePush{}})
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

	svc := New(Config{DB: panicDB{t: t}, Notifications: fakeNotes{}, Push: fakePush{}})
	if svc.lang != notification.LanguageCzech || svc.log == nil {
		t.Fatalf("defaults: lang %q, logger set %v", svc.lang, svc.log != nil)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("New without Push did not panic")
		}
	}()
	New(Config{DB: panicDB{t: t}, Notifications: fakeNotes{}})
}

// TestNewRecorder_defaults checks a non-positive window falls back to the
// default, a set one is kept, and Preferences is required.
func TestNewRecorder_defaults(t *testing.T) {
	t.Parallel()

	if r := NewRecorder(RecorderConfig{Preferences: fakeNotes{}}); r.window != DefaultWindow || r.now == nil {
		t.Fatalf("zero config: window %s, clock set %v", r.window, r.now != nil)
	}
	if r := NewRecorder(RecorderConfig{Preferences: fakeNotes{}, Window: time.Minute}); r.window != time.Minute {
		t.Fatalf("window = %s, want 1m", r.window)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewRecorder without Preferences did not panic")
		}
	}()
	NewRecorder(RecorderConfig{})
}

// TestRecorder_disabledRecordsNothing checks that with push off a tagging is
// not even looked at: the transaction is never touched (it is nil here).
func TestRecorder_disabledRecordsNothing(t *testing.T) {
	t.Parallel()

	r := NewRecorder(RecorderConfig{Preferences: fakeNotes{}})
	if err := r.Tagged(t.Context(), nil, people.Tagging{PhotoUID: "p", SubjectUID: "s"}); err != nil {
		t.Fatalf("Tagged with push off = %v, want nil", err)
	}
}
