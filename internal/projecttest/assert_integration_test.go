//go:build integration

package projecttest

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// recorder is a testing.TB that records failures instead of failing the real
// test, so the assertions' failure paths can be exercised. Fatal* end the
// calling goroutine, as the real ones do.
type recorder struct {
	testing.TB

	// mu guards msgs.
	mu sync.Mutex
	// msgs are the recorded failure messages.
	msgs []string
}

// Helper does nothing; the recorder reports no file:line.
func (*recorder) Helper() {}

// Errorf records a failure.
func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

// Fatalf records a failure and ends the goroutine.
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// failures returns the recorded messages joined, "" when nothing failed.
func (r *recorder) failures() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.msgs, "\n")
}

// record runs fn against a fresh recorder on its own goroutine (so a Fatalf
// ends only fn) and returns what it recorded.
func record(t *testing.T, fn func(tb testing.TB)) string {
	t.Helper()
	rec := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rec)
	}()
	<-done
	return rec.failures()
}

// answer is one canned response of fakeFixture's server.
type answer struct {
	// status is the HTTP status.
	status int
	// body is the JSON body.
	body string
}

// fakeFixture returns a fixture with made-up tenants and an httptest server
// that accepts any login and answers every other path from routes (404 with
// the same JSON body otherwise), plus one client signed in on it per side.
func fakeFixture(t *testing.T, routes map[string]answer) (*Fixture, [2]*http.Client) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "x", Path: "/"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if a, ok := routes[r.URL.Path]; ok {
			w.WriteHeader(a.status)
			_, _ = w.Write([]byte(a.body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	t.Cleanup(server.Close)

	f := &Fixture{
		A:       Tenant{ProjectUID: "pj-a", Slug: "v", UserUID: "us-a", Username: "a", PhotoUID: "ph-a", AlbumUID: "al-a"},
		B:       Tenant{ProjectUID: "pj-b", Slug: "b-1", UserUID: "us-b", Username: "b", PhotoUID: "ph-b", AlbumUID: "al-b"},
		clients: map[*http.Client]signedIn{},
	}
	return f, [2]*http.Client{f.ClientFor(t, server, SideA), f.ClientFor(t, server, SideB)}
}

// TestPathFor_buildsTheProjectPrefix pins the path shape, with and without a
// leading slash on the suffix.
func TestPathFor_buildsTheProjectPrefix(t *testing.T) {
	t.Parallel()
	f := &Fixture{A: Tenant{Slug: "v"}, B: Tenant{Slug: "b-1"}}
	for _, tc := range []struct {
		side   Side
		suffix string
		want   string
	}{
		{SideA, "photos", "/api/v1/p/v/photos"},
		{SideB, "/photos/ph1", "/api/v1/p/b-1/photos/ph1"},
	} {
		if got := f.PathFor(tc.side, tc.suffix); got != tc.want {
			t.Errorf("PathFor(%s, %q) = %q, want %q", tc.side, tc.suffix, got, tc.want)
		}
	}
}

// TestSide_otherAndString pins the two small helpers of Side.
func TestSide_otherAndString(t *testing.T) {
	t.Parallel()
	if SideA.Other() != SideB || SideB.Other() != SideA {
		t.Error("Other does not swap the sides")
	}
	if SideA.String() != "A" || SideB.String() != "B" {
		t.Errorf("String = %q/%q, want A/B", SideA, SideB)
	}
}

// TestAssertInvisible covers the pass and both failure modes: a visible answer
// and a 404 whose body names the other side's row. A uid the path carries may
// be echoed.
func TestAssertInvisible(t *testing.T) {
	t.Parallel()
	f, clients := fakeFixture(t, map[string]answer{
		"/visible":   {http.StatusOK, `{"uid":"ph-a"}`},
		"/leaky-404": {http.StatusNotFound, `{"error":"not found","hint":"al-a"}`},
	})
	if got := record(t, func(tb testing.TB) { f.AssertInvisible(tb, clients[SideB], "/missing") }); got != "" {
		t.Errorf("a plain 404 failed: %s", got)
	}
	got := record(t, func(tb testing.TB) { f.AssertInvisible(tb, clients[SideB], "/visible") })
	if !strings.Contains(got, "want 404") || !strings.Contains(got, "leaks the other project's ph-a") {
		t.Errorf("a visible foreign row was not reported: %q", got)
	}
	got = record(t, func(tb testing.TB) { f.AssertInvisible(tb, clients[SideB], "/leaky-404") })
	if strings.Contains(got, "want 404") || !strings.Contains(got, "leaks the other project's al-a") {
		t.Errorf("a 404 naming a foreign row was not reported as a leak alone: %q", got)
	}
	// B's own uid is not "the other side's": a body naming it is no leak.
	if got := record(t, func(tb testing.TB) { f.AssertInvisible(tb, clients[SideA], "/visible") }); strings.Contains(
		got, "leaks") {
		t.Errorf("A's own uid was reported as a leak to A: %s", got)
	}
}

// TestAssertInvisible_pathUIDIsExempt proves a 404 that echoes the uid the
// caller asked for is not a leak.
func TestAssertInvisible_pathUIDIsExempt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no ` + strings.TrimPrefix(r.URL.Path, "/") + `"}`))
	}))
	t.Cleanup(server.Close)
	f := &Fixture{A: Tenant{PhotoUID: "ph-a"}, B: Tenant{PhotoUID: "ph-b"}, clients: map[*http.Client]signedIn{}}
	client := f.ClientFor(t, server, SideB)
	if got := record(t, func(tb testing.TB) { f.AssertInvisible(tb, client, "/ph-a") }); got != "" {
		t.Errorf("an echoed path uid was reported: %s", got)
	}
}

// TestAssertListExcludes covers a clean list, a leaking list, a non-200 and
// the refusal of an assertion that excludes nothing.
func TestAssertListExcludes(t *testing.T) {
	t.Parallel()
	f, clients := fakeFixture(t, map[string]answer{
		"/clean": {http.StatusOK, `[{"uid":"ph-b"}]`},
		"/leaky": {http.StatusOK, `[{"uid":"ph-b"},{"uid":"ph-a"}]`},
	})
	b := clients[SideB]
	for _, tc := range []struct {
		name string
		path string
		uids []string
		want string
	}{
		{"clean", "/clean", f.A.UIDs(), ""},
		{"leaky", "/leaky", []string{"ph-a"}, "leaks the other project's ph-a"},
		{"not 200", "/nothing", []string{"ph-a"}, "want 200"},
		{"nothing to exclude", "/clean", nil, "needs at least one uid"},
	} {
		got := record(t, func(tb testing.TB) { f.AssertListExcludes(tb, b, tc.path, tc.uids...) })
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: recorded %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAssertSameShape covers identical answers and each way two answers can
// differ.
func TestAssertSameShape(t *testing.T) {
	t.Parallel()
	const notFound = `{"error":"photo not found"}`
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		want        string
	}{
		{"same", http.StatusNotFound, "application/json", notFound, ""},
		{"status", http.StatusForbidden, "application/json", notFound, "the status tells them apart"},
		{"content type", http.StatusNotFound, "text/plain", notFound, "Content-Type"},
		{"body", http.StatusNotFound, "application/json", `{"error":"forbidden"}`, "the body tells"},
	} {
		foreign := httptest.NewRecorder()
		foreign.Header().Set("Content-Type", tc.contentType)
		foreign.WriteHeader(tc.status)
		_, _ = foreign.WriteString(tc.body)
		missing := httptest.NewRecorder()
		missing.Header().Set("Content-Type", "application/json")
		missing.WriteHeader(http.StatusNotFound)
		_, _ = missing.WriteString(notFound)
		foreignResp, missingResp := foreign.Result(), missing.Result()
		got := record(t, func(tb testing.TB) { AssertSameShape(tb, foreignResp, missingResp) })
		_ = foreignResp.Body.Close()
		_ = missingResp.Body.Close()
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: recorded %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAssertStoreScoped covers a scoped read, a read that ignores the project,
// a read that finds nothing and a failing read.
func TestAssertStoreScoped(t *testing.T) {
	t.Parallel()
	f := &Fixture{A: Tenant{ProjectUID: "pj-a"}, B: Tenant{ProjectUID: "pj-b"}}
	rows := map[string][]string{"pj-a": {"ph-a"}, "pj-b": {"ph-b"}}
	errRead := errors.New("boom")
	for _, tc := range []struct {
		name string
		fn   func(string) ([]string, error)
		want string
	}{
		{"scoped", func(p string) ([]string, error) { return rows[p], nil }, ""},
		{"unscoped", func(string) ([]string, error) { return []string{"ph-a", "ph-b"}, nil }, "for both A and B"},
		{"empty", func(p string) ([]string, error) {
			if p == "pj-b" {
				return nil, nil
			}
			return rows[p], nil
		}, "for B returned nothing"},
		{"error", func(string) ([]string, error) { return nil, errRead }, "boom"},
	} {
		got := record(t, func(tb testing.TB) { f.AssertStoreScoped(tb, tc.fn) })
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: recorded %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestGet_refusesAForeignClient proves the HTTP helpers refuse a client the
// fixture did not sign in, rather than sending the path nowhere.
func TestGet_refusesAForeignClient(t *testing.T) {
	t.Parallel()
	f := &Fixture{clients: map[*http.Client]signedIn{}}
	got := record(t, func(tb testing.TB) {
		resp := f.Get(tb, &http.Client{}, "/x")
		_ = resp.Body.Close()
	})
	if !strings.Contains(got, "not signed in through this fixture") {
		t.Errorf("recorded %q", got)
	}
}

// TestMissingUID pins the shape: the prefix plus 24 symbols of the uid
// alphabet, fresh every call.
func TestMissingUID(t *testing.T) {
	t.Parallel()
	a, b := MissingUID("ph"), MissingUID("ph")
	if len(a) != 26 || !strings.HasPrefix(a, "ph") || strings.Trim(a[2:], uidAlphabet) != "" {
		t.Errorf("MissingUID = %q, want ph + 24 symbols of %q", a, uidAlphabet)
	}
	if a == b {
		t.Errorf("MissingUID returned %q twice", a)
	}
}
