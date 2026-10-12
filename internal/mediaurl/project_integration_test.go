//go:build integration

package mediaurl_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
)

// TestIntegration_DecorateOne_readsTheRequestsProject proves DecorateOne takes
// the project from the request principal (what the Require* guards leave in the
// context): a row of that project is addressed, a row of another is not.
func TestIntegration_DecorateOne_readsTheRequestsProject(t *testing.T) {
	t.Parallel()

	ctx := auth.ContextWithProject(t.Context(), auth.User{UID: "usr_a"},
		&project.Project{UID: "pj_a", Slug: "a"}, project.RoleViewer)
	builder := mediaurl.NewBuilder(publishing{prefix: "https://media.example/"})

	own := photos.Photo{UID: "ph1", FileHash: testHash, FilePath: "2024/05/one.jpg", ProjectID: "pj_a"}
	builder.DecorateOne(ctx, &own)
	if own.ThumbURL == "" || own.DownloadURL == "" {
		t.Errorf("own project's photo not decorated: %+v", own)
	}

	foreign := photos.Photo{UID: "ph2", FileHash: testHash, FilePath: "2024/05/two.jpg", ProjectID: "pj_b"}
	builder.DecorateOne(ctx, &foreign)
	if foreign.ThumbURL != "" || foreign.PreviewURL != "" || foreign.DownloadURL != "" {
		t.Errorf("foreign photo decorated: %+v", foreign)
	}
}
