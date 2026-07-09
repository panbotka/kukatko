package storage

import (
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"
)

const (
	testBaseURL  = "https://media.example.com"
	testSecret   = "current-secret"
	testPrevious = "previous-secret"
	testKey      = "2024/05/IMG_0001.jpg"
)

// fixedClock is the instant every signing test pins time.Now to, so an expiry is
// exact rather than "about an hour from whenever the test ran".
var fixedClock = time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)

// newTestSigner returns a signer over the test base URL with a frozen clock. A
// non-empty previous secret is accepted on verification alongside the current one.
func newTestSigner(t *testing.T, secret, previous string, ttl time.Duration) *URLSigner {
	t.Helper()
	signer, err := NewURLSigner(testBaseURL, secret, previous, ttl)
	if err != nil {
		t.Fatalf("NewURLSigner: %v", err)
	}
	signer.now = func() time.Time { return fixedClock }
	return signer
}

// signedParts signs key and returns the exp and sig query values, which is what a
// verifier (the edge Worker, or Verify) receives.
func signedParts(t *testing.T, signer *URLSigner, key string) (expiry, signature string) {
	t.Helper()
	parsed, err := url.Parse(signer.SignedURL(key))
	if err != nil {
		t.Fatalf("parsing signed URL: %v", err)
	}
	query := parsed.Query()
	return query.Get(QueryExpires), query.Get(QuerySignature)
}

func TestSignedURLShape(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, testSecret, "", time.Hour)

	raw := signer.SignedURL(testKey)
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	if parsed.Scheme != "https" || parsed.Host != "media.example.com" {
		t.Errorf("SignedURL host = %s://%s, want https://media.example.com", parsed.Scheme, parsed.Host)
	}
	if got, want := parsed.Path, "/"+testKey; got != want {
		t.Errorf("SignedURL path = %q, want %q", got, want)
	}
	if got, want := parsed.Query().Get(QueryExpires), strconv.FormatInt(fixedClock.Add(time.Hour).Unix(), 10); got != want {
		t.Errorf("exp = %q, want %q (now + ttl)", got, want)
	}
	if len(parsed.Query().Get(QuerySignature)) != 64 {
		t.Errorf("sig = %q, want 64 hex characters of HMAC-SHA256", parsed.Query().Get(QuerySignature))
	}
}

func TestSignedURLEscapesUTF8Key(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, testSecret, "", time.Hour)
	const key = "2024/05/Šťastné Vánoce.jpg"

	raw := signer.SignedURL(key)
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	// The rendered URL is percent-encoded, but it decodes back to the exact key…
	if got, want := parsed.Path, "/"+key; got != want {
		t.Errorf("decoded path = %q, want %q", got, want)
	}
	// …and the signature is over the unescaped key, so the Worker can verify what
	// it decodes from the path.
	expiry, signature := signedParts(t, signer, key)
	if err := signer.Verify(key, expiry, signature); err != nil {
		t.Errorf("Verify(utf-8 key) = %v, want nil", err)
	}
}

func TestVerifyAcceptsValidSignature(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, testSecret, "", time.Hour)

	expiry, signature := signedParts(t, signer, testKey)
	if err := signer.Verify(testKey, expiry, signature); err != nil {
		t.Errorf("Verify(fresh signature) = %v, want nil", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, testSecret, "", time.Hour)
	expiry, signature := signedParts(t, signer, testKey)
	farFuture := strconv.FormatInt(fixedClock.Add(100*365*24*time.Hour).Unix(), 10)

	tests := []struct {
		name      string
		key       string
		expiry    string
		signature string
	}{
		{name: "tampered key", key: "2024/05/other.jpg", expiry: expiry, signature: signature},
		{name: "tampered key path", key: "../etc/passwd", expiry: expiry, signature: signature},
		{name: "extended expiry", key: testKey, expiry: farFuture, signature: signature},
		{name: "tampered signature", key: testKey, expiry: expiry, signature: flipLastHexDigit(signature)},
		{name: "missing signature", key: testKey, expiry: expiry, signature: ""},
		{name: "non-hex signature", key: testKey, expiry: expiry, signature: "not-hex"},
		{name: "malformed expiry", key: testKey, expiry: "soon", signature: signature},
		{name: "empty expiry", key: testKey, expiry: "", signature: signature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := signer.Verify(tt.key, tt.expiry, tt.signature)
			if !errors.Is(err, ErrInvalidSignature) {
				t.Errorf("Verify(%s) = %v, want ErrInvalidSignature", tt.name, err)
			}
		})
	}
}

func TestVerifyRejectsExpiredURL(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t, testSecret, "", time.Hour)
	expiry, signature := signedParts(t, signer, testKey)

	// One second past the expiry the signature is still authentic, but stale.
	signer.now = func() time.Time { return fixedClock.Add(time.Hour + time.Second) }
	err := signer.Verify(testKey, expiry, signature)
	if !errors.Is(err, ErrURLExpired) {
		t.Errorf("Verify(expired) = %v, want ErrURLExpired", err)
	}

	// Exactly at the expiry the URL is still good.
	signer.now = func() time.Time { return fixedClock.Add(time.Hour) }
	if err := signer.Verify(testKey, expiry, signature); err != nil {
		t.Errorf("Verify(at expiry) = %v, want nil", err)
	}
}

func TestVerifyAcceptsPreviousSecretDuringRotation(t *testing.T) {
	t.Parallel()
	// A URL handed out before the rotation, signed with what is now the previous
	// secret.
	old := newTestSigner(t, testPrevious, "", time.Hour)
	expiry, signature := signedParts(t, old, testKey)

	rotated := newTestSigner(t, testSecret, testPrevious, time.Hour)
	if err := rotated.Verify(testKey, expiry, signature); err != nil {
		t.Errorf("Verify(previous secret) = %v, want nil after rotation", err)
	}

	// Once the previous secret is dropped, the same URL stops verifying.
	current := newTestSigner(t, testSecret, "", time.Hour)
	if err := current.Verify(testKey, expiry, signature); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("Verify(dropped secret) = %v, want ErrInvalidSignature", err)
	}
}

func TestSignedURLUsesCurrentSecret(t *testing.T) {
	t.Parallel()
	rotated := newTestSigner(t, testSecret, testPrevious, time.Hour)
	currentOnly := newTestSigner(t, testSecret, "", time.Hour)

	if got, want := rotated.SignedURL(testKey), currentOnly.SignedURL(testKey); got != want {
		t.Errorf("SignedURL signed with the previous secret:\n got %q\nwant %q", got, want)
	}
}

func TestSignedURLKeepsBasePathPrefix(t *testing.T) {
	t.Parallel()
	signer, err := NewURLSigner("https://media.example.com/photos", testSecret, "", time.Hour)
	if err != nil {
		t.Fatalf("NewURLSigner: %v", err)
	}
	parsed, err := url.Parse(signer.SignedURL(testKey))
	if err != nil {
		t.Fatalf("parsing signed URL: %v", err)
	}
	if got, want := parsed.Path, "/photos/"+testKey; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestNewURLSignerValidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		secret  string
		wantErr error
	}{
		{name: "empty base URL", baseURL: "", secret: testSecret, wantErr: ErrInvalidBaseURL},
		{name: "no scheme", baseURL: "media.example.com", secret: testSecret, wantErr: ErrInvalidBaseURL},
		{name: "bad scheme", baseURL: "ftp://media.example.com", secret: testSecret, wantErr: ErrInvalidBaseURL},
		{name: "no host", baseURL: "https:///photos", secret: testSecret, wantErr: ErrInvalidBaseURL},
		{name: "no secret", baseURL: testBaseURL, secret: "", wantErr: ErrMissingSigningSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewURLSigner(tt.baseURL, tt.secret, "", time.Hour)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewURLSigner(%q) = %v, want %v", tt.baseURL, err, tt.wantErr)
			}
		})
	}
}

func TestNewURLSignerDefaultsTTL(t *testing.T) {
	t.Parallel()
	signer, err := NewURLSigner(testBaseURL, testSecret, "", 0)
	if err != nil {
		t.Fatalf("NewURLSigner: %v", err)
	}
	if signer.ttl != DefaultURLTTL {
		t.Errorf("ttl = %s, want %s", signer.ttl, DefaultURLTTL)
	}
}

// flipLastHexDigit returns signature with its final hex digit changed, producing
// a well-formed signature of the right length that is nonetheless wrong.
func flipLastHexDigit(signature string) string {
	if signature == "" {
		return "0"
	}
	last := signature[len(signature)-1]
	if last == '0' {
		return signature[:len(signature)-1] + "1"
	}
	return signature[:len(signature)-1] + "0"
}
