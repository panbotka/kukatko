package auth

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/panbotka/kukatko/internal/audit"
)

// Sentinel errors of the passkey flow, so handlers and tests can branch with
// errors.Is.
var (
	// ErrPasskeysUnavailable indicates this instance has no relying party
	// configured, so no ceremony can be started. It is deliberately not a missing
	// route: a client must be able to tell "this instance does not offer
	// passkeys" from "this build does not know the word", and only the first is
	// something an operator can fix.
	ErrPasskeysUnavailable = errors.New("auth: passkeys are not available on this instance")
	// ErrPasskeyCeremony indicates the second half of a ceremony arrived without
	// the first: no ceremony cookie, an unknown one, or one that has expired.
	// Every one of those means the same thing to the person in front of the
	// browser — start again — so they share an error.
	ErrPasskeyCeremony = errors.New("auth: no passkey ceremony is in progress; start again")
	// ErrPasskeyRejected indicates the authenticator's answer did not verify:
	// a wrong challenge, a foreign origin, a signature that does not check out, a
	// credential this instance has never seen. Like ErrInvalidCredentials it is
	// deliberately unspecific — the whole point of a public sign-in endpoint is
	// that a failed attempt teaches the caller nothing.
	ErrPasskeyRejected = errors.New("auth: the authenticator's answer was refused")
	// ErrPasskeyNotFound indicates no passkey with that id belongs to the caller.
	ErrPasskeyNotFound = errors.New("auth: passkey not found")
	// ErrPasskeyAlreadyRegistered indicates the authenticator offered a
	// credential this instance already stores — the same key registered twice.
	ErrPasskeyAlreadyRegistered = errors.New("auth: this passkey is already registered")
	// ErrPasskeyNameTooLong indicates the name exceeds MaxPasskeyNameLen
	// characters. Its message names the offending field so it can be surfaced
	// verbatim in a 400.
	ErrPasskeyNameTooLong = errors.New("auth: passkey name must be at most 64 characters")
)

// MaxPasskeyNameLen is the maximum length of the name a user gives a passkey,
// measured in runes rather than bytes so accented text is not penalised. An
// empty name is allowed: somebody adding their first key should not be stopped
// by a form field, and the listing falls back to the authenticator's own words.
const MaxPasskeyNameLen = 64

// passkeyIDPrefix marks UIDs that identify passkey_credentials rows. It names
// the row, never the credential: the credential's own identifier is the opaque
// byte string the authenticator minted, which is what a login is resolved by.
const passkeyIDPrefix = "pk"

// newPasskeyID returns a fresh UID for a passkey_credentials row.
func newPasskeyID() (string, error) {
	return newUID(passkeyIDPrefix)
}

// defaultCeremonyTTL is how long a begun ceremony waits for its answer. It has
// to outlast a person picking up their phone and unlocking it, and it must not
// outlast the tab they started in; five minutes is the value the WebAuthn
// specification suggests as a timeout for the same reason.
const defaultCeremonyTTL = 5 * time.Minute

// Passkey is one WebAuthn credential registered to an account: what the store
// holds, and what a login is verified against.
//
// Credential carries the protocol's own record — the public key, the sign
// counter and the latched flags. It is not serialised: a client has no use for a
// COSE key, and the flags are an implementation detail of the verification. What
// a client does see is View.
type Passkey struct {
	// ID is this row's UID (see passkeyIDPrefix), the handle the delete endpoint
	// takes. It is not the credential id.
	ID string
	// UserUID is the account the credential signs in.
	UserUID string
	// Name is what the owner calls this authenticator; it may be empty.
	Name string
	// CreatedAt is when the credential was registered.
	CreatedAt time.Time
	// LastUsedAt is when it last signed anybody in, or nil if it never has.
	LastUsedAt *time.Time
	// Credential is the WebAuthn credential record itself.
	Credential webauthn.Credential
}

// PasskeyView is one passkey as a client sees it: what the owner needs to tell
// their authenticators apart and decide which to remove. It carries nothing of
// the credential beyond its transports, which is what lets an interface draw a
// phone rather than a security key.
type PasskeyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Transports []string   `json:"transports"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// View returns the client-facing projection of p.
func (p Passkey) View() PasskeyView {
	transports := make([]string, 0, len(p.Credential.Transport))
	for _, transport := range p.Credential.Transport {
		transports = append(transports, string(transport))
	}
	return PasskeyView{
		ID:         p.ID,
		Name:       p.Name,
		Transports: transports,
		CreatedAt:  p.CreatedAt,
		LastUsedAt: p.LastUsedAt,
	}
}

// normalizePasskeyName trims the name and rejects one over MaxPasskeyNameLen
// runes. An empty result is valid.
func normalizePasskeyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) > MaxPasskeyNameLen {
		return "", ErrPasskeyNameTooLong
	}
	return trimmed, nil
}

// passkeyUser adapts an account and its stored credentials to the webauthn.User
// interface the library's ceremonies are driven by.
//
// The user handle is the account UID: 26 printable characters, well inside the
// protocol's 64-byte limit, stable for the life of the account and — crucially —
// resolvable back to that account, which is what makes a discoverable
// ("usernameless") login possible at all. It is not a secret: it is handed to
// the authenticator at registration and returned by it at every login.
type passkeyUser struct {
	user        User
	credentials []webauthn.Credential
}

// WebAuthnID returns the account UID as the user handle.
func (u passkeyUser) WebAuthnID() []byte { return []byte(u.user.UID) }

// WebAuthnName returns the account's username, the name an authenticator lists
// the credential under.
func (u passkeyUser) WebAuthnName() string { return u.user.Username }

// WebAuthnDisplayName returns the account's display name, falling back to the
// username when nobody set one — an authenticator showing an empty line is worse
// than one showing a login name.
func (u passkeyUser) WebAuthnDisplayName() string {
	if u.user.DisplayName != "" {
		return u.user.DisplayName
	}
	return u.user.Username
}

// WebAuthnCredentials returns the account's registered credentials.
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// PasskeysConfig bundles what NewPasskeys needs. RPID and Origins come from the
// resolved relying party (see internal/config); a caller that has neither must
// not build a Passkeys at all, which is how the feature stays cleanly off.
type PasskeysConfig struct {
	// Service is the auth domain service whose store and session policy the
	// passkey flow reuses (required).
	Service *Service
	// RPID is the relying-party ID credentials are scoped to (required).
	RPID string
	// RPDisplayName is what an authenticator shows while it asks the user.
	RPDisplayName string
	// Origins are the page origins a ceremony may run from (at least one).
	Origins []string
	// CeremonyTTL bounds how long a begun ceremony waits for its answer; zero
	// uses defaultCeremonyTTL.
	CeremonyTTL time.Duration
}

// Passkeys is the WebAuthn sign-in flow: it begins and finishes both ceremonies,
// keeps the in-flight challenges, and owns the credential list of an account.
//
// It is deliberately a peer of Registration and PasswordReset rather than a
// package of its own: a passkey login produces exactly the session a password
// login produces, from the same Service and under the same policy, and splitting
// that across a package boundary would mean exporting the session machinery to
// build a second door into the same house.
type Passkeys struct {
	svc        *Service
	web        *webauthn.WebAuthn
	ceremonies *ceremonyStore
}

// NewPasskeys returns a Passkeys from cfg. It fails when the relying party is
// not usable — an empty ID, no origin, an origin that is not an origin — because
// every one of those would mint credentials no authenticator could ever return.
// A caller with nothing configured must not build a Passkeys at all; it gets
// ErrPasskeysUnavailable if it tries.
func NewPasskeys(cfg PasskeysConfig) (*Passkeys, error) {
	// Checked here rather than left to the library, which tolerates an empty
	// relying-party ID by inferring one from the origin. Inferring it is exactly
	// what must not happen: the ID is what a credential is permanently bound to,
	// so an instance that never said which domain it is would mint keys against a
	// guess and stop recognising them the moment the guess changed.
	if strings.TrimSpace(cfg.RPID) == "" || len(cfg.Origins) == 0 {
		return nil, ErrPasskeysUnavailable
	}
	web, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     cfg.Origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			// A discoverable credential is what makes the sign-in screen able to
			// offer "sign in with a passkey" before anybody has typed a username.
			ResidentKey: protocol.ResidentKeyRequirementRequired,
			// Preferred, not required: a hardware key without a PIN is still a far
			// better second factor than a typed password, and refusing it outright
			// would turn a working authenticator into a dead end.
			UserVerification: protocol.VerificationPreferred,
		},
	})
	if err != nil {
		return nil, errors.New("auth: building the passkey relying party: " + err.Error())
	}
	ttl := cfg.CeremonyTTL
	if ttl <= 0 {
		ttl = defaultCeremonyTTL
	}
	return &Passkeys{svc: cfg.Service, web: web, ceremonies: newCeremonyStore(ttl)}, nil
}

// BeginRegistration starts a registration ceremony for user and returns the
// creation options the browser passes to navigator.credentials.create, together
// with the opaque id of the stored challenge. The caller hands that id back to
// the client (as a cookie) and returns it to FinishRegistration.
//
// The account's existing credentials are sent as an exclusion list, so an
// authenticator that already holds a key for this account says so instead of
// silently minting a second one.
func (p *Passkeys) BeginRegistration(
	ctx context.Context, user User,
) (*protocol.CredentialCreation, string, error) {
	credentials, err := p.credentialsOf(ctx, user.UID)
	if err != nil {
		return nil, "", err
	}
	options, session, err := p.web.BeginRegistration(
		passkeyUser{user: user, credentials: credentials},
		webauthn.WithExclusions(webauthn.Credentials(credentials).CredentialDescriptors()),
	)
	if err != nil {
		return nil, "", ErrPasskeyRejected
	}
	id, err := p.ceremonies.put(ceremony{session: *session, userUID: user.UID}, p.svc.now())
	if err != nil {
		return nil, "", err
	}
	return options, id, nil
}

// FinishRegistration verifies the authenticator's answer to the ceremony named
// by ceremonyID and stores the resulting credential under name for user, writing
// entry in the same transaction. response is the raw PublicKeyCredential JSON the
// browser produced.
//
// It returns ErrPasskeyCeremony when the ceremony is unknown, expired or belongs
// to somebody else, ErrPasskeyRejected when the answer does not verify,
// ErrPasskeyAlreadyRegistered when this instance already stores the credential,
// and ErrPasskeyNameTooLong for an over-long name.
func (p *Passkeys) FinishRegistration(
	ctx context.Context, user User, ceremonyID, name string, response []byte, entry audit.Entry,
) (Passkey, error) {
	cleanName, err := normalizePasskeyName(name)
	if err != nil {
		return Passkey{}, err
	}
	cer, ok := p.ceremonies.take(ceremonyID, p.svc.now())
	if !ok || cer.userUID != user.UID {
		return Passkey{}, ErrPasskeyCeremony
	}
	credential, err := p.verifyRegistration(ctx, user, cer, response)
	if err != nil {
		return Passkey{}, err
	}

	id, err := newPasskeyID()
	if err != nil {
		return Passkey{}, err
	}
	pk := Passkey{
		ID: id, UserUID: user.UID, Name: cleanName, CreatedAt: p.svc.now(), Credential: *credential,
	}
	entry.TargetUID = pk.ID
	entry.Details = withName(entry.Details, cleanName)
	if err := p.svc.store.CreatePasskeyAudited(ctx, pk, entry); err != nil {
		return Passkey{}, err
	}
	return pk, nil
}

// verifyRegistration parses and validates the authenticator's creation response
// against the stored ceremony, returning the credential it attests to. Every
// verification failure collapses to ErrPasskeyRejected.
func (p *Passkeys) verifyRegistration(
	ctx context.Context, user User, cer ceremony, response []byte,
) (*webauthn.Credential, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, ErrPasskeyRejected
	}
	credentials, err := p.credentialsOf(ctx, user.UID)
	if err != nil {
		return nil, err
	}
	credential, err := p.web.CreateCredential(
		passkeyUser{user: user, credentials: credentials}, cer.session, parsed,
	)
	if err != nil {
		return nil, ErrPasskeyRejected
	}
	return credential, nil
}

// BeginLogin starts a discoverable ("usernameless") login ceremony and returns
// the assertion options the browser passes to navigator.credentials.get,
// together with the opaque id of the stored challenge. No account is named:
// which one is being signed into is decided by the credential the authenticator
// picks, which is the whole point of the flow.
func (p *Passkeys) BeginLogin() (*protocol.CredentialAssertion, string, error) {
	options, session, err := p.web.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", ErrPasskeyRejected
	}
	id, err := p.ceremonies.put(ceremony{session: *session}, p.svc.now())
	if err != nil {
		return nil, "", err
	}
	return options, id, nil
}

// FinishLogin verifies the authenticator's answer to the login ceremony named by
// ceremonyID and, on success, creates exactly the session a password login
// creates. response is the raw PublicKeyCredential JSON the browser produced;
// entry is stamped with the account and written in the same transaction as the
// credential's use.
//
// It returns ErrPasskeyCeremony for an unknown or expired ceremony,
// ErrPasskeyRejected for an answer that does not verify or an account that may
// not sign in, and ErrNotApproved for an account still waiting for an
// administrator — the same distinction a password login draws, and for the same
// reason: it is only ever reached by somebody who already holds the credential.
func (p *Passkeys) FinishLogin(
	ctx context.Context, ceremonyID string, response []byte, entry audit.Entry,
) (Session, User, error) {
	cer, ok := p.ceremonies.take(ceremonyID, p.svc.now())
	if !ok || cer.userUID != "" {
		return Session{}, User{}, ErrPasskeyCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return Session{}, User{}, ErrPasskeyRejected
	}
	owner, credential, err := p.web.ValidatePasskeyLogin(p.discoverUser(ctx), cer.session, parsed)
	if err != nil {
		return Session{}, User{}, ErrPasskeyRejected
	}
	holder, held := owner.(passkeyUser)
	if !held {
		return Session{}, User{}, ErrPasskeyRejected
	}
	if err := checkPasskeyLogin(holder.user); err != nil {
		return Session{}, User{}, err
	}
	return p.completeLogin(ctx, holder.user, *credential, entry)
}

// completeLogin records the credential's use together with entry, then mints the
// session and stamps the account's last login — the same two writes a password
// login performs, in the same order.
func (p *Passkeys) completeLogin(
	ctx context.Context, user User, credential webauthn.Credential, entry audit.Entry,
) (Session, User, error) {
	now := p.svc.now()
	entry.ActorUID = user.UID
	stored, err := p.svc.store.TouchPasskeyAudited(ctx, credential, now, entry)
	if err != nil {
		return Session{}, User{}, err
	}
	if !stored {
		return Session{}, User{}, ErrPasskeyRejected
	}
	sess, err := p.svc.createSession(ctx, user)
	if err != nil {
		return Session{}, User{}, err
	}
	if err := p.svc.store.SetLastLogin(ctx, user.UID, now); err != nil {
		return Session{}, User{}, err
	}
	return sess, user, nil
}

// checkPasskeyLogin applies the account checks a password login applies once the
// credential has been verified: a disabled account is refused as unspecifically
// as a bad signature, and an account nobody has approved is told what it is
// waiting for.
func checkPasskeyLogin(user User) error {
	if user.Disabled {
		return ErrPasskeyRejected
	}
	if user.ApprovedAt == nil {
		return ErrNotApproved
	}
	return nil
}

// discoverUser returns the handler the library calls with the credential and
// user handle an authenticator returned, resolving them to the account that owns
// the credential. An unknown handle, an unknown credential or a handle that does
// not own the credential all fail the ceremony.
func (p *Passkeys) discoverUser(ctx context.Context) webauthn.DiscoverableUserHandler {
	return func(rawID, userHandle []byte) (webauthn.User, error) {
		stored, err := p.svc.store.GetPasskeyByCredentialID(ctx, rawID)
		if err != nil {
			return nil, ErrPasskeyRejected
		}
		if string(userHandle) != stored.UserUID {
			return nil, ErrPasskeyRejected
		}
		user, err := p.svc.store.GetUserByUID(ctx, stored.UserUID)
		if err != nil {
			return nil, ErrPasskeyRejected
		}
		credentials, err := p.credentialsOf(ctx, user.UID)
		if err != nil {
			return nil, err
		}
		return passkeyUser{user: user, credentials: credentials}, nil
	}
}

// List returns the account's passkeys, newest first.
func (p *Passkeys) List(ctx context.Context, userUID string) ([]Passkey, error) {
	return p.svc.store.ListPasskeysByUser(ctx, userUID)
}

// Delete removes the passkey identified by id on behalf of actor, writing entry
// in the same transaction. A passkey belonging to somebody else is reported as
// ErrPasskeyNotFound rather than as a permission error, so a caller cannot probe
// which ids exist.
//
// Removing the last one is allowed on purpose: the password is never taken away,
// so an account can always be signed into, and refusing would strand a person
// whose only authenticator was lost with a credential they can no longer use.
func (p *Passkeys) Delete(ctx context.Context, id string, actor User, entry audit.Entry) error {
	stored, err := p.svc.store.GetPasskeyByID(ctx, id)
	if err != nil {
		return err
	}
	if stored.UserUID != actor.UID {
		return ErrPasskeyNotFound
	}
	entry.TargetUID = stored.ID
	entry.Details = withName(entry.Details, stored.Name)
	deleted, err := p.svc.store.DeletePasskeyAudited(ctx, id, entry)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrPasskeyNotFound
	}
	return nil
}

// credentialsOf returns the WebAuthn credential records of the account's stored
// passkeys, the list every ceremony is driven from.
func (p *Passkeys) credentialsOf(ctx context.Context, userUID string) ([]webauthn.Credential, error) {
	stored, err := p.svc.store.ListPasskeysByUser(ctx, userUID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(stored))
	for _, pk := range stored {
		credentials = append(credentials, pk.Credential)
	}
	return credentials, nil
}

// Cleanup drops the ceremonies that have expired as of now. It rides the API's
// existing limiter-maintenance tick.
func (p *Passkeys) Cleanup(now time.Time) {
	p.ceremonies.cleanup(now)
}

// CeremonyTTL is how long a begun ceremony stays valid, which the HTTP layer
// needs to give the ceremony cookie the same lifetime.
func (p *Passkeys) CeremonyTTL() time.Duration {
	return p.ceremonies.ttl
}

// withName returns details carrying the passkey's name, allocating the map when
// the caller passed none. Handlers build an entry without details and the flow
// adds what it learned, so every path has to survive a nil map — and the name is
// what makes an entry readable months later, when the credential it names is
// gone and only the trail is left.
func withName(details map[string]any, name string) map[string]any {
	if details == nil {
		details = map[string]any{}
	}
	details["name"] = name
	return details
}
