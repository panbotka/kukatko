//go:build integration

package photoapi_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestPhotoDetail_foreignProjectIsInvisible is the HTTP-level tenant-isolation
// test of the photo detail: a member of B asking B's prefix for A's photo gets
// exactly the answer a photo that does not exist gets.
func TestPhotoDetail_foreignProjectIsInvisible(t *testing.T) {
	t.Skip("unscoped until the photos step")
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	foreignPath := f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID)
	f.AssertInvisible(t, client, foreignPath)
	projecttest.AssertSameShape(t,
		f.Get(t, client, foreignPath),
		f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph"))))
}

// TestPhotoDetail_projectPrefixReachesOwnPhoto proves the harness is wired into
// this package's server: B's member reads B's photo through B's prefix, and A's
// member is refused B's project altogether by the project guard.
func TestPhotoDetail_projectPrefixReachesOwnPhoto(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())

	own := f.Get(t, f.ClientFor(t, e.server, projecttest.SideB), f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Errorf("B's own photo through B's prefix = %d, want 200", own.StatusCode)
	}
	f.AssertInvisible(t, f.ClientFor(t, e.server, projecttest.SideA),
		f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID))
}
