package taskassignnotify

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/phototask"
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

// TestWanted checks the rules of who is told about a hand-over: the person
// asked, unless nobody asked, they asked themselves, or their account is
// disabled or not yet approved.
func TestWanted(t *testing.T) {
	t.Parallel()

	ok := account{Approved: true}
	tests := []struct {
		name string
		a    phototask.Assignment
		acct account
		want bool
	}{
		{
			name: "asked by somebody else", want: true,
			a: phototask.Assignment{TaskUID: "pt1", UserUID: "us-asked", ActorUID: "us-actor"}, acct: ok,
		},
		{
			name: "nobody asked",
			a:    phototask.Assignment{TaskUID: "pt1", UserUID: "us-asked"}, acct: ok,
		},
		{
			name: "asked themselves",
			a:    phototask.Assignment{TaskUID: "pt1", UserUID: "us-actor", ActorUID: "us-actor"}, acct: ok,
		},
		{
			name: "disabled account",
			a:    phototask.Assignment{TaskUID: "pt1", UserUID: "us-asked", ActorUID: "us-actor"},
			acct: account{Disabled: true, Approved: true},
		},
		{
			name: "not yet approved",
			a:    phototask.Assignment{TaskUID: "pt1", UserUID: "us-asked", ActorUID: "us-actor"},
			acct: account{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wanted(tt.a, tt.acct); got != tt.want {
				t.Errorf("wanted(%+v, %+v) = %v, want %v", tt.a, tt.acct, got, tt.want)
			}
		})
	}
}

// TestTaskPath pins the link a notification opens.
func TestTaskPath(t *testing.T) {
	t.Parallel()

	if got := TaskPath("pt1"); got != "/tasks/pt1" {
		t.Errorf("TaskPath = %q, want /tasks/pt1", got)
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

// TestMessage checks the notification the asked person gets: the kind, the
// Czech text with the asker and the title, the task link, no photographs and
// the task_assigned mail with the absolute link; and the English text.
func TestMessage(t *testing.T) {
	t.Parallel()

	msg := New(Config{Deliver: fakeDeliver{}}).message("pt1", "Pan Botka", "Kdy to bylo?")
	if msg.Kind != notification.KindTaskAssigned || msg.SelfLink || msg.SkipMail || msg.PhotoUIDs != nil {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Text.Title != "Pan Botka tě přidal(a) k úkolu: Kdy to bylo?" || msg.Link != "/tasks/pt1" {
		t.Fatalf("text %+v, link %q", msg.Text, msg.Link)
	}
	mail := msg.Mail(notifydeliver.Recipient{Email: "anna@example.test", DisplayName: "Anna"},
		notification.Notification{}, "https://k.example/tasks/pt1")
	wantData := mailer.TaskAssignedData{
		DisplayName: "Anna", Who: "Pan Botka", Title: "Kdy to bylo?", URL: "https://k.example/tasks/pt1",
	}
	if mail.Template != mailer.TemplateTaskAssigned || mail.To != "anna@example.test" ||
		!reflect.DeepEqual(mail.Data, wantData) {
		t.Fatalf("mail = %+v, want the task_assigned template with %+v", mail, wantData)
	}

	english := New(Config{Deliver: fakeDeliver{}, Language: notification.LanguageEnglish})
	if got := english.message("pt2", "Anna", "When?").Text.Title; got != "Anna added you to the task: When?" {
		t.Fatalf("English title = %q", got)
	}
}
