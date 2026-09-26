package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"testing"
)

// testBrowser is the receiving side of a subscription: the key pair and auth
// secret a browser keeps, and the Subscription it would hand to the server.
type testBrowser struct {
	private *ecdh.PrivateKey
	auth    []byte
	sub     Subscription
}

// newTestBrowser mints a browser's subscription keys for endpoint.
func newTestBrowser(t *testing.T, endpoint string) testBrowser {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating the browser key: %v", err)
	}
	auth := make([]byte, authSecretSize)
	if _, err := rand.Read(auth); err != nil {
		t.Fatalf("generating the auth secret: %v", err)
	}
	return testBrowser{
		private: private,
		auth:    auth,
		sub: Subscription{
			Endpoint: endpoint,
			P256dh:   base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
			Auth:     base64.RawURLEncoding.EncodeToString(auth),
		},
	}
}

// decrypt reverses RFC 8291 + RFC 8188 (aes128gcm, one record) the way a
// browser does, returning the plaintext payload. It fails the test on any
// deviation from the format.
func (b testBrowser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < recordHeaderSize {
		t.Fatalf("body is %d bytes, shorter than the header", len(body))
	}
	salt := body[:16]
	recordSize := binary.BigEndian.Uint32(body[16:20])
	if int(recordSize) != len(body) {
		t.Fatalf("record size header = %d, body is %d bytes", recordSize, len(body))
	}
	if keyIDLen := body[20]; keyIDLen != 65 {
		t.Fatalf("key id length = %d, want 65", keyIDLen)
	}
	serverPublicRaw := body[21:86]
	serverPublic, err := ecdh.P256().NewPublicKey(serverPublicRaw)
	if err != nil {
		t.Fatalf("server key: %v", err)
	}
	shared, err := b.private.ECDH(serverPublic)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}
	keyInfo := append([]byte("WebPush: info\x00"), b.private.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, serverPublicRaw...)
	ikm := mustHKDF(t, shared, b.auth, keyInfo, 32)
	cek := mustHKDF(t, ikm, salt, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := mustHKDF(t, ikm, salt, []byte("Content-Encoding: nonce\x00"), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("AES: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("GCM: %v", err)
	}
	plain, err := gcm.Open(nil, nonce, body[recordHeaderSize:], nil)
	if err != nil {
		t.Fatalf("decrypting the record: %v", err)
	}
	end := len(plain) - 1
	for end >= 0 && plain[end] == 0 {
		end--
	}
	if end < 0 || plain[end] != 2 {
		t.Fatalf("missing the last-record delimiter")
	}
	return plain[:end]
}

// mustHKDF derives length bytes with HKDF-SHA256.
func mustHKDF(t *testing.T, secret, salt, info []byte, length int) []byte {
	t.Helper()
	key, err := hkdf.Key(sha256.New, secret, salt, string(info), length)
	if err != nil {
		t.Fatalf("HKDF: %v", err)
	}
	return key
}

// mustKeys mints a VAPID key pair or fails the test.
func mustKeys(t *testing.T) KeyPair {
	t.Helper()
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	return keys
}

// ecdsaCurve is the curve VAPID signs on.
func ecdsaCurve() elliptic.Curve {
	return elliptic.P256()
}

// verifyRS verifies a JWS ES256 signature — r and s as two 32-byte big-endian
// halves — over digest.
func verifyRS(public *ecdsa.PublicKey, digest, signature []byte) bool {
	if len(signature) != 64 {
		return false
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	return ecdsa.Verify(public, digest, r, s)
}
