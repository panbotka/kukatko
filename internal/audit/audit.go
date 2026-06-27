// Package audit writes the durable audit trail. Entries are appended to the
// audit_log table and, crucially, can be written through a caller-supplied
// executor so the record joins the same transaction as the mutation it
// describes (see ARCHITECTURE.md §5.1, §12: "audit log durable — written in the
// same transaction as the mutation"). A nil-safe Store wraps a pool for
// standalone writes and reads (admin listing, tests).
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Action labels classify audit entries. They are stored verbatim in the action
// column; new auditable operations add a constant here.
const (
	// ActionPhotosBulk records a bulk metadata edit applied to many photos at
	// once via the bulk API.
	ActionPhotosBulk = "photos.bulk"
)

// insertSQL appends one audit entry. It is shared by Store.Record and the
// package-level Record so the column order stays in one place.
const insertSQL = `
INSERT INTO audit_log (actor_uid, action, target_type, target_uid, details)
VALUES ($1, $2, $3, $4, $5)`

// Entry is a single audit record describing who did what. ActorUID and TargetUID
// are optional (empty string stores SQL NULL); Details is serialised to JSONB
// and defaults to an empty object when nil.
type Entry struct {
	// ActorUID is the UID of the user who performed the action, or empty for a
	// system action.
	ActorUID string
	// Action is the operation label, for example ActionPhotosBulk.
	Action string
	// TargetType names the kind of entity affected (for example "photos"), or
	// empty when the action spans several kinds.
	TargetType string
	// TargetUID is the affected entity's UID when a single entity is targeted,
	// or empty for batch actions that list their targets in Details.
	TargetUID string
	// Details carries structured context (operation summary, counts, ids). It is
	// stored as JSONB.
	Details map[string]any
}

// Record is a stored audit entry as read back from the table.
type Record struct {
	ID         int64          `json:"id"`
	ActorUID   *string        `json:"actor_uid"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetUID  *string        `json:"target_uid"`
	Details    map[string]any `json:"details"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Execer is the subset of pgx a single statement needs. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so an audit write can run on its own connection or join a
// caller's transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Write appends entry using exec, which may be a pool or an open transaction.
// Passing the caller's pgx.Tx makes the audit row part of that transaction, so
// it commits or rolls back atomically with the mutation. It returns a wrapped
// error if the insert fails or the details cannot be encoded.
func Write(ctx context.Context, exec Execer, entry Entry) error {
	details, err := json.Marshal(detailsOrEmpty(entry.Details))
	if err != nil {
		return fmt.Errorf("audit: encoding details: %w", err)
	}
	if _, err := exec.Exec(ctx, insertSQL,
		nullable(entry.ActorUID), entry.Action, entry.TargetType,
		nullable(entry.TargetUID), details,
	); err != nil {
		return fmt.Errorf("audit: writing %q entry: %w", entry.Action, err)
	}
	return nil
}

// detailsOrEmpty returns details, or an empty map when nil, so the JSONB column
// never receives a SQL NULL.
func detailsOrEmpty(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	return details
}

// nullable returns nil for an empty string so the column stores SQL NULL, or the
// value otherwise.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// Store wraps a pgx pool for standalone audit writes and reads. The bulk write
// path uses the package-level Write with a transaction instead; Store backs
// admin listing and tests.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Record appends entry on the store's pool in its own transaction. Prefer the
// package-level Write with a caller transaction when the audit row must commit
// atomically with a mutation.
func (s *Store) Record(ctx context.Context, entry Entry) error {
	return Write(ctx, s.pool, entry)
}

// listSQL reads recent audit entries newest-first.
const listSQL = `
SELECT id, actor_uid, action, target_type, target_uid, details, created_at
FROM audit_log
ORDER BY id DESC
LIMIT $1 OFFSET $2`

// List returns up to limit audit records newest-first, skipping offset rows. A
// non-positive limit is clamped to a default page so the query never returns the
// whole table by accident.
func (s *Store) List(ctx context.Context, limit, offset int) ([]Record, error) {
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, listSQL, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("audit: listing entries: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var (
			rec     Record
			details []byte
		)
		if err := rows.Scan(&rec.ID, &rec.ActorUID, &rec.Action, &rec.TargetType,
			&rec.TargetUID, &details, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("audit: scanning entry: %w", err)
		}
		if err := json.Unmarshal(details, &rec.Details); err != nil {
			return nil, fmt.Errorf("audit: decoding details for entry %d: %w", rec.ID, err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterating entries: %w", err)
	}
	return records, nil
}

// List paging bounds.
const (
	defaultListLimit = 100
	maxListLimit     = 500
)
