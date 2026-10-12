package mediaurl

import (
	"context"

	"github.com/panbotka/kukatko/internal/photos"
)

// DecorateForProject exposes decorateForProject to the external test package,
// which has no way to put a project principal into a context without the auth
// stack: it decorates photo as DecorateOne would on a request whose project is
// current.
func (b *Builder) DecorateForProject(ctx context.Context, current string, photo *photos.Photo) {
	b.decorateForProject(ctx, current, photo)
}
