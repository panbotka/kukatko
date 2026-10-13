package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/settings"
)

// ApprovalPath is the frontend route where an administrator approves a waiting
// account — the user administration screen (web/src/App.tsx, UsersPage). The
// administrators' notification opens it, and their mail links to it (the
// caller builds the absolute address, see RegistrationConfig.ApprovalURL).
const ApprovalPath = "/users"

// SettingsSource reads the instance settings self-service registration depends
// on: whether it is open at all and the shared secret it asks for. It is an
// interface rather than *settings.Store so this package stays testable without a
// settings row, and so the dependency reads as "registration consults the
// instance settings" rather than "auth owns them".
type SettingsSource interface {
	// Get returns the project's settings, or the defaults when none are stored.
	Get(ctx context.Context, projectUID string) (settings.Settings, error)
}

// MailScheduler schedules one message for delivery through a caller-supplied
// executor, which is how a mail joins the transaction of the change that caused
// it. It is satisfied by *mailjob.Enqueuer.
type MailScheduler interface {
	// Enqueue schedules m using exec, a pool or an open transaction.
	Enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error
}

// noMail is the scheduler a flow built without one uses (Rename,
// PasswordReset): it schedules nothing and reports success, so an instance that
// wires no mail still performs the action. It is a value rather than a nil
// check at every call site, so the mailing path has exactly one shape.
type noMail struct{}

// Enqueue discards the message and reports success.
func (noMail) Enqueue(context.Context, jobs.Execer, mailjob.Mail) error { return nil }

// Notifier delivers one notification to the channels an account wants — the
// inbox, its devices, its e-mail — on a caller-supplied transaction, which is
// how the administrators' notice joins the transaction of the registration that
// caused it. It is satisfied by *notifydeliver.Deliverer.
type Notifier interface {
	// Deliver records msg for userUID and fans it out to the wanted channels,
	// all on tx.
	Deliver(ctx context.Context, tx pgx.Tx, userUID string, msg notifydeliver.Message) (notifydeliver.Result, error)
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
	// UploadLink is the short code of an upload link the person uploaded
	// through. When set it stands in for Secret (see RegisterWithLink); the
	// handler resolves it, the service never reads it.
	UploadLink string
}

// UploadLinkGate lets a live upload link stand in for the shared registration
// secret: somebody who just uploaded through a link a curator posted is somebody
// the community told. It is an interface so auth stays ignorant of upload links
// (internal/uploadlinkapi implements it).
type UploadLinkGate interface {
	// AdmitRegistration resolves code, read from r's body by the caller, to a
	// live link and returns the grant; it returns ErrRegistrationLink when code
	// names no live link. r is passed so the gate can read the anonymous
	// uploader's session cookie, which is what decides the photos to claim.
	AdmitRegistration(ctx context.Context, r *http.Request, code string) (LinkGrant, error)
}

// LinkGrant is an upload link admitted in place of the secret.
type LinkGrant struct {
	// LinkUID names the link, for the audit entry.
	LinkUID string
	// ProjectUID is the link's project: its registration settings decide
	// whether the door is open, the account becomes a member of it (a viewer,
	// unapproved — the same approval flow as any registration) and of no other,
	// and its admins are the ones told somebody is waiting. Empty is the
	// bootstrap project.
	ProjectUID string
	// Claim runs on the account's transaction once the account row exists: it
	// hands the photos the registering browser uploaded through links to the new
	// account, so they are claimed if and only if the account commits. Nil claims
	// nothing.
	Claim func(ctx context.Context, tx pgx.Tx, userUID string) error
	// Committed runs after the account committed — the place for follow-up work
	// that must not join the transaction (rescheduling the claimed photos'
	// sidecars). Nil does nothing.
	Committed func(ctx context.Context)
}

// RegistrationConfig bundles what NewRegistration needs.
type RegistrationConfig struct {
	// Service is the auth domain service whose validation, hashing and store the
	// registration reuses (required).
	Service *Service
	// Settings reads whether registration is open and the shared secret
	// (required).
	Settings SettingsSource
	// Mail schedules the person's own confirmation (required).
	Mail MailScheduler
	// ApprovalURL is the absolute address of the approval screen
	// (ApprovalPath under the instance's public URL), the last line of the
	// administrators' mail. Empty leaves the link out.
	ApprovalURL string
	// Notify delivers the administrators' "a registration is waiting"
	// notification to whichever channels each of them opted into. It is
	// optional: without it a registration notifies nobody but the person
	// registering.
	Notify Notifier
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
	svc         *Service
	settings    SettingsSource
	mail        MailScheduler
	approvalURL string
	notify      Notifier
	log         *slog.Logger
}

// NewRegistration returns a Registration from cfg, defaulting Logger to
// slog.Default().
func NewRegistration(cfg RegistrationConfig) *Registration {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Registration{
		svc: cfg.Service, settings: cfg.Settings, mail: cfg.Mail, approvalURL: cfg.ApprovalURL,
		notify: cfg.Notify, log: log,
	}
}

// Register creates the account described by in and returns it, unapproved.
//
// The account, the audit entry (entry, whose actor and target are stamped with
// the new account — nobody else was involved), the confirmation mail and the
// administrators' notifications are written on one transaction, so a
// registration that fails at any point leaves neither an account, nor a trail
// entry, nor a mail, an inbox entry or a push anybody will receive.
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
	return rg.register(ctx, registrationProject(ctx), in, entry, nil)
}

// RegisterWithLink creates the account described by in exactly as Register
// does — viewer role, unapproved, the same notifications and audit entry — but
// with grant, an upload link the caller already admitted, in place of the
// shared secret. Only the secret check is replaced: registration must still be
// switched on, though it may be on without a secret, because the link is the
// lock. Everything happens in the link's project (grant.ProjectUID): its
// registration switch, the new membership and the approvers told about it. The
// entry's details name the link, and grant.Claim runs on the account's
// transaction so the photos the person uploaded become theirs atomically.
//
// It returns ErrRegistrationClosed when registration is switched off, and the
// same account errors as Register.
func (rg *Registration) RegisterWithLink(
	ctx context.Context, in RegisterInput, grant LinkGrant, entry audit.Entry,
) (User, error) {
	projectUID := grant.ProjectUID
	if projectUID == "" {
		projectUID = registrationProject(ctx)
	}
	stored, err := rg.settings.Get(ctx, projectUID)
	if err != nil {
		return User{}, fmt.Errorf("auth: reading the registration settings: %w", err)
	}
	if !stored.RegistrationEnabled {
		return User{}, ErrRegistrationClosed
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["upload_link"] = grant.LinkUID
	user, err := rg.register(ctx, projectUID, in, entry, grant.Claim)
	if err != nil {
		return User{}, err
	}
	if grant.Committed != nil {
		grant.Committed(ctx)
	}
	return user, nil
}

// register is the account creation both registration paths share once their
// gate let the request through: the account joins projectUID, unapproved.
// claim, when not nil, runs on the account's transaction after the account row
// exists.
func (rg *Registration) register(
	ctx context.Context, projectUID string, in RegisterInput, entry audit.Entry,
	claim func(ctx context.Context, tx pgx.Tx, userUID string) error,
) (User, error) {
	user, err := rg.svc.prepareRegistration(CreateUserInput{
		Username:    in.Username,
		Password:    in.Password,
		DisplayName: in.DisplayName,
		Email:       in.Email,
		// A plain user and the lowest project role there is, in a membership
		// nobody has approved yet. An administrator who wants this person to
		// edit or govern anything raises it when they approve the account;
		// registration itself hands out nothing.
		Role: AccountViewer,
	})
	if err != nil {
		return User{}, err
	}
	user.joinProject = projectUID

	// Read before the transaction opens: the recipients are unrelated rows and
	// holding them under the insert would only lengthen it. A failure here costs
	// the notification, not the registration.
	recipients := rg.approvalRecipients(ctx, projectUID)

	entry.ActorUID = user.UID
	entry.TargetUID = user.UID
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["username"] = user.Username
	entry.Details["role"] = string(user.AccountRole)
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}

	if err := rg.svc.store.CreateUserAuditedWith(ctx, user, entry,
		func(ctx context.Context, tx pgx.Tx) error {
			if claim != nil {
				if err := claim(ctx, tx, user.UID); err != nil {
					return fmt.Errorf("auth: claiming the uploaded photos: %w", err)
				}
			}
			if err := rg.scheduleConfirmation(ctx, tx, user); err != nil {
				return err
			}
			rg.notifyApprovers(ctx, tx, projectUID, user, recipients)
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
	stored, err := rg.settings.Get(ctx, registrationProject(ctx))
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
func (rg *Registration) approvalRecipients(ctx context.Context, projectUID string) []User {
	recipients, err := rg.svc.store.ListApprovalRecipients(ctx, projectUID)
	if err != nil {
		rg.log.WarnContext(ctx, "registration: could not read the accounts to notify",
			slog.String("error", err.Error()))
		return nil
	}
	return recipients
}

// scheduleConfirmation enqueues the person's own confirmation on tx, so it is
// scheduled if and only if the account commits. It is the one message that
// must not be lost: failing to schedule it fails the registration, and they are
// better off retrying than holding an account they were never told about.
func (rg *Registration) scheduleConfirmation(ctx context.Context, tx pgx.Tx, user User) error {
	if err := rg.mail.Enqueue(ctx, tx, mailjob.RegistrationReceived(user.Email,
		mailer.RegistrationReceivedData{DisplayName: user.DisplayName, Username: user.Username},
	)); err != nil {
		return fmt.Errorf("auth: scheduling the registration confirmation: %w", err)
	}
	return nil
}

// notifyApprovers delivers the "a registration is waiting" notification to
// every recipient, on tx, through whichever channels each of them opted into
// (in-app, push, e-mail — all off unless chosen), so it exists if and only if
// the account commits.
//
// It is best-effort, the opposite of the confirmation: nobody registering can
// do anything about an administrator whose delivery fails, so a recipient whose
// notification will not schedule is logged and skipped, and the registration
// goes on. Each delivery runs under a savepoint of tx — a statement that fails
// inside a PostgreSQL transaction aborts all of it, so without one a single
// broken notification would take the account down with it. A Registration
// built without a Notifier notifies nobody.
func (rg *Registration) notifyApprovers(
	ctx context.Context, tx pgx.Tx, projectUID string, user User, recipients []User,
) {
	if rg.notify == nil {
		return
	}
	msg := rg.pendingMessage(user)
	// The notification lands in the inbox of the project the registration is
	// for, and its approval link opens there.
	msg.ProjectUID = projectUID
	for _, recipient := range recipients {
		err := inSavepoint(ctx, tx, func(savepoint pgx.Tx) error {
			if _, err := rg.notify.Deliver(ctx, savepoint, recipient.UID, msg); err != nil {
				return fmt.Errorf("auth: delivering the notification: %w", err)
			}
			return nil
		})
		if err != nil {
			rg.log.WarnContext(ctx, "registration: could not notify an administrator",
				slog.String("recipient_uid", recipient.UID), slog.String("error", err.Error()))
		}
	}
}

// pendingMessage is the administrators' notification about user: the short
// text the inbox and the push show, opening the approval screen, and the
// detailed mail naming the registrant's username and address.
func (rg *Registration) pendingMessage(user User) notifydeliver.Message {
	pending := mailer.NewRegistrationPendingData{
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		ApprovalURL: rg.approvalURL,
	}
	return notifydeliver.Message{
		Kind: notification.KindRegistrationPending,
		Text: notification.RenderRegistrationPending(notification.RegistrationPendingData{
			DisplayName: user.DisplayName, Username: user.Username,
		}),
		Link: ApprovalPath,
		Mail: func(to notifydeliver.Recipient, _ notification.Notification, _ string) mailjob.Mail {
			return mailjob.NewRegistrationPending(to.Email, pending)
		},
	}
}

// inSavepoint runs fn on a savepoint of tx and releases it when fn returns nil;
// on an error, fn's writes are rolled back and tx stays usable — the reason a
// best-effort step runs under one at all. It returns fn's error, or the failure
// to open or release the savepoint.
func inSavepoint(ctx context.Context, tx pgx.Tx, fn func(savepoint pgx.Tx) error) error {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: opening a savepoint: %w", err)
	}
	// After a commit the rollback is a no-op; on every early return it undoes
	// whatever the savepoint wrote.
	defer func() { _ = savepoint.Rollback(ctx) }()
	if err := fn(savepoint); err != nil {
		return err
	}
	if err := savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("auth: releasing the savepoint: %w", err)
	}
	return nil
}

// registrationProject returns the project whose settings govern a registration.
// Registration does not name a project yet, so it is always the bootstrap
// project, which holds the whole library.
//
//nolint:godox // the marker below is swept for at the end of the multi-tenancy programme
func registrationProject(context.Context) string {
	// TODO multi-tenancy: project from context
	return project.BootstrapUID
}
