package push

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription is one browser or device an account receives notifications on:
// one push_subscriptions row. A sender reads only Endpoint, P256dh and Auth.
type Subscription struct {
	// ID is the row's own identifier ("ps" + 24 base32 characters).
	ID string `json:"id"`
	// UserUID is the account the notifications are for.
	UserUID string `json:"user_uid"`
	// Endpoint is the push service URL the browser handed back; it is unique —
	// the identity of the subscription.
	Endpoint string `json:"endpoint"`
	// P256dh is the browser's P-256 public key the payload is encrypted to.
	P256dh string `json:"p256dh"`
	// Auth is the browser's 16-byte authentication secret.
	Auth string `json:"auth"`
	// UserAgent lets a person tell their devices apart; it may be empty.
	UserAgent string `json:"user_agent"`
	// CreatedAt is when this browser subscribed for this account.
	CreatedAt time.Time `json:"created_at"`
	// LastUsedAt is the last successful send, nil when there has been none.
	LastUsedAt *time.Time `json:"last_used_at"`
	// FailureCount counts the failed sends since the last successful one.
	FailureCount int `json:"failure_count"`
	// LastFailureAt is when the latest failed send happened, nil if none has.
	LastFailureAt *time.Time `json:"last_failure_at"`
}

const (
	// subscriptionIDPrefix marks ids that identify push_subscriptions rows.
	subscriptionIDPrefix = "ps"
	// idSuffixLen is the number of random base32 characters after the prefix:
	// ~120 bits, 26 characters in all — within VARCHAR(32).
	idSuffixLen = 24
	// idAlphabet is a 32-symbol lowercase base32 alphabet; 32 divides 256, so
	// masking a random byte's low five bits picks a symbol without bias.
	idAlphabet = "0123456789abcdefghijklmnopqrstuv"
)

// newSubscriptionID returns a fresh row id, failing only if the system random
// source does.
func newSubscriptionID() (string, error) {
	buf := make([]byte, idSuffixLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("push: reading random bytes: %w", err)
	}
	var sb strings.Builder
	sb.Grow(len(subscriptionIDPrefix) + idSuffixLen)
	sb.WriteString(subscriptionIDPrefix)
	for _, b := range buf {
		sb.WriteByte(idAlphabet[b&0x1f])
	}
	return sb.String(), nil
}

// Store is the database access layer for push subscriptions. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// subscriptionColumns is the column list every query returns, in scan order.
const subscriptionColumns = `id, user_uid, endpoint, p256dh, auth, user_agent,
created_at, last_used_at, failure_count, last_failure_at`

// row is the column scanner shared by single-row and multi-row reads.
type row interface {
	Scan(dest ...any) error
}

// scanSubscription reads one row into a Subscription, mapping pgx.ErrNoRows to
// ErrNotFound.
func scanSubscription(r row) (Subscription, error) {
	var sub Subscription
	err := r.Scan(&sub.ID, &sub.UserUID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.UserAgent,
		&sub.CreatedAt, &sub.LastUsedAt, &sub.FailureCount, &sub.LastFailureAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("push: scanning a subscription: %w", err)
	}
	return sub, nil
}

// upsertSubscriptionSQL inserts a subscription or, when its endpoint is already
// stored, rewrites that row in place. A re-subscription is a fresh start, so
// the failure bookkeeping is cleared; when the endpoint changes hands (another
// person signed in on the same browser and subscribed) it is a new subscription
// for that person, so its created and last-used stamps start over too.
const upsertSubscriptionSQL = `
INSERT INTO push_subscriptions (id, user_uid, endpoint, p256dh, auth, user_agent)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (endpoint) DO UPDATE SET
    user_uid        = EXCLUDED.user_uid,
    p256dh          = EXCLUDED.p256dh,
    auth            = EXCLUDED.auth,
    user_agent      = EXCLUDED.user_agent,
    created_at      = CASE WHEN push_subscriptions.user_uid = EXCLUDED.user_uid
                           THEN push_subscriptions.created_at ELSE now() END,
    last_used_at    = CASE WHEN push_subscriptions.user_uid = EXCLUDED.user_uid
                           THEN push_subscriptions.last_used_at END,
    failure_count   = 0,
    last_failure_at = NULL
RETURNING ` + subscriptionColumns

// Upsert stores sub for sub.UserUID, keyed by its endpoint: a new endpoint adds
// a row, a known one is updated in place and keeps its id, so the same browser
// subscribing twice never ends up with two rows. It returns the stored row.
// sub.ID and the bookkeeping fields are ignored. A subscription that could not
// be sent to is refused with ErrInvalidSubscription before it reaches the table;
// a missing sub.UserUID is refused the same way.
func (s *Store) Upsert(ctx context.Context, sub Subscription) (Subscription, error) {
	if strings.TrimSpace(sub.UserUID) == "" {
		return Subscription{}, fmt.Errorf("%w: no account", ErrInvalidSubscription)
	}
	if err := ValidateSubscription(sub); err != nil {
		return Subscription{}, err
	}
	id, err := newSubscriptionID()
	if err != nil {
		return Subscription{}, err
	}
	stored, err := scanSubscription(s.pool.QueryRow(ctx, upsertSubscriptionSQL, id, sub.UserUID,
		sub.Endpoint, strings.TrimSpace(sub.P256dh), strings.TrimSpace(sub.Auth), sub.UserAgent))
	if err != nil {
		return Subscription{}, fmt.Errorf("push: storing the subscription for %s: %w", sub.UserUID, err)
	}
	return stored, nil
}

// listSubscriptionsSQL returns one account's subscriptions, oldest first, with
// the id as a stable tie-break.
const listSubscriptionsSQL = `
SELECT ` + subscriptionColumns + `
FROM push_subscriptions
WHERE user_uid = $1
ORDER BY created_at, id`

// ListForUser returns every subscription of userUID, oldest first. An account
// with none yields an empty (non-nil) slice.
func (s *Store) ListForUser(ctx context.Context, userUID string) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, listSubscriptionsSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("push: listing the subscriptions of %s: %w", userUID, err)
	}
	defer rows.Close()

	out := make([]Subscription, 0)
	for rows.Next() {
		sub, scanErr := scanSubscription(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: iterating the subscriptions of %s: %w", userUID, err)
	}
	return out, nil
}

// Get returns the subscription id, whoever it belongs to. It returns
// ErrNotFound when no row has that id — the browser unsubscribed, or its
// account was deleted and the cascade took its subscriptions along.
func (s *Store) Get(ctx context.Context, id string) (Subscription, error) {
	sub, err := scanSubscription(s.pool.QueryRow(ctx,
		`SELECT `+subscriptionColumns+` FROM push_subscriptions WHERE id = $1`, id))
	if errors.Is(err, ErrNotFound) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("push: reading subscription %s: %w", id, err)
	}
	return sub, nil
}

// Querier is the subset of pgx a multi-row read needs. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a read can run on its own connection or inside a
// caller's transaction.
type Querier interface {
	// Query runs sql with args and returns the rows it produced.
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// SubscriptionIDs returns the ids of every subscription of userUID, oldest
// first, read through q. It is a package function rather than a Store method
// because its caller fans a notification out inside the transaction of the
// mutation that caused it, and wants the read to see exactly what that
// transaction sees. An account with none yields an empty (non-nil) slice.
func SubscriptionIDs(ctx context.Context, q Querier, userUID string) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT id FROM push_subscriptions WHERE user_uid = $1 ORDER BY created_at, id`, userUID)
	if err != nil {
		return nil, fmt.Errorf("push: listing the subscription ids of %s: %w", userUID, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("push: reading the subscription ids of %s: %w", userUID, err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// DeleteByEndpoint removes the subscription with the given endpoint, whoever it
// belongs to — the endpoint is what a browser unsubscribing and a push service
// answering ErrGone both name. It returns ErrNotFound when no row has it.
func (s *Store) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	if err != nil {
		return fmt.Errorf("push: deleting a subscription by endpoint: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByID removes the subscription id of the account userUID. The owner is
// part of the match, so an id belonging to somebody else is ErrNotFound exactly
// like one that does not exist — a person managing their devices can never
// remove another person's.
func (s *Store) DeleteByID(ctx context.Context, userUID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM push_subscriptions WHERE id = $1 AND user_uid = $2`, id, userUID)
	if err != nil {
		return fmt.Errorf("push: deleting subscription %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAllForUser removes every subscription of userUID and returns how many
// there were; an account with none is not an error.
func (s *Store) DeleteAllForUser(ctx context.Context, userUID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_uid = $1`, userUID)
	if err != nil {
		return 0, fmt.Errorf("push: deleting the subscriptions of %s: %w", userUID, err)
	}
	return tag.RowsAffected(), nil
}

// RecordSuccess stamps a successful send through subscription id: last_used_at
// becomes now and the failure count starts over. It returns ErrNotFound for an
// unknown id.
func (s *Store) RecordSuccess(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE push_subscriptions
SET last_used_at = now(), failure_count = 0
WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("push: recording a send through %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordFailure counts one more failed send through subscription id and stamps
// last_failure_at, returning the new count — the number of failures since the
// last success — so the caller can retire a subscription that keeps failing. It
// returns ErrNotFound for an unknown id.
func (s *Store) RecordFailure(ctx context.Context, id string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
UPDATE push_subscriptions
SET failure_count = failure_count + 1, last_failure_at = now()
WHERE id = $1
RETURNING failure_count`, id).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("push: recording a failed send through %s: %w", id, err)
	}
	return count, nil
}
