package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/vectors"
)

// stubSearcher satisfies FaceSearcher without a database, for wiring tests.
type stubSearcher struct{}

// FindSimilarFaces returns no neighbours.
func (stubSearcher) FindSimilarFaces(
	context.Context, []float32, int, float64,
) ([]vectors.FaceMatch, error) {
	return nil, nil
}

// FindSimilarFaceCandidates returns no candidates.
func (stubSearcher) FindSimilarFaceCandidates(
	context.Context, []float32, int, float64,
) ([]vectors.FaceCandidate, error) {
	return nil, nil
}

// stubAssigner satisfies FaceAssigner without a database, for wiring tests.
type stubAssigner struct{}

// Apply returns an empty result.
func (stubAssigner) Apply(
	context.Context, facematch.AssignRequest, audit.Meta,
) (facematch.AssignResult, error) {
	return facematch.AssignResult{}, nil
}

// TestNewAppliesDefaults verifies the optional tunables fall back to their
// package defaults when left zero.
func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()
	svc := New(Config{Store: &Store{}, Faces: stubSearcher{}, Assigner: stubAssigner{}})
	if svc.threshold != DefaultThreshold {
		t.Errorf("threshold = %g, want %g", svc.threshold, DefaultThreshold)
	}
	if svc.minSize != DefaultMinSize {
		t.Errorf("minSize = %d, want %d", svc.minSize, DefaultMinSize)
	}
	if svc.suggestionMaxDistance != DefaultSuggestionMaxDistance {
		t.Errorf("suggestionMaxDistance = %g, want %g", svc.suggestionMaxDistance, DefaultSuggestionMaxDistance)
	}
}

// TestNewKeepsOverrides verifies positive Config values override the defaults.
func TestNewKeepsOverrides(t *testing.T) {
	t.Parallel()
	svc := New(Config{
		Store: &Store{}, Faces: stubSearcher{}, Assigner: stubAssigner{},
		Threshold: 0.3, MinSize: 5, SuggestionMaxDistance: 0.6,
	})
	if svc.threshold != 0.3 || svc.minSize != 5 || svc.suggestionMaxDistance != 0.6 {
		t.Errorf("overrides not applied: %+v", svc)
	}
}

// TestNewPanicsOnMissingDependency verifies a nil required collaborator is a
// startup panic, not a per-request nil dereference.
func TestNewPanicsOnMissingDependency(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New did not panic on missing dependencies")
		}
	}()
	New(Config{})
}

// TestNewClusterUID checks the generated uid has the right prefix and length.
func TestNewClusterUID(t *testing.T) {
	t.Parallel()
	uid, err := newClusterUID()
	if err != nil {
		t.Fatalf("newClusterUID: %v", err)
	}
	if !strings.HasPrefix(uid, clusterUIDPrefix) {
		t.Errorf("uid %q missing prefix %q", uid, clusterUIDPrefix)
	}
	if len(uid) != len(clusterUIDPrefix)+uidSuffixLen {
		t.Errorf("uid %q length = %d, want %d", uid, len(uid), len(clusterUIDPrefix)+uidSuffixLen)
	}
}
