//go:build integration

package push_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/push"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns a push.Store plus the auth store used to seed accounts, over
// a freshly truncated integration database.
func newStore(t *testing.T) (*push.Store, *auth.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return push.NewStore(db.Pool()), auth.NewStore(db.Pool()), db
}

// makeUser inserts a viewer account with the given uid/username and returns the uid.
func makeUser(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		Email:        username + "@example.test",
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// subscription returns a valid subscription of userUID for endpoint, with fresh
// client keys.
func subscription(t *testing.T, userUID, endpoint, userAgent string) push.Subscription {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatalf("generating auth: %v", err)
	}
	return push.Subscription{
		UserUID:   userUID,
		Endpoint:  endpoint,
		P256dh:    base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		Auth:      base64.RawURLEncoding.EncodeToString(auth),
		UserAgent: userAgent,
	}
}

// TestStore_upsertIsKeyedByEndpoint proves the same browser subscribing again
// updates its row (same id, new keys, failures cleared) instead of adding one.
func TestStore_upsertIsKeyedByEndpoint(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "push_owner", "owner")
	const endpoint = "https://push.example.org/send/one"

	first, err := store.Upsert(ctx, subscription(t, owner, endpoint, "Firefox"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() || first.LastUsedAt != nil || first.FailureCount != 0 {
		t.Fatalf("unexpected new row: %+v", first)
	}
	if _, err := store.RecordFailure(ctx, first.ID); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if err := store.RecordSuccess(ctx, first.ID); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	if _, err := store.RecordFailure(ctx, first.ID); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	again := subscription(t, owner, endpoint, "Firefox 131")
	second, err := store.Upsert(ctx, again)
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("re-subscription got a new identity: %+v vs %+v", second, first)
	}
	if second.P256dh != again.P256dh || second.Auth != again.Auth || second.UserAgent != "Firefox 131" {
		t.Fatalf("re-subscription did not take the new keys: %+v", second)
	}
	if second.FailureCount != 0 || second.LastFailureAt != nil || second.LastUsedAt == nil {
		t.Fatalf("re-subscription bookkeeping = %+v, want failures cleared and last use kept", second)
	}
	list, err := store.ListForUser(ctx, owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListForUser = %d rows, %v; want exactly 1", len(list), err)
	}
}

// TestStore_upsertMovesAnEndpointBetweenAccounts proves a browser that another
// person subscribes on becomes theirs, with its history started over.
func TestStore_upsertMovesAnEndpointBetweenAccounts(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	alice := makeUser(t, users, "push_alice", "alice")
	bob := makeUser(t, users, "push_bob", "bob")
	const endpoint = "https://push.example.org/send/shared"

	first, err := store.Upsert(ctx, subscription(t, alice, endpoint, ""))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.RecordSuccess(ctx, first.ID); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	moved, err := store.Upsert(ctx, subscription(t, bob, endpoint, ""))
	if err != nil {
		t.Fatalf("Upsert for bob: %v", err)
	}
	if moved.UserUID != bob || moved.LastUsedAt != nil {
		t.Fatalf("moved row = %+v, want bob's with no last use", moved)
	}
	if list, _ := store.ListForUser(ctx, alice); len(list) != 0 {
		t.Fatalf("alice still lists %d subscriptions", len(list))
	}
}

// TestStore_upsertRefusesInvalid proves nothing unsendable reaches the table.
func TestStore_upsertRefusesInvalid(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "push_owner", "owner")

	bad := subscription(t, owner, "http://push.example.org/x", "")
	if _, err := store.Upsert(ctx, bad); !errors.Is(err, push.ErrInvalidSubscription) {
		t.Fatalf("http endpoint: error = %v", err)
	}
	orphan := subscription(t, "", "https://push.example.org/x", "")
	if _, err := store.Upsert(ctx, orphan); !errors.Is(err, push.ErrInvalidSubscription) {
		t.Fatalf("no account: error = %v", err)
	}
}

// TestStore_listAndDelete covers listing per account and the three deletes,
// including that DeleteByID never touches another account's row.
func TestStore_listAndDelete(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	alice := makeUser(t, users, "push_alice", "alice")
	bob := makeUser(t, users, "push_bob", "bob")

	phone, err := store.Upsert(ctx, subscription(t, alice, "https://push.example.org/phone", "phone"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	laptop, err := store.Upsert(ctx, subscription(t, alice, "https://push.example.org/laptop", "laptop"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	tablet, err := store.Upsert(ctx, subscription(t, alice, "https://push.example.org/tablet", "tablet"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	bobs, err := store.Upsert(ctx, subscription(t, bob, "https://push.example.org/bob", "bob"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	list, err := store.ListForUser(ctx, alice)
	if err != nil || len(list) != 3 || list[0].ID != phone.ID {
		t.Fatalf("ListForUser(alice) = %+v, %v", list, err)
	}
	if empty, err := store.ListForUser(ctx, "nobody"); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ListForUser(nobody) = %#v, %v; want an empty non-nil slice", empty, err)
	}

	if err := store.DeleteByID(ctx, alice, bobs.ID); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("DeleteByID of a foreign row: error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteByID(ctx, alice, laptop.ID); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if err := store.DeleteByEndpoint(ctx, tablet.Endpoint); err != nil {
		t.Fatalf("DeleteByEndpoint: %v", err)
	}
	if err := store.DeleteByEndpoint(ctx, tablet.Endpoint); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("second DeleteByEndpoint: error = %v, want ErrNotFound", err)
	}
	if list, _ := store.ListForUser(ctx, alice); len(list) != 1 || list[0].ID != phone.ID {
		t.Fatalf("after deletes alice lists %+v, want only the phone", list)
	}

	removed, err := store.DeleteAllForUser(ctx, alice)
	if err != nil || removed != 1 {
		t.Fatalf("DeleteAllForUser = %d, %v; want 1", removed, err)
	}
	if list, _ := store.ListForUser(ctx, bob); len(list) != 1 {
		t.Fatalf("bob lost his subscription: %+v", list)
	}
}

// TestStore_bookkeeping covers the failure count and its reset on success, and
// ErrNotFound for an unknown id.
func TestStore_bookkeeping(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "push_owner", "owner")
	sub, err := store.Upsert(ctx, subscription(t, owner, "https://push.example.org/x", ""))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for want := 1; want <= 3; want++ {
		got, err := store.RecordFailure(ctx, sub.ID)
		if err != nil || got != want {
			t.Fatalf("RecordFailure #%d = %d, %v", want, got, err)
		}
	}
	list, _ := store.ListForUser(ctx, owner)
	if list[0].FailureCount != 3 || list[0].LastFailureAt == nil {
		t.Fatalf("after failures: %+v", list[0])
	}
	if err := store.RecordSuccess(ctx, sub.ID); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	list, _ = store.ListForUser(ctx, owner)
	if list[0].FailureCount != 0 || list[0].LastUsedAt == nil || list[0].LastFailureAt == nil {
		t.Fatalf("after success: %+v, want count reset, last use stamped, last failure kept", list[0])
	}

	if err := store.RecordSuccess(ctx, "psmissing"); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("RecordSuccess(unknown) = %v", err)
	}
	if _, err := store.RecordFailure(ctx, "psmissing"); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("RecordFailure(unknown) = %v", err)
	}
}

// TestStore_accountDeletionCascades proves the foreign key: deleting an account
// takes its subscriptions with it.
func TestStore_accountDeletionCascades(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "push_owner", "owner")
	if _, err := store.Upsert(ctx, subscription(t, owner, "https://push.example.org/x", "")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM users WHERE uid = $1`, owner); err != nil {
		t.Fatalf("deleting the account: %v", err)
	}
	var left int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM push_subscriptions`).Scan(&left); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d subscriptions survived their account", left)
	}
}
