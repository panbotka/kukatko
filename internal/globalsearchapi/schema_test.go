package globalsearchapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/query"
)

// TestSchemaPublishesEveryFilterKey checks that the endpoint hands a client the
// parser's registry verbatim: every recognised key, with its value kind and the
// words it accepts. The search palette completes what a user types from this
// response, so a key missing here is a filter nobody can be told about.
func TestSchemaPublishesEveryFilterKey(t *testing.T) {
	t.Parallel()

	srv := newTestServer(&fakeSearcher{}, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/schema")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body schemaBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := query.Keys()
	if len(body.Keys) != len(want) {
		t.Fatalf("published %d keys, parser knows %d", len(body.Keys), len(want))
	}
	for i, info := range want {
		got := body.Keys[i]
		if got.Key != string(info.Key) {
			t.Errorf("key %d = %q, want %q", i, got.Key, info.Key)
		}
		if got.Kind != info.Kind.String() {
			t.Errorf("key %q: kind %q, want %q", got.Key, got.Kind, info.Kind.String())
		}
		if !slices.Equal(got.Values, info.Values) {
			t.Errorf("key %q: values %v, want %v", got.Key, got.Values, info.Values)
		}
	}
}

// TestSchemaOmitsValuesForOpenKinds checks that a key with no closed vocabulary
// carries no `values` field at all, rather than an empty list: the palette reads
// its absence as "there is nothing to offer here, let the user type".
func TestSchemaOmitsValuesForOpenKinds(t *testing.T) {
	t.Parallel()

	srv := newTestServer(&fakeSearcher{}, 0)
	defer srv.Close()

	resp := getGlobal(t, srv.URL+"/api/v1/search/schema")
	defer resp.Body.Close()

	var raw struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, entry := range raw.Keys {
		key := string(entry["key"])
		kind := string(entry["kind"])
		_, hasValues := entry["values"]
		closed := kind == `"enum"` || kind == `"bool"` || kind == `"count"`
		if closed != hasValues {
			t.Errorf("key %s of kind %s: values present = %v, want %v", key, kind, hasValues, closed)
		}
	}
}

// localeFiles are the shipped translations of the frontend, keyed by language.
var localeFiles = map[string]string{
	"cs": filepath.Join("..", "..", "web", "src", "i18n", "locales", "cs", "common.json"),
	"en": filepath.Join("..", "..", "web", "src", "i18n", "locales", "en", "common.json"),
}

// TestQueryKeysAreAllDescribed checks that every filter key the parser knows has
// a sentence explaining it, in every language the app ships, and that no
// sentence describes a key that no longer exists.
//
// The palette offers the keys the server publishes and names them from these
// translations, so the two lists have to agree. Nothing at runtime would say
// they don't: a key added to the parser would simply be offered under its bare
// name, and a key removed from it would leave a description nobody ever reads.
// This test is what notices instead — adding a filter means adding its sentence
// in the same commit.
func TestQueryKeysAreAllDescribed(t *testing.T) {
	t.Parallel()

	for lang, path := range localeFiles {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			var locale struct {
				SearchCommand struct {
					QueryKeys map[string]string `json:"queryKeys"`
				} `json:"searchCommand"`
			}
			if err := json.Unmarshal(raw, &locale); err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			described := locale.SearchCommand.QueryKeys

			canonical := make(map[string]bool)
			for _, info := range query.Keys() {
				canonical[string(info.Key)] = true
			}

			for _, info := range query.Keys() {
				text, ok := described[string(info.Key)]
				if !ok {
					t.Errorf("filter key %q has no searchCommand.queryKeys entry in %s", info.Key, path)
					continue
				}
				if text == "" {
					t.Errorf("filter key %q has an empty description in %s", info.Key, path)
				}
			}
			for key := range described {
				if !canonical[key] {
					t.Errorf("%s describes %q, which is not a filter key of the query language", path, key)
				}
			}
		})
	}
}

// filterKeysPattern extracts the frontend's own `FILTER_KEYS` literal — the
// array between its declaration and the closing `] as const`.
var filterKeysPattern = regexp.MustCompile(`(?s)export const FILTER_KEYS = \[(.*?)\] as const`)

// quotedPattern extracts the single-quoted strings of that literal.
var quotedPattern = regexp.MustCompile(`'([^']+)'`)

// TestFrontendFilterKeysMatchTheParser checks the one key list the frontend
// still keeps of its own: `FILTER_KEYS` in web/src/lib/queryLanguage.ts, which
// the search page's box uses to recognise and complete filters.
//
// The command palette does not have this problem — it completes from what the
// server publishes (handleSchema) — but this list predates that and is read
// synchronously, so it stays. What it must not do is quietly disagree with the
// parser: a key missing from it is a filter the search box will not complete,
// and one left in it after the parser dropped it is a filter the box offers and
// the server then ignores. Neither shows up at runtime, so the test is the only
// thing that can notice.
//
// Unlike the palette's list, this one includes the aliases: recognising a typed
// `subject:` is exactly what it is for.
func TestFrontendFilterKeysMatchTheParser(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "web", "src", "lib", "queryLanguage.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	literal := filterKeysPattern.FindSubmatch(source)
	if literal == nil {
		t.Fatalf("%s no longer declares FILTER_KEYS as an array literal", path)
	}

	got := make(map[string]bool)
	for _, match := range quotedPattern.FindAllSubmatch(literal[1], -1) {
		got[string(match[1])] = true
	}

	want := make(map[string]bool)
	for _, info := range query.Keys() {
		want[string(info.Key)] = true
	}
	for spelling := range query.Aliases() {
		want[spelling] = true
	}

	for key := range want {
		if !got[key] {
			t.Errorf("%s: FILTER_KEYS is missing the filter key %q", path, key)
		}
	}
	for key := range got {
		if !want[key] {
			t.Errorf("%s: FILTER_KEYS carries %q, which the parser does not recognise", path, key)
		}
	}
}
