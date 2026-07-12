// Package audit writes the durable audit trail. Entries are appended to the
// audit_log table and, crucially, can be written through a caller-supplied
// executor so the record joins the same transaction as the mutation it
// describes (see ARCHITECTURE.md §5.1, §11, §12: "audit log durable — written in
// the same transaction as the mutation", fixing photo-sorter's after-commit
// gap). A nil-safe Store wraps a pool for standalone writes and reads (admin
// listing, tests).
//
// # The in-transaction convention
//
// A mutating path records audit by running its change and the audit insert on
// the same pgx.Tx, committing them together:
//
//	tx, err := pool.Begin(ctx)
//	// ... defer rollback ...
//	// mutate using tx ...
//	if err := audit.Write(ctx, tx, meta.Entry(audit.ActionPhotoUpdate, "photos", uid, details)); err != nil {
//	    return err
//	}
//	return tx.Commit(ctx)
//
// If the mutation fails the transaction rolls back and the audit row vanishes
// with it; on success both commit atomically. Handlers build a Meta once per
// request with FromRequest (actor UID from the auth context, client IP and
// User-Agent from the request) and stamp it onto every entry.
//
// # Column naming
//
// The table (migration 0012, extended by 0014) uses actor_uid/target_type/
// target_uid/details/ip/user_agent. The M6 spec names the first four
// user_uid/entity_type/entity_uid/metadata; they are the same concepts under the
// originally shipped column names.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	// ActionPhotoUpdate records a single-photo metadata edit (PATCH).
	ActionPhotoUpdate = "photo.update"
	// ActionPhotoArchive records moving a photo to the trash (soft delete).
	ActionPhotoArchive = "photo.archive"
	// ActionPhotoUnarchive records restoring a photo from the trash.
	ActionPhotoUnarchive = "photo.unarchive"
	// ActionAlbumCreate records creating an album.
	ActionAlbumCreate = "album.create"
	// ActionAlbumUpdate records editing an album's metadata.
	ActionAlbumUpdate = "album.update"
	// ActionAlbumDelete records deleting an album.
	ActionAlbumDelete = "album.delete"
	// ActionAlbumAddPhotos records adding one or more photos to an album; the
	// affected photo UIDs are listed in the entry's details.
	ActionAlbumAddPhotos = "album.add_photos"
	// ActionAlbumRemovePhotos records removing one or more photos from an album;
	// the affected photo UIDs are listed in the entry's details.
	ActionAlbumRemovePhotos = "album.remove_photos"
	// ActionLabelCreate records creating a label.
	ActionLabelCreate = "label.create"
	// ActionLabelUpdate records editing a label.
	ActionLabelUpdate = "label.update"
	// ActionLabelDelete records deleting a label.
	ActionLabelDelete = "label.delete"
	// ActionLabelAttach records attaching a label to a photo; the photo UID is
	// recorded in the entry's details.
	ActionLabelAttach = "label.attach"
	// ActionLabelDetach records detaching a label from a photo; the photo UID is
	// recorded in the entry's details.
	ActionLabelDetach = "label.detach"
	// ActionFaceAssign records assigning a face (marker) to a subject, whether by
	// creating a new marker for the face or pointing an existing marker at the
	// subject; the marker and subject UIDs are recorded in the entry's details.
	ActionFaceAssign = "face.assign"
	// ActionFaceUnassign records clearing a face marker's subject; the marker UID
	// is recorded in the entry's details.
	ActionFaceUnassign = "face.unassign"
	// ActionSubjectCreate records creating a subject (person/pet/other).
	ActionSubjectCreate = "subject.create"
	// ActionSubjectUpdate records editing a subject's fields.
	ActionSubjectUpdate = "subject.update"
	// ActionSubjectDelete records deleting a subject; its name and type are
	// recorded in the entry's details.
	ActionSubjectDelete = "subject.delete"
	// ActionUserCreate records creating a user account.
	ActionUserCreate = "user.create"
	// ActionUserUpdate records editing a user account.
	ActionUserUpdate = "user.update"
	// ActionUserDisable records disabling/enabling a user account.
	ActionUserDisable = "user.disable"
	// ActionUserPassword records an admin password reset for a user.
	ActionUserPassword = "user.password"
	// ActionAPITokenCreate records minting a long-lived API token.
	ActionAPITokenCreate = "api_token.create"
	// ActionAPITokenRevoke records revoking a long-lived API token.
	ActionAPITokenRevoke = "api_token.revoke"
)

// insertSQL appends one audit entry. It is shared by Store.Record and the
// package-level Write so the column order stays in one place.
const insertSQL = `
INSERT INTO audit_log (actor_uid, action, target_type, target_uid, details, ip, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

// Entry is a single audit record describing who did what. ActorUID, TargetUID,
// IP and UserAgent are optional (empty string stores SQL NULL); Details is
// serialised to JSONB and defaults to an empty object when nil.
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
	// IP is the client IP address the action came from, or empty when unknown.
	IP string
	// UserAgent is the client User-Agent string, or empty when unknown.
	UserAgent string
}

// Meta carries the request-derived audit fields shared by every entry a handler
// records: who is acting and from where. Build it once per request with
// FromRequest and stamp it onto entries with Entry.
type Meta struct {
	// ActorUID is the acting user's UID (resolved from the auth context).
	ActorUID string
	// IP is the client IP address the request came from.
	IP string
	// UserAgent is the request's User-Agent header.
	UserAgent string
}

// FromRequest builds a Meta from r and the actorUID resolved by the caller from
// the auth context. The audit package must not depend on internal/auth, so the
// actor UID is passed in rather than read here. The client IP prefers the first
// X-Forwarded-For hop, then X-Real-IP, then the request's RemoteAddr host.
func FromRequest(r *http.Request, actorUID string) Meta {
	return Meta{ActorUID: actorUID, IP: clientIP(r), UserAgent: r.UserAgent()}
}

// Entry stamps the meta's actor/IP/User-Agent onto a new Entry with the given
// action, target and details, so handlers do not repeat those fields.
func (m Meta) Entry(action, targetType, targetUID string, details map[string]any) Entry {
	return Entry{
		ActorUID:   m.ActorUID,
		Action:     action,
		TargetType: targetType,
		TargetUID:  targetUID,
		Details:    details,
		IP:         m.IP,
		UserAgent:  m.UserAgent,
	}
}

// metaContextKey is the private key under which a request's Meta is carried in a
// context. It lets mutation code reached through several intermediary interfaces
// (for example the facematch assignment state machine, invoked by both the photo
// faces endpoint and the cluster-assign endpoint) recover the acting user, client
// IP and User-Agent without every layer threading an explicit parameter.
type metaContextKey struct{}

// ContextWithMeta returns a copy of ctx carrying meta. A handler stashes the Meta
// it built with FromRequest here so a downstream mutation, too far from the HTTP
// request to receive it as a parameter, can stamp it onto its audit entry.
func ContextWithMeta(ctx context.Context, meta Meta) context.Context {
	return context.WithValue(ctx, metaContextKey{}, meta)
}

// MetaFromContext returns the Meta stored by ContextWithMeta, or the zero Meta
// (empty actor/IP/User-Agent, which the entry stores as SQL NULL) when none is
// present — so a code path exercised without a request still records an
// unattributed entry rather than failing.
func MetaFromContext(ctx context.Context) Meta {
	meta, _ := ctx.Value(metaContextKey{}).(Meta)
	return meta
}

// clientIP extracts the originating client IP from r, preferring proxy headers
// (X-Forwarded-For first hop, then X-Real-IP) and falling back to the RemoteAddr
// host. It returns an empty string when no address can be determined.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// Record is a stored audit entry as read back from the table.
type Record struct {
	ID         int64          `json:"id"`
	ActorUID   *string        `json:"actor_uid"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetUID  *string        `json:"target_uid"`
	Details    map[string]any `json:"details"`
	IP         *string        `json:"ip"`
	UserAgent  *string        `json:"user_agent"`
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
		nullable(entry.TargetUID), details, nullable(entry.IP), nullable(entry.UserAgent),
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

// Store wraps a pgx pool for standalone audit writes and reads. The in-transaction
// write path uses the package-level Write with a caller transaction instead;
// Store backs admin listing and tests.
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

// Filter narrows an audit listing. Empty string fields and nil times are
// ignored, so a zero Filter matches every entry. Limit/Offset paginate the
// result newest-first.
type Filter struct {
	// ActorUID restricts to entries by one acting user.
	ActorUID string
	// TargetType restricts to one entity kind (for example "photos").
	TargetType string
	// TargetUID restricts to one entity instance.
	TargetUID string
	// Action restricts to one action label.
	Action string
	// Since restricts to entries created at or after this instant.
	Since *time.Time
	// Until restricts to entries created at or before this instant.
	Until *time.Time
	// Limit caps the page size; non-positive or oversized values clamp to the
	// default/maximum page.
	Limit int
	// Offset skips this many leading rows.
	Offset int
}

// buildWhere assembles the WHERE clause and positional arguments shared by List
// and Count from f's non-empty filter fields. It returns the clause (including
// the leading " WHERE " or empty when unfiltered) and the argument slice.
func (f Filter) buildWhere() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	add := func(expr string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expr, len(args)))
	}
	if f.ActorUID != "" {
		add("actor_uid = $%d", f.ActorUID)
	}
	if f.TargetType != "" {
		add("target_type = $%d", f.TargetType)
	}
	if f.TargetUID != "" {
		add("target_uid = $%d", f.TargetUID)
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.Since != nil {
		add("created_at >= $%d", *f.Since)
	}
	if f.Until != nil {
		add("created_at <= $%d", *f.Until)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// List returns audit records matching f, newest-first, applying the filter's
// pagination. A non-positive or oversized limit clamps to the default/maximum
// page so the query never returns the whole table by accident.
func (s *Store) List(ctx context.Context, f Filter) ([]Record, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}
	offset := max(f.Offset, 0)
	where, args := f.buildWhere()
	query := "SELECT id, actor_uid, action, target_type, target_uid, details, ip, user_agent, created_at" +
		" FROM audit_log" + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: listing entries: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterating entries: %w", err)
	}
	return records, nil
}

// rowScanner is the subset of pgx.Rows scanRecord needs, so it can scan from a
// query result row.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRecord reads one audit row into a Record, decoding the details JSONB. It
// returns a wrapped error on scan or JSON failure.
func scanRecord(row rowScanner) (Record, error) {
	var (
		rec     Record
		details []byte
	)
	if err := row.Scan(&rec.ID, &rec.ActorUID, &rec.Action, &rec.TargetType,
		&rec.TargetUID, &details, &rec.IP, &rec.UserAgent, &rec.CreatedAt); err != nil {
		return Record{}, fmt.Errorf("audit: scanning entry: %w", err)
	}
	if err := json.Unmarshal(details, &rec.Details); err != nil {
		return Record{}, fmt.Errorf("audit: decoding details for entry %d: %w", rec.ID, err)
	}
	return rec, nil
}

// Count returns the total number of audit entries matching f, ignoring its
// pagination fields. It backs the total in a paginated admin response.
func (s *Store) Count(ctx context.Context, f Filter) (int, error) {
	where, args := f.buildWhere()
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log"+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("audit: counting entries: %w", err)
	}
	return total, nil
}

// List paging bounds.
const (
	defaultListLimit = 100
	maxListLimit     = 500
)
