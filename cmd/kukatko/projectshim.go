package main

import (
	"context"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The request-project shim (multi-tenancy transition).
//
// internal/vectors' similarity searches take the project uid explicitly. The
// one consumer that still speaks the old, project-less signature — expand, the
// "similar photos for an album or label" search, scoped together with albums and
// labels in a later step — reaches them through the adapter below, which reads
// the project of the request the call is serving (auth.ProjectUIDFromContext), so
// the search already runs inside the caller's project instead of across the
// instance. A call without a request project fails with the store's
// ErrNoProject rather than reading everything.
//
// The adapter disappears when expand is scoped and passes the project itself;
// nothing new may be wired through it. It returns the store's errors unwrapped
// on purpose: the consumer branches on the store's sentinels exactly as it did
// when it held the store itself.

// reqProject returns the project of the request ctx is serving.
func reqProject(ctx context.Context) string {
	return auth.ProjectUIDFromContext(ctx)
}

// requestProjectVectors adapts *vectors.Store to the project-less similarity
// search of expand.
type requestProjectVectors struct {
	*vectors.Store
}

// FindSimilar searches the request's project.
func (v requestProjectVectors) FindSimilar(
	ctx context.Context, vec []float32, limit int, maxDistance float64,
) ([]vectors.Match, error) {
	return v.Store.FindSimilar(ctx, reqProject(ctx), vec, limit, maxDistance) //nolint:wrapcheck // transparent
}
