// Package notifydeliver is the one way a notification leaves Kukátko: it reads
// which channels the account wants the kind through, records the notification
// once, and fans it out — into the in-app inbox, to the account's devices
// (internal/pushjob) and to its e-mail address (internal/mailjob) — all on the
// caller's transaction, so a notification shares the fate of the mutation that
// caused it.
//
// A producer (the tagging window, a registration waiting for approval) builds a
// Message and calls Deliver; it never checks a preference or touches a queue
// itself. Adding a kind is therefore a registry entry in internal/notification,
// a render function for its text and, optionally, a mail template — without
// one the generic notification mail carries the title, body and link.
//
// One record serves every channel. It is written whenever any channel will
// deliver (the push opens it, the e-mail links to it, the inbox lists it), and
// its in_app flag says whether the inbox lists it — so a record that exists
// only behind a push or an e-mail never turns up behind the bell. A channel the
// instance has switched off (push.enabled, mail.enabled) counts as not wanted:
// nothing is recorded for a delivery that could never happen.
//
// Every notification belongs to one project: it is listed in that project's
// inbox, and every deep link it carries — the stored link, the push's URL, the
// e-mail's — opens inside the project, under /p/<slug>/. A producer names the
// project (Message.ProjectUID) and the path inside it (Message.Link, e.g.
// "/tasks/tk1"); Deliver loads the project (project.Store.Get) and puts the path
// under the project's slug, so no producer spells or hard-codes a slug. The
// project's name travels too: the push's title is prefixed by it ("Veselice ·
// …") and every mail builder receives it (MailContext.ProjectName), so a
// person in several projects can tell them apart before opening anything.
//
// Nobody out of standing is notified. Before anything else Deliver asks
// internal/notifystanding whether the account may receive a notification of
// the project at all — an approved member, enabled, the project active (a
// maintainer only as a member, except for the kinds addressed to maintainers)
// — and drops the delivery silently, counted, when it may not. Every producer
// delivers through here, so that one check covers them all. The push and the
// mail it queues carry the addressee, and their handlers ask the same question
// again when they run, so an account that falls out of standing while its
// notification waits in the queue does not get it either.
package notifydeliver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// Projects loads the project a notification belongs to, for its slug. It is
// satisfied by *project.Store.
type Projects interface {
	// Get returns the project uid names, or project.ErrNotFound.
	Get(ctx context.Context, uid string) (*project.Project, error)
}

// Recorder reads an account's wanted channels and records a notification, both
// on the caller's transaction. It is satisfied by *notification.Store.
type Recorder interface {
	// WantedTx returns every channel through which userUID wants kind, read on tx.
	WantedTx(ctx context.Context, tx pgx.Tx, userUID string, kind notification.Kind) ([]notification.Channel, error)
	// CreateTx records n on tx and returns the stored notification.
	CreateTx(ctx context.Context, tx pgx.Tx, n notification.New) (notification.Notification, error)
}

// PushScheduler schedules a push to every device of an account. It is satisfied
// by *pushjob.Enqueuer, which itself queues nothing for an account without a
// device.
type PushScheduler interface {
	// Enabled reports whether this instance sends push notifications at all.
	Enabled() bool
	// Enqueue schedules n for the devices of to's account, addressed to it and
	// its project, using exec and returns how many deliveries it queued.
	Enqueue(ctx context.Context, exec pushjob.Execer, to notifystanding.Addressee, n push.Notification) (int, error)
}

// StandingFunc reports, read through q (the producer's transaction), whether
// an addressee may receive a notification. notifystanding.InStanding is the one
// production uses.
type StandingFunc func(ctx context.Context, q notifystanding.Querier, a notifystanding.Addressee) (bool, error)

// MailScheduler schedules one e-mail. It is satisfied by *mailjob.Enqueuer,
// which itself refuses nothing but caller bugs and skips a placeholder
// (.invalid) recipient.
type MailScheduler interface {
	// Enabled reports whether this instance sends mail at all.
	Enabled() bool
	// Enqueue schedules m using exec.
	Enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error
}

// Recipient is who an e-mail goes to: the account's address and the name to
// greet it by.
type Recipient struct {
	// Email is the account's address; empty when it has none.
	Email string
	// DisplayName may be empty; the templates greet impersonally then.
	DisplayName string
}

// MailContext is what a MailBuilder knows beyond the stored record: the
// absolute address of its link and the project it belongs to.
type MailContext struct {
	// URL is the absolute address of the notification's link, under /p/<slug>/
	// — empty when it has no link or no public URL is configured.
	URL string
	// ProjectName is the name of the notification's project, which the mail's
	// subject and body name (never the slug).
	ProjectName string
}

// MailBuilder turns a recorded notification into its e-mail: to is the
// recipient, n the stored record and mail its link and project.
type MailBuilder func(to Recipient, n notification.Notification, mail MailContext) mailjob.Mail

// Message is one notification to deliver to one account.
type Message struct {
	// ProjectUID is the project the notification is about: the inbox it is
	// listed in and the project its links open in. Required.
	ProjectUID string
	// Kind must be a known kind.
	Kind notification.Kind
	// Text is the title and the line under it, as the inbox and the push show
	// them.
	Text notification.Text
	// Link is the path the notification opens inside its project ("/tasks/tk1",
	// "/photos/ph1?panel=info"); Deliver stores it under the project's root
	// (/p/<slug>/tasks/tk1). Empty opens nothing in particular.
	Link string
	// SelfLink makes the notification open its own page (/p/<slug>/n/<uid>),
	// the one that shows its frozen photo set. Link must be empty when it is set.
	SelfLink bool
	// PhotoUIDs is the frozen photo set, in display order; empty for a kind
	// about no photographs.
	PhotoUIDs []string
	// Mail builds the e-mail. Nil uses the generic notification mail (title,
	// body, link), so every kind can be mailed.
	Mail MailBuilder
	// SkipMail leaves the e-mail channel out of this delivery, whatever the
	// account wants: a producer that records several notifications for one
	// event (the tagging window, one per album) mails once, from the last of
	// them, so nobody gets five mails for one afternoon of tagging. The other
	// channels deliver as usual; Result.Channels.Email is then false.
	SkipMail bool
}

// Channels is what one delivery will do: which channels are both wanted by the
// account and deliverable on this instance.
type Channels struct {
	// InApp lists the notification in the inbox.
	InApp bool
	// Push schedules it to the account's devices.
	Push bool
	// Email mails it.
	Email bool
}

// Any reports whether at least one channel will deliver.
func (c Channels) Any() bool {
	return c.InApp || c.Push || c.Email
}

// Result is what Deliver did.
type Result struct {
	// Channels are the channels it delivered through.
	Channels Channels
	// Notification is the stored record; nil when no channel was wanted.
	Notification *notification.Notification
	// Devices is how many push deliveries were queued.
	Devices int
}

// Config bundles the dependencies of New.
type Config struct {
	// Notifications reads the preferences and records the notification.
	// Required.
	Notifications Recorder
	// Projects loads a notification's project for the slug its links live
	// under. Required.
	Projects Projects
	// Push schedules the push channel. Required.
	Push PushScheduler
	// Mail schedules the e-mail channel. Required.
	Mail MailScheduler
	// BaseURL is the instance's public address (mail.base_url) the e-mail's
	// links are built on; empty leaves the links out of the mail.
	BaseURL string
	// Standing decides whether the account may be notified at all; nil uses
	// notifystanding.InStanding. Unit tests substitute a stub.
	Standing StandingFunc
	// Logger records the deliveries that were skipped; nil uses slog.Default().
	Logger *slog.Logger
}

// Deliverer fans one notification out to the channels its account wants.
type Deliverer struct {
	notes    Recorder
	projects Projects
	push     PushScheduler
	mail     MailScheduler
	standing StandingFunc
	baseURL  string
	log      *slog.Logger
}

// New returns a Deliverer from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup rather than on the
// first notification.
func New(cfg Config) *Deliverer {
	if cfg.Notifications == nil || cfg.Projects == nil || cfg.Push == nil || cfg.Mail == nil {
		panic("notifydeliver: Notifications, Projects, Push and Mail are required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	standing := cfg.Standing
	if standing == nil {
		standing = notifystanding.InStanding
	}
	return &Deliverer{
		notes: cfg.Notifications, projects: cfg.Projects, push: cfg.Push, mail: cfg.Mail, standing: standing,
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), log: log,
	}
}

// ChannelsTx returns, read on tx, the channels a notification of kind to
// userUID would be delivered through: wanted by the account and switched on
// for the instance. A producer that collects work before it delivers (the
// tagging window) asks this first, so it collects nothing for an account that
// would receive nothing. An unknown kind is notification.ErrUnknownKind.
func (d *Deliverer) ChannelsTx(
	ctx context.Context, tx pgx.Tx, userUID string, kind notification.Kind,
) (Channels, error) {
	wanted, err := d.notes.WantedTx(ctx, tx, userUID, kind)
	if err != nil {
		return Channels{}, fmt.Errorf("notifydeliver: reading the channels of %s: %w", userUID, err)
	}
	return Channels{
		InApp: slices.Contains(wanted, notification.ChannelInApp),
		Push:  slices.Contains(wanted, notification.ChannelPush) && d.push.Enabled(),
		Email: slices.Contains(wanted, notification.ChannelEmail) && d.mail.Enabled(),
	}, nil
}

// Deliver delivers msg to userUID on tx: unless the account is out of standing
// in msg's project (see the package documentation; dropped, counted, a zero
// Result), it records the notification when any channel will deliver (listed
// in the inbox only when in-app is one of them),
// schedules the push when push is wanted and on, and schedules the e-mail when
// e-mail is wanted and on. An account that wants none of them gets nothing —
// no record either — and a zero Result.
//
// Every write joins tx, so the notification is delivered if and only if tx
// commits. A failure is returned and leaves tx aborted, as any failed
// statement does; a caller that must survive one (a registration notifying
// several administrators) runs Deliver under a savepoint. An account without
// an e-mail address is not mailed — the other channels still deliver. A message
// without a project is notification.ErrNoProject and delivers nothing.
func (d *Deliverer) Deliver(ctx context.Context, tx pgx.Tx, userUID string, msg Message) (Result, error) {
	to, ok, err := d.admit(ctx, tx, userUID, msg)
	if err != nil || !ok {
		return Result{}, err
	}
	channels, err := d.ChannelsTx(ctx, tx, userUID, msg.Kind)
	if err != nil {
		return Result{}, err
	}
	if !channels.Any() {
		d.log.DebugContext(ctx, "notification not delivered: no channel wanted",
			slog.String("user_uid", userUID), slog.String("kind", string(msg.Kind)))
		return Result{}, nil
	}
	p, record, err := d.record(ctx, tx, userUID, msg, channels.InApp)
	if err != nil {
		return Result{}, err
	}
	result := Result{Channels: channels, Notification: &record}
	if result.Devices, err = d.schedulePush(ctx, tx, channels.Push, to, p.Name, record); err != nil {
		return Result{}, err
	}
	result.Channels.Email = false
	if channels.Email && !msg.SkipMail {
		mailed, err := d.sendMail(ctx, tx, to, record, p.Name, msg.Mail)
		if err != nil {
			return Result{}, err
		}
		result.Channels.Email = mailed
	}
	return result, nil
}

// admit returns the addressee of msg for userUID and whether the account may
// receive it at all, read on tx: a message without a project is
// notification.ErrNoProject; an account out of standing in the project
// (internal/notifystanding) is not admitted — the drop is counted and logged,
// and is no error.
func (d *Deliverer) admit(
	ctx context.Context, tx pgx.Tx, userUID string, msg Message,
) (notifystanding.Addressee, bool, error) {
	if msg.ProjectUID == "" {
		return notifystanding.Addressee{}, false,
			fmt.Errorf("notifydeliver: delivering to %s: %w", userUID, notification.ErrNoProject)
	}
	to := notifystanding.Addressee{UserUID: userUID, ProjectUID: msg.ProjectUID, Kind: string(msg.Kind)}
	ok, err := d.standing(ctx, tx, to)
	if err != nil {
		return to, false, fmt.Errorf("notifydeliver: reading the standing of %s: %w", userUID, err)
	}
	if !ok {
		notifystanding.CountDrop(notifystanding.StageDeliver, notifystanding.ReasonStanding)
		d.log.DebugContext(ctx, "notification not delivered: the account is out of standing in the project",
			slog.String("user_uid", userUID), slog.String("project_uid", msg.ProjectUID),
			slog.String("kind", string(msg.Kind)))
	}
	return to, ok, nil
}

// schedulePush queues the push of record, titled with projectName, to the
// devices of to's account on tx, addressed to it, and returns how many devices
// it queued — none, without touching the queue, when push is not wanted.
func (d *Deliverer) schedulePush(
	ctx context.Context, tx pgx.Tx, wanted bool, to notifystanding.Addressee, projectName string,
	record notification.Notification,
) (int, error) {
	if !wanted {
		return 0, nil
	}
	devices, err := d.push.Enqueue(ctx, tx, to, push.Notification{
		Title: PushTitle(projectName, record.Title), Body: record.Body, URL: record.Link,
		Kind: string(record.Kind), Tag: record.UID,
	})
	if err != nil {
		return 0, fmt.Errorf("notifydeliver: scheduling the push of %s: %w", to.UserUID, err)
	}
	return devices, nil
}

// record loads msg's project and records msg for userUID on tx, its link put
// under the project's root and listed in the inbox when inApp. It returns the
// project too, whose name the push and the mail carry.
func (d *Deliverer) record(
	ctx context.Context, tx pgx.Tx, userUID string, msg Message, inApp bool,
) (*project.Project, notification.Notification, error) {
	p, err := d.projects.Get(ctx, msg.ProjectUID)
	if err != nil {
		return nil, notification.Notification{}, fmt.Errorf("notifydeliver: loading project %s: %w",
			msg.ProjectUID, err)
	}
	record, err := d.notes.CreateTx(ctx, tx, notification.New{
		ProjectUID: p.UID, ProjectSlug: p.Slug,
		UserUID: userUID, Kind: msg.Kind, Title: msg.Text.Title, Body: msg.Text.Body, Params: msg.Text.Params,
		Link: projectLink(p.Slug, msg.Link), SelfLink: msg.SelfLink, PhotoUIDs: msg.PhotoUIDs, InApp: inApp,
	})
	if err != nil {
		return nil, notification.Notification{}, fmt.Errorf("notifydeliver: recording the notification of %s: %w",
			userUID, err)
	}
	return p, record, nil
}

// PushTitle is a push's title naming the project it came from: "Veselice ·
// Označili vás na fotkách". The device's shade shows no address, so the name is
// the only way to tell two projects' notifications apart there. A blank name
// leaves the title as it is.
func PushTitle(projectName, title string) string {
	name := strings.TrimSpace(projectName)
	if name == "" {
		return title
	}
	return name + " · " + title
}

// projectLink returns link, a path inside the project, under the project's root
// /p/<slug>; an empty link stays empty.
func projectLink(slug, link string) string {
	if link == "" {
		return ""
	}
	return project.AppPath(slug, link)
}

// recipientSQL reads where an account's mail goes and whom it greets.
const recipientSQL = `SELECT email, display_name FROM users WHERE uid = $1`

// errNoAddress marks an account that has no e-mail address to mail.
var errNoAddress = errors.New("notifydeliver: the account has no e-mail address")

// sendMail schedules the e-mail of record on tx, built by build (nil means the
// generic notification mail), naming projectName and addressed to to, so the
// mail handler checks the account's standing again when it sends. It reports
// whether a mail was scheduled: an account without an address is skipped and
// logged, not failed.
func (d *Deliverer) sendMail(
	ctx context.Context, tx pgx.Tx, to notifystanding.Addressee, record notification.Notification,
	projectName string, build MailBuilder,
) (bool, error) {
	recipient, err := readRecipient(ctx, tx, record.UserUID)
	if errors.Is(err, errNoAddress) {
		d.log.InfoContext(ctx, "notification mail skipped: no address",
			slog.String("user_uid", record.UserUID), slog.String("kind", string(record.Kind)))
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if build == nil {
		build = GenericMail
	}
	mail := MailContext{URL: d.Absolute(record.Link), ProjectName: projectName}
	if err := d.mail.Enqueue(ctx, tx, build(recipient, record, mail).Addressed(to)); err != nil {
		return false, fmt.Errorf("notifydeliver: scheduling the mail of %s: %w", record.UserUID, err)
	}
	return true, nil
}

// readRecipient reads userUID's address and display name on tx, returning
// errNoAddress for an account with a blank address.
func readRecipient(ctx context.Context, tx pgx.Tx, userUID string) (Recipient, error) {
	var to Recipient
	if err := tx.QueryRow(ctx, recipientSQL, userUID).Scan(&to.Email, &to.DisplayName); err != nil {
		return Recipient{}, fmt.Errorf("notifydeliver: reading the address of %s: %w", userUID, err)
	}
	if strings.TrimSpace(to.Email) == "" {
		return Recipient{}, errNoAddress
	}
	return to, nil
}

// Absolute returns the in-app link under the configured public address
// (mail.base_url), or "" when there is no link or no address to put it under.
// It is what a MailBuilder receives as url; a producer whose one mail names
// several notifications (the tagging window's per-album summary) builds the
// other links with it.
func (d *Deliverer) Absolute(link string) string {
	if link == "" || d.baseURL == "" {
		return ""
	}
	return d.baseURL + link
}

// GenericMail is the e-mail of a kind without a template of its own: the
// notification's title, body and link, naming its project.
func GenericMail(to Recipient, n notification.Notification, mail MailContext) mailjob.Mail {
	return mailjob.Notification(to.Email, mailer.NotificationData{
		DisplayName: to.DisplayName, ProjectName: mail.ProjectName, Title: n.Title, Body: n.Body, URL: mail.URL,
	})
}
