//go:build integration

package photoapi_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestHideFromLibrary drives the whole feature over HTTP: the toggle, what it
// removes from the library surfaces, what it deliberately leaves alone, and the
// documented way back.
func TestHideFromLibrary(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL
	ctx := t.Context()

	keep := env.seedPhoto(t, photos.Photo{Title: "Keep", TakenAtSource: "unknown"}, "keep.jpg", 5, 5, 200)
	scan := env.seedPhoto(t, photos.Photo{Title: "Scan", TakenAtSource: "unknown"}, "scan.jpg", 200, 5, 5)

	album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Documents"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if err = env.organize.AddPhoto(ctx, album.UID, scan.UID); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}
	label, err := env.organize.CreateLabel(ctx, organize.Label{Name: "Scans"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err = env.organize.AttachLabel(ctx, scan.UID, label.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}

	t.Run("viewer cannot hide", func(t *testing.T) {
		resp := mustDo(t, viewer, http.MethodPost, base+"/api/v1/photos/"+scan.UID+"/hide", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer hide status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("editor hides and it leaves the library", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPost, base+"/api/v1/photos/"+scan.UID+"/hide", nil)
		if !detail.HiddenFromLibrary {
			t.Errorf("hide response hidden_from_library = false, want true")
		}

		list := getList(t, editor, base, "")
		if list.Total != 1 || list.Photos[0].UID != keep.UID {
			t.Errorf("library list = %v, want only [%s]", uids(list.Photos), keep.UID)
		}
		years := getYears(t, editor, base, "")
		if years.Total != 1 {
			t.Errorf("years total = %d, want 1 — the timeline must agree with the grid", years.Total)
		}
	})

	t.Run("but stays visible where it was filed", func(t *testing.T) {
		inAlbum := getList(t, editor, base, "album="+album.UID)
		if inAlbum.Total != 1 || inAlbum.Photos[0].UID != scan.UID {
			t.Errorf("album list = %v, want [%s]", uids(inAlbum.Photos), scan.UID)
		}
		onLabel := getList(t, editor, base, "label="+label.UID)
		if onLabel.Total != 1 || onLabel.Photos[0].UID != scan.UID {
			t.Errorf("label list = %v, want [%s]", uids(onLabel.Photos), scan.UID)
		}
	})

	t.Run("and is reachable by its own url", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodGet, base+"/api/v1/photos/"+scan.UID, nil)
		if detail.UID != scan.UID || !detail.HiddenFromLibrary {
			t.Errorf("detail = %+v, want the hidden photo", detail)
		}
	})

	t.Run("hidden:yes finds it and nothing else", func(t *testing.T) {
		found := getList(t, editor, base, "q=hidden%3Ayes")
		if found.Total != 1 || found.Photos[0].UID != scan.UID {
			t.Errorf("q=hidden:yes = %v, want [%s]", uids(found.Photos), scan.UID)
		}
	})

	t.Run("unhide brings it back", func(t *testing.T) {
		detail, _ := decodeDetail(t, editor, http.MethodPost, base+"/api/v1/photos/"+scan.UID+"/unhide", nil)
		if detail.HiddenFromLibrary {
			t.Error("unhide response hidden_from_library = true, want false")
		}
		list := getList(t, editor, base, "")
		if list.Total != 2 {
			t.Errorf("library total after unhide = %d, want 2", list.Total)
		}
	})

	t.Run("a missing photo is a 404", func(t *testing.T) {
		resp := mustDo(t, editor, http.MethodPost, base+"/api/v1/photos/nope/hide", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("hide missing photo status = %d, want 404", resp.StatusCode)
		}
	})
}
