//go:build integration

package auth_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// virtualAuthenticator is a software WebAuthn authenticator: an ES256 key pair
// plus the byte formats a real one produces. It exists because the passkey
// ceremonies cannot be tested any other way — every step of them is a signature
// over a challenge, so a stub that returns "yes, verified" would test the stub.
//
// It deliberately produces the bytes rather than mocking the library: what these
// tests are for is the wiring between this instance's stored credential columns
// and a verification that actually runs, and only real bytes exercise that.
type virtualAuthenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	aaguid       []byte
	signCount    uint32
}

// Authenticator data flags, §6.1 of the WebAuthn specification. Backup
// eligibility and state are set because a synced platform passkey — the common
// case — sets them, and because the library refuses a login whose eligibility
// disagrees with the one recorded at registration: a fixture that left them off
// would never exercise that column at all.
const (
	flagUserPresent            = 0x01
	flagUserVerified           = 0x04
	flagBackupEligible         = 0x08
	flagBackupState            = 0x10
	flagAttestedCredentialData = 0x40
)

// newVirtualAuthenticator returns an authenticator with a fresh key pair and a
// random credential id.
func newVirtualAuthenticator(t *testing.T) *virtualAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating the authenticator key: %v", err)
	}
	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		t.Fatalf("generating the credential id: %v", err)
	}
	return &virtualAuthenticator{key: key, credentialID: credentialID, aaguid: make([]byte, 16)}
}

// coseKey returns the credential's public key in the COSE_Key encoding the
// attested credential data carries: an EC2 key on P-256 signing with ES256.
func (a *virtualAuthenticator) coseKey(t *testing.T) []byte {
	t.Helper()
	// The uncompressed SEC 1 encoding is 0x04 followed by the two 32-byte
	// coordinates, which is exactly the pair COSE wants under -2 and -3.
	point, err := a.key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encoding the public key: %v", err)
	}
	if len(point) != 65 {
		t.Fatalf("public key is %d bytes, want the 65-byte uncompressed P-256 point", len(point))
	}
	encoded, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:65]})
	if err != nil {
		t.Fatalf("encoding the COSE key: %v", err)
	}
	return encoded
}

// authData assembles the authenticator data: the RP ID hash, the flags, the
// signature counter and — when attested is set — the attested credential data
// that carries the new public key.
func (a *virtualAuthenticator) authData(t *testing.T, rpID string, attested bool) []byte {
	t.Helper()
	hash := sha256.Sum256([]byte(rpID))
	flags := byte(flagUserPresent | flagUserVerified | flagBackupEligible | flagBackupState)
	if attested {
		flags |= flagAttestedCredentialData
	}
	data := append([]byte{}, hash[:]...)
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, a.signCount)
	if !attested {
		return data
	}
	data = append(data, a.aaguid...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.credentialID)))
	data = append(data, a.credentialID...)
	return append(data, a.coseKey(t)...)
}

// clientData returns the CollectedClientData JSON for one ceremony: which
// ceremony it is, the challenge it answers and the origin it ran on. All three
// are covered by the signature, which is what binds a passkey to this site.
func clientData(t *testing.T, ceremonyType, challenge, origin string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"type":        ceremonyType,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encoding the client data: %v", err)
	}
	return encoded
}

// register produces the PublicKeyCredential JSON a browser returns from
// navigator.credentials.create for the given challenge and origin, with an
// attestation of format "none" — the format a platform passkey uses.
func (a *virtualAuthenticator) register(t *testing.T, rpID, origin, challenge string) json.RawMessage {
	t.Helper()
	clientJSON := clientData(t, "webauthn.create", challenge, origin)
	attestation, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": a.authData(t, rpID, true),
	})
	if err != nil {
		t.Fatalf("encoding the attestation object: %v", err)
	}
	return mustJSON(t, map[string]any{
		"id":                     b64(a.credentialID),
		"rawId":                  b64(a.credentialID),
		"type":                   "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64(clientJSON),
			"attestationObject": b64(attestation),
			"transports":        []string{"internal", "hybrid"},
		},
	})
}

// assert produces the PublicKeyCredential JSON a browser returns from
// navigator.credentials.get: the authenticator data, the client data and an
// ECDSA signature over both, plus the user handle that makes the login
// discoverable. The signature counter is advanced first, exactly as a real
// authenticator advances it on every use.
func (a *virtualAuthenticator) assert(t *testing.T, rpID, origin, challenge string, userHandle []byte) json.RawMessage {
	t.Helper()
	a.signCount++
	clientJSON := clientData(t, "webauthn.get", challenge, origin)
	authData := a.authData(t, rpID, false)
	clientHash := sha256.Sum256(clientJSON)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("signing the assertion: %v", err)
	}
	return mustJSON(t, map[string]any{
		"id":                     b64(a.credentialID),
		"rawId":                  b64(a.credentialID),
		"type":                   "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64(clientJSON),
			"authenticatorData": b64(authData),
			"signature":         b64(signature),
			"userHandle":        b64(userHandle),
		},
	})
}

// b64 encodes raw bytes the way every binary field of a WebAuthn response is
// encoded: base64url without padding.
func b64(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// mustJSON marshals value or fails the test.
func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding JSON: %v", err)
	}
	return encoded
}

// ceremonyChallenge digs the challenge out of a begin response, which nests the
// options exactly as navigator.credentials expects them ("options.publicKey").
func ceremonyChallenge(t *testing.T, body []byte) string {
	t.Helper()
	var parsed struct {
		Options struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decoding the ceremony options: %v", err)
	}
	if parsed.Options.PublicKey.Challenge == "" {
		t.Fatalf("ceremony options carried no challenge: %s", body)
	}
	return parsed.Options.PublicKey.Challenge
}
