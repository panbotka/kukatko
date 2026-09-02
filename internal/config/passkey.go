package config

import (
	"fmt"
	"net/url"
	"strings"
)

// RelyingParty is the resolved WebAuthn relying party of this instance: the
// values internal/auth needs to run a ceremony, after the configured keys and
// the fallback derived from mail.base_url have been folded together.
//
// Enabled is the single question every caller asks. It is false whenever the
// resolution produced no relying-party ID or no origin, which is the honest
// answer for an instance nobody told its own address: a passkey bound to a
// guessed domain would be a passkey that never works again.
type RelyingParty struct {
	// ID is the relying-party ID a credential is scoped to (a bare domain).
	ID string
	// DisplayName is what an authenticator shows while asking the user.
	DisplayName string
	// Origins are the page origins a ceremony may be run from.
	Origins []string
	// Enabled reports whether passkeys can be offered at all.
	Enabled bool
}

// defaultRPDisplayName is the relying-party name shown when the operator
// configured none. It is the product's name rather than the hostname, because it
// is read by a person deciding whether to hand over a key, not by a machine.
const defaultRPDisplayName = "Kukátko"

// Passkey resolves the instance's WebAuthn relying party from the auth.passkey
// keys, falling back to mail.base_url for anything they leave out (see
// PasskeyConfig). The result is Enabled only when both an ID and at least one
// origin came out of that; every other case leaves passkeys off.
//
// It assumes the configuration has already passed Validate, so every configured
// origin parses; a malformed one that reaches here is skipped rather than
// guessed at.
func (c *Config) Passkey() RelyingParty {
	rp := RelyingParty{DisplayName: strings.TrimSpace(c.Auth.Passkey.RPDisplayName)}
	if rp.DisplayName == "" {
		rp.DisplayName = defaultRPDisplayName
	}
	rp.Origins = normalizeOrigins(c.Auth.Passkey.Origins)
	if len(rp.Origins) == 0 {
		if derived, _, err := parseOrigin(c.Mail.BaseURL); err == nil && derived != "" {
			rp.Origins = []string{derived}
		}
	}
	rp.ID = strings.TrimSpace(c.Auth.Passkey.RPID)
	if rp.ID == "" && len(rp.Origins) > 0 {
		if _, host, err := parseOrigin(rp.Origins[0]); err == nil {
			rp.ID = host
		}
	}
	rp.Enabled = rp.ID != "" && len(rp.Origins) > 0
	return rp
}

// normalizeOrigins returns the canonical scheme://host[:port] form of every
// entry of origins that parses, dropping blanks and duplicates while keeping the
// configured order. A malformed entry is dropped rather than passed through:
// Validate has already refused to start on one, so reaching this branch means
// the caller skipped validation and an unparseable string in the allow-list
// would be matched by exact comparison against an origin no browser can send.
func normalizeOrigins(origins []string) []string {
	out := make([]string, 0, len(origins))
	seen := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		origin, _, err := parseOrigin(raw)
		if err != nil || origin == "" {
			continue
		}
		if _, dup := seen[origin]; dup {
			continue
		}
		seen[origin] = struct{}{}
		out = append(out, origin)
	}
	return out
}

// parseOrigin reduces rawURL to its origin (scheme://host[:port]) and the bare
// host inside it, so "https://photos.example.com/login/" yields
// ("https://photos.example.com", "photos.example.com"). An empty input yields
// two empty strings and no error — "nothing configured" is not a mistake — while
// anything that is not an absolute http(s) URL with a host is
// ErrInvalidPasskeyOrigin.
func parseOrigin(rawURL string) (origin, host string, err error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", "", nil
	}
	parsed, parseErr := url.Parse(trimmed)
	if parseErr != nil {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidPasskeyOrigin, trimmed)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidPasskeyOrigin, trimmed)
	}
	return scheme + "://" + strings.ToLower(parsed.Host), strings.ToLower(parsed.Hostname()), nil
}

// validate checks that every configured passkey origin is an absolute http(s)
// URL with a host. It says nothing about whether passkeys end up enabled: an
// instance that configures none is valid and simply does not offer them.
func (p PasskeyConfig) validate() error {
	for _, origin := range p.Origins {
		if _, _, err := parseOrigin(origin); err != nil {
			return err
		}
	}
	return nil
}
