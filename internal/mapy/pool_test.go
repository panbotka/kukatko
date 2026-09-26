package mapy

import (
	"net/http"
	"testing"
)

// TestNew_clientGetsAPoolOfItsOwn pins that a client built without an explicit
// HTTPClient does not run on http.DefaultTransport. Sharing that transport means
// sharing everyone who empties it, which is how an upstream connection used to
// disappear between two requests of a parallel test.
func TestNew_clientGetsAPoolOfItsOwn(t *testing.T) {
	t.Parallel()

	client, err := New(Config{BaseURL: "https://api.mapy.example", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.client.Transport == nil {
		t.Fatal("Transport is nil, so the client runs on http.DefaultTransport")
	}
	if client.client.Transport == http.DefaultTransport {
		t.Fatal("Transport is http.DefaultTransport, which every httptest server empties on teardown")
	}
}

// TestOwnConnectionPool_keepsTheDefaultSettings verifies the pool is a clone of
// the stdlib default rather than a bare transport, so it still honours the
// environment's proxy.
func TestOwnConnectionPool_keepsTheDefaultSettings(t *testing.T) {
	t.Parallel()

	transport, ok := ownConnectionPool().(*http.Transport)
	if !ok {
		t.Fatalf("pool is %T, want *http.Transport", ownConnectionPool())
	}
	if transport.Proxy == nil {
		t.Error("Proxy is nil: the pool is a bare transport, not a clone of the default")
	}
}
