package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	// DefaultTTL is how long a push service holds a message for a device that is
	// offline before dropping it. A day covers a phone left off overnight; a
	// notification older than that is news nobody needs any more.
	DefaultTTL = 24 * time.Hour
	// DefaultTimeout bounds one delivery attempt when the caller's context does
	// not bound it more tightly.
	DefaultTimeout = 30 * time.Second
	// maxErrorBody is how much of a push service's error answer is kept in a
	// StatusError; the rest is drained and discarded.
	maxErrorBody = 512
	// maxDrain bounds how much of any answer is read before the connection is
	// given back, so a misbehaving endpoint cannot stream into the process.
	maxDrain = 64 << 10
)

// topicPattern is what RFC 8030 §5.4 allows in a Topic header: at most 32
// characters of the base64url alphabet.
var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Config configures New: whether push is on and, when it is, the VAPID
// identity to send with.
type Config struct {
	// Enabled is the master switch. Off, New returns Noop and nothing else is
	// read, so a disabled instance needs no keys.
	Enabled bool
	// PublicKey and PrivateKey are the VAPID key pair (see GenerateKeys).
	PublicKey  string
	PrivateKey string
	// Subject is the sender's contact, a mailto: or an https: URL.
	Subject string
	// TTL is how long a push service keeps an undelivered message; <= 0 means
	// DefaultTTL.
	TTL time.Duration
	// HTTPClient sends the requests; nil means a client with DefaultTimeout.
	// Tests hand in the client of an httptest TLS server.
	HTTPClient *http.Client
}

// New returns the Sender cfg describes: Noop when push is disabled — nothing is
// validated and nothing will ever be dialled — and a VAPIDSender otherwise. An
// enabled configuration with a missing or malformed key pair or subject is
// ErrInvalidConfig, so a misconfigured instance fails at startup rather than
// looking configured while every notification is lost.
func New(cfg Config) (Sender, error) {
	if !cfg.Enabled {
		return Noop{}, nil
	}
	return NewVAPID(cfg)
}

// VAPIDSender delivers notifications over the Web Push protocol, encrypting
// each payload to the subscription's keys and identifying itself with a VAPID
// token signed by the configured private key. It is safe for concurrent use.
type VAPIDSender struct {
	publicKey  string
	privateKey string
	subscriber string
	ttlSeconds int
	client     *http.Client
}

// NewVAPID validates cfg's key pair and subject and returns a sender for them,
// ignoring cfg.Enabled. It returns ErrInvalidConfig when the keys are missing,
// malformed or not a pair, or the subject is neither mailto: nor https:.
func NewVAPID(cfg Config) (*VAPIDSender, error) {
	if err := ValidateKeys(cfg.PublicKey, cfg.PrivateKey); err != nil {
		return nil, err
	}
	subscriber, err := subscriberFor(cfg.Subject)
	if err != nil {
		return nil, err
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &VAPIDSender{
		publicKey:  strings.TrimSpace(cfg.PublicKey),
		privateKey: strings.TrimSpace(cfg.PrivateKey),
		subscriber: subscriber,
		ttlSeconds: int(ttl / time.Second),
		client:     client,
	}, nil
}

// Send encodes n, encrypts it for sub and posts it to sub's endpoint, then
// classifies the push service's answer (see Sender). A subscription or
// notification that fails validation is refused before anything is dialled.
func (s *VAPIDSender) Send(ctx context.Context, sub Subscription, n Notification) error {
	if err := ValidateSubscription(sub); err != nil {
		return err
	}
	payload, err := n.Encode()
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{Auth: strings.TrimSpace(sub.Auth), P256dh: strings.TrimSpace(sub.P256dh)},
	}, &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.subscriber,
		Topic:           topicFor(n.Tag),
		TTL:             s.ttlSeconds,
		Urgency:         webpush.UrgencyNormal,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
	})
	if err != nil {
		// Validation above already refused every input the library could choke
		// on, and the payload fits by construction, so what is left is the
		// transport: the endpoint was unreachable or the context ran out.
		if errors.Is(err, webpush.ErrMaxPadExceeded) {
			return fmt.Errorf("%w: %w", ErrPayloadTooLarge, err)
		}
		return fmt.Errorf("%w: posting to the push service: %w", ErrRetryable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain)) // best effort: lets the connection be reused
		_ = resp.Body.Close()                                           // nothing useful to do with a close error
	}()
	return classify(resp)
}

// topicFor returns tag as the Topic header when it is a valid topic, so a push
// service holding an undelivered message for an offline device replaces it
// instead of queueing a second one. Any other tag yields "" (no header); it
// still collapses on screen through the payload.
func topicFor(tag string) string {
	if topicPattern.MatchString(tag) {
		return tag
	}
	return ""
}

// StatusError is a push service's non-2xx answer. It unwraps to the sentinel
// the status maps to — ErrGone, ErrRetryable, ErrPayloadTooLarge or
// ErrRejected — so callers match it with errors.Is and read the details with
// errors.As.
type StatusError struct {
	// StatusCode is the HTTP status the push service answered with.
	StatusCode int
	// RetryAfter is the push service's Retry-After in seconds form, zero when it
	// sent none (or sent a date, which no push service does in practice).
	RetryAfter time.Duration
	// Body is the start of the push service's explanation, trimmed.
	Body string
	kind error
}

// Error describes the answer: the sentinel, the status and the explanation.
func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%v: HTTP %d", e.kind, e.StatusCode)
	}
	return fmt.Sprintf("%v: HTTP %d: %s", e.kind, e.StatusCode, e.Body)
}

// Unwrap returns the sentinel the status maps to.
func (e *StatusError) Unwrap() error {
	return e.kind
}

// classify turns resp's status into the send's result: nil for any 2xx,
// otherwise a *StatusError whose sentinel is ErrGone for 404 and 410,
// ErrRetryable for 429 and every 5xx, ErrPayloadTooLarge for 413 and
// ErrRejected for the rest. It reads at most maxErrorBody of the body and leaves
// closing it to the caller.
func classify(resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody)) // the body only decorates the error
	return &StatusError{
		StatusCode: resp.StatusCode,
		RetryAfter: retryAfter(resp.Header.Get("Retry-After")),
		Body:       strings.TrimSpace(string(body)),
		kind:       kindOf(resp.StatusCode),
	}
}

// kindOf maps a non-2xx status to the sentinel describing it.
func kindOf(status int) error {
	switch {
	case status == http.StatusNotFound || status == http.StatusGone:
		return ErrGone
	case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		return ErrRetryable
	case status == http.StatusRequestEntityTooLarge:
		return ErrPayloadTooLarge
	default:
		return ErrRejected
	}
}

// retryAfter parses a Retry-After header given in seconds; anything else —
// absent, negative, or an HTTP date — is zero.
func retryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
