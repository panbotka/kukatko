package notification

import (
	"context"
	"fmt"
	"maps"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// selectPrefsSQL reads an account's stored choices; kinds it never chose have
// no row and fall back to their default.
const selectPrefsSQL = `SELECT kind, enabled FROM notification_prefs WHERE user_uid = $1`

// Preferences returns userUID's effective preferences: one entry per known kind,
// in display order, the stored choice where the account made one and the kind's
// default (IsDefault) where it did not. A stored row for a kind this package no
// longer defines is ignored. An account with no rows — or no account at all —
// reads as all defaults.
func (s *Store) Preferences(ctx context.Context, userUID string) ([]Preference, error) {
	return readPrefs(ctx, s.pool, userUID)
}

// Wants reports whether userUID wants notifications of kind: its stored choice,
// or the kind's default when it never chose. An unknown kind is ErrUnknownKind.
func (s *Store) Wants(ctx context.Context, userUID string, kind Kind) (bool, error) {
	if !kind.Known() {
		return false, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	prefs, err := s.Preferences(ctx, userUID)
	if err != nil {
		return false, err
	}
	for _, pref := range prefs {
		if pref.Kind == kind {
			return pref.Enabled, nil
		}
	}
	return kind.Default(), nil
}

// ReplacePreferences replaces every stored choice of userUID with prefs and
// writes entry to the audit log in the same transaction, so the change and the
// record of who made it commit together or not at all. A kind prefs leaves out
// goes back to its default; a kind it names is stored as given, even when that
// equals the default, so an explicit choice survives a later change of default.
//
// An unknown kind is ErrUnknownKind and a kind named twice ErrDuplicateKind —
// last-write-wins is not allowed to decide silently — and an account that does
// not exist is ErrUserNotFound; in each case nothing is written. entry's action,
// target type and target default to ActionNotificationPrefsUpdate, "users" and
// userUID when left empty, and its details gain the stored choices under
// "preferences". The returned slice is the new effective preferences.
func (s *Store) ReplacePreferences(
	ctx context.Context, userUID string, prefs []Preference, entry audit.Entry,
) ([]Preference, error) {
	if err := validatePrefs(prefs); err != nil {
		return nil, err
	}
	kinds := make([]string, 0, len(prefs))
	enabled := make([]bool, 0, len(prefs))
	for _, pref := range prefs {
		kinds = append(kinds, string(pref.Kind))
		enabled = append(enabled, pref.Enabled)
	}
	var out []Preference
	err := s.inTx(ctx, "replacing preferences", func(tx pgx.Tx) error {
		if err := requireUser(ctx, tx, userUID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM notification_prefs WHERE user_uid = $1", userUID); err != nil {
			return fmt.Errorf("notification: clearing preferences of %s: %w", userUID, err)
		}
		if _, err := tx.Exec(ctx, insertPrefsSQL, userUID, kinds, enabled); err != nil {
			return mapForeignKey(err)
		}
		if err := audit.Write(ctx, tx, prefsEntry(entry, userUID, prefs)); err != nil {
			return fmt.Errorf("notification: writing audit entry: %w", err)
		}
		var err error
		out, err = readPrefs(ctx, tx, userUID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// requireUser returns ErrUserNotFound unless the account userUID exists. It is
// asked explicitly because a replace that stores no rows never meets the
// foreign key that would otherwise say so.
func requireUser(ctx context.Context, tx pgx.Tx, userUID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE uid = $1)", userUID).Scan(&exists); err != nil {
		return fmt.Errorf("notification: looking up account %s: %w", userUID, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrUserNotFound, userUID)
	}
	return nil
}

// insertPrefsSQL stores a whole set of choices in one statement, pairing the two
// arrays by position.
const insertPrefsSQL = `
INSERT INTO notification_prefs (user_uid, kind, enabled)
SELECT $1, k, e FROM unnest($2::text[], $3::boolean[]) AS u (k, e)`

// querier is what readPrefs needs: the pool, or the transaction of a replace.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// readPrefs reads userUID's stored choices through q and merges them over the
// defaults.
func readPrefs(ctx context.Context, q querier, userUID string) ([]Preference, error) {
	rows, err := q.Query(ctx, selectPrefsSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("notification: reading preferences of %s: %w", userUID, err)
	}
	defer rows.Close()
	stored := make(map[Kind]bool)
	for rows.Next() {
		var (
			kind    string
			enabled bool
		)
		if err := rows.Scan(&kind, &enabled); err != nil {
			return nil, fmt.Errorf("notification: scanning preference of %s: %w", userUID, err)
		}
		stored[Kind(kind)] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notification: reading preferences of %s: %w", userUID, err)
	}
	return effective(stored), nil
}

// prefsEntry fills the parts of a preference-change audit entry the caller left
// empty and records the stored choices in its details. The caller's details map
// is copied, never written into.
func prefsEntry(entry audit.Entry, userUID string, prefs []Preference) audit.Entry {
	if entry.Action == "" {
		entry.Action = audit.ActionNotificationPrefsUpdate
	}
	if entry.TargetType == "" {
		entry.TargetType = "users"
	}
	if entry.TargetUID == "" {
		entry.TargetUID = userUID
	}
	choices := make(map[string]bool, len(prefs))
	for _, pref := range prefs {
		choices[string(pref.Kind)] = pref.Enabled
	}
	details := make(map[string]any, len(entry.Details)+1)
	maps.Copy(details, entry.Details)
	details["preferences"] = choices
	entry.Details = details
	return entry
}
