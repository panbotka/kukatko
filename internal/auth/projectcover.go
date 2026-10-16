package auth

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/project"
)

// ProjectCovers finds the picture that stands for a project on the project
// chooser (GET /auth/me's cover_thumb_url). The auth package owns neither the
// catalogue nor the media addresses, so cmd/kukatko wires an implementation over
// internal/organize and internal/mediaurl.
type ProjectCovers interface {
	// CoverThumbURL returns where the reader, a member with role of the project
	// projectUID (addressed as slug), fetches the thumbnail of the project's
	// cover, or "" when the project has no photo to offer. It is asked only
	// about a project the reader may enter.
	CoverThumbURL(ctx context.Context, projectUID, slug string, role project.Role) (string, error)
}

// coverThumbURL returns the cover of the membership ms for GET /auth/me: the
// address ProjectCovers names for an enterable membership — approved, in an
// active project — and "" for every other one, or when no covers are wired. A
// project the reader may not enter never yields a photo of it. A lookup that
// fails is logged by the caller and drops the picture, never the
// response: the chooser draws a placeholder instead.
func (a *API) coverThumbURL(ctx context.Context, ms project.MembershipSummary) (string, error) {
	if a.covers == nil || !ms.Active || !ms.Approved {
		return "", nil
	}
	url, err := a.covers.CoverThumbURL(ctx, ms.ProjectUID, ms.Slug, ms.Role)
	if err != nil {
		return "", fmt.Errorf("auth: the cover of project %s: %w", ms.ProjectUID, err)
	}
	return url, nil
}
