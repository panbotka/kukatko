package expand

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// otherProject is a project the harness collections do not belong to.
const otherProject = "pr_other"

// TestService_noProject checks both searches refuse an empty project before
// touching any store.
func TestService_noProject(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.album("al", "src")
	h.label("lb", "src")
	if _, err := h.svc.Album(context.Background(), "", "al", Request{}); !errors.Is(err, ErrNoProject) {
		t.Errorf("Album without a project = %v, want ErrNoProject", err)
	}
	if _, err := h.svc.Label(context.Background(), "", "lb", Request{}); !errors.Is(err, ErrNoProject) {
		t.Errorf("Label without a project = %v, want ErrNoProject", err)
	}
	if len(h.vectors.searched) != 0 {
		t.Errorf("a refused search still ran %d kNN queries", len(h.vectors.searched))
	}
}

// TestService_foreignCollectionIsNotFound checks an album or a label of another
// project answers the same sentinel as a missing one.
func TestService_foreignCollectionIsNotFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addSource("src", oneHot(0), nil)
	h.album("al", "src")
	h.label("lb", "src")
	if _, err := h.svc.Album(context.Background(), otherProject, "al", Request{}); !errors.Is(
		err, organize.ErrAlbumNotFound) {
		t.Errorf("Album of another project = %v, want ErrAlbumNotFound", err)
	}
	if _, err := h.svc.Label(context.Background(), otherProject, "lb", Request{}); !errors.Is(
		err, organize.ErrLabelNotFound) {
		t.Errorf("Label of another project = %v, want ErrLabelNotFound", err)
	}
}

// TestService_searchesAndReturnsOnlyTheProject checks every per-member kNN is
// asked about the request's project, and that a neighbour of another project —
// should a store ever hand one back — is dropped at hydration instead of being
// returned.
func TestService_searchesAndReturnsOnlyTheProject(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.addPhoto("own")
	h.photos.byUID["foreign"] = photos.Photo{UID: "foreign", ProjectID: otherProject, FileHash: "foreignhash"}
	h.addSource("s1", oneHot(0), []vectors.Match{match("own", 0.2), match("foreign", 0.1)})
	h.addSource("s2", oneHot(1), []vectors.Match{match("own", 0.2), match("foreign", 0.1)})
	h.label("lb", "s1", "s2")

	res, err := h.svc.Label(context.Background(), testProject, "lb", Request{})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != "own" {
		t.Errorf("candidates = %+v, want only the project's photo 'own'", res.Candidates)
	}
	if len(h.vectors.searched) != 2 {
		t.Fatalf("kNN queries = %d, want one per embedded member (2)", len(h.vectors.searched))
	}
	for _, searched := range h.vectors.searched {
		if searched != testProject {
			t.Errorf("a kNN searched project %q, want %q", searched, testProject)
		}
	}
}
