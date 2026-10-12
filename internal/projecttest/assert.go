//go:build integration

package projecttest

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// AssertInvisible asserts that GET path, sent as client (from ClientFor), is
// answered 404 and that the body names none of the other side's UIDs. A UID
// the path itself carries is exempt: echoing back what the caller sent leaks
// nothing.
func (f *Fixture) AssertInvisible(tb testing.TB, client *http.Client, path string) {
	tb.Helper()
	info := f.lookup(tb, client)
	status, _, body := fetch(tb, client, info.baseURL, path)
	if status != http.StatusNotFound {
		tb.Errorf("GET %s as %s = %d, want 404; body %s", path, info.side, status, body)
	}
	foreign := slices.DeleteFunc(f.Tenant(info.side.Other()).UIDs(), func(uid string) bool {
		return strings.Contains(path, uid)
	})
	assertNoneIn(tb, "GET "+path+" as "+info.side.String(), body, foreign)
}

// AssertListExcludes asserts that GET path, sent as client (from ClientFor), is
// answered 200 and that the body contains none of otherUIDs — typically the
// other tenant's UIDs() or a chosen few of them. At least one UID is required:
// an assertion that excludes nothing would pass for every leak.
func (f *Fixture) AssertListExcludes(tb testing.TB, client *http.Client, path string, otherUIDs ...string) {
	tb.Helper()
	if len(otherUIDs) == 0 {
		tb.Fatalf("projecttest: AssertListExcludes(%s) needs at least one uid to exclude", path)
	}
	info := f.lookup(tb, client)
	status, _, body := fetch(tb, client, info.baseURL, path)
	if status != http.StatusOK {
		tb.Errorf("GET %s as %s = %d, want 200; body %s", path, info.side, status, body)
	}
	assertNoneIn(tb, "GET "+path+" as "+info.side.String(), body, otherUIDs)
}

// AssertSameShape asserts that the answer about another project's row is
// indistinguishable from the answer about a row that does not exist: the same
// status, the same Content-Type and byte-identical bodies. Both bodies are read
// and closed. Build respMissing with MissingUID of the same kind.
func AssertSameShape(tb testing.TB, respForeign, respMissing *http.Response) {
	tb.Helper()
	foreign := readBody(tb, respForeign.Body)
	missing := readBody(tb, respMissing.Body)
	if respForeign.StatusCode != respMissing.StatusCode {
		tb.Errorf("foreign row answered %d, missing row %d: the status tells them apart",
			respForeign.StatusCode, respMissing.StatusCode)
	}
	if a, b := respForeign.Header.Get("Content-Type"), respMissing.Header.Get("Content-Type"); a != b {
		tb.Errorf("foreign row answered Content-Type %q, missing row %q", a, b)
	}
	if !bytes.Equal(foreign, missing) {
		tb.Errorf("foreign row answered body %s, missing row %s: the body tells them apart", foreign, missing)
	}
}

// AssertStoreScoped asserts a store-level read is scoped to its project: fn is
// called with A's and B's project uid and must return, for each, a non-empty
// list of uids (so the read does find the side's own rows), and the two lists
// must share no uid. A read that ignores the project returns the same rows for
// both and fails.
func (f *Fixture) AssertStoreScoped(tb testing.TB, fn func(projectUID string) ([]string, error)) {
	tb.Helper()
	var got [2][]string
	for _, side := range []Side{SideA, SideB} {
		uids, err := fn(f.Tenant(side).ProjectUID)
		if err != nil {
			tb.Fatalf("store read for %s: %v", side, err)
		}
		if len(uids) == 0 {
			tb.Errorf("store read for %s returned nothing: the side's own rows are missing", side)
		}
		got[side] = uids
	}
	for _, uid := range got[SideA] {
		if slices.Contains(got[SideB], uid) {
			tb.Errorf("store read returned %s for both A and B: the read is not scoped by project", uid)
		}
	}
}

// assertNoneIn fails tb once, naming every uid of uids that body contains.
// what names the request in the message.
func assertNoneIn(tb testing.TB, what string, body []byte, uids []string) {
	tb.Helper()
	var leaked []string
	for _, uid := range uids {
		if uid != "" && bytes.Contains(body, []byte(uid)) {
			leaked = append(leaked, uid)
		}
	}
	if len(leaked) > 0 {
		tb.Errorf("%s leaks the other project's %s; body %s", what, strings.Join(leaked, ", "), body)
	}
}

// readBody reads and closes a response body, failing tb when it cannot.
func readBody(tb testing.TB, rc io.ReadCloser) []byte {
	tb.Helper()
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(rc)
	if err != nil {
		tb.Fatalf("projecttest: reading the response body: %v", err)
	}
	return body
}
