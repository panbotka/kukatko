package maintenance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// ErrStorageMoveActive indicates a repair that deletes objects at the root of
// the store was refused because a storage move is running or paused. During a
// move a photo's row can already point under its project's prefix while its
// thumbnails and streaming segments still wait at the root to be copied; to a
// sweep reconciling the root against the catalogue those objects look orphaned,
// and deleting them would lose what the move is about to copy. The repair can be
// run again once the move is done (or failed — a failed move's photos that kept
// derived objects at the root are excused below, and are the root-derived
// repair's to move).
var ErrStorageMoveActive = errors.New("maintenance: a storage move is under way")

// MoveWatcher reports the storage moves still under way. It is satisfied by
// *rekey.Service.
type MoveWatcher interface {
	// ActiveRuns returns every storage move that is running or paused.
	ActiveRuns(ctx context.Context) ([]rekey.Run, error)
}

// compile-time assertion that the storage move is what the repair consults.
var _ MoveWatcher = (*rekey.Service)(nil)

// refuseDuringMove returns ErrStorageMoveActive, naming the repair and the
// oldest active move, while any storage move is running or paused; without a
// MoveWatcher it has no objection.
//
// Any move counts, not only one of a particular project: the repairs it guards
// delete under the root layout, and the root is exactly what every move is
// emptying — a project being moved is by definition one whose objects are still
// (partly) there.
func (s *Service) refuseDuringMove(ctx context.Context, repair string) error {
	if s.moves == nil {
		return nil
	}
	runs, err := s.moves.ActiveRuns(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: checking for a storage move: %w", err)
	}
	if len(runs) == 0 {
		return nil
	}
	run := runs[0]
	return fmt.Errorf("%w: %s is refused while the storage move of project %s is %s; "+
		"let the move finish (or fail) and run the repair again", ErrStorageMoveActive, repair, run.ProjectUID, run.State)
}

// movedRootPrefixes returns the hls/<file_hash>/<rendition>/ prefixes that
// recorded renditions of videos already under a project prefix had in the root
// layout. Their objects at the root are not orphans: they are segments a storage
// move has not copied under the prefix yet (a failed copy is recorded for the
// move's retry), and moving them is the root-derived repair's business — the
// orphan sweep keeps them.
func movedRootPrefixes(recorded []hlsjob.Recorded) map[string]struct{} {
	prefixes := make(map[string]struct{})
	for _, rec := range recorded {
		if storekeys.PrefixOf(rec.FilePath) == "" {
			continue
		}
		key, err := hls.Key("", rec.FileHash, rec.Rendition, hls.InitName)
		if err != nil {
			continue
		}
		prefixes[strings.TrimSuffix(key, hls.InitName)] = struct{}{}
	}
	return prefixes
}
