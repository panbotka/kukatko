package photoapi

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/phototask"
)

// TaskLookup is the subset of the task store the photo detail needs: the open
// questions a photograph is part of. It is an interface so photoapi depends on
// the behaviour rather than on phototask's construction, and so an instance
// without the work queue wired simply passes nil.
type TaskLookup interface {
	// OpenForPhotos returns the open tasks each photo UID belongs to, keyed by
	// photo UID. Photos in no open task are absent from the map.
	OpenForPhotos(ctx context.Context, photoUIDs []string) (map[string][]phototask.Ref, error)
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
	byPhoto, err := a.tasks.OpenForPhotos(ctx, []string{photoUID})
	if err != nil {
		log.Printf("photoapi: reading open tasks of photo %s: %v", photoUID, err)
		return nil
	}
	return byPhoto[photoUID]
}
