// Package taskassignnotify is the "somebody put you on a task" notification:
// the task_assigned kind, delivered inside the transaction that asked the
// person. It is the phototask store's AssignObserver — the store reports every
// fresh hand-over (through Assign, or the participants a task is opened with)
// and this package turns each into one notification through
// internal/notifydeliver, which reads the account's channels (in-app, push,
// e-mail — every one off until chosen) and records it once.
//
// The rules are few and all in one place (wanted): nobody is told about their
// own doing (asking yourself onto a task), a hand-over by nobody (an internal
// path with no acting account, a NULL added_by) is not a hand-over, and an
// account out of standing in the task's project (internal/notifystanding:
// disabled, not an approved member — a maintainer included —, or the project
// deactivated) hears nothing. "Already on the task" is
// the store's rule — it reports only a person who was not on the task before,
// so re-asking is silent — and whether the person wants the kind at all is
// notifydeliver's question.
//
// A notification is worth less than the assignment that caused it, so every
// delivery runs under a savepoint of the assignment's transaction: one that
// fails is rolled back to it and logged, and the assignment commits all the
// same. The daily task digest (internal/taskdigestjob) is a separate thing
// with its own switch; this kind is the immediate "you were asked".
package taskassignnotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/phototask"
)

// Deliver fans one notification out to the channels its account wants, on the
// caller's transaction. It is satisfied by *notifydeliver.Deliverer.
type Deliver interface {
	// Deliver delivers msg to userUID on tx; an account that wants no channel
	// gets nothing and a zero Result.
	Deliver(ctx context.Context, tx pgx.Tx, userUID string, msg notifydeliver.Message) (notifydeliver.Result, error)
}

// Config bundles what New needs.
type Config struct {
	// Deliver is the delivery path. Required.
	Deliver Deliver
	// Language is the language the message is written in; empty is Czech, the
	// instance's language. Accounts carry no language of their own yet.
	Language notification.Language
	// Logger records the deliveries that failed and were skipped; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// Notifier is the phototask store's assign observer that announces a hand-over
// to the person asked.
type Notifier struct {
	deliver Deliver
	lang    notification.Language
	log     *slog.Logger
}

// Notifier satisfies the task store's hook.
var _ phototask.AssignObserver = (*Notifier)(nil)

// New builds a Notifier from cfg. It panics without Deliver: that is a wiring
// mistake and should fail at startup rather than on the first hand-over.
func New(cfg Config) *Notifier {
	if cfg.Deliver == nil {
		panic("taskassignnotify: Deliver is required")
	}
	lang := cfg.Language
	if lang == "" {
		lang = notification.LanguageCzech
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Notifier{deliver: cfg.Deliver, lang: lang, log: log}
}

// PathPrefix is the route of one task's page inside its project, the uid
// appended; internal/notifydeliver puts it under the project's root.
const PathPrefix = "/tasks/"

// TaskPath returns the path of the task uid's page inside its project, the
// link the notification opens.
func TaskPath(uid string) string {
	return PathPrefix + uid
}

// account is what the rules know about the person asked.
type account struct {
	// Disabled is whether the account was switched off.
	Disabled bool
	// Approved is whether the account is in standing in the task's project —
	// an approved member of an active project (internal/notifystanding); one
	// still waiting for approval could not open the task.
	Approved bool
}

// wanted is the rule of who is told about a hand-over: the person asked, unless
// nobody asked (an empty actor — the row counts as joined by acting), they
// asked themselves, or their account is disabled or not yet approved. Whether
// they want the kind through any channel is not asked here; notifydeliver
// answers that per delivery. It is a pure function of its arguments.
func wanted(a phototask.Assignment, acct account) bool {
	switch {
	case a.ActorUID == "":
		return false
	case a.UserUID == a.ActorUID:
		return false
	case acct.Disabled, !acct.Approved:
		return false
	default:
		return true
	}
}

// contextSQL reads what one notification needs, in one round trip: the task's
// project (the inbox the notification lands in) and title, the asking account's
// name (display name, else username, empty when the account is gone or nobody
// asked) and the asked account's standing (internal/notifystanding). The task
// and the asked account are both known to exist — the assignment that just
// wrote the participant row holds foreign keys to both.
var contextSQL = `
SELECT t.project_id, t.title,
       COALESCE(NULLIF(a.display_name, ''), a.username, ''),
       u.disabled,
       ` + notifystanding.MemberSQL("u.uid", "t.project_id") + `
FROM photo_tasks t
JOIN users u ON u.uid = $2
LEFT JOIN users a ON a.uid = $3
WHERE t.uid = $1`

// taskContext is one contextSQL row.
type taskContext struct {
	projectUID string
	title      string
	who        string
	acct       account
}

// Assigned delivers the task_assigned notification to a.UserUID on tx when the
// rules (wanted) allow it, under a savepoint: a delivery that fails is undone,
// logged and swallowed so the assignment goes on. Only a savepoint that cannot
// be opened or released — a transaction already broken — is returned.
func (n *Notifier) Assigned(ctx context.Context, tx pgx.Tx, a phototask.Assignment) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("taskassignnotify: opening a savepoint: %w", err)
	}
	if err := n.notify(ctx, sp, a); err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("taskassignnotify: rolling back to the savepoint: %w", rbErr)
		}
		n.log.WarnContext(ctx, "taskassignnotify: announcing the assignment failed; the assignment goes on without it",
			slog.String("task_uid", a.TaskUID), slog.String("user_uid", a.UserUID),
			slog.String("error", err.Error()))
		return nil
	}
	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("taskassignnotify: releasing the savepoint: %w", err)
	}
	return nil
}

// notify is Assigned's work on the savepoint sp: read the context, apply the
// rules, deliver.
func (n *Notifier) notify(ctx context.Context, sp pgx.Tx, a phototask.Assignment) error {
	var c taskContext
	err := sp.QueryRow(ctx, contextSQL, a.TaskUID, a.UserUID, a.ActorUID).
		Scan(&c.projectUID, &c.title, &c.who, &c.acct.Disabled, &c.acct.Approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("task %s or account %s not found", a.TaskUID, a.UserUID)
	}
	if err != nil {
		return fmt.Errorf("reading the context of %s: %w", a.TaskUID, err)
	}
	if !wanted(a, c.acct) {
		return nil
	}
	if _, err := n.deliver.Deliver(ctx, sp, a.UserUID, n.message(c.projectUID, a.TaskUID, c.who, c.title)); err != nil {
		return fmt.Errorf("delivering to %s: %w", a.UserUID, err)
	}
	return nil
}

// message is the notification the asked person gets in the task's project
// projectUID: the task_assigned kind,
// the text in the notifier's language naming who asked and the task, the
// task's page as the link, no photographs, and the task_assigned mail.
func (n *Notifier) message(projectUID, taskUID, who, title string) notifydeliver.Message {
	return notifydeliver.Message{
		ProjectUID: projectUID,
		Kind:       notification.KindTaskAssigned,
		Text:       notification.RenderTaskAssigned(n.lang, who, title),
		Link:       TaskPath(taskUID),
		Mail:       mailBuilder(who, title),
	}
}

// mailBuilder returns the e-mail of the notification: the task_assigned
// template with who asked, the task's title, the project and the absolute task
// link.
func mailBuilder(who, title string) notifydeliver.MailBuilder {
	return func(to notifydeliver.Recipient, _ notification.Notification, mail notifydeliver.MailContext) mailjob.Mail {
		return mailjob.TaskAssigned(to.Email, mailer.TaskAssignedData{
			DisplayName: to.DisplayName, ProjectName: mail.ProjectName, Who: who, Title: title, URL: mail.URL,
		})
	}
}
