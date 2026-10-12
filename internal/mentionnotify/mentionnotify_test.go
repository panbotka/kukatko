package mentionnotify

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
)

// fakeDeliver satisfies Deliver without a database; the unit tests never reach
// it.
type fakeDeliver struct{}

// Deliver reports an empty result.
func (fakeDeliver) Deliver(
	context.Context, pgx.Tx, string, notifydeliver.Message,
) (notifydeliver.Result, error) {
	return notifydeliver.Result{}, nil
}

// TestWanted checks the rules of who is told about a mention: the mentioned
// account, unless it is the writer's own, out of standing, or the photograph
// may not be named.
func TestWanted(t *testing.T) {
	t.Parallel()

	visible := place{kind: notification.MentionPlacePhoto, visible: true}
	tests := []struct {
		name     string
		user     string
		author   string
		standing bool
		place    place
		want     bool
	}{
		{name: "somebody else, in standing, visible", user: "us-anna", author: "us-tom", standing: true, place: visible, want: true},
		{name: "self-mention", user: "us-tom", author: "us-tom", standing: true, place: visible},
		{name: "no uid", author: "us-tom", standing: true, place: visible},
		{name: "authorless comment still announces", user: "us-anna", standing: true, place: visible, want: true},
		{name: "out of standing", user: "us-anna", author: "us-tom", place: visible},
		{name: "private, hidden or archived photo", user: "us-anna", author: "us-tom", standing: true, place: place{}},
		{
			name: "a task is always visible", user: "us-anna", author: "us-tom", standing: true,
			place: place{kind: notification.MentionPlaceTask, visible: true}, want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wanted(tt.user, tt.author, tt.standing, tt.place); got != tt.want {
				t.Errorf("wanted(%q, %q, %v, %+v) = %v, want %v", tt.user, tt.author, tt.standing, tt.place, got, tt.want)
			}
		})
	}
}

// TestCommentPath pins the links a notification opens: the photo viewer with
// its drawer open, or the task page, both anchored to the comment.
func TestCommentPath(t *testing.T) {
	t.Parallel()

	if got := CommentPath(comments.PhotoSubject("ph1"), "cm1"); got != "/photos/ph1?panel=info#comment-cm1" {
		t.Errorf("photo path = %q", got)
	}
	if got := CommentPath(comments.TaskSubject("pt1"), "cm1"); got != "/tasks/pt1#comment-cm1" {
		t.Errorf("task path = %q", got)
	}
}

// TestNew_defaultsAndRequirements checks the language defaults to Czech and
// that a notifier without a delivery path refuses to be built.
func TestNew_defaultsAndRequirements(t *testing.T) {
	t.Parallel()

	n := New(Config{Deliver: fakeDeliver{}})
	if n.lang != notification.LanguageCzech || n.log == nil {
		t.Fatalf("defaults: lang %q, log %v", n.lang, n.log)
	}
	defer func() {
		if recover() == nil {
			t.Error("New without Deliver did not panic")
		}
	}()
	New(Config{})
}

// TestMessage checks the notification a mentioned person gets: the kind, the
// Czech text with the writer, the place and the excerpt, the anchored link, no
// photographs and the comment_mention mail with the absolute link.
func TestMessage(t *testing.T) {
	t.Parallel()

	c := comments.Comment{
		UID: "cm1", TaskUID: "pt1", AuthorUID: "us-tom", AuthorName: "Pan Botka",
		Body: "Hele @[Anna](user:us-anna), víš to?",
	}
	p := place{kind: notification.MentionPlaceTask, title: "Kdy to bylo?", visible: true}
	msg := New(Config{Deliver: fakeDeliver{}}).message(c, c.Subject(), p)
	if msg.Kind != notification.KindCommentMention || msg.SelfLink || msg.SkipMail || msg.PhotoUIDs != nil {
		t.Fatalf("message = %+v", msg)
	}
	wantText := notification.Text{
		Title: "Pan Botka tě zmínil(a) v komentáři u úkolu Kdy to bylo?", Body: "Hele @Anna, víš to?",
	}
	if msg.Text != wantText || msg.Link != "/tasks/pt1#comment-cm1" {
		t.Fatalf("text %+v, link %q", msg.Text, msg.Link)
	}
	mail := msg.Mail(notifydeliver.Recipient{Email: "anna@example.test", DisplayName: "Anna"},
		notification.Notification{}, "https://k.example/tasks/pt1#comment-cm1")
	wantData := mailer.CommentMentionData{
		DisplayName: "Anna", Who: "Pan Botka", OnTask: true, Title: "Kdy to bylo?",
		Excerpt: "Hele @Anna, víš to?", URL: "https://k.example/tasks/pt1#comment-cm1",
	}
	if mail.Template != mailer.TemplateCommentMention || mail.To != "anna@example.test" ||
		!reflect.DeepEqual(mail.Data, wantData) {
		t.Fatalf("mail = %+v, want the comment_mention template with %+v", mail, wantData)
	}
}
