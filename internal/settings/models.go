// Package settings is the database access layer for the instance-wide settings
// an administrator edits at runtime: whether self-service registration is open,
// the shared secret registration asks for, and the Markdown greeting a person
// sees the first time they sign in. None of the three is a deployment concern —
// they describe how this instance receives people — so they live in the database
// and change without a redeploy.
//
// There is at most one settings record, enforced by a single-row table (see
// migration 0062), so writing is an upsert. The migration seeds the row, so a
// read normally finds it; Get still answers with the defaults when it does not,
// because the anonymous sign-in screen asks whether registration is open and a
// missing row must never turn that into an error page.
//
// The store keeps no state beyond the shared pgx pool. A change is audited in
// the same transaction as the update (mirroring internal/announcement), so the
// record of who changed the instance commits atomically with the change or not
// at all. Access control — anonymous reads only the registration flag, any
// signed-in role reads the welcome text, only an admin sees the secret or
// writes — is enforced by the HTTP layer above this store.
package settings

import (
	"errors"
	"strings"
	"time"
)

// ErrSecretRequired is returned by Set when registration is being enabled while
// the shared secret is blank. An open door with no lock is never what the
// administrator meant, and the flag and the secret are saved together, so the
// combination can be refused instead of briefly existing.
var ErrSecretRequired = errors.New("registration secret must not be empty when registration is enabled")

// Settings is the single instance-wide settings record.
//
// RegistrationSecret is stored and returned in readable form on purpose: an
// administrator has to be able to read it back to tell people what it is. It is
// the reason the full record is admin-only — the JSON tags here describe the
// admin wire shape, and the narrower anonymous and authenticated responses are
// built explicitly in internal/settingsapi rather than by omitting fields.
type Settings struct {
	// RegistrationEnabled is whether the sign-in screen offers self-service
	// registration. False on a fresh instance.
	RegistrationEnabled bool `json:"registration_enabled"`
	// RegistrationSecret is the shared secret registration asks a newcomer for,
	// in readable form. Never leaves the server below the admin role.
	RegistrationSecret string `json:"registration_secret"`
	// WelcomeMarkdown is the Markdown greeting shown to a person the first time
	// they sign in. Empty means there is nothing to show.
	WelcomeMarkdown string `json:"welcome_markdown"`
	// UpdatedAt is when the settings were last written. It is the seed time
	// until an administrator first saves them.
	UpdatedAt time.Time `json:"updated_at"`
	// UpdatedByUID is the UID of the administrator who last saved the settings,
	// empty when nobody has yet or when that account has since been deleted (the
	// column cascades to NULL).
	UpdatedByUID string `json:"updated_by_uid"`
}

// Update is the set of values a Set call replaces. All three are written
// together — the registration flag and the secret guard each other, so they are
// never saved apart.
type Update struct {
	// RegistrationEnabled is the new self-service registration flag.
	RegistrationEnabled bool `json:"registration_enabled"`
	// RegistrationSecret is the new shared registration secret.
	RegistrationSecret string `json:"registration_secret"`
	// WelcomeMarkdown is the new first-sign-in greeting, in Markdown.
	WelcomeMarkdown string `json:"welcome_markdown"`
}

// validate normalizes an update and reports why it cannot be stored. The secret
// is trimmed, because surrounding whitespace in a value people type to each
// other is a trap rather than part of the secret; the welcome text is left
// exactly as written, since Markdown's leading whitespace is meaningful.
// Enabling registration with a blank (or whitespace-only) secret yields
// ErrSecretRequired.
func (u Update) validate() (Update, error) {
	normalized := u
	normalized.RegistrationSecret = strings.TrimSpace(u.RegistrationSecret)
	if normalized.RegistrationEnabled && normalized.RegistrationSecret == "" {
		return Update{}, ErrSecretRequired
	}
	return normalized, nil
}
