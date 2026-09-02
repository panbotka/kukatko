package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// passkeyColumns is the canonical, ordered column list for passkey reads,
// matched by scanPasskey.
//
//nolint:gosec // G101: a list of column names, not a credential; there is no secret in this table at all.
const passkeyColumns = `id, user_uid, name, created_at, last_used_at,
	credential_id, public_key, sign_count, transports, aaguid,
	attestation_type, attestation_format,
	backup_eligible, backup_state, user_present, user_verified`

// scanPasskey reads one passkey_credentials row in passkeyColumns order,
// reassembling the WebAuthn credential record from its columns.
//
// The flags are read back and not defaulted: go-webauthn refuses a login whose
// backup-eligibility flag disagrees with the one recorded at registration, so a
// row scanned without them would reject the very authenticator it belongs to.
//
// The signature counter is narrowed back to the uint32 it was stored from; the
// column is BIGINT only because PostgreSQL's INTEGER is signed and the
// protocol's counter is not.
func scanPasskey(row pgx.Row) (Passkey, error) {
	var (
		pk         Passkey
		transports []string
		signCount  int64
		flags      webauthn.CredentialFlags
	)
	if err := row.Scan(
		&pk.ID, &pk.UserUID, &pk.Name, &pk.CreatedAt, &pk.LastUsedAt,
		&pk.Credential.ID, &pk.Credential.PublicKey, &signCount, &transports,
		&pk.Credential.Authenticator.AAGUID,
		&pk.Credential.AttestationType, &pk.Credential.AttestationFormat,
		&flags.BackupEligible, &flags.BackupState, &flags.UserPresent, &flags.UserVerified,
	); err != nil {
		return Passkey{}, fmt.Errorf("auth: scanning passkey: %w", err)
	}
	pk.Credential.Authenticator.SignCount = uint32(signCount) //nolint:gosec // stored from a uint32, see above.
	pk.Credential.Flags = flags
	pk.Credential.Transport = make([]protocol.AuthenticatorTransport, 0, len(transports))
	for _, transport := range transports {
		pk.Credential.Transport = append(pk.Credential.Transport, protocol.AuthenticatorTransport(transport))
	}
	return pk, nil
}

// transportStrings returns the credential's transports as plain strings, the
// form the TEXT[] column takes.
func transportStrings(credential webauthn.Credential) []string {
	transports := make([]string, 0, len(credential.Transport))
	for _, transport := range credential.Transport {
		transports = append(transports, string(transport))
	}
	return transports
}

// insertPasskeyQuery inserts one credential. created_at is written from the
// caller-supplied value so the credential's timeline follows the service clock;
// last_used_at is deliberately absent, because a credential that has just been
// registered has never signed anybody in.
const insertPasskeyQuery = `INSERT INTO passkey_credentials
		(id, user_uid, name, created_at, credential_id, public_key, sign_count, transports,
		 aaguid, attestation_type, attestation_format,
		 backup_eligible, backup_state, user_present, user_verified)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

// CreatePasskeyAudited inserts pk and writes entry to the audit log in the same
// transaction, so the credential and the record of who registered it commit
// atomically (the durable-audit convention; see internal/audit). A credential
// this instance already stores is ErrPasskeyAlreadyRegistered.
func (s *Store) CreatePasskeyAudited(ctx context.Context, pk Passkey, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = pk.ID
	}
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, insertPasskeyQuery,
			pk.ID, pk.UserUID, pk.Name, pk.CreatedAt,
			pk.Credential.ID, pk.Credential.PublicKey, int64(pk.Credential.Authenticator.SignCount),
			transportStrings(pk.Credential), pk.Credential.Authenticator.AAGUID,
			pk.Credential.AttestationType, pk.Credential.AttestationFormat,
			pk.Credential.Flags.BackupEligible, pk.Credential.Flags.BackupState,
			pk.Credential.Flags.UserPresent, pk.Credential.Flags.UserVerified,
		)
		if execErr != nil {
			if isUniqueViolation(execErr) {
				return ErrPasskeyAlreadyRegistered
			}
			return fmt.Errorf("auth: inserting passkey: %w", execErr)
		}
		return nil
	})
	return err
}

// GetPasskeyByID returns the passkey row with the given UID, or
// ErrPasskeyNotFound.
func (s *Store) GetPasskeyByID(ctx context.Context, id string) (Passkey, error) {
	return s.getPasskey(ctx, "id", id)
}

// GetPasskeyByCredentialID returns the passkey holding the authenticator's own
// credential identifier, or ErrPasskeyNotFound. It is the single indexed lookup
// behind a discoverable login: the browser returns a credential id and this is
// what turns it into an account.
func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (Passkey, error) {
	return s.getPasskey(ctx, "credential_id", credentialID)
}

// getPasskey fetches a single passkey filtered by an equality on the trusted
// column name col (an internal constant, never user input), translating
// pgx.ErrNoRows into ErrPasskeyNotFound.
func (s *Store) getPasskey(ctx context.Context, col string, val any) (Passkey, error) {
	q := "SELECT " + passkeyColumns + " FROM passkey_credentials WHERE " + col + " = $1"
	pk, err := scanPasskey(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Passkey{}, ErrPasskeyNotFound
		}
		return Passkey{}, err
	}
	return pk, nil
}

// ListPasskeysByUser returns every passkey belonging to userUID, newest first.
// The slice is empty (not nil) when the account has none.
func (s *Store) ListPasskeysByUser(ctx context.Context, userUID string) ([]Passkey, error) {
	q := "SELECT " + passkeyColumns + ` FROM passkey_credentials
		WHERE user_uid = $1 ORDER BY created_at DESC, id`
	rows, err := s.pool.Query(ctx, q, userUID)
	if err != nil {
		return nil, fmt.Errorf("auth: querying passkeys: %w", err)
	}
	defer rows.Close()

	passkeys := make([]Passkey, 0)
	for rows.Next() {
		pk, scanErr := scanPasskey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		passkeys = append(passkeys, pk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating passkeys: %w", err)
	}
	return passkeys, nil
}

// touchPasskeyQuery records a credential's use: the moment, the authenticator's
// new signature counter and the flags the assertion latched. The counter and the
// backup state are the two values the protocol expects a relying party to carry
// forward — the first is what would expose a cloned authenticator, the second
// changes when a synced key is backed up — and user_verified is latched here for
// the same reason the library latches it in memory.
const touchPasskeyQuery = `UPDATE passkey_credentials
	SET last_used_at = $2, sign_count = $3, backup_state = $4,
	    user_present = user_present OR $5, user_verified = user_verified OR $6
	WHERE credential_id = $1`

// TouchPasskeyAudited records that credential signed somebody in at `at`,
// carrying its updated counter and flags forward, and writes entry in the same
// transaction — so the audit trail of a passkey login and the state that login
// left behind commit together. It reports whether the credential was still
// there: a row deleted between the verification and this call is not an error,
// but it must not produce a session.
func (s *Store) TouchPasskeyAudited(
	ctx context.Context, credential webauthn.Credential, at time.Time, entry audit.Entry,
) (bool, error) {
	touched := false
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		tag, execErr := tx.Exec(ctx, touchPasskeyQuery,
			credential.ID, at, int64(credential.Authenticator.SignCount),
			credential.Flags.BackupState, credential.Flags.UserPresent, credential.Flags.UserVerified,
		)
		if execErr != nil {
			return fmt.Errorf("auth: recording passkey use: %w", execErr)
		}
		touched = tag.RowsAffected() > 0
		if !touched {
			return errNoAuditableChange
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return touched, nil
}

// DeletePasskeyAudited removes the passkey identified by id and writes entry in
// the same transaction, reporting whether a row actually went. A passkey that
// had already been removed changes nothing: the function reports false and
// writes no audit entry.
func (s *Store) DeletePasskeyAudited(ctx context.Context, id string, entry audit.Entry) (bool, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = id
	}
	deleted := false
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		tag, execErr := tx.Exec(ctx, `DELETE FROM passkey_credentials WHERE id = $1`, id)
		if execErr != nil {
			return fmt.Errorf("auth: deleting passkey: %w", execErr)
		}
		deleted = tag.RowsAffected() > 0
		if !deleted {
			return errNoAuditableChange
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}
