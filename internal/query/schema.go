package query

import (
	"maps"
	"slices"
	"strings"
)

// String returns the wire name of a value kind — the word a client sees in the
// published schema. It is a stable identifier, not a label: the UI translates
// it, so renaming one here is an API change.
func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindNumber:
		return "number"
	case KindDate:
		return "date"
	case KindBool:
		return "bool"
	case KindEnum:
		return "enum"
	case KindID:
		return "id"
	case KindCount:
		return "count"
	case KindDuration:
		return "duration"
	default:
		return "unknown"
	}
}

// boolWords are the two words a client offers for a yes/no value. The parser
// also accepts true/false and 1/0 (see parseBoolValue), but something
// suggesting a value wants one spelling of it, not three.
var boolWords = []string{"yes", "no"}

// KeyInfo describes one filter key of the query language to a client: the
// canonical key the user types before the colon, how its value is parsed, and —
// when the key accepts a fixed vocabulary — the words it accepts.
//
// It exists so nothing outside this package has to keep its own copy of the
// filter registry. A list of keys typed out by hand somewhere else drifts from
// the parser the moment a filter is added, and the drift is invisible: the UI
// keeps offering a key that no longer parses, or stops offering one that does.
type KeyInfo struct {
	// Key is the canonical key, lowercase and without the colon.
	Key Key
	// Kind is how the key's value is parsed.
	Kind Kind
	// Values is the complete set of words the key accepts, for the kinds that
	// have one: the enum's words, or yes/no for a boolean (and for the yes/no
	// form of a count). It is nil for the open-ended kinds — a text, a number, a
	// date or an id cannot be enumerated.
	Values []string
}

// Keys returns every canonical filter key of the query language with its value
// spec, sorted by key. Aliases are deliberately absent: they keep parsing, but
// offering two spellings of one filter teaches the longer one for no gain.
//
// The returned slices are copies, so a caller may sort or trim them without
// touching the registry.
func Keys() []KeyInfo {
	out := make([]KeyInfo, 0, len(specs))
	for key, sp := range specs {
		out = append(out, KeyInfo{Key: key, Kind: sp.kind, Values: fixedValues(sp)})
	}
	slices.SortFunc(out, func(a, b KeyInfo) int {
		return strings.Compare(string(a.Key), string(b.Key))
	})
	return out
}

// fixedValues returns the words a spec's value kind accepts, or nil when the
// kind is open-ended.
func fixedValues(sp spec) []string {
	switch sp.kind {
	case KindEnum:
		return slices.Clone(sp.enum)
	case KindBool, KindCount:
		return slices.Clone(boolWords)
	default:
		return nil
	}
}

// Aliases returns the alternative spellings the parser accepts, mapped to the
// canonical key each resolves to. A returned copy, so a caller cannot edit the
// table the parser reads.
//
// Nothing suggests them — offering two spellings of one filter only teaches the
// longer one — but a client that has to *recognise* a typed filter (to tell
// which ones a query already sets, say) needs them, and reading them from here
// is how it avoids keeping its own list.
func Aliases() map[string]Key {
	out := make(map[string]Key, len(aliases))
	maps.Copy(out, aliases)
	return out
}
