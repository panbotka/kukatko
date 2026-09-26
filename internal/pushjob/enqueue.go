package pushjob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/push"
)

// MaxAttempts is how often a `push_send` job is tried before it is
// dead-lettered. With the queue's exponential backoff (30 s, doubling) eight
// attempts span about an hour — long enough to ride out a push service's bad
// spell, short enough that the notification is still news when it arrives. A
// permanent failure never gets that far: it is terminal on the first attempt.
const MaxAttempts = 8

// Execer is what Enqueue runs on: a pool or, as intended, the open transaction
// of the mutation that caused the notification. It reads the account's
// subscriptions (push.Querier) and inserts the jobs (jobs.Execer); both
// *pgxpool.Pool and pgx.Tx satisfy it.
type Execer interface {
	jobs.Execer
	push.Querier
}

// Scheduler inserts a job into the persistent queue through a caller-supplied
// executor. It is satisfied by jobs.Enqueue, which is what production wires; a
// test substitutes its own function and needs no database.
type Scheduler func(
	ctx context.Context, exec jobs.Execer, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions,
) (jobs.Job, error)

// Lister returns the ids of an account's subscriptions, read through q. It is
// satisfied by push.SubscriptionIDs, which is what production wires.
type Lister func(ctx context.Context, q push.Querier, userUID string) ([]string, error)

// EnqueuerConfig bundles what the Enqueuer needs.
type EnqueuerConfig struct {
	// Enabled mirrors push.enabled: with push switched off nothing is enqueued.
	Enabled bool
	// Schedule inserts one job; nil uses jobs.Enqueue.
	Schedule Scheduler
	// List reads the account's subscriptions; nil uses push.SubscriptionIDs.
	List Lister
	// Logger records the notifications that were not enqueued; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// Enqueuer schedules push notifications on the persistent queue. It is what
// callers use instead of touching the queue — or the sender — themselves, so
// the fan-out to devices and the two refusals (push is off, the account has no
// device) happen in exactly one place.
type Enqueuer struct {
	schedule Scheduler
	list     Lister
	enabled  bool
	log      *slog.Logger
}

// NewEnqueuer builds an Enqueuer from cfg, defaulting Schedule to jobs.Enqueue,
// List to push.SubscriptionIDs and Logger to slog.Default().
func NewEnqueuer(cfg EnqueuerConfig) *Enqueuer {
	schedule := cfg.Schedule
	if schedule == nil {
		schedule = jobs.Enqueue
	}
	list := cfg.List
	if list == nil {
		list = push.SubscriptionIDs
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Enqueuer{schedule: schedule, list: list, enabled: cfg.Enabled, log: log}
}

// Enabled reports whether this instance sends push notifications at all, for a
// caller that wants to skip preparing one nobody will receive.
func (e *Enqueuer) Enabled() bool {
	return e.enabled
}

// Enqueue schedules n for every browser userUID is subscribed on — one
// `push_send` job per device — using exec both to read the subscriptions and to
// insert the jobs. Passing the transaction of the mutation that caused the
// notification is the intended use: the notification is scheduled if and only
// if that mutation commits. It returns how many jobs were queued.
//
// Two cases return 0 and nil without enqueuing anything, because in both
// nothing queued is the correct outcome and the mutation that asked must not
// fail: push is disabled in the configuration, and the account has no
// subscription at all. A missing account and a notification the sender would
// refuse (push.ErrInvalidNotification, push.ErrPayloadTooLarge) are refused with
// an error instead — both are caller bugs, and enqueuing them would only produce
// jobs that are dead on arrival.
func (e *Enqueuer) Enqueue(ctx context.Context, exec Execer, userUID string, n push.Notification) (int, error) {
	if !e.enabled {
		e.log.DebugContext(ctx, "push not scheduled: push is disabled", slog.String("kind", n.Kind))
		return 0, nil
	}
	if strings.TrimSpace(userUID) == "" {
		return 0, ErrMissingUser
	}
	if _, err := n.Encode(); err != nil {
		return 0, fmt.Errorf("pushjob: refusing to enqueue %q: %w", n.Kind, err)
	}
	ids, err := e.list(ctx, exec, userUID)
	if err != nil {
		return 0, fmt.Errorf("pushjob: reading the devices of %s: %w", userUID, err)
	}
	if len(ids) == 0 {
		e.log.DebugContext(ctx, "push not scheduled: no subscribed device",
			slog.String("user_uid", userUID), slog.String("kind", n.Kind))
		return 0, nil
	}
	for i, id := range ids {
		raw, err := json.Marshal(payload{SubscriptionID: id, Notification: n})
		if err != nil {
			return i, fmt.Errorf("pushjob: encoding the payload for %s: %w", id, err)
		}
		if _, err := e.schedule(ctx, exec, jobs.TypePushSend, raw, jobs.EnqueueOptions{
			MaxAttempts: MaxAttempts,
		}); err != nil {
			return i, fmt.Errorf("pushjob: enqueuing %q for %s: %w", n.Kind, id, err)
		}
	}
	return len(ids), nil
}
