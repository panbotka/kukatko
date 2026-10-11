package tagnotifyjob

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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

// Absolute puts link under a fixed address.
func (fakeDeliver) Absolute(link string) string {
	if link == "" {
		return ""
	}
	return "https://k.example" + link
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

// TestTaggedMail checks the window's one e-mail: a window of plain photos
// carries the count and the link, a window with albums lists every group — the
// ones delivered before and the last, whose link the builder receives — with
// title, count and link, under the window's total.
func TestTaggedMail(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Deliver: fakeDeliver{}})
	to := notifydeliver.Recipient{Email: "anna@example.org", DisplayName: "Anna"}
	plain := svc.taggedMail(nil, group{Photos: []string{"p1", "p2", "p3"}}, 3)(to, notification.Notification{}, "https://k.example/n/nt1")
	if plain.Template != mailer.TemplateTagged || plain.To != "anna@example.org" {
		t.Fatalf("mail = %+v, want the tagged template to anna", plain)
	}
	want := mailer.TaggedData{DisplayName: "Anna", Count: 3, URL: "https://k.example/n/nt1"}
	if !reflect.DeepEqual(plain.Data, want) {
		t.Fatalf("plain data = %+v, want %+v", plain.Data, want)
	}

	fair := albumRef{UID: "al-fair", Title: "Pouť 2026"}
	before := []mailGroup{{group: group{Album: &fair, Photos: []string{"p1", "p2"}}, url: "https://k.example/n/a"}}
	grouped := svc.taggedMail(before, group{Photos: []string{"p3"}}, 3)(to, notification.Notification{}, "https://k.example/n/b")
	wantGrouped := mailer.TaggedData{DisplayName: "Anna", Count: 3, Groups: []mailer.TaggedGroup{
		{Album: "Pouť 2026", Count: 2, URL: "https://k.example/n/a"},
		{Count: 1, URL: "https://k.example/n/b"},
	}}
	if !reflect.DeepEqual(grouped.Data, wantGrouped) {
		t.Fatalf("grouped data = %+v, want %+v", grouped.Data, wantGrouped)
	}

	// A single album group is still the breakdown: the album deserves its name.
	only := svc.taggedMail(nil, group{Album: &fair, Photos: []string{"p1"}}, 1)(to, notification.Notification{}, "https://k.example/n/a")
	if data, ok := only.Data.(mailer.TaggedData); !ok || len(data.Groups) != 1 || data.URL != "" || data.Groups[0].Album != "Pouť 2026" {
		t.Fatalf("single album data = %+v, want one group named Pouť 2026", only.Data)
	}
}

// TestServiceText checks which wording a group gets: the album's for an album
// group, the plain one for the only group of a window without albums, and
// "other photos" beside album groups.
func TestServiceText(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Deliver: fakeDeliver{}})
	fair := albumRef{UID: "al-fair", Title: "Pouť 2026"}
	if got := svc.text(group{Album: &fair, Photos: []string{"p1", "p2"}}, 2); got.Title != "Označili vás na fotkách v albu Pouť 2026" {
		t.Errorf("album group title = %q", got.Title)
	}
	if got := svc.text(group{Photos: []string{"p1"}}, 1); got.Title != "Označili vás na fotce" {
		t.Errorf("only group title = %q", got.Title)
	}
	if got := svc.text(group{Photos: []string{"p1"}}, 2); got.Title != "Označili vás na další fotce" {
		t.Errorf("other group title = %q", got.Title)
	}
}
