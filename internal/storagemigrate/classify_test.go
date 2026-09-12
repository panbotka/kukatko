package storagemigrate

import (
	"testing"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// TestLivesLocally_decidesEveryKind is the regression guard against a new prefix
// appearing in the store that this migration never learned about: every kind of
// object the store can hold must say here whether emptying the local disk means
// emptying it too. A new kind fails this test — and the exhaustive linter over
// livesLocally and planKind — until someone decides.
func TestLivesLocally_decidesEveryKind(t *testing.T) {
	t.Parallel()

	want := map[storekeys.Kind]bool{
		storekeys.KindOriginal:  true,
		storekeys.KindSidecar:   true,
		storekeys.KindFamilies:  true,
		storekeys.KindHLS:       true,
		storekeys.KindThumbnail: false,
		storekeys.KindDump:      false,
		storekeys.KindPartial:   false,
		storekeys.KindForeign:   false,
	}
	kinds := storekeys.Kinds()
	if len(want) != len(kinds) {
		t.Fatalf("the store holds %d kinds but %d are decided here: decide the new one",
			len(kinds), len(want))
	}
	for _, kind := range kinds {
		expected, ok := want[kind]
		if !ok {
			t.Fatalf("kind %v has no local-deletion verdict", kind)
		}
		if got := livesLocally(kind); got != expected {
			t.Errorf("livesLocally(%v) = %v, want %v", kind, got, expected)
		}
	}
}
