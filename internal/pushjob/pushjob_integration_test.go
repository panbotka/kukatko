//go:build integration

package pushjob_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/worker"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture is a freshly truncated database with one account, the stores over it
// and a fake sender.
type fixture struct {
	db     *database.DB
	store  *push.Store
	jobs   *jobs.Store
	sender *push.Fake
	user   string
}

// newFixture truncates the integration database and seeds one account.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	const uid = "pushjob_owner"
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID: uid, Username: "owner", Email: "owner@example.test", PasswordHash: "x", Role: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating the account: %v", err)
	}
	return fixture{
		db: db, store: push.NewStore(db.Pool()), jobs: jobs.NewStore(db.Pool()),
		sender: push.NewFake(), user: uid,
	}
}

// subscribe stores a subscription of the fixture's account for endpoint.
func (f fixture) subscribe(t *testing.T, endpoint string) push.Subscription {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generating auth: %v", err)
	}
	sub, err := f.store.Upsert(context.Background(), push.Subscription{
		UserUID:  f.user,
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(secret),
	})
	if err != nil {
		t.Fatalf("subscribing %s: %v", endpoint, err)
	}
	return sub
}

// service returns the handler over the fixture with push switched on.
func (f fixture) service(maxFailures int) *pushjob.Service {
	return pushjob.NewService(pushjob.ServiceConfig{
		Enabled: true, Sender: f.sender, Store: f.store, MaxFailures: maxFailures,
	})
}

// enqueuer returns an Enqueuer writing to the real queue.
func enqueuer(enabled bool) *pushjob.Enqueuer {
	return pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: enabled})
}

// claimAll claims every queued push_send job, in insertion order.
func (f fixture) claimAll(t *testing.T) []jobs.Job {
	t.Helper()
	var out []jobs.Job
	for {
		job, err := f.jobs.Claim(context.Background(), "test", jobs.TypePushSend)
		if errors.Is(err, jobs.ErrNoJobs) {
			return out
		}
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		out = append(out, job)
	}
}

// countPushJobs returns how many push_send jobs exist in any state.
func (f fixture) countPushJobs(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM jobs WHERE type = $1`, jobs.TypePushSend).Scan(&n); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	return n
}

// isTerminal reports whether err is the worker's permanent-failure signal.
func isTerminal(err error) bool {
	var terminal *worker.TerminalError
	return errors.As(err, &terminal)
}

// note is the notification the tests deliver.
var note = push.Notification{Title: "Nová odpověď", Body: "Jana odpověděla", URL: "/tasks/t1", Kind: "task"}

// enqueueOne queues note for the fixture's account on the pool and returns the
// single claimed job.
func (f fixture) enqueueOne(t *testing.T) jobs.Job {
	t.Helper()
	if _, err := enqueuer(true).Enqueue(context.Background(), f.db.Pool(), f.user, note); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed := f.claimAll(t)
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs, want 1", len(claimed))
	}
	return claimed[0]
}

// TestPushSend_deliversToEveryDeviceOnce proves the fan-out end to end: two
// devices get one job each, each job delivers to its own device only, and the
// send is recorded on the subscription.
func TestPushSend_deliversToEveryDeviceOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	phone := f.subscribe(t, "https://push.example.org/phone")
	laptop := f.subscribe(t, "https://push.example.org/laptop")

	queued, err := enqueuer(true).Enqueue(ctx, f.db.Pool(), f.user, note)
	if err != nil || queued != 2 {
		t.Fatalf("Enqueue = %d, %v; want 2", queued, err)
	}
	claimed := f.claimAll(t)
	if len(claimed) != 2 {
		t.Fatalf("claimed %d jobs, want 2", len(claimed))
	}
	if claimed[0].MaxAttempts != pushjob.MaxAttempts {
		t.Errorf("MaxAttempts = %d, want %d", claimed[0].MaxAttempts, pushjob.MaxAttempts)
	}
	svc := f.service(0)
	for _, job := range claimed {
		if err := svc.Handle(ctx, job); err != nil {
			t.Fatalf("Handle(%d): %v", job.ID, err)
		}
	}

	sent := f.sender.Sent()
	if len(sent) != 2 || sent[0].Subscription.Endpoint == sent[1].Subscription.Endpoint {
		t.Fatalf("sent %+v, want one delivery per device", sent)
	}
	if sent[0].Notification != note {
		t.Errorf("delivered %+v, want %+v", sent[0].Notification, note)
	}
	for _, sub := range []push.Subscription{phone, laptop} {
		got, err := f.store.Get(ctx, sub.ID)
		if err != nil || got.LastUsedAt == nil || got.FailureCount != 0 {
			t.Errorf("%s after delivery = %+v, %v; want the send recorded", sub.Endpoint, got, err)
		}
	}
}

// TestPushSend_goneDeletesTheSubscription proves a 410 prunes the device and
// completes the job.
func TestPushSend_goneDeletesTheSubscription(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := f.subscribe(t, "https://push.example.org/dead")
	job := f.enqueueOne(t)
	f.sender.FailEndpoint(sub.Endpoint, fmt.Errorf("the push service answered 410: %w", push.ErrGone))

	if err := f.service(0).Handle(ctx, job); err != nil {
		t.Fatalf("Handle = %v, want the job to complete", err)
	}
	if _, err := f.store.Get(ctx, sub.ID); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("Get after 410 = %v, want ErrNotFound", err)
	}
}

// TestPushSend_transientFailureRetries proves a 500 records a failure on the
// device and fails the job so the queue retries it.
func TestPushSend_transientFailureRetries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := f.subscribe(t, "https://push.example.org/flaky")
	job := f.enqueueOne(t)
	f.sender.FailWith(fmt.Errorf("the push service answered 500: %w", push.ErrRetryable))

	err := f.service(0).Handle(ctx, job)
	if err == nil || isTerminal(err) {
		t.Fatalf("Handle = %v, want a retryable failure", err)
	}
	got, getErr := f.store.Get(ctx, sub.ID)
	if getErr != nil || got.FailureCount != 1 || got.LastFailureAt == nil {
		t.Fatalf("after a 500: %+v, %v; want one failure recorded", got, getErr)
	}

	// The queue takes the ordinary error as a retry: the job is requeued, not
	// dead-lettered or parked.
	failed, failErr := f.jobs.Fail(ctx, job.ID, "test", err)
	if failErr != nil {
		t.Fatalf("Fail: %v", failErr)
	}
	if failed.State != jobs.StateQueued {
		t.Errorf("job state = %s, want queued for a retry", failed.State)
	}
}

// TestPushSend_repeatedFailuresRetireTheDevice proves the failure that reaches
// the threshold deletes the subscription and completes the job.
func TestPushSend_repeatedFailuresRetireTheDevice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := f.subscribe(t, "https://push.example.org/broken")
	job := f.enqueueOne(t)
	f.sender.FailWith(fmt.Errorf("the push service answered 503: %w", push.ErrRetryable))
	svc := f.service(3)

	for attempt := 1; attempt < 3; attempt++ {
		if err := svc.Handle(ctx, job); err == nil || isTerminal(err) {
			t.Fatalf("attempt %d: Handle = %v, want a retryable failure", attempt, err)
		}
	}
	if err := svc.Handle(ctx, job); err != nil {
		t.Fatalf("third attempt: Handle = %v, want the job to complete", err)
	}
	if _, err := f.store.Get(ctx, sub.ID); !errors.Is(err, push.ErrNotFound) {
		t.Fatalf("Get after the threshold = %v, want ErrNotFound", err)
	}
}

// TestPushSend_oversizedPayloadIsTerminal proves a notification too big for
// Web Push fails for good and leaves the device alone. The payload is written
// straight into the queue, since Enqueue itself refuses such a notification.
func TestPushSend_oversizedPayloadIsTerminal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := f.subscribe(t, "https://push.example.org/fine")
	raw := `{"subscription_id":"` + sub.ID + `","notification":{"title":"x","body":"` +
		strings.Repeat("a", push.MaxPayloadSize) + `"}}`
	if _, err := jobs.Enqueue(ctx, f.db.Pool(), jobs.TypePushSend, []byte(raw), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("inserting the job: %v", err)
	}
	claimed := f.claimAll(t)

	if err := f.service(0).Handle(ctx, claimed[0]); !isTerminal(err) || !errors.Is(err, push.ErrPayloadTooLarge) {
		t.Fatalf("Handle = %v, want a terminal ErrPayloadTooLarge", err)
	}
	got, err := f.store.Get(ctx, sub.ID)
	if err != nil || got.FailureCount != 0 {
		t.Fatalf("device after an oversized payload = %+v, %v; want untouched", got, err)
	}
	if len(f.sender.Sent()) != 0 {
		t.Error("an oversized notification was sent")
	}
}

// TestPushSend_nothingToDoCompletes proves a job whose device unsubscribed —
// or whose account was deleted — and a job claimed after push was switched off
// both complete without sending.
func TestPushSend_nothingToDoCompletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.subscribe(t, "https://push.example.org/phone")
	job := f.enqueueOne(t)

	off := pushjob.NewService(pushjob.ServiceConfig{Sender: f.sender, Store: f.store})
	if err := off.Handle(ctx, job); err != nil {
		t.Fatalf("Handle with push off = %v, want nil", err)
	}
	if _, err := f.db.Pool().Exec(ctx, `DELETE FROM users WHERE uid = $1`, f.user); err != nil {
		t.Fatalf("deleting the account: %v", err)
	}
	if err := f.service(0).Handle(ctx, job); err != nil {
		t.Fatalf("Handle after the account was deleted = %v, want nil", err)
	}
	if len(f.sender.Sent()) != 0 {
		t.Errorf("sent %d notifications, want none", len(f.sender.Sent()))
	}
}

// TestEnqueue_queuesNothingWhenItShouldNot proves the refusals against the real
// queue: push off, an account without devices, and a transaction that rolls
// back.
func TestEnqueue_queuesNothingWhenItShouldNot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if n, err := enqueuer(true).Enqueue(ctx, f.db.Pool(), f.user, note); err != nil || n != 0 {
		t.Fatalf("Enqueue without devices = %d, %v; want 0, nil", n, err)
	}
	f.subscribe(t, "https://push.example.org/phone")
	if n, err := enqueuer(false).Enqueue(ctx, f.db.Pool(), f.user, note); err != nil || n != 0 {
		t.Fatalf("Enqueue with push off = %d, %v; want 0, nil", n, err)
	}

	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if n, err := enqueuer(true).Enqueue(ctx, tx, f.user, note); err != nil || n != 1 {
		t.Fatalf("Enqueue inside the transaction = %d, %v; want 1", n, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if n := f.countPushJobs(t); n != 0 {
		t.Fatalf("%d push_send jobs exist, want none", n)
	}

	// The same call committed does queue — the rollback, not the call, was what
	// kept the queue empty.
	tx, err = f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := enqueuer(true).Enqueue(ctx, tx, f.user, note); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if n := f.countPushJobs(t); n != 1 {
		t.Fatalf("%d push_send jobs after commit, want 1", n)
	}
}
