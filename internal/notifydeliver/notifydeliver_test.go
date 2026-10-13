package notifydeliver

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// fakeProjects is a Projects that knows every uid as the project "rodina", or
// fails with err.
type fakeProjects struct{ err error }

// Get returns the project uid as "rodina", or the configured failure.
func (f fakeProjects) Get(_ context.Context, uid string) (*project.Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &project.Project{UID: uid, Slug: "rodina", Name: "Rodina"}, nil
}

// fakeNotes is a Recorder over an in-memory choice of channels.
type fakeNotes struct {
	wanted    []notification.Channel
	wantedErr error
	createErr error
	created   []notification.New
}

// WantedTx returns the configured channels.
func (f *fakeNotes) WantedTx(context.Context, pgx.Tx, string, notification.Kind) ([]notification.Channel, error) {
	return f.wanted, f.wantedErr
}

// CreateTx records n and returns it as stored, with uid nt1.
func (f *fakeNotes) CreateTx(_ context.Context, _ pgx.Tx, n notification.New) (notification.Notification, error) {
	if f.createErr != nil {
		return notification.Notification{}, f.createErr
	}
	f.created = append(f.created, n)
	link := n.Link
	if n.SelfLink {
		link = notification.Path(n.ProjectSlug, "nt1")
	}
	return notification.Notification{
		UID: "nt1", UserUID: n.UserUID, Kind: n.Kind, Title: n.Title, Body: n.Body, Link: link, InApp: n.InApp,
	}, nil
}

// fakePush counts the pushes it is asked to schedule.
type fakePush struct {
	on   bool
	err  error
	sent []push.Notification
}

// Enabled reports the configured switch.
func (f *fakePush) Enabled() bool { return f.on }

// Enqueue records n as delivered to two devices.
func (f *fakePush) Enqueue(_ context.Context, _ pushjob.Execer, _ string, n push.Notification) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.sent = append(f.sent, n)
	return 2, nil
}

// fakeMail records the mail it is asked to schedule.
type fakeMail struct {
	on   bool
	sent []mailjob.Mail
}

// Enabled reports the configured switch.
func (f *fakeMail) Enabled() bool { return f.on }

// Enqueue records m.
func (f *fakeMail) Enqueue(_ context.Context, _ jobs.Execer, m mailjob.Mail) error {
	f.sent = append(f.sent, m)
	return nil
}

// fakeTx is a transaction that answers only the recipient lookup; every other
// method panics through the nil embedded interface.
type fakeTx struct {
	pgx.Tx
	email, name string
}

// QueryRow answers the recipient lookup.
func (f fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{email: f.email, name: f.name}
}

// fakeRow scans the recipient's address and name.
type fakeRow struct{ email, name string }

// Scan fills the two string destinations.
func (r fakeRow) Scan(dest ...any) error {
	*dest[0].(*string) = r.email
	*dest[1].(*string) = r.name
	return nil
}

// message is the notification every fan-out case delivers.
func message() Message {
	return Message{
		ProjectUID: "pj1", Kind: notification.KindTagged, Text: notification.Text{Title: "Title", Body: "Body"},
		SelfLink: true, PhotoUIDs: []string{"p1", "p2"},
	}
}

// TestDeliver_fanOut walks the channel combinations: in-app only, push only,
// mail only, all three and none — what is recorded, whether it is listed, what
// is pushed and what is mailed.
func TestDeliver_fanOut(t *testing.T) {
	t.Parallel()

	in, ps, em := notification.ChannelInApp, notification.ChannelPush, notification.ChannelEmail
	tests := []struct {
		name                   string
		wanted                 []notification.Channel
		wantRecord, wantListed bool
		wantPushes, wantMails  int
		wantChannels           Channels
		wantDevices            int
	}{
		{name: "none", wanted: nil},
		{
			name: "in-app only", wanted: []notification.Channel{in},
			wantRecord: true, wantListed: true, wantChannels: Channels{InApp: true},
		},
		{
			name: "push only", wanted: []notification.Channel{ps},
			wantRecord: true, wantPushes: 1, wantDevices: 2, wantChannels: Channels{Push: true},
		},
		{
			name: "mail only", wanted: []notification.Channel{em},
			wantRecord: true, wantMails: 1, wantChannels: Channels{Email: true},
		},
		{
			name: "all", wanted: []notification.Channel{in, ps, em},
			wantRecord: true, wantListed: true, wantPushes: 1, wantMails: 1, wantDevices: 2,
			wantChannels: Channels{InApp: true, Push: true, Email: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			notes := &fakeNotes{wanted: tt.wanted}
			pusher := &fakePush{on: true}
			mail := &fakeMail{on: true}
			d := New(Config{Projects: fakeProjects{}, Notifications: notes, Push: pusher, Mail: mail, BaseURL: "https://k.example/"})

			got, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org", name: "Anna"}, "us1", message())
			if err != nil {
				t.Fatalf("Deliver: %v", err)
			}
			if (got.Notification != nil) != tt.wantRecord || len(notes.created) != boolToInt(tt.wantRecord) {
				t.Fatalf("record = %+v (%d created), want recorded=%v", got.Notification, len(notes.created), tt.wantRecord)
			}
			if tt.wantRecord && notes.created[0].InApp != tt.wantListed {
				t.Errorf("in_app = %v, want %v", notes.created[0].InApp, tt.wantListed)
			}
			if len(pusher.sent) != tt.wantPushes || len(mail.sent) != tt.wantMails {
				t.Errorf("pushes %d, mails %d; want %d, %d", len(pusher.sent), len(mail.sent), tt.wantPushes, tt.wantMails)
			}
			if got.Channels != tt.wantChannels || got.Devices != tt.wantDevices {
				t.Errorf("result = %+v, want channels %+v and %d devices", got, tt.wantChannels, tt.wantDevices)
			}
		})
	}
}

// boolToInt is 1 for true.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestDeliver_payloads checks what the push and the generic mail carry: the
// stored text, the notification's own link (absolute in the mail), and its uid
// as the push tag.
func TestDeliver_payloads(t *testing.T) {
	t.Parallel()

	notes := &fakeNotes{wanted: notification.AllChannels()}
	pusher := &fakePush{on: true}
	mail := &fakeMail{on: true}
	d := New(Config{Projects: fakeProjects{}, Notifications: notes, Push: pusher, Mail: mail, BaseURL: " https://k.example/ "})
	if _, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org", name: "Anna"}, "us1", message()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	// The push is titled with the project's name, its URL under the project.
	wantPush := push.Notification{Title: "Rodina · Title", Body: "Body", URL: "/p/rodina/n/nt1", Kind: "tagged", Tag: "nt1"}
	if !reflect.DeepEqual(pusher.sent, []push.Notification{wantPush}) {
		t.Errorf("push = %+v, want %+v", pusher.sent, wantPush)
	}
	wantMail := mailjob.Notification("anna@example.org", mailer.NotificationData{
		DisplayName: "Anna", ProjectName: "Rodina", Title: "Title", Body: "Body",
		URL: "https://k.example/p/rodina/n/nt1",
	})
	if !reflect.DeepEqual(mail.sent, []mailjob.Mail{wantMail}) {
		t.Errorf("mail = %+v, want %+v", mail.sent, wantMail)
	}
	if got := notes.created[0]; !slices.Equal(got.PhotoUIDs, []string{"p1", "p2"}) || !got.SelfLink ||
		got.ProjectUID != "pj1" || got.ProjectSlug != "rodina" {
		t.Errorf("recorded %+v, want the photo set, the self link and the project", got)
	}
}

// TestDeliver_linkUnderTheProject checks a producer's path inside the project
// is stored — and pushed and mailed — under the project's root, with the slug
// loaded from the project rather than spelled by the producer.
func TestDeliver_linkUnderTheProject(t *testing.T) {
	t.Parallel()

	notes := &fakeNotes{wanted: notification.AllChannels()}
	pusher := &fakePush{on: true}
	mail := &fakeMail{on: true}
	d := New(Config{
		Projects: fakeProjects{}, Notifications: notes, Push: pusher, Mail: mail, BaseURL: "https://k.example",
	})
	msg := message()
	msg.SelfLink, msg.Link = false, "/tasks/tk1#comment-cm1"
	if _, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org"}, "us1", msg); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	const want = "/p/rodina/tasks/tk1#comment-cm1"
	if got := notes.created[0].Link; got != want {
		t.Errorf("stored link = %q, want %q", got, want)
	}
	if len(pusher.sent) != 1 || pusher.sent[0].URL != want {
		t.Errorf("push = %+v, want URL %q", pusher.sent, want)
	}
	if len(mail.sent) != 1 || !reflect.DeepEqual(mail.sent[0], mailjob.Notification("anna@example.org",
		mailer.NotificationData{ProjectName: "Rodina", Title: "Title", Body: "Body", URL: "https://k.example" + want})) {
		t.Errorf("mail = %+v, want the link under the base URL", mail.sent)
	}

	msg.Link = ""
	if _, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org"}, "us1", msg); err != nil {
		t.Fatalf("Deliver without a link: %v", err)
	}
	if got := notes.created[1].Link; got != "" {
		t.Errorf("stored link without one = %q, want empty", got)
	}
}

// TestDeliver_needsAProject checks a message without a project delivers
// nothing and says why.
func TestDeliver_needsAProject(t *testing.T) {
	t.Parallel()

	notes := &fakeNotes{wanted: notification.AllChannels()}
	d := New(Config{Projects: fakeProjects{}, Notifications: notes, Push: &fakePush{on: true}, Mail: &fakeMail{on: true}})
	msg := message()
	msg.ProjectUID = ""
	if _, err := d.Deliver(t.Context(), fakeTx{}, "us1", msg); !errors.Is(err, notification.ErrNoProject) {
		t.Fatalf("Deliver without a project = %v, want ErrNoProject", err)
	}
	if len(notes.created) != 0 {
		t.Errorf("recorded %d notifications without a project, want none", len(notes.created))
	}
}

// TestDeliver_customMailAndNoBaseURL checks a kind's own mail builder is used
// and gets an empty URL when no public address is configured.
func TestDeliver_customMailAndNoBaseURL(t *testing.T) {
	t.Parallel()

	mail := &fakeMail{on: true}
	d := New(Config{
		Projects:      fakeProjects{},
		Notifications: &fakeNotes{wanted: []notification.Channel{notification.ChannelEmail}},
		Push:          &fakePush{on: true}, Mail: mail,
	})
	msg := message()
	var gotURL, gotProject string
	msg.Mail = func(to Recipient, n notification.Notification, mail MailContext) mailjob.Mail {
		gotURL, gotProject = mail.URL, mail.ProjectName
		return mailjob.Tagged(to.Email, mailer.TaggedData{DisplayName: to.DisplayName, Count: 2, URL: mail.URL})
	}
	if _, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org"}, "us1", msg); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(mail.sent) != 1 || mail.sent[0].Template != mailer.TemplateTagged || gotURL != "" || gotProject != "Rodina" {
		t.Fatalf("mail = %+v (url %q), want the tagged template without a link", mail.sent, gotURL)
	}
}

// TestDeliver_skipMail checks SkipMail leaves only the e-mail out: the record
// is written and listed, the push goes, nothing is mailed and the result says
// so — the producer that mails once for several notifications relies on it.
// Absolute is what that producer builds the other links with.
func TestDeliver_skipMail(t *testing.T) {
	t.Parallel()

	notes := &fakeNotes{wanted: notification.AllChannels()}
	pusher := &fakePush{on: true}
	mail := &fakeMail{on: true}
	d := New(Config{Projects: fakeProjects{}, Notifications: notes, Push: pusher, Mail: mail, BaseURL: "https://k.example/"})
	msg := message()
	msg.SkipMail = true
	got, err := d.Deliver(t.Context(), fakeTx{email: "anna@example.org", name: "Anna"}, "us1", msg)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got.Notification == nil || !got.Notification.InApp || len(pusher.sent) != 1 || len(mail.sent) != 0 {
		t.Fatalf("result = %+v (pushes %d, mails %d), want a listed record, one push and no mail",
			got, len(pusher.sent), len(mail.sent))
	}
	if want := (Channels{InApp: true, Push: true}); got.Channels != want {
		t.Errorf("channels = %+v, want %+v", got.Channels, want)
	}
	if u := d.Absolute(got.Notification.Link); u != "https://k.example/p/rodina/n/nt1" {
		t.Errorf("Absolute = %q, want the link under the base URL", u)
	}
	if u := d.Absolute(""); u != "" {
		t.Errorf("Absolute of no link = %q, want empty", u)
	}
}

// TestDeliver_instanceSwitches checks a channel switched off for the instance
// counts as not wanted: no push, no mail, and no record for them alone.
func TestDeliver_instanceSwitches(t *testing.T) {
	t.Parallel()

	notes := &fakeNotes{wanted: []notification.Channel{notification.ChannelPush, notification.ChannelEmail}}
	pusher := &fakePush{on: false}
	mail := &fakeMail{on: false}
	d := New(Config{Projects: fakeProjects{}, Notifications: notes, Push: pusher, Mail: mail})
	got, err := d.Deliver(t.Context(), fakeTx{}, "us1", message())
	if err != nil || got.Notification != nil || len(notes.created) != 0 || len(pusher.sent)+len(mail.sent) != 0 {
		t.Fatalf("Deliver with both switches off = %+v, %v; want nothing at all", got, err)
	}
	channels, err := d.ChannelsTx(t.Context(), fakeTx{}, "us1", notification.KindTagged)
	if err != nil || channels.Any() {
		t.Fatalf("ChannelsTx = %+v, %v; want none", channels, err)
	}
}

// TestDeliver_noAddressSkipsOnlyTheMail checks an account without an e-mail
// address still gets its other channels, and the result says no mail went.
func TestDeliver_noAddressSkipsOnlyTheMail(t *testing.T) {
	t.Parallel()

	mail := &fakeMail{on: true}
	d := New(Config{
		Projects:      fakeProjects{},
		Notifications: &fakeNotes{wanted: []notification.Channel{notification.ChannelInApp, notification.ChannelEmail}},
		Push:          &fakePush{on: true}, Mail: mail,
	})
	got, err := d.Deliver(t.Context(), fakeTx{email: "  "}, "us1", message())
	if err != nil || got.Notification == nil || len(mail.sent) != 0 || got.Channels != (Channels{InApp: true}) {
		t.Fatalf("Deliver without an address = %+v, %v; want the inbox entry and no mail", got, err)
	}
}

// TestDeliver_errors checks a failing step is returned wrapped.
func TestDeliver_errors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tests := []struct {
		name     string
		projects fakeProjects
		notes    *fakeNotes
		push     *fakePush
	}{
		{name: "preferences", notes: &fakeNotes{wantedErr: boom}, push: &fakePush{on: true}},
		{
			name:     "project",
			projects: fakeProjects{err: boom},
			notes:    &fakeNotes{wanted: []notification.Channel{notification.ChannelInApp}},
			push:     &fakePush{on: true},
		},
		{
			name:  "record",
			notes: &fakeNotes{wanted: []notification.Channel{notification.ChannelInApp}, createErr: boom},
			push:  &fakePush{on: true},
		},
		{
			name:  "push",
			notes: &fakeNotes{wanted: []notification.Channel{notification.ChannelPush}},
			push:  &fakePush{on: true, err: boom},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := New(Config{Projects: tt.projects, Notifications: tt.notes, Push: tt.push, Mail: &fakeMail{on: true}})
			if _, err := d.Deliver(t.Context(), fakeTx{}, "us1", message()); !errors.Is(err, boom) {
				t.Fatalf("Deliver = %v, want the wrapped failure", err)
			}
		})
	}
}

// TestNew_requiresDependencies checks wiring without a dependency panics.
func TestNew_requiresDependencies(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New without Mail did not panic")
		}
	}()
	New(Config{Projects: fakeProjects{}, Notifications: &fakeNotes{}, Push: &fakePush{}})
}

// TestChannelsAny checks Any is true exactly when one channel is set.
func TestChannelsAny(t *testing.T) {
	t.Parallel()

	for _, c := range []Channels{{InApp: true}, {Push: true}, {Email: true}} {
		if !c.Any() {
			t.Errorf("%+v.Any() = false", c)
		}
	}
	if (Channels{}).Any() {
		t.Error("the zero Channels reads as delivering")
	}
}

// TestPushTitle checks the push title names the project, and a blank name
// leaves the title alone.
func TestPushTitle(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{"": "Title", " ": "Title", " Rodina ": "Rodina · Title"} {
		if got := PushTitle(name, "Title"); got != want {
			t.Errorf("PushTitle(%q) = %q, want %q", name, got, want)
		}
	}
}
