package auth

import (
	"context"
	"errors"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// CreateAPITokenInput holds the caller-supplied fields of a new API token. A nil
// ExpiresAt mints a token that never expires.
type CreateAPITokenInput struct {
	Name      string
	ExpiresAt *time.Time
}

// CreateAPIToken mints a token for userUID and returns it together with the
// plaintext credential, which exists only in this return value: the store keeps
// nothing but a hash, so the secret can never be shown again. The token row and
// entry are committed in one transaction. It returns ErrAPITokenNameRequired for
// an empty name and ErrAPITokenExpiryInPast for an expiry that has already
// passed.
func (s *Service) CreateAPIToken(
	ctx context.Context, userUID string, in CreateAPITokenInput, entry audit.Entry,
) (APIToken, string, error) {
	name, err := normalizeAPITokenName(in.Name)
	if err != nil {
		return APIToken{}, "", err
	}
	now := s.now()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return APIToken{}, "", ErrAPITokenExpiryInPast
	}

	id, secretHash, plaintext, err := generateAPIToken()
	if err != nil {
		return APIToken{}, "", err
	}
	tok := APIToken{
		ID:         id,
		UserUID:    userUID,
		Name:       name,
		CreatedAt:  now,
		ExpiresAt:  in.ExpiresAt,
		SecretHash: secretHash,
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["name"] = name
	if err := s.store.CreateAPITokenAudited(ctx, tok, entry); err != nil {
		return APIToken{}, "", err
	}
	return tok, plaintext, nil
}

// ListAPITokens returns every token belonging to userUID, newest first. The
// secret hashes are present on the values but never serialised.
func (s *Service) ListAPITokens(ctx context.Context, userUID string) ([]APIToken, error) {
	return s.store.ListAPITokensByUser(ctx, userUID)
}

// RevokeAPIToken revokes the token identified by id on behalf of actor. A user
// may revoke only their own tokens; an admin may revoke anyone's. A token that
// belongs to somebody else is reported as ErrAPITokenNotFound rather than a
// permission error, so a non-admin cannot probe which token ids exist.
// Revocation is idempotent: revoking an already-revoked token succeeds and
// writes no second audit entry.
func (s *Service) RevokeAPIToken(ctx context.Context, id string, actor User, entry audit.Entry) error {
	tok, err := s.store.GetAPITokenByID(ctx, id)
	if err != nil {
		return err
	}
	if tok.UserUID != actor.UID && !actor.Role.IsAdmin() {
		return ErrAPITokenNotFound
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["name"] = tok.Name
	entry.Details["owner_uid"] = tok.UserUID
	if _, err := s.store.RevokeAPITokenAudited(ctx, id, s.now(), entry); err != nil {
		return err
	}
	return nil
}

// AuthenticateAPIToken validates a plaintext bearer credential and returns the
// live owner and the token. Every failure mode — malformed, unknown id, wrong
// secret, revoked, expired, disabled owner — returns ErrInvalidAPIToken, so a
// caller learns only that the credential does not work. Storage failures
// propagate wrapped.
//
// On success it records the token's use, but at most once per apiTokenUseInterval
// so a busy client does not write a row on every request.
func (s *Service) AuthenticateAPIToken(ctx context.Context, plaintext string) (User, APIToken, error) {
	id, secret, err := parseAPIToken(plaintext)
	if err != nil {
		return User{}, APIToken{}, err
	}
	tok, err := s.store.GetAPITokenByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrAPITokenNotFound) {
			return User{}, APIToken{}, ErrInvalidAPIToken
		}
		return User{}, APIToken{}, err
	}
	if !apiTokenSecretMatches(tok.SecretHash, secret) {
		return User{}, APIToken{}, ErrInvalidAPIToken
	}
	now := s.now()
	if !tok.Active(now) {
		return User{}, APIToken{}, ErrInvalidAPIToken
	}

	user, err := s.store.GetUserByUID(ctx, tok.UserUID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return User{}, APIToken{}, ErrInvalidAPIToken
		}
		return User{}, APIToken{}, err
	}
	if user.Disabled {
		return User{}, APIToken{}, ErrInvalidAPIToken
	}

	tok, err = s.recordAPITokenUse(ctx, tok, now)
	if err != nil {
		return User{}, APIToken{}, err
	}
	return user, tok, nil
}

// recordAPITokenUse stamps last_used_at on tok when the previous stamp is older
// than apiTokenUseInterval, returning the updated token value. It mirrors the
// sliding session's write-coalescing guard.
func (s *Service) recordAPITokenUse(ctx context.Context, tok APIToken, now time.Time) (APIToken, error) {
	if !tok.shouldRecordUse(now) {
		return tok, nil
	}
	if err := s.store.TouchAPIToken(ctx, tok.ID, now); err != nil {
		return APIToken{}, err
	}
	tok.LastUsedAt = &now
	return tok, nil
}
