package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// pushRequest is what the fake push service saw of one request.
type pushRequest struct {
	header http.Header
	body   []byte
}

// fakePushService is an httptest TLS server standing in for a browser vendor's
// push service: it records every request and answers with a configurable status.
type fakePushService struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []pushRequest
	status   int
	header   http.Header
	body     string
}

// newFakePushService starts a push service answering 201 Created.
func newFakePushService(t *testing.T) *fakePushService {
	t.Helper()
	svc := &fakePushService{status: http.StatusCreated, header: http.Header{}}
	svc.server = httptest.NewTLSServer(http.HandlerFunc(svc.serve))
	t.Cleanup(svc.server.Close)
	return svc
}

// serve records the request and writes the configured answer.
func (s *fakePushService) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body) // a short read only shows up as a failed decrypt in the test
	s.mu.Lock()
	s.requests = append(s.requests, pushRequest{header: r.Header.Clone(), body: body})
	status, header, answer := s.status, s.header.Clone(), s.body
	s.mu.Unlock()
	maps.Copy(w.Header(), header)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, answer) // the client may not read it; nothing to do about a write error
}

// answer makes every later request get status, header and body.
func (s *fakePushService) answer(status int, header http.Header, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.header, s.body = status, header, body
}

// received returns a copy of the requests seen so far.
func (s *fakePushService) received() []pushRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pushRequest(nil), s.requests...)
}

// newTestSender builds a VAPIDSender over svc's TLS client with fresh keys.
func newTestSender(t *testing.T, svc *fakePushService) (*VAPIDSender, KeyPair) {
	t.Helper()
	keys := mustKeys(t)
	sender, err := NewVAPID(Config{
		Enabled: true, PublicKey: keys.PublicKey, PrivateKey: keys.PrivateKey,
		Subject: "mailto:ops@example.org", HTTPClient: svc.server.Client(),
	})
	if err != nil {
		t.Fatalf("NewVAPID: %v", err)
	}
	return sender, keys
}

// TestVAPIDSender_roundTrip sends one notification and verifies it the way a
// browser and a push service would: the payload decrypts to the notification's
// JSON, and the VAPID token is signed by the configured key for the endpoint's
// origin with the configured subject.
func TestVAPIDSender_roundTrip(t *testing.T) {
	t.Parallel()

	svc := newFakePushService(t)
	sender, keys := newTestSender(t, svc)
	browser := newTestBrowser(t, svc.server.URL+"/send/abc")
	want := Notification{Title: "Nový úkol", Body: "Kdo je na fotce?", URL: "/tasks/t1", Kind: "task", Tag: "task-t1"}

	if err := sender.Send(context.Background(), browser.sub, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	requests := svc.received()
	if len(requests) != 1 {
		t.Fatalf("push service saw %d requests, want 1", len(requests))
	}
	req := requests[0]

	var got Notification
	if err := json.Unmarshal(browser.decrypt(t, req.body), &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if got != want {
		t.Fatalf("decrypted notification = %+v, want %+v", got, want)
	}
	for header, value := range map[string]string{
		"Content-Encoding": "aes128gcm",
		"Ttl":              "86400",
		"Topic":            "task-t1",
		"Urgency":          "normal",
	} {
		if got := req.header.Get(header); got != value {
			t.Errorf("header %s = %q, want %q", header, got, value)
		}
	}
	verifyVAPID(t, req.header.Get("Authorization"), keys, svc.server.URL)
}

// verifyVAPID checks an RFC 8292 Authorization header: the k= key is the public
// key, the ES256 signature verifies against it, and the claims name the
// endpoint's origin and the subject exactly once prefixed with mailto:.
func verifyVAPID(t *testing.T, authorization string, keys KeyPair, origin string) {
	t.Helper()
	token, key, ok := strings.Cut(strings.TrimPrefix(authorization, "vapid t="), ", k=")
	if !ok || !strings.HasPrefix(authorization, "vapid t=") {
		t.Fatalf("Authorization = %q, not a vapid header", authorization)
	}
	if key != keys.PublicKey {
		t.Fatalf("k = %q, want the configured public key", key)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	public, err := ecdsa.ParseUncompressedPublicKey(ecdsaCurve(), mustDecode(t, keys.PublicKey))
	if err != nil {
		t.Fatalf("parsing the public key: %v", err)
	}
	signature := mustDecode(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !verifyRS(public, digest[:], signature) {
		t.Fatal("the VAPID token signature does not verify against the public key")
	}
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(mustDecode(t, parts[1]), &claims); err != nil {
		t.Fatalf("claims: %v", err)
	}
	if claims.Aud != origin || claims.Sub != "mailto:ops@example.org" {
		t.Fatalf("claims = %+v, want aud %q and sub mailto:ops@example.org", claims, origin)
	}
	if time.Until(time.Unix(claims.Exp, 0)) <= 0 {
		t.Fatalf("token already expired at %d", claims.Exp)
	}
}

// TestVAPIDSender_statusClassification covers how every push-service answer
// maps to a result the caller can act on.
func TestVAPIDSender_statusClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status     int
		retryAfter string
		want       error
		wantAfter  time.Duration
	}{
		{status: http.StatusOK},
		{status: http.StatusCreated},
		{status: http.StatusAccepted},
		{status: http.StatusNotFound, want: ErrGone},
		{status: http.StatusGone, want: ErrGone},
		{status: http.StatusTooManyRequests, retryAfter: "120", want: ErrRetryable, wantAfter: 2 * time.Minute},
		{status: http.StatusInternalServerError, want: ErrRetryable},
		{status: http.StatusBadGateway, retryAfter: "soon", want: ErrRetryable},
		{status: http.StatusServiceUnavailable, want: ErrRetryable},
		{status: http.StatusRequestEntityTooLarge, want: ErrPayloadTooLarge},
		{status: http.StatusBadRequest, want: ErrRejected},
		{status: http.StatusUnauthorized, want: ErrRejected},
		{status: http.StatusForbidden, want: ErrRejected},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			svc := newFakePushService(t)
			header := http.Header{}
			if tt.retryAfter != "" {
				header.Set("Retry-After", tt.retryAfter)
			}
			svc.answer(tt.status, header, "  explanation  ")
			sender, _ := newTestSender(t, svc)
			browser := newTestBrowser(t, svc.server.URL+"/send/x")

			err := sender.Send(context.Background(), browser.sub, Notification{Title: "T"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Send() error = %v, want %v", err, tt.want)
			}
			if tt.want == nil {
				return
			}
			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error %v is not a *StatusError", err)
			}
			if statusErr.StatusCode != tt.status || statusErr.RetryAfter != tt.wantAfter ||
				statusErr.Body != "explanation" {
				t.Fatalf("StatusError = %+v, want status %d, retry after %v", statusErr, tt.status, tt.wantAfter)
			}
			if gone := errors.Is(err, ErrGone); gone == Retryable(err) && gone {
				t.Fatal("an error is both gone and retryable")
			}
		})
	}
}

// TestVAPIDSender_refusesBeforeDialling proves invalid input never reaches the
// push service.
func TestVAPIDSender_refusesBeforeDialling(t *testing.T) {
	t.Parallel()

	svc := newFakePushService(t)
	sender, _ := newTestSender(t, svc)
	browser := newTestBrowser(t, svc.server.URL+"/send/x")
	badSub := browser.sub
	badSub.Auth = ""

	tests := []struct {
		name string
		sub  Subscription
		n    Notification
		want error
	}{
		{name: "oversized", sub: browser.sub,
			n: Notification{Title: "T", Body: strings.Repeat("x", MaxPayloadSize)}, want: ErrPayloadTooLarge},
		{name: "no title", sub: browser.sub, n: Notification{}, want: ErrInvalidNotification},
		{name: "bad subscription", sub: badSub, n: Notification{Title: "T"}, want: ErrInvalidSubscription},
	}
	for _, tt := range tests {
		if err := sender.Send(context.Background(), tt.sub, tt.n); !errors.Is(err, tt.want) {
			t.Errorf("%s: Send() error = %v, want %v", tt.name, err, tt.want)
		}
	}
	if n := len(svc.received()); n != 0 {
		t.Fatalf("push service saw %d requests, want none", n)
	}
}

// TestVAPIDSender_largestPayloadFits proves MaxPayloadSize matches what
// webpush-go really encrypts: a notification of exactly that size goes through
// and decrypts intact.
func TestVAPIDSender_largestPayloadFits(t *testing.T) {
	t.Parallel()

	svc := newFakePushService(t)
	sender, _ := newTestSender(t, svc)
	browser := newTestBrowser(t, svc.server.URL+"/send/x")
	overhead, err := Notification{Title: "T"}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	n := Notification{Title: "T", Body: strings.Repeat("b", MaxPayloadSize-len(overhead))}

	if err := sender.Send(context.Background(), browser.sub, n); err != nil {
		t.Fatalf("Send at the limit: %v", err)
	}
	plain := browser.decrypt(t, svc.received()[0].body)
	if len(plain) != MaxPayloadSize {
		t.Fatalf("decrypted %d bytes, want %d", len(plain), MaxPayloadSize)
	}
}

// TestVAPIDSender_unreachableIsRetryable proves a transport failure and a
// cancelled context are transient, not a verdict on the subscription.
func TestVAPIDSender_unreachableIsRetryable(t *testing.T) {
	t.Parallel()

	svc := newFakePushService(t)
	sender, _ := newTestSender(t, svc)
	browser := newTestBrowser(t, svc.server.URL+"/send/x")
	svc.server.Close()

	err := sender.Send(context.Background(), browser.sub, Notification{Title: "T"})
	if !Retryable(err) || errors.Is(err, ErrGone) {
		t.Fatalf("Send to a closed server: error = %v, want retryable", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sender.Send(ctx, browser.sub, Notification{Title: "T"})
	if !Retryable(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Send with a cancelled context: error = %v, want retryable wrapping context.Canceled", err)
	}
}

// TestNew covers the wiring decision: disabled is Noop without looking at the
// keys, enabled demands a valid pair and subject.
func TestNew(t *testing.T) {
	t.Parallel()

	keys := mustKeys(t)
	tests := []struct {
		name     string
		cfg      Config
		wantNoop bool
		wantErr  bool
	}{
		{name: "disabled without keys", cfg: Config{}, wantNoop: true},
		{name: "disabled with garbage keys", cfg: Config{PublicKey: "x", PrivateKey: "y"}, wantNoop: true},
		{name: "enabled and complete", cfg: Config{Enabled: true, PublicKey: keys.PublicKey,
			PrivateKey: keys.PrivateKey, Subject: "https://kukatko.example.org"}},
		{name: "enabled without keys", cfg: Config{Enabled: true, Subject: "mailto:a@b.cz"}, wantErr: true},
		{name: "enabled with a bad subject", cfg: Config{Enabled: true, PublicKey: keys.PublicKey,
			PrivateKey: keys.PrivateKey, Subject: "a@b.cz"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sender, err := New(tt.cfg)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, isNoop := sender.(Noop)
			if isNoop != tt.wantNoop {
				t.Fatalf("New() = %T, want Noop %v", sender, tt.wantNoop)
			}
		})
	}
}

// TestTopicFor covers which tags become a Topic header.
func TestTopicFor(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                      "",
		"task-t1":               "task-t1",
		"A_b-9":                 "A_b-9",
		"has space":             "",
		"task:t1":               "",
		strings.Repeat("a", 32): strings.Repeat("a", 32),
		strings.Repeat("a", 33): "",
		"příliš":                "",
		base64.RawURLEncoding.EncodeToString([]byte("x")): "eA",
	}
	for tag, want := range tests {
		if got := topicFor(tag); got != want {
			t.Errorf("topicFor(%q) = %q, want %q", tag, got, want)
		}
	}
}

// TestRetryAfter covers the Retry-After forms a push service may send.
func TestRetryAfter(t *testing.T) {
	t.Parallel()

	tests := map[string]time.Duration{
		"":                              0,
		"30":                            30 * time.Second,
		" 5 ":                           5 * time.Second,
		"0":                             0,
		"-3":                            0,
		"Wed, 21 Oct 2026 07:28:00 GMT": 0,
	}
	for header, want := range tests {
		if got := retryAfter(header); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", header, got, want)
		}
	}
}

// TestStatusError_message covers the rendered message with and without a body.
func TestStatusError_message(t *testing.T) {
	t.Parallel()

	withBody := (&StatusError{StatusCode: 410, Body: "expired", kind: ErrGone}).Error()
	if withBody != "push: the subscription is gone: HTTP 410: expired" {
		t.Errorf("Error() = %q", withBody)
	}
	bare := (&StatusError{StatusCode: 503, kind: ErrRetryable}).Error()
	if bare != "push: transient delivery failure: HTTP 503" {
		t.Errorf("Error() = %q", bare)
	}
}
