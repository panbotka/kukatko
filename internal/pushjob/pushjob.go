// Package pushjob delivers Kukátko's Web Push notifications through the
// persistent job queue instead of from the request that caused them.
//
// It is the push-shaped twin of internal/mailjob, and it exists for the same
// two reasons: a request must not wait on a remote host, and a push service
// that is briefly unreachable must delay a notification rather than lose it. A
// caller schedules the notification — inside its own transaction, so a mutation
// that rolls back notifies nobody — and the worker delivers it later, retrying
// with the queue's own backoff.
//
// The one real difference from mail is the fan-out. A person may be subscribed
// on several browsers, and one `push_send` job delivers to exactly one of them:
// Enqueue reads the account's subscriptions and queues a job per device. That is
// what makes a retry safe — a job retried because the laptop's push service was
// unreachable re-sends to the laptop only, never a second time to the phone
// that already showed it.
//
// The handler also owns the lifecycle of a subscription, because it is the one
// place that learns a device is dead: a subscription the push service reports
// as gone is deleted, and one that keeps failing is retired once its run of
// consecutive failures reaches MaxFailures. The sender in internal/push only
// reports; it never touches the table.
package pushjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/worker"
)

// Sentinel errors this package returns so callers and tests can branch with
// errors.Is.
var (
	// ErrMissingSubscription indicates a `push_send` payload named no
	// subscription. It is permanent: the payload is already written and will not
	// gain one.
	ErrMissingSubscription = errors.New("pushjob: payload names no subscription")
	// ErrMissingUser indicates Enqueue was asked to notify nobody — a caller bug.
	ErrMissingUser = errors.New("pushjob: no account to notify")
)

// MaxFailures is the run of consecutive failed sends after which a subscription
// is deleted as hopeless. The count is the one push.Store keeps and resets on
// every success, so a working device never gets near it; it is set well above
// MaxAttempts, so a single notification retried through a push-service outage
// cannot retire a device on its own — it takes a few notifications in a row
// that all failed every attempt.
const MaxFailures = 20

// payload is the JSON shape of a `push_send` job: which subscription to deliver
// to and what. The subscription is named by id and read when the job runs, so
// a browser that re-subscribed with fresh keys meanwhile still gets the message
// and one that unsubscribed gets nothing.
type payload struct {
	// SubscriptionID is the push_subscriptions row to deliver to.
	SubscriptionID string `json:"subscription_id"`
	// Notification is what to show on that device.
	Notification push.Notification `json:"notification"`
}

// SubscriptionStore is the part of push.Store the handler needs: reading the
// device a job is addressed to and keeping its bookkeeping. *push.Store
// satisfies it; unit tests substitute an in-memory fake.
type SubscriptionStore interface {
	// Get returns the subscription id, or push.ErrNotFound when it is gone.
	Get(ctx context.Context, id string) (push.Subscription, error)
	// RecordSuccess stamps a successful send and resets the failure count.
	RecordSuccess(ctx context.Context, id string) error
	// RecordFailure counts one more failed send and returns the new count.
	RecordFailure(ctx context.Context, id string) (int, error)
	// DeleteByID removes subscription id of the account userUID.
	DeleteByID(ctx context.Context, userUID, id string) error
}

// ServiceConfig bundles the handler's collaborators. Sender and Store are
// required.
type ServiceConfig struct {
	// Enabled mirrors push.enabled. With push switched off a job left in the
	// queue completes without sending anything.
	Enabled bool
	// Sender delivers the notification. Production wires push.New's choice;
	// tests wire push.Fake.
	Sender push.Sender
	// Store reads the subscription and keeps its bookkeeping.
	Store SubscriptionStore
	// MaxFailures overrides the package's MaxFailures; <= 0 uses it.
	MaxFailures int
	// Logger records what was delivered and what was retired; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// Service is the worker handler for `push_send` jobs: it reads the subscription
// the job is addressed to, hands the notification to the sender and reacts to
// the answer.
type Service struct {
	enabled     bool
	sender      push.Sender
	store       SubscriptionStore
	maxFailures int
	log         *slog.Logger
}

// NewService builds a Service from cfg. It panics if Sender or Store is nil,
// since a push handler without either is a wiring bug that should surface at
// startup rather than fail every notification.
func NewService(cfg ServiceConfig) *Service {
	if cfg.Sender == nil || cfg.Store == nil {
		panic("pushjob: NewService requires a Sender and a Store")
	}
	maxFailures := cfg.MaxFailures
	if maxFailures <= 0 {
		maxFailures = MaxFailures
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		enabled: cfg.Enabled, sender: cfg.Sender, store: cfg.Store,
		maxFailures: maxFailures, log: log,
	}
}

// Handle is the worker.HandlerFunc for `push_send` jobs. It decodes the
// payload, reads the subscription and delivers the notification to it.
//
// Three situations complete the job without sending, because nothing about
// them is a failure: push has been switched off since the job was queued (an
// operator turning push off must not fill the queue with failures), and the
// subscription no longer exists (the person unsubscribed, or their account was
// deleted and the cascade removed its devices). A payload that does not decode
// or names no subscription is a worker.Terminal failure; so is any answer that
// says this notification can never be delivered. See deliver for the rest.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	p, err := decodePayload(job.Payload)
	if err != nil {
		// Terminal is our own worker control-flow signal, not a foreign error to
		// annotate; wrapping it would obscure the type the worker matches with
		// errors.As.
		return worker.Terminal(err) //nolint:wrapcheck
	}
	if !s.enabled {
		s.log.InfoContext(ctx, "push not sent: push is disabled",
			slog.Int64("job_id", job.ID), slog.String("kind", p.Notification.Kind))
		return nil
	}
	sub, err := s.store.Get(ctx, p.SubscriptionID)
	if errors.Is(err, push.ErrNotFound) {
		s.log.DebugContext(ctx, "push not sent: the subscription is gone",
			slog.Int64("job_id", job.ID), slog.String("subscription", p.SubscriptionID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("pushjob: reading subscription %s: %w", p.SubscriptionID, err)
	}
	return s.deliver(ctx, job, sub, p.Notification)
}

// decodePayload parses a `push_send` payload, returning a wrapped decoding
// error or ErrMissingSubscription; both are permanent.
func decodePayload(raw json.RawMessage) (payload, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return payload{}, fmt.Errorf("pushjob: decoding payload: %w", err)
	}
	if strings.TrimSpace(p.SubscriptionID) == "" {
		return payload{}, ErrMissingSubscription
	}
	return p, nil
}

// deliver sends n to sub and turns the sender's answer into the job's outcome:
//
//   - accepted: the send is recorded on the subscription and the job completes;
//   - push.ErrGone: the device is dead for good, so its subscription is deleted
//     and the job completes — the notification was fine, the device was not;
//   - push.ErrPayloadTooLarge or push.ErrInvalidNotification: the notification
//     itself can never be sent, so the job fails terminally and the device's
//     record is left alone;
//   - anything else is the device's failure and is recorded on it (see
//     recordFailure).
func (s *Service) deliver(ctx context.Context, job jobs.Job, sub push.Subscription, n push.Notification) error {
	sendErr := s.sender.Send(ctx, sub, n)
	switch {
	case sendErr == nil:
		s.recordSuccess(ctx, job, sub)
		return nil
	case errors.Is(sendErr, push.ErrGone):
		return s.retire(ctx, sub, "the push service reports it gone")
	case errors.Is(sendErr, push.ErrPayloadTooLarge), errors.Is(sendErr, push.ErrInvalidNotification):
		return worker.Terminal( //nolint:wrapcheck // see Handle
			fmt.Errorf("pushjob: the notification can never be sent: %w", sendErr))
	default:
		return s.recordFailure(ctx, sub, sendErr)
	}
}

// recordSuccess stamps the delivery on the subscription. A failure to do so is
// only logged: the push service already accepted the notification, and failing
// the job would have the queue send it a second time.
func (s *Service) recordSuccess(ctx context.Context, job jobs.Job, sub push.Subscription) {
	if err := s.store.RecordSuccess(ctx, sub.ID); err != nil && !errors.Is(err, push.ErrNotFound) {
		s.log.WarnContext(ctx, "push delivered but not recorded",
			slog.String("subscription", sub.ID), slog.Any("error", err))
	}
	s.log.InfoContext(ctx, "push delivered",
		slog.Int64("job_id", job.ID), slog.String("subscription", sub.ID))
}

// recordFailure counts sendErr against the subscription and decides what the
// job does next. Once the run of failures reaches the threshold the device is
// retired as hopeless and the job completes — otherwise a permanently broken
// endpoint would keep one job after another retrying until the queue gave up on
// each. Below it, a transient failure (push.Retryable) is returned as an
// ordinary error so the queue retries with its backoff, and a permanent one
// (push.ErrRejected, push.ErrInvalidSubscription) fails the job terminally: the
// same send would get the same answer. A subscription deleted while the send
// was in flight completes the job — there is nobody left to deliver to.
func (s *Service) recordFailure(ctx context.Context, sub push.Subscription, sendErr error) error {
	count, err := s.store.RecordFailure(ctx, sub.ID)
	if errors.Is(err, push.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pushjob: recording a failed send through %s (%w): %w", sub.ID, sendErr, err)
	}
	if count >= s.maxFailures {
		return s.retire(ctx, sub, fmt.Sprintf("%d sends in a row failed", count))
	}
	if push.Retryable(sendErr) {
		return fmt.Errorf("pushjob: sending through %s: %w", sub.ID, sendErr)
	}
	return worker.Terminal( //nolint:wrapcheck // see Handle
		fmt.Errorf("pushjob: sending through %s failed permanently: %w", sub.ID, sendErr))
}

// retire deletes sub and completes the job. This is the only place a
// subscription is pruned for being dead. One already gone is fine; a database
// error is returned so the queue retries, and the retry either finds the row
// gone or deletes it then.
func (s *Service) retire(ctx context.Context, sub push.Subscription, reason string) error {
	if err := s.store.DeleteByID(ctx, sub.UserUID, sub.ID); err != nil && !errors.Is(err, push.ErrNotFound) {
		return fmt.Errorf("pushjob: deleting dead subscription %s: %w", sub.ID, err)
	}
	s.log.InfoContext(ctx, "push subscription removed",
		slog.String("subscription", sub.ID), slog.String("user_uid", sub.UserUID),
		slog.String("reason", reason))
	return nil
}
