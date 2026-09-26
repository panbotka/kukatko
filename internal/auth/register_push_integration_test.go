//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// failingPush is a PushScheduler that refuses every notification, standing in
// for a queue that will not take a job.
type failingPush struct{}

// Enqueue always fails.
func (failingPush) Enqueue(context.Context, pushjob.Execer, string, push.Notification) (int, error) {
	return 0, errors.New("the queue is on fire")
}

// pushRegistration builds a Registration over env wired as production wires it —
// real mail and push enqueuers inserting real jobs, the real notification store
// — except that pusher replaces the push enqueuer when it is not nil. The
// returned buffer collects what the registration logged.
func pushRegistration(t *testing.T, env *httpEnv, pusher auth.PushScheduler) (*auth.Registration, *bytes.Buffer) {
	t.Helper()
	if pusher == nil {
		pusher = pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: true})
	}
	logs := &bytes.Buffer{}
	return auth.NewRegistration(auth.RegistrationConfig{
		Service:       env.svc,
		Settings:      env.settings,
		Mail:          mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true}),
		Notifications: notification.NewStore(env.db.Pool()),
		Push:          pusher,
		Logger:        slog.New(slog.NewTextHandler(logs, nil)),
	}), logs
}

// registerNewcomer registers "newcomer" with the right secret and entry.
func registerNewcomer(t *testing.T, rg *auth.Registration, entry audit.Entry) (auth.User, error) {
	t.Helper()
	return rg.Register(t.Context(), auth.RegisterInput{
		Username:    "newcomer",
		DisplayName: "Nový Rodák",
		Email:       "newcomer@example.test",
		Password:    testPassword,
		Secret:      theSecret,
	}, entry)
}

// registerEntry is the audit entry the registration handler stamps.
func registerEntry() audit.Entry {
	return audit.Entry{Action: audit.ActionUserRegister, TargetType: "users"}
}

// subscribeDevice stores one push subscription for userUID. It writes the row
// directly: the delivery is never attempted here, only its scheduling.
func subscribeDevice(t *testing.T, env *httpEnv, userUID, endpoint string) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`INSERT INTO push_subscriptions (id, user_uid, endpoint, p256dh, auth, user_agent)
		 VALUES ($1, $2, $3, 'key', 'secret', 'test')`,
		"ps"+strings.TrimPrefix(endpoint, "https://push.example.test/"), userUID, endpoint); err != nil {
		t.Fatalf("subscribing %s: %v", userUID, err)
	}
}

// turnOff stores userUID's choice not to be told about pending registrations.
func turnOff(t *testing.T, env *httpEnv, userUID string) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`INSERT INTO notification_prefs (user_uid, kind, enabled) VALUES ($1, $2, FALSE)`,
		userUID, string(notification.KindRegistrationPending)); err != nil {
		t.Fatalf("turning the kind off for %s: %v", userUID, err)
	}
}

// storedNotification is the part of a notifications row these tests read.
type storedNotification struct {
	uid, userUID, kind, title, body, link string
}

// storedNotifications returns every notification, ordered by owner.
func storedNotifications(t *testing.T, env *httpEnv) []storedNotification {
	t.Helper()
	rows, err := env.db.Pool().Query(t.Context(),
		`SELECT uid, user_uid, kind, title, body, link FROM notifications ORDER BY user_uid`)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	defer rows.Close()
	var out []storedNotification
	for rows.Next() {
		var n storedNotification
		if err := rows.Scan(&n.uid, &n.userUID, &n.kind, &n.title, &n.body, &n.link); err != nil {
			t.Fatalf("scanning a notification: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating notifications: %v", err)
	}
	return out
}

// pushPayload is the part of a queued `push_send` job these tests read.
type pushPayload struct {
	SubscriptionID string            `json:"subscription_id"`
	Notification   push.Notification `json:"notification"`
}

// queuedPushes returns every `push_send` job's payload, oldest first.
func queuedPushes(t *testing.T, env *httpEnv) []pushPayload {
	t.Helper()
	rows, err := env.db.Pool().Query(t.Context(),
		`SELECT payload FROM jobs WHERE type = 'push_send' ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the push queue: %v", err)
	}
	defer rows.Close()
	var out []pushPayload
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scanning a push job: %v", err)
		}
		var p pushPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decoding a push payload: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating push jobs: %v", err)
	}
	return out
}

// pendingMailTo reports whether a "new registration pending" mail to addr is on
// the queue.
func pendingMailTo(t *testing.T, env *httpEnv, addr string) bool {
	t.Helper()
	for _, m := range queuedMail(t, env) {
		if m.Template == mailer.TemplateNewRegistrationPending && m.To == addr {
			return true
		}
	}
	return false
}

// TestRegister_pushesEveryApproverWhoWantsIt is the push half of the happy path:
// every enabled admin and maintainer who has not turned the kind off gets one
// notification record, naming the newcomer and opening the approval screen,
// and one delivery per device; the one who turned it off gets no record and no
// delivery but still the mail; an editor gets nothing at all.
func TestRegister_pushesEveryApproverWhoWantsIt(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	keeper := env.mustCreate(t, "keeper", auth.RoleMaintainer)
	quiet := env.mustCreate(t, "quiet", auth.RoleAdmin)
	scribe := env.mustCreate(t, "scribe", auth.RoleEditor)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-laptop")
	subscribeDevice(t, env, quiet.UID, "https://push.example.test/quiet-phone")
	subscribeDevice(t, env, scribe.UID, "https://push.example.test/scribe-phone")
	turnOff(t, env, quiet.UID)
	openRegistration(t, env)
	rg, logs := pushRegistration(t, env, nil)

	if _, err := registerNewcomer(t, rg, registerEntry()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	stored := storedNotifications(t, env)
	owners := map[string]storedNotification{}
	for _, n := range stored {
		owners[n.userUID] = n
	}
	if len(stored) != 2 || owners[boss.UID].uid == "" || owners[keeper.UID].uid == "" {
		t.Fatalf("notifications = %+v, want one each for boss and keeper", stored)
	}
	for _, n := range stored {
		if n.kind != string(notification.KindRegistrationPending) || n.link != "/users" {
			t.Errorf("notification %+v: want kind registration_pending, link /users", n)
		}
		if n.title != "Nová registrace čeká na schválení" || !strings.Contains(n.body, "Nový Rodák") {
			t.Errorf("notification %+v: want the fixed title and a body naming Nový Rodák", n)
		}
	}

	pushes := queuedPushes(t, env)
	if len(pushes) != 2 {
		t.Fatalf("queued %d pushes, want 2 (boss's two devices): %+v", len(pushes), pushes)
	}
	for _, p := range pushes {
		want := push.Notification{
			Title: owners[boss.UID].title, Body: owners[boss.UID].body, URL: "/users",
			Kind: string(notification.KindRegistrationPending), Tag: owners[boss.UID].uid,
		}
		if p.Notification != want || !strings.HasPrefix(p.SubscriptionID, "psboss-") {
			t.Errorf("push = %+v, want %+v to one of boss's devices", p, want)
		}
	}

	if !pendingMailTo(t, env, quiet.Email) {
		t.Error("the admin who turned push off got no mail; the mail must not depend on it")
	}
	if logs.Len() != 0 {
		t.Errorf("a clean registration logged: %s", logs)
	}
}

// TestRegister_failedPushIsLoggedAndRegistrationGoesOn checks the best-effort
// rule: a push that will not schedule is logged, leaves no half-written record,
// and neither the account nor any mail is lost.
func TestRegister_failedPushIsLoggedAndRegistrationGoesOn(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	openRegistration(t, env)
	rg, logs := pushRegistration(t, env, failingPush{})

	user, err := registerNewcomer(t, rg, registerEntry())
	if err != nil {
		t.Fatalf("Register with a failing push = %v, want success", err)
	}
	if user.ApprovedAt != nil {
		t.Error("the registered account is approved; it must wait")
	}
	if got := storedNotifications(t, env); len(got) != 0 {
		t.Errorf("notifications = %+v, want none: a record without its delivery must roll back", got)
	}
	if !pendingMailTo(t, env, boss.Email) {
		t.Error("the admin's mail is missing after a failed push")
	}
	if got := countAudit(t, env, audit.ActionUserRegister); got != 1 {
		t.Errorf("registration audit entries = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), "could not push to an administrator") ||
		!strings.Contains(logs.String(), boss.UID) {
		t.Errorf("log = %q, want the failed push logged with the recipient", logs)
	}
}

// TestRegister_rollbackLeavesNoNotification checks that the notifications share
// the account's fate: a registration whose transaction fails after they were
// written leaves no account, no notification and nothing queued.
func TestRegister_rollbackLeavesNoNotification(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	openRegistration(t, env)
	rg, _ := pushRegistration(t, env, nil)

	// The audit entry is the last write of the transaction; details it cannot
	// encode fail it after the mails and the notifications are in.
	entry := registerEntry()
	entry.Details = map[string]any{"unencodable": func() {}}
	if _, err := registerNewcomer(t, rg, entry); err == nil {
		t.Fatal("Register with an unwritable audit entry succeeded; the test needs it to fail")
	}

	if _, err := env.store.GetUserByUsername(t.Context(), "newcomer"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("GetUserByUsername after a rollback = %v, want ErrUserNotFound", err)
	}
	if got := storedNotifications(t, env); len(got) != 0 {
		t.Errorf("notifications = %+v, want none after a rollback", got)
	}
	if got := queuedPushes(t, env); len(got) != 0 {
		t.Errorf("pushes = %+v, want none after a rollback", got)
	}
	if got := queuedMail(t, env); len(got) != 0 {
		t.Errorf("mails = %+v, want none after a rollback", got)
	}
}

// TestRegister_closedRegistrationNotifiesNobody checks that only a registration
// that really leaves somebody waiting produces a notification.
func TestRegister_closedRegistrationNotifiesNobody(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	rg, _ := pushRegistration(t, env, nil)

	if _, err := registerNewcomer(t, rg, registerEntry()); !errors.Is(err, auth.ErrRegistrationClosed) {
		t.Fatalf("Register on a closed instance = %v, want ErrRegistrationClosed", err)
	}
	if got := storedNotifications(t, env); len(got) != 0 {
		t.Errorf("notifications = %+v, want none", got)
	}
	if got := queuedPushes(t, env); len(got) != 0 {
		t.Errorf("pushes = %+v, want none", got)
	}
}

// TestRegister_pushWithNobodyToNotify checks the instance whose only admin is
// disabled: nothing is recorded or queued, and the registration succeeds.
func TestRegister_pushWithNobodyToNotify(t *testing.T) {
	env := newHTTPEnv(t, 10)
	retired := env.mustCreate(t, "retired", auth.RoleAdmin)
	subscribeDevice(t, env, retired.UID, "https://push.example.test/retired-phone")
	if _, err := env.svc.SetUserDisabled(t.Context(), retired.UID, true); err != nil {
		t.Fatalf("disabling the admin: %v", err)
	}
	openRegistration(t, env)
	rg, _ := pushRegistration(t, env, nil)

	if _, err := registerNewcomer(t, rg, registerEntry()); err != nil {
		t.Fatalf("Register with nobody to notify: %v", err)
	}
	if got := storedNotifications(t, env); len(got) != 0 {
		t.Errorf("notifications = %+v, want none", got)
	}
	if got := queuedPushes(t, env); len(got) != 0 {
		t.Errorf("pushes = %+v, want none", got)
	}
}
