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

// postMembershipSummary asks the membership summary for uids as client c and
// returns the response.
func (e *env) postMembershipSummary(t *testing.T, c *http.Client, uids []string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"photo_uids": uids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return e.mustDo(t, c, http.MethodPost, "/api/v1/photos/bulk/membership-summary", body)
}

// TestBulkMembershipSummary_countsAndNamesFiledPhotos covers the question the
// upload page asks once a batch has settled with nothing chosen: how many of the
// photos already sit in an album or under a label, and where. A repeated UID is
// one photo and a missing one is none; an album and a label are both "filed",
// and a photo in neither is not.
func TestBulkMembershipSummary_countsAndNamesFiledPhotos(t *testing.T) {
	env := newEnv(t, 1000)
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	ctx := t.Context()

	album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Panorama"})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	label, err := env.organize.CreateLabel(ctx, organize.Label{Name: "beach"})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	inAlbum := env.seedPhoto(t, "mem-album-1")
	inBoth := env.seedPhoto(t, "mem-both-1")
	loose := env.seedPhoto(t, "mem-loose-1")
	for _, uid := range []string{inAlbum, inBoth} {
		if err := env.organize.AddPhoto(ctx, album.UID, uid); err != nil {
			t.Fatalf("add %s to album: %v", uid, err)
		}
	}
	if err := env.organize.AttachLabel(ctx, inBoth, label.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("attach label: %v", err)
	}

	resp := env.postMembershipSummary(t, curator,
		[]string{inAlbum, inBoth, inBoth, loose, "phMISSING0000000000000000000000"})
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("summary status = %d, want 200", resp.StatusCode)
	}
	var summary bulk.MembershipSummary
	decodeBody(t, resp, &summary)
	if summary.Total != 3 || summary.Filed != 2 {
		t.Errorf("summary = %+v, want total=3 filed=2", summary)
	}
	if len(summary.Albums) != 1 || summary.Albums[0].UID != album.UID ||
		summary.Albums[0].Title != "Panorama" || summary.Albums[0].PhotoCount != 2 {
		t.Errorf("albums = %+v, want [Panorama ×2]", summary.Albums)
	}
	if len(summary.Labels) != 1 || summary.Labels[0].UID != label.UID ||
		summary.Labels[0].Name != "beach" || summary.Labels[0].PhotoCount != 1 {
		t.Errorf("labels = %+v, want [beach ×1]", summary.Labels)
	}
}

// TestBulkMembershipSummary_unfiledBatch is the case the upload page's "not in
// any album or label yet" sentence is for: nothing is filed, and the lists are
// empty arrays rather than null so the client never has to guard them.
func TestBulkMembershipSummary_unfiledBatch(t *testing.T) {
	env := newEnv(t, 1000)
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	uid := env.seedPhoto(t, "mem-unfiled-1")

	resp := env.postMembershipSummary(t, curator, []string{uid})
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("summary status = %d, want 200", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	decodeBody(t, resp, &raw)
	if string(raw["total"]) != "1" || string(raw["filed"]) != "0" {
		t.Errorf("total/filed = %s/%s, want 1/0", raw["total"], raw["filed"])
	}
	if string(raw["albums"]) != "[]" || string(raw["labels"]) != "[]" {
		t.Errorf("albums/labels = %s/%s, want []/[]", raw["albums"], raw["labels"])
	}
}

// TestBulkMembershipSummary_guardsAndLimits verifies the summary is guarded like
// the upload it follows — a viewer, who cannot upload, is refused — and rejects
// the batches the apply rejects: empty is 400, oversized 413.
func TestBulkMembershipSummary_guardsAndLimits(t *testing.T) {
	env := newEnv(t, 2)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	uid := env.seedPhoto(t, "mem-guard-1")

	tests := []struct {
		name   string
		client *http.Client
		uids   []string
		want   int
	}{
		{name: "viewer is refused", client: viewer, uids: []string{uid}, want: http.StatusForbidden},
		{name: "empty selection", client: curator, uids: []string{}, want: http.StatusBadRequest},
		{name: "oversized selection", client: curator, uids: []string{uid, uid, uid}, want: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		resp := env.postMembershipSummary(t, tt.client, tt.uids)
		_ = resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, resp.StatusCode, tt.want)
		}
	}
}
