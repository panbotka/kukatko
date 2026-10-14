// Package mentionnotify is the "somebody mentioned you in a comment"
// notification: the comment_mention kind, delivered inside the transaction
// that wrote the comment. It is the comments store's MentionObserver — the
// store resolves a body's `@[Name](user:<uid>)` tokens, keeps the mentioned
// set in comment_mentions and reports the mentions that are new on the comment
// — and this package turns each into one notification through
// internal/notifydeliver, which reads the account's channels (in-app, push,
// e-mail — every one off until chosen) and records it once.
//
// The rules are few and all in one place (wanted): the writer is never told
// about naming themselves (the store already leaves a self-mention out), an
// account out of standing in the project (internal/notifystanding: disabled,
// not an approved member — a maintainer included —, or the project
// deactivated) hears nothing, and nobody is told
// about a comment on a photograph they could not open — a private, hidden or
// archived photo, read through notification.VisiblePhotoSQL so this agrees
// with what every other notification is allowed to name. The mention is still
// highlighted in the thread for whoever reads it; only the announcement is
// skipped. A task has no such flag: every signed-in role may open every
// task, so a task comment is announced to every mentioned account in good
// standing. An album's thread follows the album's own read rule, which is the
// same — GET /albums/{uid} answers every signed-in role, a private album is a
// badge there and not a wall — so an album comment is announced like a task's;
// should the album rule ever tighten, this is the one place to follow it.
// "Already mentioned" is the store's rule — an edit reports only the accounts
// the previous body did not name — and whether the person wants the kind at
// all is notifydeliver's question.
//
// A notification is worth less than the comment that caused it, so every
// delivery runs under a savepoint of the comment's transaction: one that fails
// is rolled back to it and logged, and the comment commits all the same.
// Deleting the comment afterwards cancels nothing — a notification is a frozen
// record of what was said when it was said, exactly like a push already sent.
package mentionnotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/notifystanding"
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

// Notifier is the comments store's mention observer that announces a fresh
// mention to the account named.
type Notifier struct {
	deliver Deliver
	lang    notification.Language
	log     *slog.Logger
}

// Notifier satisfies the comments store's hook.
var _ comments.MentionObserver = (*Notifier)(nil)

// New builds a Notifier from cfg. It panics without Deliver: that is a wiring
// mistake and should fail at startup rather than on the first mention.
func New(cfg Config) *Notifier {
	if cfg.Deliver == nil {
		panic("mentionnotify: Deliver is required")
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

// AnchorPrefix is the fragment a comment is addressed by on its page, the
// comment uid appended: the thread scrolls to it and marks it.
const AnchorPrefix = "#comment-"

// CommentPath returns the path inside the subject's project that opens the
// comment commentUID in the thread of subj (internal/notifydeliver puts it
// under the project's root /p/<slug>): a photograph's viewer with its drawer open (the thread
// mounts only while it is), the task's page, or the album's page with its
// comments drawer open (the same reason as the viewer's), each anchored to the
// comment.
func CommentPath(subj comments.Subject, commentUID string) string {
	anchor := AnchorPrefix + commentUID
	switch subj.Kind {
	case comments.SubjectTask:
		return "/tasks/" + subj.UID + anchor
	case comments.SubjectAlbum:
		return "/albums/" + subj.UID + "?panel=comments" + anchor
	default:
		return "/photos/" + subj.UID + "?panel=info" + anchor
	}
}

// place is what the rules know about the thread's subject.
type place struct {
	// kind is a photo's thread, a task's or an album's.
	kind notification.MentionPlace
	// title names the subject; a photograph's may be blank.
	title string
	// visible is whether an account may be told about the subject at all: a
	// task or an album always, a photograph unless private, hidden or archived.
	visible bool
	// projectUID is the subject's project, the one the notification belongs to.
	projectUID string
}

// wanted is the rule of who is told about a mention: the mentioned account,
// unless it is the writer's own, it is not in good standing (disabled, not an
// approved member, the project deactivated — standing says so), or the
// subject may not be announced.
// Whether they want the kind through any channel is not asked here;
// notifydeliver answers that per delivery. It is a pure function of its
// arguments.
func wanted(userUID, authorUID string, standing bool, p place) bool {
	switch {
	case userUID == "" || userUID == authorUID:
		return false
	case !standing:
		return false
	case !p.visible:
		return false
	default:
		return true
	}
}

// photoPlaceSQL reads a photograph's title, whether it may be named in a
// notification, by the one definition every producer uses, and its project.
const photoPlaceSQL = `SELECT p.title, ` + notification.VisiblePhotoSQL + `, p.project_id
FROM photos p WHERE p.uid = $1`

// taskPlaceSQL reads a task's title and project; a task is always announceable.
const taskPlaceSQL = `SELECT t.title, true, t.project_id FROM photo_tasks t WHERE t.uid = $1`

// albumPlaceSQL reads an album's title and project; an album is announceable to every
// signed-in account, exactly as its page is open to every one (see the package
// comment).
const albumPlaceSQL = `SELECT a.title, true, a.project_id FROM albums a WHERE a.uid = $1`

// standingSQL reads, of the given accounts, the ones in good standing in the
// project $2 by the one rule every notification follows
// (internal/notifystanding): an approved member there, enabled, the project
// active — a maintainer only as a member.
var standingSQL = `SELECT u.uid FROM users u
WHERE u.uid = ANY($1) AND ` + notifystanding.MemberSQL("u.uid", "$2")

// Mentioned delivers the comment_mention notification to every account in
// mentioned the rules (wanted) allow, under a savepoint: a delivery that fails
// is undone, logged and swallowed so the comment goes on. Only a savepoint
// that cannot be opened or released — a transaction already broken — is
// returned.
func (n *Notifier) Mentioned(ctx context.Context, tx pgx.Tx, c comments.Comment, mentioned []comments.Mention) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mentionnotify: opening a savepoint: %w", err)
	}
	if err := n.notify(ctx, sp, c, mentioned); err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("mentionnotify: rolling back to the savepoint: %w", rbErr)
		}
		n.log.WarnContext(ctx, "mentionnotify: announcing the mentions failed; the comment goes on without it",
			slog.String("comment_uid", c.UID), slog.String("error", err.Error()))
		return nil
	}
	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("mentionnotify: releasing the savepoint: %w", err)
	}
	return nil
}

// notify is Mentioned's work on the savepoint sp: read the place and the
// accounts' standing, apply the rules, deliver to each.
func (n *Notifier) notify(ctx context.Context, sp pgx.Tx, c comments.Comment, mentioned []comments.Mention) error {
	subj := c.Subject()
	p, err := readPlace(ctx, sp, subj)
	if err != nil {
		return err
	}
	if !p.visible {
		n.log.DebugContext(ctx, "mentionnotify: not announced, the photo may not be named",
			slog.String("comment_uid", c.UID), slog.String("photo_uid", subj.UID))
		return nil
	}
	standing, err := readStanding(ctx, sp, p.projectUID, mentioned)
	if err != nil {
		return err
	}
	msg := n.message(c, subj, p)
	for _, m := range mentioned {
		if !wanted(m.UserUID, c.AuthorUID, standing[m.UserUID], p) {
			continue
		}
		if _, err := n.deliver.Deliver(ctx, sp, m.UserUID, msg); err != nil {
			return fmt.Errorf("delivering to %s: %w", m.UserUID, err)
		}
	}
	return nil
}

// placeQueries maps each subject kind to the place it reads and how.
var placeQueries = map[comments.SubjectKind]struct {
	kind  notification.MentionPlace
	query string
}{
	comments.SubjectPhoto: {kind: notification.MentionPlacePhoto, query: photoPlaceSQL},
	comments.SubjectTask:  {kind: notification.MentionPlaceTask, query: taskPlaceSQL},
	comments.SubjectAlbum: {kind: notification.MentionPlaceAlbum, query: albumPlaceSQL},
}

// readPlace reads what the rules and the text need about subj on sp. A subject
// kind the notifier does not know is an error rather than a guess: the store
// refuses such a comment before it gets here.
func readPlace(ctx context.Context, sp pgx.Tx, subj comments.Subject) (place, error) {
	q, ok := placeQueries[subj.Kind]
	if !ok {
		return place{}, fmt.Errorf("unknown subject kind %q", subj.Kind)
	}
	p := place{kind: q.kind}
	err := sp.QueryRow(ctx, q.query, subj.UID).Scan(&p.title, &p.visible, &p.projectUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return place{}, fmt.Errorf("%s %s not found", subj.Kind, subj.UID)
	}
	if err != nil {
		return place{}, fmt.Errorf("reading %s %s: %w", subj.Kind, subj.UID, err)
	}
	return p, nil
}

// readStanding returns which of the mentioned accounts are in good standing in
// the project projectUID, keyed by uid; an account absent from the map is not.
func readStanding(
	ctx context.Context, sp pgx.Tx, projectUID string, mentioned []comments.Mention,
) (map[string]bool, error) {
	uids := make([]string, 0, len(mentioned))
	for _, m := range mentioned {
		uids = append(uids, m.UserUID)
	}
	rows, err := sp.Query(ctx, standingSQL, uids, projectUID)
	if err != nil {
		return nil, fmt.Errorf("reading the accounts' standing: %w", err)
	}
	good, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scanning the accounts' standing: %w", err)
	}
	standing := make(map[string]bool, len(good))
	for _, uid := range good {
		standing[uid] = true
	}
	return standing, nil
}

// message is the notification a mentioned person gets: the comment_mention
// kind, the text in the notifier's language naming the writer, the place and
// quoting the comment, the comment's own anchor as the link, no photographs,
// and the comment_mention mail.
func (n *Notifier) message(c comments.Comment, subj comments.Subject, p place) notifydeliver.Message {
	excerpt := comments.Excerpt(c.Body, comments.MaxExcerptLen)
	return notifydeliver.Message{
		ProjectUID: p.projectUID,
		Kind:       notification.KindCommentMention,
		Text:       notification.RenderCommentMention(n.lang, c.AuthorName, p.kind, p.title, excerpt),
		Link:       CommentPath(subj, c.UID),
		Mail:       mailBuilder(c.AuthorName, p, excerpt),
	}
}

// mailBuilder returns the e-mail of the notification: the comment_mention
// template with the writer, the place, the excerpt and the absolute link.
func mailBuilder(who string, p place, excerpt string) notifydeliver.MailBuilder {
	return func(to notifydeliver.Recipient, _ notification.Notification, mail notifydeliver.MailContext) mailjob.Mail {
		return mailjob.CommentMention(to.Email, mailer.CommentMentionData{
			DisplayName: to.DisplayName, ProjectName: mail.ProjectName, Who: strings.TrimSpace(who),
			OnTask:  p.kind == notification.MentionPlaceTask,
			OnAlbum: p.kind == notification.MentionPlaceAlbum,
			Title:   p.title, Excerpt: excerpt, URL: mail.URL,
		})
	}
}
