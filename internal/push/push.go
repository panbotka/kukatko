// Package push sends Web Push notifications to the browsers people allowed to
// show them, and keeps the list of those browsers.
//
// It is the push-shaped twin of internal/mailer. Everything goes through the
// Sender interface, so a caller never depends on the protocol: production wires
// VAPIDSender (RFC 8030 delivery, RFC 8291 payload encryption, RFC 8292 VAPID
// identification — no FCM, no vendor SDK), an instance with push turned off
// wires Noop, and tests wire Fake, which records what it was asked to send and
// never opens a socket. New picks between the first two from the configuration.
//
// A send can end four ways, and the caller must tell them apart, because each
// asks for a different reaction:
//
//   - ErrGone: the push service says the subscription no longer exists (HTTP 404
//     or 410). It never will again — the caller deletes the row. The sender only
//     reports it; the lifecycle of a subscription belongs to the caller.
//   - ErrRetryable: the push service is overloaded or down (429, 5xx) or could
//     not be reached. The same send may well succeed later.
//   - ErrPayloadTooLarge, ErrInvalidNotification, ErrInvalidSubscription and
//     ErrRejected: permanent. Retrying the same send gets the same answer.
//   - nil: the push service accepted the message. Whether the device shows it is
//     beyond anybody's knowledge.
//
// The Store keeps one row per subscribed browser in push_subscriptions.
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by this package so callers can match them with
// errors.Is. A *StatusError unwraps to one of ErrGone, ErrRetryable,
// ErrPayloadTooLarge or ErrRejected.
var (
	// ErrGone indicates the push service answered 404 or 410: the subscription
	// has expired or was withdrawn and will never accept a message again. The
	// caller should delete it.
	ErrGone = errors.New("push: the subscription is gone")
	// ErrRetryable indicates a transient failure — the push service answered 429
	// or a 5xx, or could not be reached at all. The same send may succeed later.
	ErrRetryable = errors.New("push: transient delivery failure")
	// ErrRejected indicates the push service refused the message for a reason
	// retrying will not change (any other 4xx — typically a 403 because the
	// subscription was made for a different VAPID public key).
	ErrRejected = errors.New("push: the push service rejected the message")
	// ErrPayloadTooLarge indicates the encoded notification does not fit in one
	// encrypted Web Push record (see MaxPayloadSize). Nothing was sent; the same
	// notification will never fit, so it is permanent.
	ErrPayloadTooLarge = errors.New("push: the notification payload is too large")
	// ErrInvalidNotification indicates a notification that must not be sent: no
	// title, or a deeplink that is not a path on this instance.
	ErrInvalidNotification = errors.New("push: invalid notification")
	// ErrInvalidSubscription indicates a subscription that cannot be sent to: an
	// endpoint that is not an https URL, or client keys that do not decode to a
	// P-256 public key and a 16-byte auth secret.
	ErrInvalidSubscription = errors.New("push: invalid subscription")
	// ErrInvalidConfig indicates a sender configuration it cannot send with — a
	// missing or malformed VAPID key pair, or a subject that is neither a mailto:
	// nor an https: URL. The error names the problem, never a key's value.
	ErrInvalidConfig = errors.New("push: invalid VAPID configuration")
	// ErrNotFound indicates the subscription a store operation addressed does not
	// exist (or, for DeleteByID, does not belong to the given account).
	ErrNotFound = errors.New("push: subscription not found")
)

// Notification is the one message a push delivers. It is marshalled to JSON and
// its only reader is the frontend's service worker, which shows it and opens URL
// when it is clicked — so the JSON field names are a contract with that file and
// must stay stable.
type Notification struct {
	// Title is the notification's headline. It is required.
	Title string `json:"title"`
	// Body is the text under the title; it may be empty.
	Body string `json:"body"`
	// URL is the deeplink opened when the notification is clicked: a path on
	// this instance such as "/tasks/abc", never an absolute URL. Empty means the
	// home page.
	URL string `json:"url"`
	// Kind names what the notification is about (a task was answered, …), so the
	// service worker can pick an icon or group it. It is free-form.
	Kind string `json:"kind"`
	// Tag is the collapse key: a newer notification with the same tag replaces
	// an older one still on screen instead of stacking under it. Empty never
	// collapses. A tag that is also a valid Topic (see topicFor) additionally
	// replaces a message the push service is still holding for an offline device.
	Tag string `json:"tag"`
}

// Encode validates n and returns its JSON form, the exact bytes a sender
// encrypts. It returns ErrInvalidNotification for a missing title or a URL that
// is not a same-origin path, and ErrPayloadTooLarge when the JSON exceeds
// MaxPayloadSize — checked here, before anything is dialled, so an oversized
// message fails permanently instead of being retried against a push service that
// will always refuse it.
func (n Notification) Encode() ([]byte, error) {
	if strings.TrimSpace(n.Title) == "" {
		return nil, fmt.Errorf("%w: the title is empty", ErrInvalidNotification)
	}
	if !isLocalPath(n.URL) {
		return nil, fmt.Errorf("%w: url %q is not a path on this instance", ErrInvalidNotification, n.URL)
	}
	payload, err := json.Marshal(n)
	if err != nil {
		return nil, fmt.Errorf("push: encoding the notification: %w", err)
	}
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("%w: %d bytes, at most %d fit", ErrPayloadTooLarge, len(payload), MaxPayloadSize)
	}
	return payload, nil
}

// isLocalPath reports whether u is empty or a path on this instance. It refuses
// anything a browser would resolve to another origin — an absolute URL, a
// scheme-relative "//host" and its backslash variant — so a notification can
// never be turned into a link off the instance.
func isLocalPath(u string) bool {
	if u == "" {
		return true
	}
	if !strings.HasPrefix(u, "/") {
		return false
	}
	return !strings.HasPrefix(u, "//") && !strings.HasPrefix(u, `/\`)
}

// Sender delivers one notification to one subscription. Every caller depends on
// this interface rather than on the protocol, so push can be turned off (Noop)
// or captured (Fake) without the caller knowing.
type Sender interface {
	// Send encrypts n for sub and hands it to sub's push service. It returns nil
	// once the push service accepted it, ErrGone for a subscription that is dead
	// for good, ErrRetryable for a transient failure, and ErrPayloadTooLarge,
	// ErrInvalidNotification, ErrInvalidSubscription or ErrRejected for a
	// permanent one. The context bounds the attempt. Only sub's Endpoint, P256dh
	// and Auth are read.
	Send(ctx context.Context, sub Subscription, n Notification) error
}

// Noop is the Sender used when push is disabled instance-wide. It accepts every
// notification and does nothing with it — nothing is encrypted and nothing is
// dialled, so an instance that never configured VAPID keys must never fail
// anything just because somebody once subscribed.
type Noop struct{}

// Send discards n and returns nil.
func (Noop) Send(_ context.Context, _ Subscription, _ Notification) error {
	return nil
}

// Retryable reports whether err is a transient failure worth sending again
// later. It is false for nil, for ErrGone and for every permanent error.
func Retryable(err error) bool {
	return errors.Is(err, ErrRetryable)
}
