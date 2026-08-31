package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// savedSearchesBody is a realistic bare {"saved_searches": […]} envelope — a
// fourth list shape, hence a fourth decoder.
const savedSearchesBody = `{"saved_searches":[
	{"uid":"sav01","name":"Léto u vody","params":{"q":"jezero","mode":"semantic"},
	 "created_at":"2024-05-01T10:22:33Z","updated_at":"2024-05-02T10:22:33Z"},
	{"uid":"sav02","name":"Nedatované","params":{}}
]}`

// TestClient_ListSavedSearches verifies the envelope reaches its own decoder with
// the stored view intact.
func TestClient_ListSavedSearches(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(savedSearchesBody))
	})

	raw, err := client.ListSavedSearches(t.Context())
	if err != nil {
		t.Fatalf("ListSavedSearches returned %v", err)
	}
	if gotPath != "/api/v1/saved-searches" {
		t.Errorf("path = %q, want the saved-search list", gotPath)
	}
	searches, err := DecodeSavedSearches(raw)
	if err != nil {
		t.Fatalf("DecodeSavedSearches returned %v", err)
	}
	if len(searches) != 2 || searches[0].Params["q"] != "jezero" {
		t.Errorf("searches = %+v, want the stored view decoded", searches)
	}
}

// TestClient_CreateSavedSearch verifies the name and the view go in one body and
// that an absent view is sent as an empty object, not as null.
func TestClient_CreateSavedSearch(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody map[string]json.RawMessage
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"sav07","name":"Léto","params":{}}`))
	})

	raw, err := client.CreateSavedSearch(t.Context(), " Léto ", nil)
	if err != nil {
		t.Fatalf("CreateSavedSearch returned %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if string(gotBody["name"]) != `"Léto"` {
		t.Errorf("body name = %s, want the trimmed name", gotBody["name"])
	}
	if string(gotBody["params"]) != "{}" {
		t.Errorf("body params = %s, want an empty object", gotBody["params"])
	}
	search, err := DecodeSavedSearch(raw)
	if err != nil || search.UID != "sav07" {
		t.Errorf("DecodeSavedSearch = %+v, %v, want the created search", search, err)
	}
}

// TestClient_CreateSavedSearch_blankName verifies a blank name never costs a
// round trip.
func TestClient_CreateSavedSearch_blankName(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite a blank name")
	})
	if _, err := client.CreateSavedSearch(t.Context(), "  ", nil); !errors.Is(err, ErrEmptySearchName) {
		t.Errorf("blank name error = %v, want ErrEmptySearchName", err)
	}
}

// TestClient_UpdateSavedSearch verifies only what the caller named is sent — this
// endpoint genuinely merges, so a read before the write would be wasted.
func TestClient_UpdateSavedSearch(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotBody map[string]json.RawMessage
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"sav01","name":"Jezera","params":{"q":"jezero"}}`))
	})

	name := "Jezera"
	if _, err := client.UpdateSavedSearch(t.Context(), "sav01", &name, nil); err != nil {
		t.Fatalf("UpdateSavedSearch returned %v", err)
	}
	if len(methods) != 1 || methods[0] != http.MethodPatch {
		t.Fatalf("requests = %v, want one PATCH and no read first", methods)
	}
	if _, sent := gotBody["params"]; sent {
		t.Errorf("body = %v, want no params: an omitted field is left alone server-side", gotBody)
	}
}

// TestClient_UpdateSavedSearch_invalid verifies an update that names nothing and
// one that would blank the name are both refused locally.
func TestClient_UpdateSavedSearch_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if _, err := client.UpdateSavedSearch(t.Context(), "sav01", nil, nil); !errors.Is(err, ErrNoSearchEdits) {
		t.Errorf("empty update error = %v, want ErrNoSearchEdits", err)
	}
	blank := " "
	_, err := client.UpdateSavedSearch(t.Context(), "sav01", &blank, nil)
	if !errors.Is(err, ErrEmptySearchName) {
		t.Errorf("blank name error = %v, want ErrEmptySearchName", err)
	}
}

// TestClient_savedSearch_notYours verifies the 404 the server answers for both a
// missing search and somebody else's becomes a message naming both readings —
// never a bare status the caller has to guess at.
func TestClient_savedSearch_notYours(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"saved search not found"}`))
	})

	var notYours *NotYoursError
	if _, err := client.GetSavedSearch(t.Context(), "sav99"); !errors.As(err, &notYours) {
		t.Fatalf("GetSavedSearch error = %v, want *NotYoursError", err)
	}
	if !strings.Contains(notYours.Error(), "not yours") || !strings.Contains(notYours.Error(), "sav99") {
		t.Errorf("message = %q, want it to name the search and say it is not yours", notYours.Error())
	}
	if err := client.DeleteSavedSearch(t.Context(), "sav99"); !errors.As(err, &notYours) {
		t.Errorf("DeleteSavedSearch error = %v, want *NotYoursError", err)
	}
}

// TestClient_savedSearch_otherStatus verifies only the 404 is rewritten; every
// other failure keeps the server's own explanation.
func TestClient_savedSearch_otherStatus(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"listing saved searches failed"}`))
	})
	var status *StatusError
	if _, err := client.GetSavedSearch(t.Context(), "sav01"); !errors.As(err, &status) {
		t.Errorf("GetSavedSearch error = %v, want the server's *StatusError", err)
	}
}

// TestParseSearchParams verifies the view is read as the flat string map the web
// app stores, and that anything else is refused rather than saved as a view the
// app cannot open.
func TestParseSearchParams(t *testing.T) {
	t.Parallel()

	params, err := ParseSearchParams(`{"q":"jezero","year":"2024"}`)
	if err != nil {
		t.Fatalf("ParseSearchParams returned %v", err)
	}
	if params["q"] != "jezero" || params["year"] != "2024" {
		t.Errorf("params = %v, want the two keys", params)
	}
	if empty, err := ParseSearchParams("  "); err != nil || len(empty) != 0 {
		t.Errorf("ParseSearchParams(blank) = %v, %v, want an empty view", empty, err)
	}
	for _, raw := range []string{`{"year":2024}`, `["q"]`, `not json`} {
		if _, err := ParseSearchParams(raw); !errors.Is(err, ErrInvalidSearchParams) {
			t.Errorf("ParseSearchParams(%q) error = %v, want ErrInvalidSearchParams", raw, err)
		}
	}
}

// TestParseSearchParam verifies one key=value pair, including a value that
// itself holds an equals sign.
func TestParseSearchParam(t *testing.T) {
	t.Parallel()

	key, value, err := ParseSearchParam("q=title=lake")
	if err != nil || key != "q" || value != "title=lake" {
		t.Errorf("ParseSearchParam = %q, %q, %v, want the value kept whole", key, value, err)
	}
	for _, raw := range []string{"q", "=jezero", " =x"} {
		if _, _, err := ParseSearchParam(raw); !errors.Is(err, ErrInvalidSearchParams) {
			t.Errorf("ParseSearchParam(%q) error = %v, want ErrInvalidSearchParams", raw, err)
		}
	}
}

// TestWriteSavedSearches verifies the list prints the stored view in a stable key
// order, so two listings of the same search read the same.
func TestWriteSavedSearches(t *testing.T) {
	t.Parallel()

	searches, err := DecodeSavedSearches([]byte(savedSearchesBody))
	if err != nil {
		t.Fatalf("DecodeSavedSearches returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteSavedSearches(&buf, searches); err != nil {
		t.Fatalf("WriteSavedSearches returned %v", err)
	}
	if !strings.Contains(buf.String(), "mode=semantic q=jezero") {
		t.Errorf("output %q does not print the view in key order", buf.String())
	}

	buf.Reset()
	if err := WriteSavedSearches(&buf, nil); err != nil {
		t.Fatalf("WriteSavedSearches returned %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no saved searches found" {
		t.Errorf("empty output = %q, want the one-line message", buf.String())
	}
}

// TestWriteSavedSearch verifies the detail spells the stored view out one key per
// row — that is what somebody opens a saved search to find out.
func TestWriteSavedSearch(t *testing.T) {
	t.Parallel()

	searches, err := DecodeSavedSearches([]byte(savedSearchesBody))
	if err != nil {
		t.Fatalf("DecodeSavedSearches returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteSavedSearch(&buf, searches[0]); err != nil {
		t.Fatalf("WriteSavedSearch returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{"sav01", "Léto u vody", "q", "jezero", "mode", "semantic"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

// TestDecodeSavedSearches_invalid verifies malformed JSON surfaces as an error.
func TestDecodeSavedSearches_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeSavedSearches([]byte(`{"saved_searches":`)); err == nil {
		t.Error("DecodeSavedSearches of malformed JSON returned no error")
	}
	if _, err := DecodeSavedSearch([]byte(`nope`)); err == nil {
		t.Error("DecodeSavedSearch of malformed JSON returned no error")
	}
}
