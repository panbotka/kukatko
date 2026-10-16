package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
)

// The two ways a project's first admin can come to be, FirstAdmin.Account.
const (
	// FirstAdminCreated means a new account was created for the e-mail
	// address, with no password until its owner follows the set-password link.
	FirstAdminCreated = "created"
	// FirstAdminExisting means the address already belonged to an account,
	// which was made the project's admin as it is.
	FirstAdminExisting = "existing"
)

// projectSetupVia is the details.via marker of every audit entry a project set
// up for somebody writes, so the trail tells them apart from an administrator
// creating an account or issuing a reset link by hand.
const projectSetupVia = "project_setup"

// maxUsernameSuffix is how many numbered variants of a derived username
// ("jana", "jana2" … "jana99") are tried before giving up. An instance holds a
// family's handful of accounts, so running out means something is wrong.
const maxUsernameSuffix = 99

// derivedUsernameFallback is the username derived from an address whose local
// part keeps no usable character.
const derivedUsernameFallback = "admin"

var (
	// ErrEmailAmbiguous is returned when several accounts share the first
	// admin's e-mail address: making the wrong one the admin is worse than
	// making none, so the administrator must resolve it first.
	ErrEmailAmbiguous = errors.New("auth: several accounts share that e-mail address")
	// ErrPasswordAlreadySet is returned when a set-password link is asked for
	// an account that already has a password: its owner signs in with it, and
	// a reset is the administrator's other tool.
	ErrPasswordAlreadySet = errors.New("auth: the account already has a password")
	// errNoUsernameLeft is returned when every numbered variant of a derived
	// username is taken.
	errNoUsernameLeft = errors.New("auth: no free username derived from the e-mail address")
)

// FirstAdminInput is who a project is set up for: the e-mail address
// (required) and, for a new account, the display name and username it gets. An
// empty username is derived from the address's local part and made unique.
type FirstAdminInput struct {
	Email       string
	DisplayName string
	Username    string
}

// FirstAdmin is what the system administrator learns about the first admin of a
// project set up for somebody (or about a re-sent set-password link): which
// account it is, whether it was created or already existed, whether the mail
// was really scheduled and — whenever the account still has no password — the
// set-password link itself, so it can be passed on by hand when the mail was
// not sent (mail off, placeholder address), exactly as a reset link is.
type FirstAdmin struct {
	UserUID     string `json:"user_uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	// Account is FirstAdminCreated or FirstAdminExisting.
	Account string `json:"account"`
	// MailSent is true when the message was scheduled for delivery.
	MailSent bool `json:"mail_sent"`
	// SetPasswordURL is the one-time link to choose the first password; empty
	// for an account that already has one.
	SetPasswordURL string `json:"set_password_url,omitempty"`
	// ExpiresAt is when SetPasswordURL stops working.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SetupMail schedules the first admin's message and tells whether it would
// really be delivered. It is satisfied by *mailjob.Enqueuer.
type SetupMail interface {
	MailScheduler
	// Delivers reports whether a message to `to` is really scheduled (mail is
	// on and the address is deliverable).
	Delivers(to string) bool
}

// noSetupMail is the SetupMail of a flow built without one: it schedules
// nothing and says so.
type noSetupMail struct{ noMail }

// Delivers reports false: nothing is ever sent.
func (noSetupMail) Delivers(string) bool { return false }

// ProjectSetupConfig bundles what NewProjectSetup needs.
type ProjectSetupConfig struct {
	// Service supplies the account store and the clock (required).
	Service *Service
	// Mail schedules the first admin's message; nil sends nothing.
	Mail SetupMail
	// LinkBase is the address of the set-password page (the password-reset
	// page, see PasswordResetConfig.LinkBase); empty is the site-relative path.
	LinkBase string
	// ProjectURL is the absolute address of a project's page for its slug;
	// nil (or an empty answer) is the site-relative /p/<slug>/.
	ProjectURL func(slug string) string
	// TTL overrides how long a set-password link lasts; zero or less means
	// PasswordResetTTL, the reset link's validity.
	TTL time.Duration
}

// ProjectSetup is the system administrator's "set up a project for somebody"
// flow: one transaction creates the project, the account of its first admin
// (with no password) or finds the one that address already has, makes it the
// project's only member, mints the one-time link to choose a password and
// schedules the mail carrying it. The administrator who does it does not
// become a member.
//
// It lives in auth rather than project because it creates accounts and mints
// password links, which are this package's; the project half is
// project.CreateTx on the same transaction.
type ProjectSetup struct {
	svc        *Service
	mail       SetupMail
	linkBase   string
	projectURL func(slug string) string
	ttl        time.Duration
}

// NewProjectSetup returns a ProjectSetup from cfg, defaulting Mail to one that
// sends nothing, LinkBase to the site-relative reset path and TTL to
// PasswordResetTTL.
func NewProjectSetup(cfg ProjectSetupConfig) *ProjectSetup {
	mail := cfg.Mail
	if mail == nil {
		mail = noSetupMail{}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.LinkBase), "/")
	if base == "" {
		base = passwordResetPath
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = PasswordResetTTL
	}
	return &ProjectSetup{svc: cfg.Service, mail: mail, linkBase: base, projectURL: cfg.ProjectURL, ttl: ttl}
}

// Create sets the project p up for the person in: it creates the project with
// that person as its only, approved admin — a new account (instance role user,
// no password) when no account has the address, the existing one otherwise —
// and, while the account has no password, a set-password link mailed in the
// project-setup wording; an existing account with a password is mailed the
// project's link instead. Everything, the audit entries (user.create,
// project.create, project.member.add, user.password_reset) and the mail job
// included, commits in one transaction, so a failure anywhere leaves neither a
// project without an admin nor an orphan account.
//
// p.CreatorUID is ignored: the caller does not become a member. It returns
// ErrInvalidEmail, ErrUsernameRequired/ErrUsernameTooLong or ErrUsernameTaken
// for the person, ErrEmailAmbiguous when several accounts share the address,
// ErrUserDisabled when the one that has it is blocked, and project.Create's
// errors for the project.
func (ps *ProjectSetup) Create(
	ctx context.Context, meta audit.Meta, p project.CreateParams, in FirstAdminInput,
) (*project.Project, FirstAdmin, error) {
	in, err := normalizeFirstAdmin(in)
	if err != nil {
		return nil, FirstAdmin{}, err
	}
	var created *project.Project
	var admin FirstAdmin
	err = ps.svc.store.inTx(ctx, func(tx pgx.Tx) error {
		account, outcome, err := ps.resolveFirstAdmin(ctx, tx, meta, in)
		if err != nil {
			return err
		}
		p.CreatorUID = account.UID
		if created, err = project.CreateTx(ctx, tx, meta, p); err != nil {
			// Unwrapped on purpose: the project's refusals (slug taken, the slug
			// rule) reach the API as they are, the rule text included.
			return err //nolint:wrapcheck // see above
		}
		admin, err = ps.welcome(ctx, tx, meta, account, *created)
		admin.Account = outcome
		return err
	})
	if err != nil {
		return nil, FirstAdmin{}, err
	}
	admin.MailSent = ps.mail.Delivers(admin.Email)
	return created, admin, nil
}

// ResendLink issues a fresh set-password link to the member userUID of the
// project projectUID, whose account still has no password, and mails it in the
// project-setup wording — the earlier link stops working. It is audited
// (user.password_reset) on the same transaction as the link and the mail. It
// returns project.ErrNotFound, ErrUserNotFound, project.ErrNotMember,
// ErrUserDisabled or ErrPasswordAlreadySet.
func (ps *ProjectSetup) ResendLink(
	ctx context.Context, meta audit.Meta, projectUID, userUID string,
) (FirstAdmin, error) {
	var admin FirstAdmin
	err := ps.svc.store.inTx(ctx, func(tx pgx.Tx) error {
		proj, err := readProjectTx(ctx, tx, projectUID)
		if err != nil {
			return err
		}
		account, err := lockUser(ctx, tx, userUID)
		if err != nil {
			return err
		}
		if err := checkAwaitingMember(ctx, tx, proj.UID, account); err != nil {
			return err
		}
		admin, err = ps.welcome(ctx, tx, meta, account, proj)
		return err
	})
	if err != nil {
		return FirstAdmin{}, err
	}
	admin.Account = FirstAdminExisting
	admin.MailSent = ps.mail.Delivers(admin.Email)
	return admin, nil
}

// AwaitingPassword lists the members of the project projectUID whose accounts
// still have no password, each with the expiry of its outstanding link (nil
// when none is left), for the administrator's "send the link again".
func (ps *ProjectSetup) AwaitingPassword(ctx context.Context, projectUID string) ([]AwaitingMember, error) {
	return ps.svc.store.AwaitingPassword(ctx, projectUID, ps.svc.now())
}

// normalizeFirstAdmin trims and validates in: the address is normalized and
// must be valid (ErrInvalidEmail), a given username is normalized and must be
// a valid one (ErrUsernameTooLong).
func normalizeFirstAdmin(in FirstAdminInput) (FirstAdminInput, error) {
	in.Email = normalizeEmail(in.Email)
	if err := validateEmail(in.Email); err != nil {
		return FirstAdminInput{}, err
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Username = normalizeUsername(in.Username)
	if in.Username != "" {
		if err := validateAccountUsername(in.Username); err != nil {
			return FirstAdminInput{}, err
		}
	}
	return in, nil
}

// resolveFirstAdmin returns the account the project is set up for, locked on
// tx, and whether it was created (FirstAdminCreated) or found
// (FirstAdminExisting). A new account is inserted with no password and audited
// as user.create.
func (ps *ProjectSetup) resolveFirstAdmin(
	ctx context.Context, tx pgx.Tx, meta audit.Meta, in FirstAdminInput,
) (User, string, error) {
	found, err := lockAccountsByEmail(ctx, tx, in.Email)
	if err != nil {
		return User{}, "", err
	}
	switch {
	case len(found) > 1:
		return User{}, "", ErrEmailAmbiguous
	case len(found) == 1 && found[0].Disabled:
		return User{}, "", ErrUserDisabled
	case len(found) == 1:
		return found[0], FirstAdminExisting, nil
	}
	account, err := ps.newFirstAdmin(ctx, tx, in)
	if err != nil {
		return User{}, "", err
	}
	if err := insertUser(ctx, tx, account); err != nil {
		return User{}, "", err
	}
	entry := meta.Entry(audit.ActionUserCreate, userTargetType, account.UID, map[string]any{
		"username": account.Username, "instance_role": string(account.Role), "via": projectSetupVia,
	})
	if err := audit.Write(ctx, tx, entry); err != nil {
		return User{}, "", fmt.Errorf("auth: writing the account's audit entry: %w", err)
	}
	return account, FirstAdminCreated, nil
}

// newFirstAdmin builds the account a project is set up for: a plain user with
// in's address and display name, no password (it cannot sign in until its
// owner chooses one) and the username in names or, when none, one derived from
// the address and made unique on tx. A given username already taken is
// ErrUsernameTaken — silently renaming what the administrator typed would hand
// out a name they do not know.
func (ps *ProjectSetup) newFirstAdmin(ctx context.Context, tx pgx.Tx, in FirstAdminInput) (User, error) {
	username := in.Username
	if username != "" {
		taken, err := usernameTaken(ctx, tx, username)
		if err != nil {
			return User{}, err
		}
		if taken {
			return User{}, ErrUsernameTaken
		}
	} else {
		var err error
		if username, err = freeUsername(ctx, tx, derivedUsername(in.Email)); err != nil {
			return User{}, err
		}
	}
	uid, err := newUserUID()
	if err != nil {
		return User{}, err
	}
	return User{
		UID: uid, Username: username, DisplayName: in.DisplayName, Email: in.Email,
		Role: RoleUser, ApprovedAt: ps.svc.approvedNow(),
	}, nil
}

// derivedUsername is the username an address's local part suggests: lower
// case, only letters, digits, dots, hyphens and underscores, no separator at
// either end, short enough to take a numeric suffix — or
// derivedUsernameFallback when nothing usable is left.
func derivedUsername(email string) string {
	local := email
	if at := strings.LastIndex(email, "@"); at >= 0 {
		local = email[:at]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if usernameRune(r) {
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), ".-_")
	// Only ASCII is left, so bytes are runes; two digits of room for the suffix.
	if limit := MaxUsernameLen - 2; len(name) > limit {
		name = strings.TrimRight(name[:limit], ".-_")
	}
	if name == "" {
		return derivedUsernameFallback
	}
	return name
}

// usernameRune reports whether r may appear in a derived username: an ASCII
// letter or digit, a dot, a hyphen or an underscore.
func usernameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune(".-_", r)
}

// freeUsername returns base, or base followed by the smallest number from 2
// that no account has taken yet, checked on tx. It returns errNoUsernameLeft
// past maxUsernameSuffix.
func freeUsername(ctx context.Context, tx pgx.Tx, base string) (string, error) {
	for n := 1; n <= maxUsernameSuffix; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s%d", base, n)
		}
		taken, err := usernameTaken(ctx, tx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: %q", errNoUsernameLeft, base)
}

// welcome mails account its way into the project proj on tx and returns what
// the administrator learns about it: while the account has no password, a
// fresh set-password link (audited, replacing any earlier one) in the
// project-setup mail; otherwise the mail announcing the project. Account and
// MailSent are the caller's to fill in.
func (ps *ProjectSetup) welcome(
	ctx context.Context, tx pgx.Tx, meta audit.Meta, account User, proj project.Project,
) (FirstAdmin, error) {
	admin := FirstAdmin{
		UserUID: account.UID, Username: account.Username, DisplayName: account.DisplayName, Email: account.Email,
	}
	if account.HasPassword() {
		m := mailjob.ProjectAdminAdded(account.Email, mailer.ProjectAdminAddedData{
			DisplayName: account.DisplayName, ProjectName: proj.Name, Username: account.Username,
			ProjectURL: ps.projectLink(proj.Slug),
		})
		return admin, ps.enqueue(ctx, tx, m)
	}
	token, row, err := mintPasswordReset(account.UID, meta.ActorUID, ps.svc.now(), ps.ttl)
	if err != nil {
		return FirstAdmin{}, err
	}
	if err := replacePasswordReset(ctx, tx, row); err != nil {
		return FirstAdmin{}, err
	}
	details := withResetDetails(map[string]any{"via": projectSetupVia, "project_uid": proj.UID}, row)
	entry := meta.Entry(audit.ActionUserPasswordReset, userTargetType, account.UID, details)
	if err := audit.Write(ctx, tx, entry); err != nil {
		return FirstAdmin{}, fmt.Errorf("auth: writing the set-password link's audit entry: %w", err)
	}
	admin.SetPasswordURL = ps.link(token, proj.Slug)
	admin.ExpiresAt = &row.ExpiresAt
	m := mailjob.ProjectSetup(account.Email, mailer.ProjectSetupData{
		DisplayName: account.DisplayName, ProjectName: proj.Name, Username: account.Username,
		SetPasswordURL: admin.SetPasswordURL, ValidFor: ps.ttl,
	})
	return admin, ps.enqueue(ctx, tx, m)
}

// enqueue schedules m on tx; a message that will not schedule fails the whole
// setup, as a reset's does — nothing has been promised to anybody yet.
func (ps *ProjectSetup) enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error {
	if err := ps.mail.Enqueue(ctx, exec, m); err != nil {
		return fmt.Errorf("auth: scheduling the %s mail: %w", m.Template, err)
	}
	return nil
}

// link is the set-password address of token. The project's slug rides along
// as ?project=, so the page can send its reader on into the project once the
// password is set; it is a navigation hint only — the token alone decides.
func (ps *ProjectSetup) link(token, slug string) string {
	return ps.linkBase + "/" + token + "?project=" + url.QueryEscape(slug)
}

// projectLink is the address of the project slug's page.
func (ps *ProjectSetup) projectLink(slug string) string {
	if ps.projectURL != nil {
		if u := ps.projectURL(slug); u != "" {
			return u
		}
	}
	return "/p/" + url.PathEscape(slug) + "/"
}
