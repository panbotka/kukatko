package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/settings"
)

// approvalPath is the frontend route where an administrator approves a waiting
// account — the user administration screen (web/src/App.tsx, UsersPage). The
// administrators' push notification opens it.
const approvalPath = "/users"

// SettingsSource reads the instance settings self-service registration depends
// on: whether it is open at all and the shared secret it asks for. It is an
// interface rather than *settings.Store so this package stays testable without a
// settings row, and so the dependency reads as "registration consults the
// instance settings" rather than "auth owns them".
type SettingsSource interface {
	// Get returns the instance settings, or the defaults when none are stored.
	Get(ctx context.Context) (settings.Settings, error)
}

// MailScheduler schedules one message for delivery through a caller-supplied
// executor, which is how a mail joins the transaction of the change that caused
// it. It is satisfied by *mailjob.Enqueuer.
type MailScheduler interface {
	// Enqueue schedules m using exec, a pool or an open transaction.
	Enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error
}

// NotificationRecorder decides whether an account wants a kind of notification
// and records one, both on the caller's transaction, so the notification shares
// the fate of the change that caused it. It is satisfied by *notification.Store.
type NotificationRecorder interface {
	// WantsTx reports whether userUID wants notifications of kind, read on tx.
	WantsTx(ctx context.Context, tx pgx.Tx, userUID string, kind notification.Kind) (bool, error)
	// CreateTx records n on tx and returns the stored notification.
	CreateTx(ctx context.Context, tx pgx.Tx, n notification.New) (notification.Notification, error)
}

// PushScheduler schedules one push notification to every device of an account
// through a caller-supplied executor, which is how a push joins the transaction
// of the change that caused it. It is satisfied by *pushjob.Enqueuer, which
// itself does nothing when push is switched off or the account has no device.
type PushScheduler interface {
	// Enqueue schedules n for userUID's devices using exec and returns how many
	// deliveries it queued.
	Enqueue(ctx context.Context, exec pushjob.Execer, userUID string, n push.Notification) (int, error)
}

// RegisterInput is one self-service registration: the account somebody asks for,
// plus the shared secret that lets them ask at all.
type RegisterInput struct {
	// Username is the account name, normalized and validated exactly as the
	// admin user API does it.
	Username string
	// DisplayName is the name shown in the interface; it may be empty.
	DisplayName string
	// Email is where the confirmation goes; a valid address is required.
	Email string
	// Password is the plaintext password, hashed by the same path as any other
	// account's.
	Password string
	// Secret is the shared registration secret as typed. Surrounding whitespace
	// is ignored, because the stored secret is trimmed too.
	Secret string
}

// RegistrationConfig bundles what NewRegistration needs.
type RegistrationConfig struct {
	// Service is the auth domain service whose validation, hashing and store the
	// registration reuses (required).
	Service *Service
	// Settings reads whether registration is open and the shared secret
	// (required).
	Settings SettingsSource
	// Mail schedules the two messages a registration sends (required).
	Mail MailScheduler
	// Notifications records the administrators' "a registration is waiting"
	// notification. Together with Push it is optional: with either nil a
	// registration notifies by mail only.
	Notifications NotificationRecorder
	// Push schedules the delivery of that notification to the administrators'
	// devices.
	Push PushScheduler
	// Logger records the notifications that could not be scheduled; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// Registration is the self-service registration flow: somebody who knows the
// instance's shared secret creates their own account, which exists but cannot be
// used until an administrator approves it.
//
// It is a separate type rather than more methods on Service because it is the
// only part of auth that consults the instance settings and sends mail, and
// because an instance may run without it — an API built with no Registration
// simply answers "registration is not open".
type Registration struct {
	svc      *Service
	settings SettingsSource
	mail     MailScheduler
	notes    NotificationRecorder
	push     PushScheduler
	log      *slog.Logger
}

// NewRegistration returns a Registration from cfg, defaulting Logger to
// slog.Default().
func NewRegistration(cfg RegistrationConfig) *Registration {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Registration{
		svc: cfg.Service, settings: cfg.Settings, mail: cfg.Mail,
		notes: cfg.Notifications, push: cfg.Push, log: log,
	}
}

// Register creates the account described by in and returns it, unapproved.
//
// The account, the audit entry (entry, whose actor and target are stamped with
// the new account — nobody else was involved), both notification mails and the
// administrators' push notifications are written on one transaction, so a
// registration that fails at any point leaves neither an account, nor a trail
// entry, nor a mail or a push anybody will receive.
//
// It returns ErrRegistrationClosed when registration is switched off — or is
// switched on with a blank secret, which is the same refusal — ErrRegistrationSecret
// for a wrong secret, ErrUsernameTaken for a name somebody already holds, and
// the validation errors of the admin user API (ErrUsernameTooLong,
// ErrInvalidEmail, ErrPasswordTooShort) for input it will not store.
func (rg *Registration) Register(ctx context.Context, in RegisterInput, entry audit.Entry) (User, error) {
	if err := rg.checkSecret(ctx, in.Secret); err != nil {
		return User{}, err
	}
	user, err := rg.svc.prepareRegistration(CreateUserInput{
		Username:    in.Username,
		Password:    in.Password,
		DisplayName: in.DisplayName,
		Email:       in.Email,
		// The lowest role there is. An administrator who wants this person to
		// edit or govern anything raises it when they approve the account;
		// registration itself hands out nothing.
		Role: RoleViewer,
	})
	if err != nil {
		return User{}, err
	}

	// Read before the transaction opens: the recipients are unrelated rows and
	// holding them under the insert would only lengthen it. A failure here costs
	// the notification, not the registration.
	recipients := rg.approvalRecipients(ctx)

	entry.ActorUID = user.UID
	entry.TargetUID = user.UID
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["username"] = user.Username
	entry.Details["role"] = string(user.Role)

	if err := rg.svc.store.CreateUserAuditedWith(ctx, user, entry,
		func(ctx context.Context, tx pgx.Tx) error {
			if err := rg.scheduleMail(ctx, tx, user, recipients); err != nil {
				return err
			}
			rg.schedulePush(ctx, tx, user, recipients)
			return nil
		}); err != nil {
		return User{}, err
	}
	return rg.svc.store.GetUserByUID(ctx, user.UID)
}

// checkSecret decides whether a registration carrying secret may proceed at all.
// It refuses with ErrRegistrationClosed when registration is switched off, and
// equally when it is on but the stored secret is blank: an open door with no
// lock is never what the administrator meant, and answering anything else would
// mean accepting every stranger who sends an empty string.
//
// The comparison is constant-time, so the time an answer takes says nothing
// about how much of the secret was right — the one thing that would turn a
// guessing game into a search.
func (rg *Registration) checkSecret(ctx context.Context, secret string) error {
	stored, err := rg.settings.Get(ctx)
	if err != nil {
		return fmt.Errorf("auth: reading the registration settings: %w", err)
	}
	want := strings.TrimSpace(stored.RegistrationSecret)
	if !stored.RegistrationEnabled || want == "" {
		return ErrRegistrationClosed
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(secret)), []byte(want)) != 1 {
		return ErrRegistrationSecret
	}
	return nil
}

// approvalRecipients returns the accounts to tell about a pending registration,
// or none when they cannot be read. The notification is best-effort by design —
// the person's own confirmation is what matters, and an instance with no
// reachable administrator must still accept registrations — so a failure here is
// logged and the registration goes on.
func (rg *Registration) approvalRecipients(ctx context.Context) []User {
	recipients, err := rg.svc.store.ListApprovalRecipients(ctx)
	if err != nil {
		rg.log.WarnContext(ctx, "registration: could not read the accounts to notify",
			slog.String("error", err.Error()))
		return nil
	}
	return recipients
}

// scheduleMail enqueues the two messages a registration sends, on tx, so they
// are scheduled if and only if the account commits.
//
// The person's own confirmation is the one that must not be lost: failing to
// schedule it fails the registration, and they are better off retrying than
// holding an account they were never told about. The administrators' notice is
// the opposite — nobody registering can do anything about an address that is
// unreachable — so a message that will not schedule is logged and the rest go
// out.
func (rg *Registration) scheduleMail(ctx context.Context, tx pgx.Tx, user User, recipients []User) error {
	if err := rg.mail.Enqueue(ctx, tx, mailjob.RegistrationReceived(user.Email,
		mailer.RegistrationReceivedData{DisplayName: user.DisplayName, Username: user.Username},
	)); err != nil {
		return fmt.Errorf("auth: scheduling the registration confirmation: %w", err)
	}
	pending := mailer.NewRegistrationPendingData{
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
	}
	for _, recipient := range recipients {
		if err := rg.mail.Enqueue(ctx, tx, mailjob.NewRegistrationPending(recipient.Email, pending)); err != nil {
			rg.log.WarnContext(ctx, "registration: could not notify an administrator",
				slog.String("recipient_uid", recipient.UID), slog.String("error", err.Error()))
		}
	}
	return nil
}

// schedulePush records a "registration pending" notification for every
// recipient who has not turned that kind off and schedules its delivery to
// their devices, on tx, so both exist if and only if the account commits.
//
// It follows the administrators' mail: best-effort. A recipient whose
// notification will not schedule is logged and skipped, and the registration
// goes on. The choice is push-only — a recipient who turned the kind off still
// gets the mail, which scheduleMail sends regardless. A Registration built
// without a recorder or a scheduler sends no push at all.
func (rg *Registration) schedulePush(ctx context.Context, tx pgx.Tx, user User, recipients []User) {
	if rg.notes == nil || rg.push == nil {
		return
	}
	text := notification.RenderRegistrationPending(notification.RegistrationPendingData{
		DisplayName: user.DisplayName,
		Username:    user.Username,
	})
	for _, recipient := range recipients {
		if err := rg.notifyApprover(ctx, tx, recipient.UID, text); err != nil {
			rg.log.WarnContext(ctx, "registration: could not push to an administrator",
				slog.String("recipient_uid", recipient.UID), slog.String("error", err.Error()))
		}
	}
}

// notifyApprover records and schedules one administrator's notification under a
// savepoint of tx. The savepoint is what makes the failure survivable: a
// statement that fails inside a PostgreSQL transaction aborts all of it, so
// without one a single broken notification would take the account down with it.
// Rolled back to, it leaves neither a record without its delivery nor a
// delivery without its record. A recipient who does not want the kind gets
// nothing and nil.
func (rg *Registration) notifyApprover(
	ctx context.Context, tx pgx.Tx, recipientUID string, text notification.Text,
) error {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: opening a savepoint: %w", err)
	}
	// After a commit the rollback is a no-op; on every early return it undoes
	// whatever the savepoint wrote.
	defer func() { _ = savepoint.Rollback(ctx) }()

	kind := notification.KindRegistrationPending
	wants, err := rg.notes.WantsTx(ctx, savepoint, recipientUID, kind)
	if err != nil {
		return fmt.Errorf("auth: reading the notification preference: %w", err)
	}
	if !wants {
		return nil
	}
	record, err := rg.notes.CreateTx(ctx, savepoint, notification.New{
		UserUID: recipientUID, Kind: kind, Title: text.Title, Body: text.Body, Link: approvalPath,
	})
	if err != nil {
		return fmt.Errorf("auth: recording the notification: %w", err)
	}
	if _, err := rg.push.Enqueue(ctx, savepoint, recipientUID, push.Notification{
		Title: record.Title, Body: record.Body, URL: record.Link, Kind: string(kind), Tag: record.UID,
	}); err != nil {
		return fmt.Errorf("auth: scheduling the push: %w", err)
	}
	if err := savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("auth: releasing the savepoint: %w", err)
	}
	return nil
}
