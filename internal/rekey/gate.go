package rekey

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Gate answers, for the handlers of the jobs that publish a photo's derived
// objects (thumbnail, hls_transcode, sidecar), whether a storage move is about
// to take the photo. It is how the move and those jobs stay out of each other's
// way: the move snapshots a photo's derived objects when it steps it, and a job
// that published under the root while the move was copying them would leave
// its fresh objects behind at a key the moved row no longer resolves to. So a
// handler asks first and defers the job while the answer is yes; once the move
// has taken the photo, the row points under the new prefix and the job publishes
// there.
//
// The direction is deliberate — the handlers wait for the move, not the move for
// the handlers: the move walks the photos in file_path order behind a cursor, and
// a photo it skipped because a job was busy with it would be left behind the
// cursor until a Retry, while a deferred job simply runs a little later.
//
// AwaitingMove is the coarse half; Publish and Purge (lock.go) are the fine
// one, keeping the move off one photo while a job publishes for it or the trash
// purges it.
type Gate struct {
	pool *pgxpool.Pool
}

// NewGate returns a Gate over pool. It panics on a nil pool, a wiring bug.
func NewGate(pool *pgxpool.Pool) *Gate {
	if pool == nil {
		panic("rekey: NewGate needs a pool")
	}
	return &Gate{pool: pool}
}

// awaitingMoveSQL reports whether project $1 has a running move whose target
// prefix the key $2 is not yet under.
const awaitingMoveSQL = `SELECT EXISTS (SELECT 1 FROM storage_rekey_runs
WHERE project_uid = $1 AND state = 'running' AND NOT starts_with($2, target_prefix))`

// AwaitingMove reports whether a running storage move of project projectUID
// has yet to take the photo whose original is stored at filePath — the photo is
// still in the layout the move is emptying. A paused, failed or finished move
// takes nothing, so it answers false for those; the publishing jobs then re-read
// the photo's key right before they publish, which covers a move resumed while
// they run.
func (g *Gate) AwaitingMove(ctx context.Context, projectUID, filePath string) (bool, error) {
	var awaiting bool
	if err := g.pool.QueryRow(ctx, awaitingMoveSQL, projectUID, filePath).Scan(&awaiting); err != nil {
		return false, fmt.Errorf("rekey: checking for a running move of project %s: %w", projectUID, err)
	}
	return awaiting, nil
}

// activeRunsSQL reads every run that is running or paused, oldest first.
const activeRunsSQL = `SELECT ` + runColumns + ` FROM storage_rekey_runs
WHERE state IN ('running', 'paused') ORDER BY started_at, uid`

// ActiveRuns returns every storage move of the instance that is running or
// paused — the moves whose photos may have a committed row while their derived
// objects still wait at the root — oldest first. It is what a repair that
// deletes objects under the root checks before it deletes anything.
func (s *Service) ActiveRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.pool.Query(ctx, activeRunsSQL)
	if err != nil {
		return nil, fmt.Errorf("rekey: listing active runs: %w", err)
	}
	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) {
		return scanRun(row)
	})
	if err != nil {
		return nil, fmt.Errorf("rekey: reading active runs: %w", err)
	}
	return runs, nil
}
