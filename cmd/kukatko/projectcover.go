package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/thumb"
)

// projectCovers is the auth.ProjectCovers the project chooser's pictures come
// from: internal/organize picks the photo, internal/mediaurl mints its
// thumbnail address — a signed edge URL, else the project's own thumb route.
type projectCovers struct {
	store *organize.Store
	media *mediaurl.Builder
}

// buildProjectCovers wires projectCovers over the database and the storage
// backend cfg configures. It fails only when that backend cannot be built.
func buildProjectCovers(cfg *config.Config, db *database.DB) (*projectCovers, error) {
	mediaStore, err := newStorage(cfg)
	if err != nil {
		return nil, fmt.Errorf("building the storage for project covers: %w", err)
	}
	return &projectCovers{store: organize.NewStore(db.Pool()), media: mediaurl.NewBuilder(mediaStore)}, nil
}

// CoverThumbURL implements auth.ProjectCovers. A private featured album counts
// only for a curator and up, as on the project's homepage. The own-route
// fallback is addressed through the project's prefix (/api/v1/p/<slug>), whose
// guard admits exactly the members the chooser asks about.
func (c *projectCovers) CoverThumbURL(
	ctx context.Context, projectUID, slug string, role project.Role,
) (string, error) {
	cover, ok, err := c.store.ProjectCover(ctx, projectUID, role.CanCurate())
	if err != nil {
		return "", fmt.Errorf("reading the cover of project %s: %w", projectUID, err)
	}
	if !ok {
		return "", nil
	}
	base := auth.ContextWithAPIBase(ctx, auth.APIBasePath+"/p/"+url.PathEscape(slug))
	return c.media.Thumb(base, cover.PhotoUID, cover.FilePath, cover.FileHash, thumb.GridSize), nil
}
