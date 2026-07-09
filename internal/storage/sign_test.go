package storage

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
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

// vectorsPath is the golden file freezing the signed-URL contract. It is a
// published artifact: the edge Worker in the infra repository
// (cloudflare-r2/) verifies its own implementation against the same file.
const vectorsPath = "testdata/url_signature_vectors.json"

// regenerate is appended to every golden-vector failure. A mismatch means the
// signing algorithm moved, and the Worker that verifies these URLs did not.
const regenerate = "the signed URL contract changed: regenerate " + vectorsPath +
	" and ship the matching Worker change in the infra repo (cloudflare-r2/) before deploying either side"

// urlSignatureVector is one fixture from the golden file: a signing secret, an
// object key, an expiry, and the signature the contract says they produce.
type urlSignatureVector struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Secret         string `json:"secret"`
	PreviousSecret string `json:"previous_secret"`
	Key            string `json:"key"`
	ExpiresAt      int64  `json:"expires_at"`
	Signature      string `json:"signature"`
	// Mints reports whether signing Key and ExpiresAt with Secret reproduces
	// Signature. It is false for a signature made with the previous secret and
	// for a deliberately wrong one.
	Mints bool `json:"mints"`
	// Verifies reports whether a verifier holding Secret and PreviousSecret
	// accepts Signature at ExpiresAt.
	Verifies bool `json:"verifies"`
}

// urlSignatureVectors is the golden file. Only the fields the Go side is held to
// are decoded; the prose the Worker's authors read is ignored here.
type urlSignatureVectors struct {
	Version   int `json:"version"`
	Algorithm struct {
		MAC               string `json:"mac"`
		SignatureEncoding string `json:"signature_encoding"`
		QueryParameters   struct {
			Expires   string `json:"expires"`
			Signature string `json:"signature"`
		} `json:"query_parameters"`
	} `json:"algorithm"`
	Vectors []urlSignatureVector `json:"vectors"`
}

// loadURLSignatureVectors reads and decodes the golden file.
func loadURLSignatureVectors(t *testing.T) urlSignatureVectors {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", vectorsPath, err)
	}
	var vectors urlSignatureVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decoding %s: %v", vectorsPath, err)
	}
	if len(vectors.Vectors) == 0 {
		t.Fatalf("%s carries no vectors", vectorsPath)
	}
	return vectors
}

// vectorSigner returns a signer configured from a vector, its clock pinned so a
// URL minted right now expires at exactly the vector's expiry.
func vectorSigner(t *testing.T, vector urlSignatureVector) *URLSigner {
	t.Helper()
	signer, err := NewURLSigner(testBaseURL, vector.Secret, vector.PreviousSecret, DefaultURLTTL)
	if err != nil {
		t.Fatalf("NewURLSigner(%s): %v", vector.Name, err)
	}
	signer.now = func() time.Time { return time.Unix(vector.ExpiresAt, 0).Add(-DefaultURLTTL) }
	return signer
}

// TestURLSignatureVectorsDescribeThisImplementation guards the parts of the
// contract the vectors state in prose rather than in a signature: the query
// parameter names and the MAC itself. Renaming a parameter here without
// regenerating the file — and telling the Worker — would 403 every image.
func TestURLSignatureVectorsDescribeThisImplementation(t *testing.T) {
	t.Parallel()
	vectors := loadURLSignatureVectors(t)

	if got := vectors.Algorithm.QueryParameters.Expires; got != QueryExpires {
		t.Errorf("expiry query parameter = %q, package uses %q; %s", got, QueryExpires, regenerate)
	}
	if got := vectors.Algorithm.QueryParameters.Signature; got != QuerySignature {
		t.Errorf("signature query parameter = %q, package uses %q; %s", got, QuerySignature, regenerate)
	}
	if got := vectors.Algorithm.MAC; got != "HMAC-SHA256" {
		t.Errorf("mac = %q, want HMAC-SHA256; %s", got, regenerate)
	}
	for _, vector := range vectors.Vectors {
		// Lowercase hex of a SHA-256 MAC, as the file promises.
		if len(vector.Signature) != 64 {
			t.Errorf("vector %s: signature %q is not 64 hex characters; %s", vector.Name, vector.Signature, regenerate)
		}
	}
}

// TestURLSignatureVectorsMint asserts SignedURL reproduces every golden
// signature — and, for the vectors that were not signed with the current secret,
// that it produces something else.
func TestURLSignatureVectorsMint(t *testing.T) {
	t.Parallel()
	for _, vector := range loadURLSignatureVectors(t).Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			signer := vectorSigner(t, vector)

			parsed, err := url.Parse(signer.SignedURL(vector.Key))
			if err != nil {
				t.Fatalf("parsing signed URL: %v", err)
			}
			if got, want := parsed.Path, "/"+vector.Key; got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			if got, want := parsed.Query().Get(QueryExpires), strconv.FormatInt(vector.ExpiresAt, 10); got != want {
				t.Fatalf("exp = %q, want %q", got, want)
			}

			got := parsed.Query().Get(QuerySignature)
			switch {
			case vector.Mints && got != vector.Signature:
				t.Errorf("sig = %q, want %q (%s); %s", got, vector.Signature, vector.Description, regenerate)
			case !vector.Mints && got == vector.Signature:
				t.Errorf("sig = %q, want anything else (%s); %s", got, vector.Description, regenerate)
			}
		})
	}
}

// TestURLSignatureVectorsVerify asserts Verify agrees with every golden vector
// at its expiry, and that an accepted one goes stale one second later.
func TestURLSignatureVectorsVerify(t *testing.T) {
	t.Parallel()
	for _, vector := range loadURLSignatureVectors(t).Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			signer := vectorSigner(t, vector)
			expiresAt := time.Unix(vector.ExpiresAt, 0)
			expiry := strconv.FormatInt(vector.ExpiresAt, 10)

			// The boundary second is still fresh, so a vector's verdict is about
			// its signature alone.
			signer.now = func() time.Time { return expiresAt }
			err := signer.Verify(vector.Key, expiry, vector.Signature)
			if vector.Verifies {
				if err != nil {
					t.Fatalf("Verify(%s) = %v, want nil (%s); %s", vector.Name, err, vector.Description, regenerate)
				}
				signer.now = func() time.Time { return expiresAt.Add(time.Second) }
				if err := signer.Verify(vector.Key, expiry, vector.Signature); !errors.Is(err, ErrURLExpired) {
					t.Errorf("Verify(%s, one second stale) = %v, want ErrURLExpired", vector.Name, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidSignature) {
				t.Errorf("Verify(%s) = %v, want ErrInvalidSignature (%s); %s",
					vector.Name, err, vector.Description, regenerate)
			}
		})
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
