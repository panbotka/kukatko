package photoapi

import (
	"context"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/phototask"
)

// TaskLookup is the subset of the task store the photo API needs: the open
// questions a photograph is part of, and whether the tasks a listing is scoped
// to exist. It is an interface so photoapi depends on the behaviour rather than
// on phototask's construction, and so an instance without the work queue wired
// simply passes nil. Both take the request's project: a task of another project
// is never named and never scopes a listing.
type TaskLookup interface {
	// OpenForPhotos returns the open tasks of the project each photo UID
	// belongs to, keyed by photo UID. Photos in no open task are absent from
	// the map.
	OpenForPhotos(ctx context.Context, projectUID string, photoUIDs []string) (map[string][]phototask.Ref, error)
	// InProject reports whether every task UID names a task of the project.
	InProject(ctx context.Context, projectUID string, taskUIDs []string) (bool, error)
}

// resolveTasks returns the open tasks the photo is part of, or nil when there
// are none, no lookup is wired, or the lookup failed.
//
// A failure is logged and swallowed on purpose. The chip is an extra on a page
// that already answers half a dozen queries; losing it costs a person one route
// to a question they can still reach from the task listing, whereas failing the
// detail would cost them the photograph.
func (a *API) resolveTasks(ctx context.Context, photoUID string) []phototask.Ref {
	if a.tasks == nil {
		return nil
	}
	byPhoto, err := a.tasks.OpenForPhotos(ctx, auth.ProjectUIDFromContext(ctx), []string{photoUID})
	if err != nil {
		log.Printf("photoapi: reading open tasks of photo %s: %v", photoUID, err)
		return nil
	}
	return byPhoto[photoUID]
}

// requireTaskScope answers 404 "task not found" — and reports false — when the
// listing is scoped to a task (task=) that is not one of the request's project:
// a missing task and another project's are the same answer, so GET /photos?task=
// cannot be used to probe for tasks elsewhere, nor to tell them from a task
// whose frozen group is merely empty. An unscoped listing, or an instance
// without the work queue, passes.
func (a *API) requireTaskScope(w http.ResponseWriter, r *http.Request, params photos.ListParams) bool {
	if len(params.TaskUIDs) == 0 || a.tasks == nil {
		return true
	}
	ok, err := a.tasks.InProject(r.Context(), auth.ProjectUIDFromContext(r.Context()), params.TaskUIDs)
	switch {
	case err != nil:
		log.Printf("photoapi: checking the tasks of a listing: %v", err)
		writeError(w, http.StatusInternalServerError, "listing photos failed")
		return false
	case !ok:
		writeError(w, http.StatusNotFound, "task not found")
		return false
	}
	return true
}
