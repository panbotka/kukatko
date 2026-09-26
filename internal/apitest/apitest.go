// Package apitest provides the HTTP client with which the API packages' tests
// drive their httptest servers.
package apitest

import "net/http"

// client is the shared pool-free client handed out by Client.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// Client returns the HTTP client for a test that drives an httptest.Server.
//
// It exists because http.DefaultClient is the wrong client for the job.
// httptest.Server.Close() closes http.DefaultTransport's idle connections as a
// courtesy to its users (see net/http/httptest/server.go), and that transport is
// process-wide. In a package of parallel tests that each raise and tear down a
// server of their own, one test's teardown therefore evicts the pooled
// connection another test is about to reuse, and the request in flight fails
// with "net/http: HTTP/1.x transport connection broken: http:
// CloseIdleConnections called". A POST is the one that surfaces it: net/http
// replays a request only when it is idempotent, so a GET is silently retried on
// a fresh connection and a POST is not. Which test loses the race is arbitrary,
// which is why such a failure moves between runs and looks like someone else's
// regression — see internal/ctl/client_test.go, where the same error was first
// diagnosed.
//
// Keep-alives are off, so this client pools nothing: there is no connection for
// anyone to evict, and none left over from a server that has already closed. A
// test request pays one local TCP handshake for that.
func Client() *http.Client {
	return client
}
