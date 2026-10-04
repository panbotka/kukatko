//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// detailUploadLink is the upload_link block of a photo detail response.
type detailUploadLink struct {
	UID          string    `json:"uid"`
	Title        string    `json:"title"`
	UploaderName *string   `json:"uploader_name"`
	UploadedAt   time.Time `json:"uploaded_at"`
	Account      *struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"account"`
}

// fetchUploadLink reads a photo's detail as client and returns its upload_link
// block, nil when the response carries none.
func fetchUploadLink(t *testing.T, client *http.Client, base, uid string) *detailUploadLink {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var detail struct {
		UploadLink *detailUploadLink `json:"upload_link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return detail.UploadLink
}

// TestDetailUploadLink verifies the detail names the upload link that created a
// photo — with the typed name, with the attributed account, with neither — to
// curators and above, hides it from viewers, and omits it for a photo that did
// not come through a link or that a link merely re-filed as a duplicate.
func TestDetailUploadLink(t *testing.T) {
	env := newEnv(t)
	base := env.server.URL
	curator, _ := env.login(t, "curator", auth.RoleCurator)
	admin, _ := env.login(t, "admin", auth.RoleAdmin)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	guest, err := env.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "jana", Email: "jana@example.test", Password: testPassword,
		DisplayName: "Jana Nováková", Role: auth.RoleViewer,
	})
	if err != nil {
		t.Fatalf("CreateUser(guest): %v", err)
	}
	links := uploadlink.NewStore(env.db.Pool())
	link := seedUploadLink(t, env, links, guest.UID)

	named := env.seedPhoto(t, photos.Photo{Title: "named"}, "named.jpg", 11, 22, 33)
	nameless := env.seedPhoto(t, photos.Photo{Title: "nameless"}, "nameless.jpg", 44, 55, 66)
	signedIn := env.seedPhoto(t, photos.Photo{Title: "signed in"}, "signedin.jpg", 77, 88, 99)
	duplicate := env.seedPhoto(t, photos.Photo{Title: "duplicate"}, "duplicate.jpg", 101, 111, 121)
	elsewhere := env.seedPhoto(t, photos.Photo{Title: "elsewhere"}, "elsewhere.jpg", 131, 141, 151)
	for _, up := range []uploadlink.Upload{
		{LinkUID: link.UID, PhotoUID: named.UID, Created: true, UploaderName: "Prerelease Tester", SessionHash: "s1"},
		{LinkUID: link.UID, PhotoUID: nameless.UID, Created: true, SessionHash: "s2"},
		{LinkUID: link.UID, PhotoUID: signedIn.UID, Created: true, UploadedBy: guest.UID},
		{LinkUID: link.UID, PhotoUID: duplicate.UID, Created: false, UploaderName: "Late", SessionHash: "s3"},
	} {
		if err := links.RecordUpload(t.Context(), up, audit.Entry{Action: audit.ActionUploadLinkUpload}); err != nil {
			t.Fatalf("RecordUpload(%s): %v", up.PhotoUID, err)
		}
	}

	got := fetchUploadLink(t, curator, base, named.UID)
	if got == nil || got.UID != link.UID || got.Title != "Pouť 2026" || got.UploadedAt.IsZero() ||
		got.UploaderName == nil || *got.UploaderName != "Prerelease Tester" || got.Account != nil {
		t.Errorf("named upload_link = %+v, want the link with the typed name and no account", got)
	}
	got = fetchUploadLink(t, admin, base, nameless.UID)
	if got == nil || got.UID != link.UID || got.UploaderName != nil || got.Account != nil {
		t.Errorf("nameless upload_link = %+v, want the link with neither name nor account", got)
	}
	got = fetchUploadLink(t, curator, base, signedIn.UID)
	if got == nil || got.Account == nil || got.Account.UID != guest.UID || got.Account.Name != "Jana Nováková" {
		t.Errorf("signed-in upload_link = %+v, want the account Jana Nováková", got)
	}
	for name, uid := range map[string]string{"duplicate": duplicate.UID, "elsewhere": elsewhere.UID} {
		if got := fetchUploadLink(t, curator, base, uid); got != nil {
			t.Errorf("%s upload_link = %+v, want absent", name, got)
		}
	}
	if got := fetchUploadLink(t, viewer, base, named.UID); got != nil {
		t.Errorf("viewer upload_link = %+v, want absent (curator+ only)", got)
	}
}

// seedUploadLink creates an upload link titled "Pouť 2026" filing into a fresh
// album, made by creatorUID.
func seedUploadLink(t *testing.T, env *env, links *uploadlink.Store, creatorUID string) uploadlink.Link {
	t.Helper()
	album, err := env.organize.CreateAlbum(t.Context(), organize.Album{Title: "Pouť"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	link, _, err := links.Create(t.Context(), uploadlink.NewLink{
		Title: "Pouť 2026", CreatedBy: creatorUID, ExpiresAt: time.Now().Add(time.Hour),
		AlbumUIDs: []string{album.UID},
	}, audit.Entry{ActorUID: creatorUID, Action: audit.ActionUploadLinkCreate})
	if err != nil {
		t.Fatalf("uploadlink Create: %v", err)
	}
	return link
}
