package query

import (
	"slices"
	"sort"
	"testing"
)

// TestKeysCoversRegistry checks that the published schema is the filter
// registry itself: every key the parser recognises is offered, nothing else is,
// and the order is stable. A client builds its suggestions from this, so a key
// missing here is a filter the user can never be told about.
func TestKeysCoversRegistry(t *testing.T) {
	t.Parallel()

	infos := Keys()
	if len(infos) != len(specs) {
		t.Fatalf("Keys() returned %d keys, registry has %d", len(infos), len(specs))
	}

	seen := make(map[Key]bool, len(infos))
	for _, info := range infos {
		sp, ok := specs[info.Key]
		if !ok {
			t.Errorf("Keys() offers %q, which the parser does not recognise", info.Key)
			continue
		}
		if seen[info.Key] {
			t.Errorf("Keys() offers %q twice", info.Key)
		}
		seen[info.Key] = true
		if info.Kind != sp.kind {
			t.Errorf("key %q: kind %v, want %v", info.Key, info.Kind, sp.kind)
		}
	}
	for key := range specs {
		if !seen[key] {
			t.Errorf("Keys() does not offer the recognised key %q", key)
		}
	}

	if !sort.SliceIsSorted(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key }) {
		t.Error("Keys() is not sorted by key")
	}
}

// TestKeysOfferedValuesParse checks that every word the schema offers for a key
// actually parses as that key's value. Offering a value the parser then rejects
// would turn a suggestion into a broken query.
func TestKeysOfferedValuesParse(t *testing.T) {
	t.Parallel()

	for _, info := range Keys() {
		for _, value := range info.Values {
			input := string(info.Key) + ":" + value
			q := Parse(input)
			if len(q.Unknown) != 0 || len(q.Filters) != 1 {
				t.Errorf("%q did not parse as a filter: %+v", input, q)
			}
		}
	}
}

// TestKeysValuesOnlyForClosedKinds checks which kinds come with a vocabulary:
// the enums and the yes/no kinds have one, the open-ended kinds must not
// pretend to.
func TestKeysValuesOnlyForClosedKinds(t *testing.T) {
	t.Parallel()

	for _, info := range Keys() {
		switch info.Kind {
		case KindEnum:
			if !slices.Equal(info.Values, specs[info.Key].enum) {
				t.Errorf("key %q: values %v, want the enum %v", info.Key, info.Values, specs[info.Key].enum)
			}
		case KindBool, KindCount:
			if !slices.Equal(info.Values, boolWords) {
				t.Errorf("key %q: values %v, want %v", info.Key, info.Values, boolWords)
			}
		default:
			if info.Values != nil {
				t.Errorf("key %q of kind %v offers values %v", info.Key, info.Kind, info.Values)
			}
		}
	}
}

// TestKeysReturnsCopies checks that a caller mutating the returned slices
// cannot corrupt the registry behind them.
func TestKeysReturnsCopies(t *testing.T) {
	t.Parallel()

	for _, info := range Keys() {
		if len(info.Values) > 0 {
			info.Values[0] = "mutated"
		}
	}
	for _, info := range Keys() {
		if len(info.Values) > 0 && info.Values[0] == "mutated" {
			t.Fatalf("key %q: Keys() handed out the registry's own slice", info.Key)
		}
	}
}

// TestKindNames checks that every kind has its own wire name, since a client
// switches on those names to decide how to complete a value.
func TestKindNames(t *testing.T) {
	t.Parallel()

	kinds := []Kind{
		KindText, KindNumber, KindDate, KindBool,
		KindEnum, KindID, KindCount, KindDuration,
	}
	seen := make(map[string]Kind, len(kinds))
	for _, kind := range kinds {
		name := kind.String()
		if name == "unknown" {
			t.Errorf("kind %d has no wire name", kind)
		}
		if other, dup := seen[name]; dup {
			t.Errorf("kinds %d and %d share the wire name %q", other, kind, name)
		}
		seen[name] = kind
	}
}
