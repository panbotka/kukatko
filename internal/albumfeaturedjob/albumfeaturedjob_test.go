package albumfeaturedjob

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/worker"
)

// TestPayloadRoundTrip checks the payload names the album under the key the
// dedup index reads, and decodes back to it.
func TestPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	raw, err := encodePayload("al-pout")
	if err != nil {
		t.Fatalf("encodePayload: %v", err)
	}
	if string(raw) != `{"album_uid":"al-pout"}` {
		t.Fatalf("payload = %s, want the album_uid key", raw)
	}
	got, err := decodePayload(raw)
	if err != nil || got != "al-pout" {
		t.Fatalf("decodePayload = %q, %v, want al-pout", got, err)
	}
}

// TestDecodePayload_refusals checks a payload that cannot name an album is an
// error.
func TestDecodePayload_refusals(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"not json":    `{`,
		"no album":    `{}`,
		"blank album": `{"album_uid":"  "}`,
		"wrong type":  `{"album_uid":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodePayload(json.RawMessage(raw)); err == nil {
				t.Fatalf("decodePayload(%s) succeeded, want an error", raw)
			}
		})
	}
}

// TestAlbumPath checks the link a notification opens.
func TestAlbumPath(t *testing.T) {
	t.Parallel()

	if got := AlbumPath("al1"); got != "/albums/al1" {
		t.Fatalf("AlbumPath = %q", got)
	}
}

// TestRecipients checks the rules: the actor is skipped, a disabled account is
// skipped, a private album reaches only accounts that may curate, and the
// order is kept.
func TestRecipients(t *testing.T) {
	t.Parallel()

	accounts := []account{
		{UID: "us-admin", Role: auth.RoleAdmin},
		{UID: "us-curator", Role: auth.RoleCurator},
		{UID: "us-editor", Role: auth.RoleEditor},
		{UID: "us-off", Role: auth.RoleEditor, Disabled: true},
		{UID: "us-viewer", Role: auth.RoleViewer},
	}
	tests := []struct {
		name    string
		actor   string
		private bool
		want    []string
	}{
		{
			name: "public album by the curator", actor: "us-curator",
			want: []string{"us-admin", "us-editor", "us-viewer"},
		},
		{
			name: "public album by a system caller", actor: "",
			want: []string{"us-admin", "us-curator", "us-editor", "us-viewer"},
		},
		{
			name: "private album reaches curators and above", actor: "us-curator", private: true,
			want: []string{"us-admin", "us-editor"},
		},
		{
			name: "private album by the admin", actor: "us-admin", private: true,
			want: []string{"us-curator", "us-editor"},
		},
		{
			name: "nobody", actor: "", private: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := accounts
			if tt.name == "nobody" {
				in = nil
			}
			got := recipients(in, tt.actor, tt.private)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("recipients(actor %q, private %v) = %v, want %v", tt.actor, tt.private, got, tt.want)
			}
		})
	}
}

// enqueueCall is one call a fake queue saw.
type enqueueCall struct {
	jobType string
	payload string
}

// fakeEnqueue records calls and answers with err.
func fakeEnqueue(calls *[]enqueueCall, err error) Enqueue {
	return func(_ context.Context, _ jobs.Execer, jobType string, raw json.RawMessage, _ jobs.EnqueueOptions) (jobs.Job, error) {
		*calls = append(*calls, enqueueCall{jobType: jobType, payload: string(raw)})
		return jobs.Job{}, err
	}
}

// TestScheduler_Featured checks the observer queues the job with the album in
// the payload, swallows a duplicate and wraps any other failure.
func TestScheduler_Featured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "queued"},
		{name: "duplicate is fine", err: jobs.ErrDuplicate},
		{name: "other failure", err: errors.New("boom"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls []enqueueCall
			s := NewScheduler(fakeEnqueue(&calls, tt.err))
			err := s.Featured(t.Context(), nil, organize.Featuring{AlbumUID: "al1", ActorUID: "us1"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Featured error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, tt.err) {
				t.Fatalf("Featured error = %v does not wrap %v", err, tt.err)
			}
			want := []enqueueCall{{jobType: jobs.TypeAlbumFeaturedNotify, payload: `{"album_uid":"al1"}`}}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("enqueue calls = %+v, want %+v", calls, want)
			}
		})
	}
}

// TestNewScheduler_default checks a nil enqueue falls back to the queue.
func TestNewScheduler_default(t *testing.T) {
	t.Parallel()

	if NewScheduler(nil).enqueue == nil {
		t.Fatal("NewScheduler(nil) has no enqueue")
	}
}

// panicDB fails the test if the handler ever opens a transaction.
type panicDB struct{ t *testing.T }

// Begin fails the test: nothing should reach the database.
func (d panicDB) Begin(context.Context) (pgx.Tx, error) {
	d.t.Error("Begin called")
	return nil, errors.New("unexpected")
}

// fakeAlbums is an AlbumReader that must not be asked.
type fakeAlbums struct{}

// FeaturedAlbumTx reports nothing featured.
func (fakeAlbums) FeaturedAlbumTx(context.Context, pgx.Tx, string) (organize.FeaturedAlbum, error) {
	return organize.FeaturedAlbum{}, organize.ErrNotFeatured
}

// fakeDeliver is a Deliverer that delivers nothing.
type fakeDeliver struct{}

// Deliver delivers nothing.
func (fakeDeliver) Deliver(
	context.Context, pgx.Tx, string, notifydeliver.Message,
) (notifydeliver.Result, error) {
	return notifydeliver.Result{}, nil
}

// TestHandle_malformedPayloadIsTerminal checks a payload no retry can fix
// fails terminally without touching the database.
func TestHandle_malformedPayloadIsTerminal(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Albums: fakeAlbums{}, Deliver: fakeDeliver{}})
	err := svc.Handle(t.Context(), jobs.Job{Type: jobs.TypeAlbumFeaturedNotify, Payload: json.RawMessage(`{}`)})
	var terminal *worker.TerminalError
	if !errors.As(err, &terminal) || !errors.Is(err, errMissingAlbum) {
		t.Fatalf("Handle = %v, want a terminal errMissingAlbum", err)
	}
}

// TestNew_defaultsAndRequirements checks the language defaults to Czech and a
// missing dependency is refused at construction.
func TestNew_defaultsAndRequirements(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Albums: fakeAlbums{}, Deliver: fakeDeliver{}})
	if svc.lang != notification.LanguageCzech || svc.log == nil {
		t.Fatalf("defaults: lang %q, logger set %v", svc.lang, svc.log != nil)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("New without Deliver did not panic")
		}
	}()
	New(Config{DB: panicDB{t: t}, Albums: fakeAlbums{}})
}

// TestMessage checks the notification every recipient gets: the kind, the
// Czech text with the title, the album link, the cover as the frozen set (none
// without a cover) and the album_featured mail with the absolute link.
func TestMessage(t *testing.T) {
	t.Parallel()

	svc := New(Config{DB: panicDB{t: t}, Albums: fakeAlbums{}, Deliver: fakeDeliver{}})
	album := organize.FeaturedAlbum{Album: organize.Album{UID: "al1", Title: "Pouť 2026"}, CoverUID: new("ph1")}
	msg := svc.message(album)
	if msg.Kind != notification.KindAlbumFeatured || msg.SelfLink || msg.SkipMail {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Text.Title != "Nové doporučené album: Pouť 2026" || msg.Link != "/albums/al1" {
		t.Fatalf("text %+v, link %q", msg.Text, msg.Link)
	}
	if !reflect.DeepEqual(msg.PhotoUIDs, []string{"ph1"}) {
		t.Fatalf("photos = %v, want the cover", msg.PhotoUIDs)
	}
	mail := msg.Mail(notifydeliver.Recipient{Email: "anna@example.test", DisplayName: "Anna"},
		notification.Notification{}, "https://k.example/albums/al1")
	wantData := mailer.AlbumFeaturedData{DisplayName: "Anna", Title: "Pouť 2026", URL: "https://k.example/albums/al1"}
	if mail.Template != mailer.TemplateAlbumFeatured || mail.To != "anna@example.test" ||
		!reflect.DeepEqual(mail.Data, wantData) {
		t.Fatalf("mail = %+v, want the album_featured template with %+v", mail, wantData)
	}

	english := New(Config{DB: panicDB{t: t}, Albums: fakeAlbums{}, Deliver: fakeDeliver{},
		Language: notification.LanguageEnglish})
	bare := english.message(organize.FeaturedAlbum{Album: organize.Album{UID: "al2", Title: "Fair"}})
	if bare.PhotoUIDs != nil || bare.Text.Title != "New featured album: Fair" {
		t.Fatalf("no cover, English: %+v", bare)
	}
}
