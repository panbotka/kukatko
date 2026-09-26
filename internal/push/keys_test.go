package push

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// TestGenerateKeys_shape proves a generated pair is valid, in the encoding the
// browsers expect, and fresh on every call.
func TestGenerateKeys_shape(t *testing.T) {
	t.Parallel()

	first := mustKeys(t)
	second := mustKeys(t)
	if err := ValidateKeys(first.PublicKey, first.PrivateKey); err != nil {
		t.Fatalf("ValidateKeys(generated) = %v", err)
	}
	if first.PrivateKey == second.PrivateKey {
		t.Fatal("two calls returned the same private key")
	}
	public, err := base64.RawURLEncoding.DecodeString(first.PublicKey)
	if err != nil || len(public) != 65 || public[0] != 0x04 {
		t.Fatalf("public key is not an unpadded base64url uncompressed point: %q", first.PublicKey)
	}
	private, err := base64.RawURLEncoding.DecodeString(first.PrivateKey)
	if err != nil || len(private) != 32 {
		t.Fatalf("private key is not an unpadded base64url 32-byte scalar: %d bytes, %v", len(private), err)
	}
}

// TestValidateKeys covers every way a configured pair can be wrong, and checks
// that no error message leaks the private key.
func TestValidateKeys(t *testing.T) {
	t.Parallel()

	keys := mustKeys(t)
	other := mustKeys(t)
	padded := base64.URLEncoding.EncodeToString(mustDecode(t, keys.PublicKey))
	tests := []struct {
		name    string
		public  string
		private string
		wantErr bool
	}{
		{name: "valid pair", public: keys.PublicKey, private: keys.PrivateKey},
		{name: "padded public key", public: padded, private: keys.PrivateKey},
		{name: "surrounding whitespace", public: " " + keys.PublicKey + "\n", private: keys.PrivateKey + "\n"},
		{name: "both missing", wantErr: true},
		{name: "private missing", public: keys.PublicKey, wantErr: true},
		{name: "public missing", private: keys.PrivateKey, wantErr: true},
		{name: "private not base64", public: keys.PublicKey, private: "not base64!!", wantErr: true},
		{name: "private too short", public: keys.PublicKey, private: "AAAA", wantErr: true},
		{name: "public not a point", public: base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
			private: keys.PrivateKey, wantErr: true},
		{name: "public swapped with private", public: keys.PrivateKey, private: keys.PublicKey, wantErr: true},
		{name: "mismatched pair", public: other.PublicKey, private: keys.PrivateKey, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateKeys(tt.public, tt.private)
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateKeys() error = %v, want error %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error %v does not wrap ErrInvalidConfig", err)
			}
			if tt.private != "" && strings.Contains(err.Error(), strings.TrimSpace(tt.private)) {
				t.Fatalf("error leaks the private key: %v", err)
			}
		})
	}
}

// mustDecode decodes an unpadded base64url string or fails the test.
func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decoding %q: %v", s, err)
	}
	return b
}

// TestSubscriberFor covers the VAPID subject rules and the shape handed to
// webpush-go (a mailto: subject loses its scheme, which the library re-adds).
func TestSubscriberFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		subject string
		want    string
		wantErr bool
	}{
		{subject: "mailto:ops@example.org", want: "ops@example.org"},
		{subject: " mailto:ops@example.org ", want: "ops@example.org"},
		{subject: "https://kukatko.example.org", want: "https://kukatko.example.org"},
		{subject: "https://kukatko.example.org/contact", want: "https://kukatko.example.org/contact"},
		{subject: "", wantErr: true},
		{subject: "ops@example.org", wantErr: true},
		{subject: "mailto:", wantErr: true},
		{subject: "mailto:not an address", wantErr: true},
		{subject: "mailto:Ops <ops@example.org>", wantErr: true},
		{subject: "http://kukatko.example.org", wantErr: true},
		{subject: "https://", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			t.Parallel()
			got, err := subscriberFor(tt.subject)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("subscriberFor(%q) error = %v, want ErrInvalidConfig", tt.subject, err)
				}
				if ValidateSubject(tt.subject) == nil {
					t.Fatalf("ValidateSubject(%q) = nil, want an error", tt.subject)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("subscriberFor(%q) = %q, %v; want %q", tt.subject, got, err, tt.want)
			}
		})
	}
}

// TestValidateSubscription covers what a subscription must carry to be sent to.
func TestValidateSubscription(t *testing.T) {
	t.Parallel()

	good := newTestBrowser(t, "https://push.example.org/send/abc").sub
	stdAuth := base64.StdEncoding.EncodeToString(mustDecode(t, good.Auth))
	tests := []struct {
		name    string
		mutate  func(*Subscription)
		wantErr bool
	}{
		{name: "valid", mutate: func(*Subscription) {}},
		{name: "standard base64 auth", mutate: func(s *Subscription) { s.Auth = stdAuth }},
		{name: "empty endpoint", mutate: func(s *Subscription) { s.Endpoint = "" }, wantErr: true},
		{name: "http endpoint", mutate: func(s *Subscription) { s.Endpoint = "http://push.example.org/x" }, wantErr: true},
		{name: "relative endpoint", mutate: func(s *Subscription) { s.Endpoint = "/send/abc" }, wantErr: true},
		{name: "overlong endpoint", mutate: func(s *Subscription) {
			s.Endpoint = "https://push.example.org/" + strings.Repeat("a", maxEndpointLength)
		}, wantErr: true},
		{name: "empty p256dh", mutate: func(s *Subscription) { s.P256dh = "" }, wantErr: true},
		{name: "p256dh not a point", mutate: func(s *Subscription) {
			s.P256dh = base64.RawURLEncoding.EncodeToString(make([]byte, 65))
		}, wantErr: true},
		{name: "p256dh garbage", mutate: func(s *Subscription) { s.P256dh = "!!!" }, wantErr: true},
		{name: "auth wrong length", mutate: func(s *Subscription) {
			s.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 15))
		}, wantErr: true},
		{name: "auth garbage", mutate: func(s *Subscription) { s.Auth = "!!!" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sub := good
			tt.mutate(&sub)
			err := ValidateSubscription(sub)
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateSubscription() error = %v, want error %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidSubscription) {
				t.Fatalf("error %v does not wrap ErrInvalidSubscription", err)
			}
		})
	}
}

// TestMaxPayloadSize pins the arithmetic behind the limit: webpush-go's single
// 4096-byte record less header, tag and delimiter.
func TestMaxPayloadSize(t *testing.T) {
	t.Parallel()

	if MaxPayloadSize != 3993 {
		t.Fatalf("MaxPayloadSize = %d, want 3993", MaxPayloadSize)
	}
}

// TestNewSubscriptionID covers the id shape the VARCHAR(32) column needs.
func TestNewSubscriptionID(t *testing.T) {
	t.Parallel()

	first, err := newSubscriptionID()
	if err != nil {
		t.Fatalf("newSubscriptionID: %v", err)
	}
	second, err := newSubscriptionID()
	if err != nil {
		t.Fatalf("newSubscriptionID: %v", err)
	}
	if !strings.HasPrefix(first, subscriptionIDPrefix) || len(first) != 26 || len(first) > 32 {
		t.Fatalf("id %q has the wrong shape", first)
	}
	if strings.Trim(first[2:], idAlphabet) != "" {
		t.Fatalf("id %q uses characters outside the alphabet", first)
	}
	if first == second {
		t.Fatal("two ids collided")
	}
}
