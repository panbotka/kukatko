package push

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	// aesGCMTagSize is the authentication tag AES-128-GCM appends to a record.
	aesGCMTagSize = 16
	// recordHeaderSize is the aes128gcm content-coding header in front of the
	// ciphertext (RFC 8188 §2.1): a 16-byte salt, the 4-byte record size, the
	// 1-byte key-id length and the 65-byte uncompressed ephemeral public key.
	recordHeaderSize = 16 + 4 + 1 + 65
	// paddingDelimiterSize is the one delimiter byte that ends the plaintext of
	// the last (here: only) record.
	paddingDelimiterSize = 1

	// MaxPayloadSize is the largest encoded notification, in bytes, that fits in
	// the single 4096-byte record webpush-go encrypts to: the record minus its
	// header, the GCM tag and the padding delimiter. Encode refuses anything
	// larger with ErrPayloadTooLarge.
	MaxPayloadSize = int(webpush.MaxRecordSize) - aesGCMTagSize - recordHeaderSize - paddingDelimiterSize

	// maxEndpointLength caps the endpoint URL a subscription may carry. Real
	// push services hand out a few hundred characters; anything near this is
	// not one of them.
	maxEndpointLength = 2048
	// authSecretSize is the length of a subscription's auth secret (RFC 8291 §3.2).
	authSecretSize = 16
)

// KeyPair is a VAPID key pair in the encoding browsers and push services use:
// unpadded base64url of the raw P-256 private scalar and of the uncompressed
// public point.
type KeyPair struct {
	// PublicKey is the application server key the frontend passes to
	// pushManager.subscribe. It is public by construction.
	PublicKey string
	// PrivateKey signs every VAPID token. It is a secret: it belongs in the
	// environment, never in a committed file.
	PrivateKey string
}

// GenerateKeys mints a fresh VAPID key pair from crypto/rand. It fails only if
// the system random source does.
//
// A pair is for life: every stored subscription was made for its public key, so
// a new pair makes each of them answer 403 until its browser subscribes again.
func GenerateKeys() (KeyPair, error) {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("push: generating a P-256 key: %w", err)
	}
	return KeyPair{
		PublicKey:  base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(private.Bytes()),
	}, nil
}

// ValidateKeys checks that publicKey and privateKey are a VAPID key pair: the
// private key decodes to a valid P-256 scalar, the public key to an uncompressed
// point, and the public key is the one the private key derives. A mismatched
// pair is refused too — every send would be signed with a key the browsers never
// subscribed against. It returns ErrInvalidConfig naming what is wrong, never a
// key's value.
func ValidateKeys(publicKey, privateKey string) error {
	if strings.TrimSpace(privateKey) == "" || strings.TrimSpace(publicKey) == "" {
		return fmt.Errorf("%w: both the public and the private key are required", ErrInvalidConfig)
	}
	rawPrivate, err := decodeKey(privateKey)
	if err != nil {
		return fmt.Errorf("%w: the private key is not base64url", ErrInvalidConfig)
	}
	private, err := ecdh.P256().NewPrivateKey(rawPrivate)
	if err != nil {
		return fmt.Errorf("%w: the private key is not a P-256 private key", ErrInvalidConfig)
	}
	rawPublic, err := decodeKey(publicKey)
	if err != nil {
		return fmt.Errorf("%w: the public key is not base64url", ErrInvalidConfig)
	}
	if _, err := ecdh.P256().NewPublicKey(rawPublic); err != nil {
		return fmt.Errorf("%w: the public key is not an uncompressed P-256 point", ErrInvalidConfig)
	}
	if !bytes.Equal(private.PublicKey().Bytes(), rawPublic) {
		return fmt.Errorf("%w: the public key does not belong to the private key", ErrInvalidConfig)
	}
	return nil
}

// ValidateSubject checks the VAPID subject: the contact a push service may use
// to reach the sender, which RFC 8292 §2.1 requires to be a mailto: or an
// https: URL. It returns ErrInvalidConfig otherwise.
func ValidateSubject(subject string) error {
	_, err := subscriberFor(subject)
	return err
}

// subscriberFor validates subject and returns it in the shape webpush-go's
// Subscriber option wants. That library prefixes "mailto:" to anything that is
// not an https: URL, so a mailto: subject is handed over as the bare address —
// passed through unchanged it would reach the push service as
// "mailto:mailto:…".
func subscriberFor(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if address, ok := strings.CutPrefix(subject, "mailto:"); ok {
		if _, err := mail.ParseAddress(address); err != nil || strings.ContainsAny(address, "<> ") {
			return "", fmt.Errorf("%w: the subject %q is not a mailto: address", ErrInvalidConfig, subject)
		}
		return address, nil
	}
	parsed, err := url.Parse(subject)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("%w: the subject %q must be a mailto: or an https: URL", ErrInvalidConfig, subject)
	}
	return subject, nil
}

// ValidateSubscription checks that sub can be sent to: an absolute https
// endpoint no longer than maxEndpointLength, a P256dh that decodes to an
// uncompressed P-256 public key and an Auth that decodes to 16 bytes. It returns
// ErrInvalidSubscription naming the problem. Only those three fields are read.
func ValidateSubscription(sub Subscription) error {
	if len(sub.Endpoint) > maxEndpointLength {
		return fmt.Errorf("%w: the endpoint is longer than %d characters", ErrInvalidSubscription, maxEndpointLength)
	}
	endpoint, err := url.Parse(sub.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return fmt.Errorf("%w: the endpoint is not an https URL", ErrInvalidSubscription)
	}
	rawP256dh, err := decodeKey(sub.P256dh)
	if err != nil {
		return fmt.Errorf("%w: p256dh is not base64", ErrInvalidSubscription)
	}
	if _, err := ecdh.P256().NewPublicKey(rawP256dh); err != nil {
		return fmt.Errorf("%w: p256dh is not an uncompressed P-256 point", ErrInvalidSubscription)
	}
	rawAuth, err := decodeKey(sub.Auth)
	if err != nil {
		return fmt.Errorf("%w: auth is not base64", ErrInvalidSubscription)
	}
	if len(rawAuth) != authSecretSize {
		return fmt.Errorf("%w: auth is %d bytes, want %d", ErrInvalidSubscription, len(rawAuth), authSecretSize)
	}
	return nil
}

// decodeKey decodes a key in any of the base64 flavours browsers and tools
// produce: base64url with or without padding (what PushSubscription.toJSON and
// GenerateKeys emit) and standard base64 with or without padding (what some
// older clients send). webpush-go itself accepts the same set.
func decodeKey(key string) ([]byte, error) {
	key = strings.TrimSpace(key)
	encodings := []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding,
	}
	var lastErr error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(key)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("push: decoding a key: %w", lastErr)
}
