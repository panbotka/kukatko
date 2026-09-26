//go:build integration

package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture bundles the stores one notification test needs over a freshly
// truncated integration database.
type fixture struct {
	db     *database.DB
	store  *notification.Store
	photos *photos.Store
	users  *auth.Store
}

// Accounts every test seeds: the owner of the notifications and a stranger.
// owner is also the audit actor — audit_log.actor_uid is a foreign key.
const (
	owner    = "us-owner"
	stranger = "us-stranger"
)

// newFixture returns the stores over a clean database with both accounts seeded.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{
		db:     db,
		store:  notification.NewStore(db.Pool()),
		photos: photos.NewStore(db.Pool()),
		users:  auth.NewStore(db.Pool()),
	}
	f.makeUser(t, owner, "owner")
	f.makeUser(t, stranger, "stranger")
	return f
}

// makeUser inserts a viewer account with the given uid and username.
func (f *fixture) makeUser(t *testing.T, uid, username string) {
	t.Helper()
	if err := f.users.CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, Email: username + "@example.test",
		PasswordHash: "x", Role: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
}

// makePhotos catalogues one photo per name and returns their uids in order.
func (f *fixture) makePhotos(t *testing.T, names ...string) []string {
	t.Helper()
	uids := make([]string, 0, len(names))
	for _, name := range names {
		created, err := f.photos.Create(context.Background(), photos.Photo{
			FileHash: (name + strings.Repeat("0", 64))[:64],
			FilePath: "2026/09/" + name + ".jpg",
			FileName: name + ".jpg",
			FileSize: 1024,
			FileMime: "image/jpeg",
		})
		if err != nil {
			t.Fatalf("creating photo %s: %v", name, err)
		}
		uids = append(uids, created.UID)
	}
	return uids
}

// exec runs one statement against the test database, failing the test on error.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// count returns the row count of a query of the form SELECT count(*) ….
func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// tagged records a tag notification for owner over photoUIDs.
func (f *fixture) tagged(t *testing.T, photoUIDs ...string) notification.Notification {
	t.Helper()
	n, err := f.store.Create(context.Background(), notification.New{
		UserUID: owner, Kind: notification.KindTagged,
		Title: "Byl jste označen", Body: "na 3 fotkách", Link: "/notifications/x",
		PhotoUIDs: photoUIDs,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return n
}

// TestCreateKeepsOrder checks the record round-trips and its set reads back in
// the order it was given, not in uid or insertion-id order.
func TestCreateKeepsOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.makePhotos(t, "a", "b", "c", "d")
	order := []string{p[2], p[0], p[3], p[1]}

	created := f.tagged(t, order...)
	if !strings.HasPrefix(created.UID, "nt") || created.UserUID != owner ||
		created.Kind != notification.KindTagged || created.Title != "Byl jste označen" ||
		created.Body != "na 3 fotkách" || created.Link != "/notifications/x" ||
		created.ReadAt != nil || created.CreatedAt.IsZero() || created.PhotoCount != 4 {
		t.Fatalf("unexpected created record: %+v", created)
	}
	got, err := f.store.Photos(ctx, owner, created.UID, notification.Visibility{})
	if err != nil {
		t.Fatalf("Photos: %v", err)
	}
	if !reflect.DeepEqual(got, order) {
		t.Fatalf("Photos = %v, want %v", got, order)
	}
	read, err := f.store.Get(ctx, owner, created.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read, created) {
		t.Fatalf("Get = %+v, want %+v", read, created)
	}
}

// TestCreateWithoutPhotos checks the photo-less kind is a legitimate record.
func TestCreateWithoutPhotos(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.Create(ctx, notification.New{
		UserUID: owner, Kind: notification.KindRegistrationPending, Title: "Nová registrace",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := f.store.Photos(ctx, owner, created.UID, notification.Visibility{})
	if err != nil || got == nil || len(got) != 0 || created.PhotoCount != 0 {
		t.Fatalf("Photos = %v, %v (count %d), want an empty non-nil set", got, err, created.PhotoCount)
	}
}

// TestCreateIsAtomic checks a failing photo leaves no half-written
// notification, and an unknown account is refused likewise.
func TestCreateIsAtomic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.makePhotos(t, "a")

	_, err := f.store.Create(ctx, notification.New{
		UserUID: owner, Kind: notification.KindTagged, Title: "t",
		PhotoUIDs: []string{p[0], "ph-missing"},
	})
	if !errors.Is(err, notification.ErrPhotoNotFound) {
		t.Fatalf("Create with a missing photo = %v, want ErrPhotoNotFound", err)
	}
	_, err = f.store.Create(ctx, notification.New{UserUID: "us-nobody", Kind: notification.KindTagged, Title: "t"})
	if !errors.Is(err, notification.ErrUserNotFound) {
		t.Fatalf("Create for a missing account = %v, want ErrUserNotFound", err)
	}
	if n := f.count(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Fatalf("%d notifications left behind, want 0", n)
	}
	if n := f.count(t, "SELECT count(*) FROM notification_photos"); n != 0 {
		t.Fatalf("%d set rows left behind, want 0", n)
	}
}

// TestForeignUIDIsNotFound checks every owner-scoped operation answers a
// stranger exactly as it answers a uid that does not exist.
func TestForeignUIDIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine := f.tagged(t, f.makePhotos(t, "a")...)

	for _, uid := range []string{mine.UID, "nt-does-not-exist"} {
		if _, err := f.store.Get(ctx, stranger, uid); !errors.Is(err, notification.ErrNotFound) {
			t.Errorf("Get(%s) as stranger = %v, want ErrNotFound", uid, err)
		}
		if _, err := f.store.MarkRead(ctx, stranger, uid); !errors.Is(err, notification.ErrNotFound) {
			t.Errorf("MarkRead(%s) as stranger = %v, want ErrNotFound", uid, err)
		}
		if _, err := f.store.Photos(ctx, stranger, uid, notification.Visibility{}); !errors.Is(err, notification.ErrNotFound) {
			t.Errorf("Photos(%s) as stranger = %v, want ErrNotFound", uid, err)
		}
	}
	after, err := f.store.Get(ctx, owner, mine.UID)
	if err != nil || after.ReadAt != nil {
		t.Fatalf("the stranger's MarkRead touched the owner's notification: %+v, %v", after, err)
	}
}

// TestMarkReadIsIdempotent checks a second mark keeps the first reading.
func TestMarkReadIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created := f.tagged(t)

	first, err := f.store.MarkRead(ctx, owner, created.UID)
	if err != nil || first.ReadAt == nil {
		t.Fatalf("MarkRead = %+v, %v; want it read", first, err)
	}
	time.Sleep(5 * time.Millisecond)
	second, err := f.store.MarkRead(ctx, owner, created.UID)
	if err != nil || second.ReadAt == nil || !second.ReadAt.Equal(*first.ReadAt) {
		t.Fatalf("second MarkRead = %+v, %v; want read_at kept at %v", second, err, first.ReadAt)
	}
}

// TestPhotosAppliesVisibility checks an archived, hidden or private photo stays
// in the set but is dropped unless the caller lets it through, and a deleted one
// is gone for everybody.
func TestPhotosAppliesVisibility(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.makePhotos(t, "live", "archived", "hidden", "private", "deleted")
	created := f.tagged(t, p...)

	f.exec(t, "UPDATE photos SET archived_at = now() WHERE uid = $1", p[1])
	f.exec(t, "UPDATE photos SET hidden_from_library = TRUE WHERE uid = $1", p[2])
	f.exec(t, "UPDATE photos SET private = TRUE WHERE uid = $1", p[3])
	f.exec(t, "DELETE FROM photos WHERE uid = $1", p[4])

	tests := []struct {
		name string
		vis  notification.Visibility
		want []string
	}{
		{name: "strict", vis: notification.Visibility{}, want: []string{p[0]}},
		{name: "archived", vis: notification.Visibility{IncludeArchived: true}, want: []string{p[0], p[1]}},
		{name: "hidden", vis: notification.Visibility{IncludeHidden: true}, want: []string{p[0], p[2]}},
		{name: "private", vis: notification.Visibility{IncludePrivate: true}, want: []string{p[0], p[3]}},
		{
			name: "everything",
			vis:  notification.Visibility{IncludeArchived: true, IncludeHidden: true, IncludePrivate: true},
			want: p[:4],
		},
	}
	for _, tt := range tests {
		got, err := f.store.Photos(ctx, owner, created.UID, tt.vis)
		if err != nil {
			t.Fatalf("%s: Photos: %v", tt.name, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: Photos = %v, want %v", tt.name, got, tt.want)
		}
	}
	read, err := f.store.Get(ctx, owner, created.UID)
	if err != nil || read.PhotoCount != 4 {
		t.Fatalf("Get = %+v, %v; want a photo count of 4 (the deleted one gone)", read, err)
	}
}

// TestWholeSetDeleted checks a notification outlives every photo of its set.
func TestWholeSetDeleted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.makePhotos(t, "a", "b")
	created := f.tagged(t, p...)
	f.exec(t, "DELETE FROM photos")

	read, err := f.store.Get(ctx, owner, created.UID)
	if err != nil || read.PhotoCount != 0 {
		t.Fatalf("Get = %+v, %v; want the record with no photos", read, err)
	}
	got, err := f.store.Photos(ctx, owner, created.UID, notification.Visibility{IncludeArchived: true})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Photos = %v, %v; want an empty non-nil set", got, err)
	}
}

// TestPreferencesMergeDefaults checks an account with no rows reads defaults and
// a stored row overrides only its own kind.
func TestPreferencesMergeDefaults(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	got, err := f.store.Preferences(ctx, owner)
	if err != nil {
		t.Fatalf("Preferences: %v", err)
	}
	want := []notification.Preference{
		{Kind: notification.KindTagged, Enabled: true, IsDefault: true},
		{Kind: notification.KindRegistrationPending, Enabled: true, IsDefault: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}

	f.exec(t, "INSERT INTO notification_prefs (user_uid, kind, enabled) VALUES ($1, 'tagged', FALSE), "+
		"($1, 'retired_kind', TRUE)", owner)
	got, err = f.store.Preferences(ctx, owner)
	if err != nil {
		t.Fatalf("Preferences: %v", err)
	}
	want[0] = notification.Preference{Kind: notification.KindTagged, Enabled: false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %+v, want %+v", got, want)
	}
	wants, err := f.store.Wants(ctx, owner, notification.KindTagged)
	if err != nil || wants {
		t.Fatalf("Wants(tagged) = %v, %v; want false", wants, err)
	}
	wants, err = f.store.Wants(ctx, owner, notification.KindRegistrationPending)
	if err != nil || !wants {
		t.Fatalf("Wants(registration_pending) = %v, %v; want the default true", wants, err)
	}
	if _, err := f.store.Wants(ctx, owner, "later"); !errors.Is(err, notification.ErrUnknownKind) {
		t.Fatalf("Wants(unknown) = %v, want ErrUnknownKind", err)
	}
}

// TestReplacePreferencesIsAudited checks a replace stores the choices, resets
// the kinds it leaves out, and records who did it in the same transaction.
func TestReplacePreferencesIsAudited(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	entry := audit.Entry{ActorUID: owner, IP: "192.0.2.1"}

	if _, err := f.store.ReplacePreferences(ctx, owner, []notification.Preference{
		{Kind: notification.KindTagged, Enabled: false},
		{Kind: notification.KindRegistrationPending, Enabled: false},
	}, entry); err != nil {
		t.Fatalf("ReplacePreferences: %v", err)
	}
	got, err := f.store.ReplacePreferences(ctx, owner, []notification.Preference{
		{Kind: notification.KindRegistrationPending, Enabled: false},
	}, entry)
	if err != nil {
		t.Fatalf("ReplacePreferences: %v", err)
	}
	want := []notification.Preference{
		{Kind: notification.KindTagged, Enabled: true, IsDefault: true},
		{Kind: notification.KindRegistrationPending, Enabled: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after replace = %+v, want %+v", got, want)
	}

	var (
		actor, target, targetType string
		raw                       []byte
	)
	if err := f.db.Pool().QueryRow(ctx, `SELECT actor_uid, target_type, target_uid, details FROM audit_log
		WHERE action = $1 ORDER BY id DESC LIMIT 1`, audit.ActionNotificationPrefsUpdate,
	).Scan(&actor, &targetType, &target, &raw); err != nil {
		t.Fatalf("reading audit entry: %v", err)
	}
	var details struct {
		Preferences map[string]bool `json:"preferences"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decoding details %s: %v", raw, err)
	}
	if actor != owner || targetType != "users" || target != owner ||
		!reflect.DeepEqual(details.Preferences, map[string]bool{"registration_pending": false}) {
		t.Fatalf("audit entry = %s/%s/%s %s", actor, targetType, target, raw)
	}
	if n := f.count(t, "SELECT count(*) FROM audit_log WHERE action = $1",
		audit.ActionNotificationPrefsUpdate); n != 2 {
		t.Fatalf("%d audit entries, want one per replace (2)", n)
	}
}

// TestReplacePreferencesRefuses checks a repeated kind, an unknown kind and a
// missing account write nothing — neither preferences nor an audit entry.
func TestReplacePreferencesRefuses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	entry := audit.Entry{ActorUID: owner}

	tests := []struct {
		name    string
		user    string
		prefs   []notification.Preference
		wantErr error
	}{
		{
			name: "same kind twice", user: owner, wantErr: notification.ErrDuplicateKind,
			prefs: []notification.Preference{
				{Kind: notification.KindTagged, Enabled: false}, {Kind: notification.KindTagged, Enabled: true},
			},
		},
		{
			name: "unknown kind", user: owner, wantErr: notification.ErrUnknownKind,
			prefs: []notification.Preference{{Kind: "later"}},
		},
		{name: "missing account", user: "us-nobody", wantErr: notification.ErrUserNotFound},
	}
	for _, tt := range tests {
		if _, err := f.store.ReplacePreferences(ctx, tt.user, tt.prefs, entry); !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: ReplacePreferences = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
	if n := f.count(t, "SELECT count(*) FROM notification_prefs"); n != 0 {
		t.Fatalf("%d preference rows written, want 0", n)
	}
	if n := f.count(t, "SELECT count(*) FROM audit_log"); n != 0 {
		t.Fatalf("%d audit entries written, want 0", n)
	}
}

// TestPurgeRespectsBothThresholds checks read and unread notifications go at
// their own ages and everything newer stays.
func TestPurgeRespectsBothThresholds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	retention := notification.Retention{Read: 30 * 24 * time.Hour, Unread: 90 * 24 * time.Hour}

	// age in days, read or not, and whether the purge must delete it.
	cases := []struct {
		name  string
		age   int
		read  bool
		purge bool
	}{
		{name: "fresh read", age: 1, read: true},
		{name: "read just inside", age: 29, read: true},
		{name: "read past its threshold", age: 31, read: true, purge: true},
		{name: "unread past the read threshold", age: 31},
		{name: "unread just inside", age: 89},
		{name: "unread past its threshold", age: 91, purge: true},
		{name: "read long ago", age: 200, read: true, purge: true},
	}
	uids := make([]string, len(cases))
	for i, c := range cases {
		n := f.tagged(t, f.makePhotos(t, "p"+string(rune('a'+i)))...)
		uids[i] = n.UID
		created := now.Add(-time.Duration(c.age) * 24 * time.Hour)
		f.exec(t, "UPDATE notifications SET created_at = $2 WHERE uid = $1", n.UID, created)
		if c.read {
			f.exec(t, "UPDATE notifications SET read_at = $2 WHERE uid = $1", n.UID, created)
		}
	}

	if _, err := f.store.Purge(ctx, now, notification.Retention{Read: 90 * time.Hour, Unread: time.Hour}); !errors.Is(err, notification.ErrInvalidRetention) {
		t.Fatalf("Purge with unread < read = %v, want ErrInvalidRetention", err)
	}
	deleted, err := f.store.Purge(ctx, now, retention)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	wantDeleted := 0
	for i, c := range cases {
		_, getErr := f.store.Get(ctx, owner, uids[i])
		gone := errors.Is(getErr, notification.ErrNotFound)
		if c.purge {
			wantDeleted++
		}
		if gone != c.purge {
			t.Errorf("%s: gone = %v (err %v), want %v", c.name, gone, getErr, c.purge)
		}
	}
	if deleted != int64(wantDeleted) {
		t.Fatalf("Purge deleted %d, want %d", deleted, wantDeleted)
	}
	if n := f.count(t, "SELECT count(*) FROM notification_photos"); n != len(cases)-wantDeleted {
		t.Fatalf("%d set rows left, want %d (the purged sets cascade)", n, len(cases)-wantDeleted)
	}
}

// TestDeletingAccountCascades checks an account's notifications and preferences
// go with it.
func TestDeletingAccountCascades(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.tagged(t, f.makePhotos(t, "a")...)
	if _, err := f.store.ReplacePreferences(ctx, owner, []notification.Preference{
		{Kind: notification.KindTagged},
	}, audit.Entry{ActorUID: stranger}); err != nil {
		t.Fatalf("ReplacePreferences: %v", err)
	}
	f.exec(t, "DELETE FROM users WHERE uid = $1", owner)
	for _, table := range []string{"notifications", "notification_photos", "notification_prefs"} {
		if n := f.count(t, "SELECT count(*) FROM "+table); n != 0 {
			t.Errorf("%s holds %d rows after the account was deleted, want 0", table, n)
		}
	}
}

// TestCreateTxSharesTheCallersFate checks the transaction-bound variants: a
// notification created on a transaction that rolls back is gone, one created on
// a transaction that commits is there, and WantsTx sees a choice the same
// transaction wrote before it was committed.
func TestCreateTxSharesTheCallersFate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := notification.New{
		UserUID: owner, Kind: notification.KindRegistrationPending,
		Title: "Nová registrace čeká na schválení", Link: "/users",
	}

	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := f.store.CreateTx(ctx, tx, n); err != nil {
		t.Fatalf("CreateTx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := f.count(t, "SELECT count(*) FROM notifications"); got != 0 {
		t.Fatalf("after a rollback %d notifications remain, want 0", got)
	}

	tx, err = f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "INSERT INTO notification_prefs (user_uid, kind, enabled) "+
		"VALUES ($1, 'registration_pending', FALSE)", stranger); err != nil {
		t.Fatalf("storing a preference: %v", err)
	}
	if wants, err := f.store.WantsTx(ctx, tx, stranger, notification.KindRegistrationPending); err != nil || wants {
		t.Fatalf("WantsTx(stranger) = %v, %v; want the uncommitted false", wants, err)
	}
	if wants, err := f.store.WantsTx(ctx, tx, owner, notification.KindRegistrationPending); err != nil || !wants {
		t.Fatalf("WantsTx(owner) = %v, %v; want the default true", wants, err)
	}
	created, err := f.store.CreateTx(ctx, tx, n)
	if err != nil {
		t.Fatalf("CreateTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := f.store.Get(ctx, owner, created.UID)
	if err != nil {
		t.Fatalf("Get after commit: %v", err)
	}
	if got.Title != n.Title || got.Link != n.Link || got.Kind != n.Kind {
		t.Errorf("stored %+v, want the fields of %+v", got, n)
	}
}
