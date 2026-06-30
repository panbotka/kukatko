package vectors

import (
	"strconv"
	"strings"
	"testing"
)

// TestEfSearchWithinBound verifies the HNSW ef_search the design pins read
// queries to stays in the allowed range: positive and strictly below the
// efSearchMax ceiling the performance design forbids reaching. This guards the
// requirement that ef_search is set to 100 and never near 400.
func TestEfSearchWithinBound(t *testing.T) {
	t.Parallel()

	if efSearch <= 0 {
		t.Fatalf("efSearch = %d, want positive", efSearch)
	}
	if efSearch >= efSearchMax {
		t.Fatalf("efSearch = %d, want < efSearchMax (%d)", efSearch, efSearchMax)
	}
	if efSearch != 100 {
		t.Errorf("efSearch = %d, want 100 (the documented design value)", efSearch)
	}
}

// TestEfSearchStmt verifies the SET LOCAL statement applied to every read
// transaction encodes efSearch and scopes the change to the transaction (SET
// LOCAL), so the tuning never leaks onto a pooled connection.
func TestEfSearchStmt(t *testing.T) {
	t.Parallel()

	want := "SET LOCAL hnsw.ef_search = " + strconv.Itoa(efSearch)
	if efSearchStmt != want {
		t.Errorf("efSearchStmt = %q, want %q", efSearchStmt, want)
	}
	if !strings.HasPrefix(efSearchStmt, "SET LOCAL ") {
		t.Errorf("efSearchStmt = %q, want a SET LOCAL statement (transaction-scoped)", efSearchStmt)
	}
}
