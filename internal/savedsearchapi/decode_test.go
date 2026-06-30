package savedsearchapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/savedsearch"
)

// newJSONRequest builds a POST request whose body is the given raw JSON string.
func newJSONRequest(body string) *http.Request {
	return httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/saved-searches", strings.NewReader(body))
}

// errKind classifies the expected outcome of a decode call so the assertions can
// distinguish the empty-name sentinel from the per-call invalid-body error.
type errKind int

const (
	errNone errKind = iota
	errEmpty
	errBody
)

// assertDecodeErr fails unless err matches the expected kind: the empty-name
// sentinel by identity, and the invalid-body error by its stable prefix.
func assertDecodeErr(t *testing.T, err error, kind errKind) {
	t.Helper()
	switch kind {
	case errNone:
		if err != nil {
			t.Fatalf("unexpected error %v", err)
		}
	case errEmpty:
		if !errors.Is(err, errEmptyName) {
			t.Fatalf("error = %v, want errEmptyName", err)
		}
	case errBody:
		if err == nil || !strings.HasPrefix(err.Error(), "invalid request body:") {
			t.Fatalf("error = %v, want invalid request body", err)
		}
	}
}

// TestDecodeCreate covers the create body's validation: a non-empty name is
// required, unknown fields are rejected and params are carried through verbatim.
func TestDecodeCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		wantKind errKind
		wantName string
	}{
		{name: "valid", body: `{"name":"Recent","params":{"sort":"newest"}}`, wantName: "Recent"},
		{name: "name trimmed", body: `{"name":"  Recent  "}`, wantName: "Recent"},
		{name: "missing name", body: `{"params":{}}`, wantKind: errEmpty},
		{name: "blank name", body: `{"name":"   "}`, wantKind: errEmpty},
		{name: "unknown field", body: `{"name":"x","bogus":1}`, wantKind: errBody},
		{name: "malformed json", body: `{`, wantKind: errBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in, err := decodeCreate(newJSONRequest(tt.body))
			if tt.wantKind != errNone {
				assertDecodeErr(t, err, tt.wantKind)
				return
			}
			assertDecodeErr(t, err, errNone)
			if in.Name != tt.wantName {
				t.Errorf("name = %q, want %q", in.Name, tt.wantName)
			}
		})
	}
}

// TestDecodeUpdate covers merging a patch onto an existing saved search: omitted
// fields keep the existing value, a supplied blank name is rejected, and supplied
// values override.
func TestDecodeUpdate(t *testing.T) {
	t.Parallel()

	existing := savedsearch.SavedSearch{
		Name:   "Old",
		Params: json.RawMessage(`{"sort":"oldest"}`),
	}
	tests := []struct {
		name       string
		body       string
		wantKind   errKind
		wantName   string
		wantParams string
	}{
		{name: "empty patch keeps all", body: `{}`, wantName: "Old", wantParams: `{"sort":"oldest"}`},
		{name: "rename only", body: `{"name":"New"}`, wantName: "New", wantParams: `{"sort":"oldest"}`},
		{
			name: "params only", body: `{"params":{"sort":"newest"}}`,
			wantName: "Old", wantParams: `{"sort":"newest"}`,
		},
		{name: "both", body: `{"name":"New","params":[1]}`, wantName: "New", wantParams: `[1]`},
		{name: "blank name rejected", body: `{"name":"  "}`, wantKind: errEmpty},
		{name: "unknown field rejected", body: `{"bogus":1}`, wantKind: errBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, params, err := decodeUpdate(newJSONRequest(tt.body), existing)
			if tt.wantKind != errNone {
				assertDecodeErr(t, err, tt.wantKind)
				return
			}
			assertDecodeErr(t, err, errNone)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if string(params) != tt.wantParams {
				t.Errorf("params = %q, want %q", params, tt.wantParams)
			}
		})
	}
}

// TestToViewAndListResponse checks that the view projection drops owner_uid and
// that the list envelope wraps the views under the saved_searches key.
func TestToViewAndListResponse(t *testing.T) {
	t.Parallel()

	s := savedsearch.SavedSearch{
		UID: "ss1", OwnerUID: "u1", Name: "Recent", Params: json.RawMessage(`{"a":1}`),
	}
	view := toView(s)
	if view.UID != "ss1" || view.Name != "Recent" || string(view.Params) != `{"a":1}` {
		t.Fatalf("unexpected view: %+v", view)
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	if strings.Contains(string(encoded), "owner_uid") {
		t.Errorf("view JSON leaks owner_uid: %s", encoded)
	}

	env := listResponse([]savedsearch.SavedSearch{s})
	if len(env.SavedSearches) != 1 || env.SavedSearches[0].UID != "ss1" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if listResponse(nil).SavedSearches == nil {
		t.Error("listResponse(nil) should yield a non-nil empty slice")
	}
}
