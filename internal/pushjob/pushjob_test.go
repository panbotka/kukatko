package pushjob

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/worker"
)

// memStore is an in-memory SubscriptionStore that can fail each operation on
// demand.
type memStore struct {
	subs       map[string]push.Subscription
	successes  int
	getErr     error
	successErr error
	failureErr error
	deleteErr  error
}

// newMemStore returns a store holding sub.
func newMemStore(sub push.Subscription) *memStore {
	return &memStore{subs: map[string]push.Subscription{sub.ID: sub}}
}

// Get returns the stored subscription or push.ErrNotFound.
func (m *memStore) Get(_ context.Context, id string) (push.Subscription, error) {
	if m.getErr != nil {
		return push.Subscription{}, m.getErr
	}
	sub, ok := m.subs[id]
	if !ok {
		return push.Subscription{}, push.ErrNotFound
	}
	return sub, nil
}

// RecordSuccess counts the success and resets the failure count.
func (m *memStore) RecordSuccess(_ context.Context, id string) error {
	if m.successErr != nil {
		return m.successErr
	}
	sub, ok := m.subs[id]
	if !ok {
		return push.ErrNotFound
	}
	sub.FailureCount = 0
	m.subs[id] = sub
	m.successes++
	return nil
}

// RecordFailure increments and returns the failure count.
func (m *memStore) RecordFailure(_ context.Context, id string) (int, error) {
	if m.failureErr != nil {
		return 0, m.failureErr
	}
	sub, ok := m.subs[id]
	if !ok {
		return 0, push.ErrNotFound
	}
	sub.FailureCount++
	m.subs[id] = sub
	return sub.FailureCount, nil
}

// DeleteByID removes the subscription when it belongs to userUID.
func (m *memStore) DeleteByID(_ context.Context, userUID, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	sub, ok := m.subs[id]
	if !ok || sub.UserUID != userUID {
		return push.ErrNotFound
	}
	delete(m.subs, id)
	return nil
}

// device is the subscription the unit tests deliver to.
var device = newDevice("psphone", "u1", "https://push.example.org/phone")

// newDevice returns a subscription with freshly generated client keys that
// push.ValidateSubscription accepts, so the push.Fake's guards let the sends
// through. It panics only if the system random source fails.
func newDevice(id, userUID, endpoint string) push.Subscription {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}
	return push.Subscription{
		ID: id, UserUID: userUID, Endpoint: endpoint,
		P256dh: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(secret),
	}
}

// stubStanding is a Standing answering every question the same way.
type stubStanding struct {
	ok  bool
	err error
	// asked records the addressees it was asked about.
	asked []notifystanding.Addressee
}

// InStanding returns the configured answer.
func (s *stubStanding) InStanding(_ context.Context, a notifystanding.Addressee) (bool, error) {
	s.asked = append(s.asked, a)
	return s.ok, s.err
}

// inStanding is a Standing that lets every account through.
func inStanding() *stubStanding { return &stubStanding{ok: true} }

// jobFor returns a push_send job addressed to subscriptionID and device's
// account u1 in the project pj1, carrying n.
func jobFor(t *testing.T, subscriptionID string, n push.Notification) jobs.Job {
	t.Helper()
	return jobTo(t, subscriptionID, notifystanding.Addressee{UserUID: "u1", ProjectUID: "pj1", Kind: n.Kind}, n)
}

// jobTo returns a push_send job for subscriptionID addressed to to, carrying n.
func jobTo(t *testing.T, subscriptionID string, to notifystanding.Addressee, n push.Notification) jobs.Job {
	t.Helper()
	raw, err := json.Marshal(payload{SubscriptionID: subscriptionID, To: to, Notification: n})
	if err != nil {
		t.Fatalf("encoding the payload: %v", err)
	}
	return jobs.Job{ID: 7, Type: jobs.TypePushSend, Payload: raw}
}

// isTerminal reports whether err is the worker's permanent-failure signal.
func isTerminal(err error) bool {
	var terminal *worker.TerminalError
	return errors.As(err, &terminal)
}

// outcome names what a Handle result means to the queue.
func outcome(err error) string {
	switch {
	case err == nil:
		return "complete"
	case isTerminal(err):
		return "terminal"
	default:
		return "retry"
	}
}

// TestHandle_senderAnswers verifies every answer of the sender maps to the
// right job outcome and the right change to the device's record.
func TestHandle_senderAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		sendErr      error
		n            push.Notification
		want         string
		wantDeleted  bool
		wantFailures int
		wantSent     int
	}{
		{name: "accepted", n: note, want: "complete", wantSent: 1},
		{name: "gone", sendErr: push.ErrGone, n: note, want: "complete", wantDeleted: true},
		{name: "transient", sendErr: push.ErrRetryable, n: note, want: "retry", wantFailures: 1},
		{name: "rejected", sendErr: push.ErrRejected, n: note, want: "terminal", wantFailures: 1},
		{
			name: "oversized", n: push.Notification{Title: "x", Body: strings.Repeat("a", push.MaxPayloadSize)},
			want: "terminal",
		},
		{name: "invalid", n: push.Notification{Title: "x", URL: "//evil.example"}, want: "terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newMemStore(device)
			sender := push.NewFake()
			if tt.sendErr != nil {
				sender.FailWith(fmt.Errorf("push service: %w", tt.sendErr))
			}
			svc := NewService(ServiceConfig{Enabled: true, Sender: sender, Store: store, Standing: inStanding()})

			err := svc.Handle(context.Background(), jobFor(t, device.ID, tt.n))
			if got := outcome(err); got != tt.want {
				t.Fatalf("Handle = %v (%s), want %s", err, got, tt.want)
			}
			sub, stillThere := store.subs[device.ID]
			if stillThere == tt.wantDeleted {
				t.Errorf("subscription still there = %v, want deleted = %v", stillThere, tt.wantDeleted)
			}
			if stillThere && sub.FailureCount != tt.wantFailures {
				t.Errorf("failure count = %d, want %d", sub.FailureCount, tt.wantFailures)
			}
			if got := len(sender.Sent()); got != tt.wantSent || store.successes != tt.wantSent {
				t.Errorf("sent %d, recorded %d successes; want %d", got, store.successes, tt.wantSent)
			}
		})
	}
}

// TestHandle_retiresAHopelessDevice verifies the failure that reaches the
// threshold deletes the subscription and completes instead of retrying.
func TestHandle_retiresAHopelessDevice(t *testing.T) {
	t.Parallel()

	store := newMemStore(device)
	sender := push.NewFake()
	sender.FailWith(push.ErrRetryable)
	svc := NewService(ServiceConfig{Enabled: true, Sender: sender, Store: store, MaxFailures: 3, Standing: inStanding()})

	for attempt := 1; attempt <= 3; attempt++ {
		err := svc.Handle(context.Background(), jobFor(t, device.ID, note))
		want := "retry"
		if attempt == 3 {
			want = "complete"
		}
		if got := outcome(err); got != want {
			t.Fatalf("attempt %d: Handle = %v (%s), want %s", attempt, err, got, want)
		}
	}
	if _, ok := store.subs[device.ID]; ok {
		t.Fatal("a device that failed MaxFailures times in a row is still subscribed")
	}
}

// TestHandle_nothingToDo verifies the cases that complete without sending:
// push switched off after the job was queued, and a device that is gone.
func TestHandle_nothingToDo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		enabled bool
		subID   string
	}{
		{name: "push disabled", enabled: false, subID: device.ID},
		{name: "unsubscribed meanwhile", enabled: true, subID: "psgone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sender := push.NewFake()
			svc := NewService(ServiceConfig{Enabled: tt.enabled, Sender: sender, Store: newMemStore(device), Standing: inStanding()})
			if err := svc.Handle(context.Background(), jobFor(t, tt.subID, note)); err != nil {
				t.Fatalf("Handle = %v, want nil", err)
			}
			if len(sender.Sent()) != 0 {
				t.Errorf("sent %d notifications, want none", len(sender.Sent()))
			}
		})
	}
}

// TestHandle_badPayloadIsTerminal verifies a payload the handler cannot act on
// is never retried.
func TestHandle_badPayloadIsTerminal(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"not json":        `{`,
		"no subscription": `{"notification":{"title":"x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := NewService(ServiceConfig{Enabled: true, Sender: push.NewFake(), Store: newMemStore(device), Standing: inStanding()})
			err := svc.Handle(context.Background(), jobs.Job{Type: jobs.TypePushSend, Payload: json.RawMessage(raw)})
			if !isTerminal(err) {
				t.Fatalf("Handle = %v, want a terminal failure", err)
			}
		})
	}
}

// TestHandle_storeFailures verifies how a database failure around the send is
// treated: reading or bookkeeping a failure retries, a lost success stamp does
// not (the notification was delivered), and a vanished row completes.
func TestHandle_storeFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("db down")
	tests := []struct {
		name    string
		mutate  func(*memStore)
		sendErr error
		want    string
	}{
		{name: "read fails", mutate: func(m *memStore) { m.getErr = boom }, want: "retry"},
		{name: "success stamp fails", mutate: func(m *memStore) { m.successErr = boom }, want: "complete"},
		{
			name: "failure stamp fails", mutate: func(m *memStore) { m.failureErr = boom },
			sendErr: push.ErrRetryable, want: "retry",
		},
		{
			name: "row vanished before the failure stamp", mutate: func(m *memStore) { m.failureErr = push.ErrNotFound },
			sendErr: push.ErrRejected, want: "complete",
		},
		{
			name: "gone but delete fails", mutate: func(m *memStore) { m.deleteErr = boom },
			sendErr: push.ErrGone, want: "retry",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newMemStore(device)
			tt.mutate(store)
			sender := push.NewFake()
			sender.FailWith(tt.sendErr)
			svc := NewService(ServiceConfig{Enabled: true, Sender: sender, Store: store, Standing: inStanding()})
			err := svc.Handle(context.Background(), jobFor(t, device.ID, note))
			if got := outcome(err); got != tt.want {
				t.Fatalf("Handle = %v (%s), want %s", err, got, tt.want)
			}
		})
	}
}

// TestNewService_requiresCollaborators verifies the wiring bug panics at
// construction and that the threshold defaults.
func TestNewService_requiresCollaborators(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]ServiceConfig{
		"no sender":   {Store: newMemStore(device), Standing: inStanding()},
		"no store":    {Sender: push.NewFake(), Standing: inStanding()},
		"no standing": {Sender: push.NewFake(), Store: newMemStore(device)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("NewService did not panic")
				}
			}()
			NewService(cfg)
		})
	}
	if svc := NewService(ServiceConfig{Sender: push.NewFake(), Store: newMemStore(device), Standing: inStanding()}); svc.maxFailures != MaxFailures {
		t.Errorf("maxFailures = %d, want %d", svc.maxFailures, MaxFailures)
	}
}

// TestHandle_addressee verifies the job is checked against whom it is for
// before anything is sent: a device that now belongs to another account, a job
// naming no account and an account out of standing all complete unsent and are
// counted; a failed standing query retries.
func TestHandle_addressee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		to         notifystanding.Addressee
		standing   *stubStanding
		want       string
		wantReason notifystanding.Reason
		wantAsked  bool
	}{
		{
			name: "device moved to another account", to: notifystanding.Addressee{UserUID: "u2", ProjectUID: "pj1"},
			standing: inStanding(), want: "complete", wantReason: notifystanding.ReasonDeviceMoved,
		},
		{
			name: "unaddressed legacy job", standing: inStanding(), want: "complete",
			wantReason: notifystanding.ReasonUnaddressed,
		},
		{
			name: "out of standing", to: notifystanding.Addressee{UserUID: "u1", ProjectUID: "pj1"},
			standing: &stubStanding{}, want: "complete", wantReason: notifystanding.ReasonStanding, wantAsked: true,
		},
		{
			name: "standing query fails", to: notifystanding.Addressee{UserUID: "u1", ProjectUID: "pj1"},
			standing: &stubStanding{err: errors.New("db down")}, want: "retry", wantAsked: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sender := push.NewFake()
			svc := NewService(ServiceConfig{
				Enabled: true, Sender: sender, Store: newMemStore(device), Standing: tt.standing,
			})
			before := pushDrops(tt.wantReason)
			err := svc.Handle(context.Background(), jobTo(t, device.ID, tt.to, note))
			if got := outcome(err); got != tt.want {
				t.Fatalf("Handle = %v (%s), want %s", err, got, tt.want)
			}
			if len(sender.Sent()) != 0 {
				t.Errorf("sent %d notifications, want none", len(sender.Sent()))
			}
			if asked := len(tt.standing.asked) > 0; asked != tt.wantAsked {
				t.Errorf("standing asked = %v, want %v", asked, tt.wantAsked)
			}
			if tt.wantReason != "" && pushDrops(tt.wantReason) <= before {
				t.Errorf("push drops for %s did not grow from %d", tt.wantReason, before)
			}
		})
	}
}

// TestHandle_asksAboutTheAddressee verifies an addressed job is delivered when
// its account is in standing, and that the question names that account, the
// project and the kind.
func TestHandle_asksAboutTheAddressee(t *testing.T) {
	t.Parallel()

	sender := push.NewFake()
	standing := inStanding()
	svc := NewService(ServiceConfig{Enabled: true, Sender: sender, Store: newMemStore(device), Standing: standing})
	if err := svc.Handle(context.Background(), jobFor(t, device.ID, note)); err != nil {
		t.Fatalf("Handle = %v", err)
	}
	want := notifystanding.Addressee{UserUID: "u1", ProjectUID: "pj1", Kind: note.Kind}
	if len(standing.asked) != 1 || standing.asked[0] != want {
		t.Errorf("asked about %+v, want %+v", standing.asked, want)
	}
	if len(sender.Sent()) != 1 {
		t.Errorf("sent %d notifications, want 1", len(sender.Sent()))
	}
}

// pushDrops reads the process-wide count of pushes dropped for reason.
func pushDrops(reason notifystanding.Reason) uint64 {
	for _, d := range notifystanding.Drops() {
		if d.Stage == notifystanding.StagePush && d.Reason == reason {
			return d.Count
		}
	}
	return 0
}
