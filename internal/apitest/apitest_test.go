package apitest_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/apitest"
)

// TestClient_poolsNothingOfItsOwn pins the two properties the whole point of the
// package rests on: the client does not run on http.DefaultTransport (which
// httptest.Server.Close() empties behind every test's back), and it keeps no idle
// connection that such a call could take away in the first place.
func TestClient_poolsNothingOfItsOwn(t *testing.T) {
	t.Parallel()

	client := apitest.Client()
	if client.Transport == nil {
		t.Fatal("Transport is nil, so the client runs on http.DefaultTransport")
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("Transport is http.DefaultTransport, which every httptest server closes on teardown")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", client.Transport)
	}
	if !transport.DisableKeepAlives {
		t.Error("keep-alives are on: the client pools connections a parallel test's teardown can evict")
	}
}
