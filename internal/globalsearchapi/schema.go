package globalsearchapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/query"
)

// schemaKey is one filter key of the query language as the search palette needs
// it: the canonical key, how its value is parsed, and the words the key accepts
// when it accepts a fixed set of them.
//
// Only the machine-readable part of a key travels: what it *means* is a
// sentence the UI translates (a Czech and an English one), so the server has no
// business holding it. What the server does hold — and what nobody else can
// derive — is which keys exist at all.
type schemaKey struct {
	// Key is the canonical key, lowercase and without the colon.
	Key string `json:"key"`
	// Kind is the wire name of the key's value kind: text, number, date, bool,
	// enum, id, count or duration.
	Kind string `json:"kind"`
	// Values lists every word the key accepts, for the keys whose vocabulary is
	// closed (an enum, or a yes/no). It is absent for the open-ended kinds.
	Values []string `json:"values,omitempty"`
}

// schemaBody is the JSON body of GET /search/schema.
type schemaBody struct {
	// Keys is every canonical filter key, sorted by key. Aliases are absent:
	// they keep parsing, but suggesting two spellings of one filter teaches the
	// longer one for nothing.
	Keys []schemaKey `json:"keys"`
}

// handleSchema returns the query language's filter keys, read straight from the
// parser's registry (query.Keys). The palette completes what a user is typing
// from this list, and reading it from the parser is the point: a list of keys
// typed out in the frontend would keep offering a filter that no longer parses,
// or stop offering one that was just added, and nothing would say so.
//
// The answer is static for the lifetime of the binary — it is compiled in — so
// a client may cache it for as long as it lives.
func (a *API) handleSchema(w http.ResponseWriter, _ *http.Request) {
	infos := query.Keys()
	keys := make([]schemaKey, 0, len(infos))
	for _, info := range infos {
		keys = append(keys, schemaKey{
			Key:    string(info.Key),
			Kind:   info.Kind.String(),
			Values: info.Values,
		})
	}
	writeJSON(w, http.StatusOK, schemaBody{Keys: keys})
}
