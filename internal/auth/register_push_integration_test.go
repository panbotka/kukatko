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
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// failingPush is a PushScheduler that refuses every notification, standing in
// for a queue that will not take a job.
type failingPush struct{}

// Enabled reports push as on, so the delivery reaches Enqueue.
func (failingPush) Enabled() bool { return true }

// Enqueue always fails.
func (failingPush) Enqueue(context.Context, pushjob.Execer, notifystanding.Addressee, push.Notification) (int, error) {
	return 0, errors.New("the queue is on fire")
}

// pushRegistration builds a Registration over env wired as production wires it —
// real mail and push enqueuers inserting real jobs, the real notification store
// behind the real delivery path — except that pusher replaces the push enqueuer
// when it is not nil. The returned buffer collects what the registration logged.
func pushRegistration(
	t *testing.T, env *httpEnv, pusher notifydeliver.PushScheduler,
) (*auth.Registration, *bytes.Buffer) {
	t.Helper()
	if pusher == nil {
		pusher = pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: true})
	}
	mail := mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true})
	logs := &bytes.Buffer{}
	return auth.NewRegistration(auth.RegistrationConfig{
		Service:  env.svc,
		Settings: env.settings,
		Mail:     mail,
		Notify: notifydeliver.New(notifydeliver.Config{
			Projects: project.NewStore(env.db.Pool()), Notifications: notification.NewStore(env.db.Pool()),
			Push: pusher, Mail: mail,
		}),
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}), logs
}

// registerNewcomer registers "newcomer" into the bootstrap project with the
// right secret and entry.
func registerNewcomer(t *testing.T, rg *auth.Registration, entry audit.Entry) (auth.User, error) {
	t.Helper()
	return rg.Register(t.Context(), auth.RegisterInput{
		Username:    "newcomer",
		DisplayName: "Nový Rodák",
		Email:       "newcomer@example.test",
		Password:    testPassword,
		Secret:      theSecret,
		ProjectUID:  project.BootstrapUID,
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

// choose stores userUID's choice whether to be told about pending registrations
// through channel.
func choose(t *testing.T, env *httpEnv, userUID string, channel notification.Channel, enabled bool) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`INSERT INTO notification_prefs (user_uid, kind, channel, enabled) VALUES ($1, $2, $3, $4)`,
		userUID, string(notification.KindRegistrationPending), string(channel), enabled); err != nil {
		t.Fatalf("storing the %s choice of %s: %v", channel, userUID, err)
	}
}

// storedNotification is the part of a notifications row these tests read.
type storedNotification struct {
	uid, userUID, kind, title, body, link string
	inApp                                 bool
}

// storedNotifications returns every notification, ordered by owner.
func storedNotifications(t *testing.T, env *httpEnv) []storedNotification {
	t.Helper()
	rows, err := env.db.Pool().Query(t.Context(),
		`SELECT uid, user_uid, kind, title, body, link, in_app FROM notifications ORDER BY user_uid`)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	defer rows.Close()
	var out []storedNotification
	for rows.Next() {
		var n storedNotification
		if err := rows.Scan(&n.uid, &n.userUID, &n.kind, &n.title, &n.body, &n.link, &n.inApp); err != nil {
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

// TestRegister_notifiesEveryApproverThroughTheirChannels is the happy path:
// every enabled admin and maintainer gets the notification through exactly the
// channels they opted into — boss by push (one record outside the inbox, one
// delivery per device), keeper in-app (one inbox record, no push), quiet by
// e-mail only (a record backing the mail's link, kept out of the inbox) — and
// nobody who never chose gets anything, because every channel is off by
// default; an editor gets nothing at all.
func TestRegister_notifiesEveryApproverThroughTheirChannels(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.AccountAdmin)
	keeper := env.mustCreate(t, "keeper", auth.AccountMaintainer)
	quiet := env.mustCreate(t, "quiet", auth.AccountAdmin)
	silent := env.mustCreate(t, "silent", auth.AccountAdmin)
	scribe := env.mustCreate(t, "scribe", auth.AccountEditor)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-laptop")
	subscribeDevice(t, env, quiet.UID, "https://push.example.test/quiet-phone")
	subscribeDevice(t, env, silent.UID, "https://push.example.test/silent-phone")
	subscribeDevice(t, env, scribe.UID, "https://push.example.test/scribe-phone")
	choose(t, env, boss.UID, notification.ChannelPush, true)
	choose(t, env, keeper.UID, notification.ChannelInApp, true)
	choose(t, env, quiet.UID, notification.ChannelEmail, true)
	choose(t, env, scribe.UID, notification.ChannelPush, true)
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
	if len(stored) != 3 || owners[boss.UID].uid == "" || owners[keeper.UID].uid == "" || owners[quiet.UID].uid == "" {
		t.Fatalf("notifications = %+v, want one each for boss, keeper and quiet", stored)
	}
	if owners[boss.UID].inApp || !owners[keeper.UID].inApp || owners[quiet.UID].inApp {
		t.Errorf("in_app flags = boss %v, keeper %v, quiet %v; want only keeper's listed",
			owners[boss.UID].inApp, owners[keeper.UID].inApp, owners[quiet.UID].inApp)
	}
	for _, n := range stored {
		if n.kind != string(notification.KindRegistrationPending) || n.link != "/p/"+project.BootstrapSlug+"/members" {
			t.Errorf("notification %+v: want kind registration_pending, link /p/v/members", n)
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
			// The push names the project; the stored title does not need to,
			// the inbox it is listed in is the project's own.
			Title: "Veselice · " + owners[boss.UID].title, Body: owners[boss.UID].body,
			URL:  "/p/" + project.BootstrapSlug + "/members",
			Kind: string(notification.KindRegistrationPending), Tag: owners[boss.UID].uid,
		}
		if p.Notification != want || !strings.HasPrefix(p.SubscriptionID, "psboss-") {
			t.Errorf("push = %+v, want %+v to one of boss's devices", p, want)
		}
	}

	if !pendingMailTo(t, env, quiet.Email) {
		t.Error("the admin who opted into the e-mail got no mail")
	}
	if pendingMailTo(t, env, boss.Email) || pendingMailTo(t, env, keeper.Email) || pendingMailTo(t, env, silent.Email) {
		t.Error("an admin who never opted into the e-mail was mailed; it is off by default")
	}
	if logs.Len() != 0 {
		t.Errorf("a clean registration logged: %s", logs)
	}
}

// TestRegister_failedDeliveryIsLoggedAndRegistrationGoesOn checks the
// best-effort rule: a recipient whose delivery will not schedule is logged and
// rolled back whole — no record, no mail without the rest — while the account
// and the other recipients' notifications are kept.
func TestRegister_failedDeliveryIsLoggedAndRegistrationGoesOn(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.AccountAdmin)
	keeper := env.mustCreate(t, "keeper", auth.AccountAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	choose(t, env, boss.UID, notification.ChannelPush, true)
	choose(t, env, boss.UID, notification.ChannelEmail, true)
	choose(t, env, keeper.UID, notification.ChannelEmail, true)
	openRegistration(t, env)
	rg, logs := pushRegistration(t, env, failingPush{})

	user, err := registerNewcomer(t, rg, registerEntry())
	if err != nil {
		t.Fatalf("Register with a failing push = %v, want success", err)
	}
	if user.ApprovedAt != nil {
		t.Error("the registered account is approved; it must wait")
	}
	got := storedNotifications(t, env)
	if len(got) != 1 || got[0].userUID != keeper.UID {
		t.Errorf("notifications = %+v, want only keeper's: boss's delivery must roll back whole", got)
	}
	if pendingMailTo(t, env, boss.Email) {
		t.Error("boss was mailed although their delivery failed; it must roll back whole")
	}
	if !pendingMailTo(t, env, keeper.Email) {
		t.Error("keeper's mail is missing after boss's delivery failed")
	}
	if got := countAudit(t, env, audit.ActionUserRegister); got != 1 {
		t.Errorf("registration audit entries = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), "could not notify an administrator") ||
		!strings.Contains(logs.String(), boss.UID) {
		t.Errorf("log = %q, want the failed delivery logged with the recipient", logs)
	}
}

// TestRegister_rollbackLeavesNoNotification checks that the notifications share
// the account's fate: a registration whose transaction fails after they were
// written leaves no account, no notification and nothing queued.
func TestRegister_rollbackLeavesNoNotification(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.AccountAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	choose(t, env, boss.UID, notification.ChannelPush, true)
	choose(t, env, boss.UID, notification.ChannelInApp, true)
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
	boss := env.mustCreate(t, "boss", auth.AccountAdmin)
	subscribeDevice(t, env, boss.UID, "https://push.example.test/boss-phone")
	choose(t, env, boss.UID, notification.ChannelPush, true)
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
	retired := env.mustCreate(t, "retired", auth.AccountAdmin)
	subscribeDevice(t, env, retired.UID, "https://push.example.test/retired-phone")
	choose(t, env, retired.UID, notification.ChannelPush, true)
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
