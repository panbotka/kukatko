package vectors

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

// The project scope of the similarity searches.
//
// embeddings and faces each carry a denormalised copy of their photo's project
// (migration 0102), so every kNN query — similar photos, face identity
// suggestions, a face's neighbours, the unassigned-face search behind exemplar
// lookups — filters project_id = $n inside the very statement the HNSW index
// serves, without joining photos. A search takes the project uid as its first
// argument after the context and never defaults it: an empty uid is ErrNoProject.
//
// A WHERE filter on an HNSW scan normally shrinks the result: the index hands
// out its first ef_search candidates and the filter throws away every one of
// another project, so a small project next to a big one would see almost no
// neighbours. pgvector 0.8 added the iterative index scan, which keeps walking
// the graph until LIMIT rows pass the filter. The searches enable it per query
// (SET LOCAL hnsw.iterative_scan = relaxed_order) when the installed extension
// supports it — probed once per process, see iterativeScanSupported — and read
// the nearest rows through a MATERIALIZED CTE re-sorted by distance, because
// relaxed_order may return them slightly out of order. The distance cutoff is
// applied outside that CTE: a distance predicate inside it is invisible to the
// index scan and would make the iterative scan walk to hnsw.max_scan_tuples (see
// docs/PERF.md).

// iterativeScanStmt enables pgvector's iterative HNSW index scan for the current
// transaction. With it, a WHERE-filtered similarity search keeps visiting graph
// neighbours until LIMIT rows pass the filter instead of stopping at the first
// ef_search candidates. relaxed_order trades exact ordering inside the scan for
// speed and recall; every query that runs under it re-sorts its rows by distance.
// SET LOCAL scopes it to the current transaction only.
const iterativeScanStmt = "SET LOCAL hnsw.iterative_scan = relaxed_order"

// iterativeScanMinMajor and iterativeScanMinMinor are the first pgvector version
// with hnsw.iterative_scan (0.8.0).
const (
	iterativeScanMinMajor = 0
	iterativeScanMinMinor = 8
)

// iterativeProbe caches, for the whole process, whether the installed pgvector
// supports the iterative scan. The extension does not change under a running
// server, so the answer is read once; a failed probe is not cached and is
// retried by the next search.
var iterativeProbe struct {
	mu        sync.Mutex
	known     bool
	supported bool
}

// requireProject returns ErrNoProject when projectUID is empty and nil otherwise.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// iterativeScanSupported reports whether the installed pgvector extension
// supports hnsw.iterative_scan, reading pg_extension on the first call only.
func (s *Store) iterativeScanSupported(ctx context.Context) (bool, error) {
	iterativeProbe.mu.Lock()
	defer iterativeProbe.mu.Unlock()
	if iterativeProbe.known {
		return iterativeProbe.supported, nil
	}
	var version string
	err := s.pool.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname = 'vector'").Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		version = ""
	} else if err != nil {
		return false, fmt.Errorf("reading the pgvector version: %w", err)
	}
	iterativeProbe.supported = supportsIterativeScan(version)
	iterativeProbe.known = true
	return iterativeProbe.supported, nil
}

// supportsIterativeScan reports whether the pgvector extension version (as in
// pg_extension.extversion, e.g. "0.8.1") is at least 0.8.0. An unparsable or
// empty version is treated as unsupported: the searches then run without the
// iterative scan, which is correct, merely less complete for a small project.
func supportsIterativeScan(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	if major != iterativeScanMinMajor {
		return major > iterativeScanMinMajor
	}
	return minor >= iterativeScanMinMinor
}

// withProjectReadTx runs fn inside a read-only transaction tuned for a
// project-filtered similarity search: hnsw.ef_search always, and the iterative
// index scan whenever the installed pgvector supports it.
func (s *Store) withProjectReadTx(ctx context.Context, fn func(pgx.Tx) error) error {
	iterative, err := s.iterativeScanSupported(ctx)
	if err != nil {
		return err
	}
	return s.withTunedReadTx(ctx, iterative, fn)
}
