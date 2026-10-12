package vectors

import (
	"strings"
	"testing"
)

// TestSupportsIterativeScan pins the version gate of the iterative HNSW scan:
// pgvector 0.8.0 introduced hnsw.iterative_scan, so anything older — and any
// version string the probe cannot read — runs the searches without it.
func TestSupportsIterativeScan(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"0.8.1":  true,
		"0.8.0":  true,
		"0.10.0": true,
		"1.0.0":  true,
		"1.0":    true,
		"0.7.4":  false,
		"0.5.1":  false,
		"":       false,
		"0":      false,
		"x.8.0":  false,
		"0.y":    false,
	}
	for version, want := range cases {
		if got := supportsIterativeScan(version); got != want {
			t.Errorf("supportsIterativeScan(%q) = %v, want %v", version, got, want)
		}
	}
}

// TestIterativeScanStmt verifies the statement every project-filtered search
// runs under is transaction-scoped and asks for the relaxed order the queries
// re-sort after.
func TestIterativeScanStmt(t *testing.T) {
	t.Parallel()

	if !strings.HasPrefix(iterativeScanStmt, "SET LOCAL ") {
		t.Errorf("iterativeScanStmt = %q, want a SET LOCAL statement", iterativeScanStmt)
	}
	if !strings.HasSuffix(iterativeScanStmt, "relaxed_order") {
		t.Errorf("iterativeScanStmt = %q, want relaxed_order", iterativeScanStmt)
	}
}

// TestKNNQueriesFilterTheProjectInsideTheScan guards the shape every kNN query
// shares: the project predicate sits inside the MATERIALIZED CTE the HNSW index
// serves, and the outer query re-sorts by distance (the relaxed iterative scan
// may return rows slightly out of order).
func TestKNNQueriesFilterTheProjectInsideTheScan(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"findSimilarSQL":                         findSimilarSQL,
		"findSimilarFacesSQL":                    findSimilarFacesSQL,
		"findSimilarFaceCandidatesSQL":           findSimilarFaceCandidatesSQL,
		"findSimilarUnassignedFaceCandidatesSQL": findSimilarUnassignedFaceCandidatesSQL,
	}
	for name, query := range queries {
		cte, outer, ok := strings.Cut(query, "\n)\n")
		if !ok || !strings.Contains(cte, "AS MATERIALIZED (") {
			t.Errorf("%s: want a MATERIALIZED CTE, got %q", name, query)
			continue
		}
		if !strings.Contains(cte, "project_id = $") {
			t.Errorf("%s: the project predicate must be inside the CTE: %q", name, cte)
		}
		if !strings.Contains(cte, "ORDER BY embedding <=> $1") {
			t.Errorf("%s: the CTE must order by the indexed distance: %q", name, cte)
		}
		if !strings.Contains(outer, "ORDER BY distance") {
			t.Errorf("%s: the outer query must re-sort by distance: %q", name, outer)
		}
		if strings.Contains(cte, "<= $2") {
			t.Errorf("%s: a distance cutoff inside the CTE blinds the iterative scan: %q", name, cte)
		}
	}
}
