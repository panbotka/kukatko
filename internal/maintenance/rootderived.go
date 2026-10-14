package maintenance

import (
	"context"
	"fmt"
	"slices"

	"github.com/panbotka/kukatko/internal/rekey"
)

// RootDerivedMover finds and moves the thumbnails and streaming segments a
// project's storage move left at the root of the store for photos that already
// live under the project's prefix. It is satisfied by *rekey.Service, which owns
// the moving rules: the bucket listing is read (never a local thumbnail cache),
// a prefixed copy that exists is kept, and no root copy of a photo is deleted
// unless every one of its objects exists under the prefix.
type RootDerivedMover interface {
	// FindRootDerived lists the moved photos whose derived objects are still at
	// the root, each with the moves of those objects.
	FindRootDerived(ctx context.Context) ([]rekey.RootDerivedPhoto, error)
	// MoveRootDerived moves the objects of the photos found under their prefix.
	MoveRootDerived(ctx context.Context, found []rekey.RootDerivedPhoto) (rekey.RootDerivedResult, error)
}

// compile-time assertion that the storage move is what the check drives.
var _ RootDerivedMover = (*rekey.Service)(nil)

// findRootDerived returns the moved photos whose derived objects the store
// still holds at the root, or nothing when the check is not wired.
func (s *Service) findRootDerived(ctx context.Context) ([]rekey.RootDerivedPhoto, error) {
	if s.rootDerived == nil {
		return nil, nil
	}
	found, err := s.rootDerived.FindRootDerived(ctx)
	if err != nil {
		return nil, fmt.Errorf("maintenance: finding derived objects left at the root: %w", err)
	}
	return found, nil
}

// rootDerivedKeys returns the root keys of every object in found, sorted.
func rootDerivedKeys(found []rekey.RootDerivedPhoto) []string {
	var keys []string
	for _, photo := range found {
		for _, move := range photo.Moves {
			keys = append(keys, move.From)
		}
	}
	slices.Sort(keys)
	return keys
}

// repairRootDerived moves the derived objects left at the root under their
// photo's prefix, when that repair is selected. It finds them afresh rather than
// trusting an earlier scan, so a second run is a no-op.
func (s *Service) repairRootDerived(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.RootDerived {
		return nil
	}
	if s.rootDerived == nil {
		return ErrRootDerivedUnavailable
	}
	found, err := s.findRootDerived(ctx)
	if err != nil {
		return err
	}
	moved, err := s.rootDerived.MoveRootDerived(ctx, found)
	res.RootDerivedMoved = moved.Photos
	res.RootDerivedObjects = moved.Objects
	res.RootDerivedFailed = moved.Failed
	if err != nil {
		return fmt.Errorf("maintenance: moving derived objects left at the root: %w", err)
	}
	return nil
}
