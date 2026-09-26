package family

import "context"

// NetworkWithin is Tree(DirectionNetwork) with a cap the test chooses, so the
// integration tests can walk a component past the cap without seeding
// NetworkLimit people first.
func (s *Store) NetworkWithin(ctx context.Context, rootUID string, limit int) (Tree, error) {
	root, err := getRelative(ctx, s.pool, rootUID)
	if err != nil {
		return Tree{}, err
	}
	return s.network(ctx, root, limit)
}
