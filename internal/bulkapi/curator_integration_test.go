//go:build integration

package bulkapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/organize"
)

// TestBulk_curatorFilesIntoAlbumsAndLabels proves the headline curator case
// through the real auth stack: a curator selects photos and puts them into an
// album and under a label, and may set its own per-user marks alongside.
func TestBulk_curatorFilesIntoAlbumsAndLabels(t *testing.T) {
	env := newEnv(t, 1000)
	curator, curatorUID := env.login(t, "curator", auth.RoleCurator)
	ctx := t.Context()

	album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Pouť"})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	label, err := env.organize.CreateLabel(ctx, organize.Label{Name: "procession"})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	p1 := env.seedPhoto(t, "cur1")
	p2 := env.seedPhoto(t, "cur2")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{p1, p2},
		"operations": map[string]any{
			"add_to_albums": []string{album.UID},
			"add_labels":    []string{label.UID},
			"set_favorite":  true,
			"set_rating":    4,
			"set_flag":      "pick",
		},
	})
	resp := env.mustDo(t, curator, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("curator bulk status = %d, want 200", resp.StatusCode)
	}
	var result bulk.Result
	decodeBody(t, resp, &result)
	if result.Counts.Updated != 2 {
		t.Fatalf("counts = %+v, want updated=2", result.Counts)
	}
	assertAlbumMembers(t, ctx, env, album.UID, p1, p2)
	assertLabelMembers(t, ctx, env, label.UID, p1, p2)
	fav, err := env.organize.IsFavorite(ctx, curatorUID, p1)
	if err != nil {
		t.Fatalf("IsFavorite: %v", err)
	}
	if !fav {
		t.Errorf("photo not favorited for the curator")
	}
}

// TestBulk_curatorRefusedBeyondCuration proves the field rule: a curator's batch
// that carries any catalogue-metadata change is refused with 403 as a whole —
// even the album membership riding along with it is not applied.
func TestBulk_curatorRefusedBeyondCuration(t *testing.T) {
	env := newEnv(t, 1000)
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	ctx := t.Context()

	album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Mixed"})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	uid := env.seedPhoto(t, "curmix1")

	extras := map[string]any{
		"set_taken_at":    map[string]string{"precision": "year", "value": "1974"},
		"clear_taken_at":  true,
		"set_location":    map[string]float64{"lat": 49.2, "lng": 16.6},
		"clear_location":  true,
		"set_caption":     "Pouť",
		"set_description": "desc",
		"archive":         true,
		"hide":            true,
	}
	for key, value := range extras {
		body, _ := json.Marshal(map[string]any{
			"photo_uids": []string{uid},
			"operations": map[string]any{"add_to_albums": []string{album.UID}, key: value},
		})
		resp := env.mustDo(t, curator, http.MethodPost, "/api/v1/photos/bulk", body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("curator batch with %s = %d, want 403", key, resp.StatusCode)
		}
	}

	members, err := env.organize.ListPhotoUIDs(ctx, album.UID)
	if err != nil {
		t.Fatalf("list album photos: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("album members after refused batches = %v, want none", members)
	}
	photo, err := env.photos.GetByUID(ctx, uid)
	if err != nil {
		t.Fatalf("get photo: %v", err)
	}
	if photo.ArchivedAt != nil || photo.Lat != nil || photo.Title != "" {
		t.Errorf("photo changed by a refused batch: archived=%v lat=%v title=%q",
			photo.ArchivedAt, photo.Lat, photo.Title)
	}
}

// TestBulk_editorUnaffectedByFieldRule proves the field rule binds only a
// curator: an editor's mixed batch goes through.
func TestBulk_editorUnaffectedByFieldRule(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	uid := env.seedPhoto(t, "edmix1")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{uid},
		"operations": map[string]any{
			"set_taken_at": map[string]string{"precision": "year", "value": "1974"},
			"archive":      true,
		},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor mixed batch = %d, want 200", resp.StatusCode)
	}
}

// TestBulkLocationSummary_curatorForbidden proves the location preview stays on
// RequireWrite: it feeds only the bulk location operation, which a curator may
// not use.
func TestBulkLocationSummary_curatorForbidden(t *testing.T) {
	env := newEnv(t, 1000)
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	uid := env.seedPhoto(t, "cursum1")

	body, _ := json.Marshal(map[string]any{"photo_uids": []string{uid}})
	resp := env.mustDo(t, curator, http.MethodPost, "/api/v1/photos/bulk/location-summary", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("curator summary status = %d, want 403", resp.StatusCode)
	}
}
