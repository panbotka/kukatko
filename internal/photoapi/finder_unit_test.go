package photoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// anyPhoto is a photoFinder that finds every uid in every project: the stand-in
// for the scoped lookup in a unit test that drives a gated route without a
// database and is about what the route does after the gate.
type anyPhoto struct{}

// GetByUIDInProject returns a bare photo carrying uid.
func (anyPhoto) GetByUIDInProject(_ context.Context, projectUID, uid string) (photos.Photo, error) {
	return photos.Photo{UID: uid, ProjectID: projectUID}, nil
}

// noPhoto is a photoFinder for which every uid is a photo of another project
// (or of none): the store's answer for both is ErrPhotoNotFound.
type noPhoto struct{}

// GetByUIDInProject always answers ErrPhotoNotFound.
func (noPhoto) GetByUIDInProject(context.Context, string, string) (photos.Photo, error) {
	return photos.Photo{}, photos.ErrPhotoNotFound
}

// withFinder returns api with the anyPhoto finder unless the test chose one,
// for the unit routers of gated handlers.
func withFinder(api *API) *API {
	if api.finder == nil {
		api.finder = anyPhoto{}
	}
	return api
}

// TestPhotoInProject_foreignPhotoNeverReachesTheService proves the gate runs
// first: with the photo outside the request's project the storyboard routes
// answer the missing photo's 404 and the service behind them is never asked,
// so no sprite byte is read for a photo of another project.
func TestPhotoInProject_foreignPhotoNeverReachesTheService(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/photos/ph_1/storyboard", "/photos/ph_1/storyboard/sprite"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			fake := &fakeStoryboards{status: storyboardjob.Status{State: storyboardjob.StateReady}}
			rec := httptest.NewRecorder()
			storyboardRouter(&API{storyboards: fake, finder: noPhoto{}}).
				ServeHTTP(rec, req(http.MethodGet, path))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
			if fake.statusUID != "" || fake.openCalls != 0 {
				t.Errorf("the service was asked about a photo outside the project: %+v", fake)
			}
		})
	}
}
