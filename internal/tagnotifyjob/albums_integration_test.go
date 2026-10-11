//go:build integration

package tagnotifyjob_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/tagnotifyjob"
)

// addAlbum inserts a hand-made album and returns its uid.
func (f *fixture) addAlbum(t *testing.T, name, title string, private bool) string {
	t.Helper()
	uid := "al" + name
	f.exec(t, `INSERT INTO albums (uid, slug, title, type, private) VALUES ($1, $2, $3, 'album', $4)`, uid, uid, title, private)
	return uid
}

// put files photoUID into albumUID.
func (f *fixture) put(t *testing.T, albumUID, photoUID string) {
	t.Helper()
	f.exec(t, `INSERT INTO album_photos (album_uid, photo_uid) VALUES ($1, $2)`, albumUID, photoUID)
}

// setRole gives anna the role.
func (f *fixture) setRole(t *testing.T, role string) {
	t.Helper()
	f.exec(t, `UPDATE users SET role = $2 WHERE uid = $1`, annaUID, role)
}

// albumWindow seeds the window every album case starts from: fair and fire are
// hand-made albums, secret a private one; p1 (newest) is in fair and fire, p2
// in no album, p3 in fair, p4 in secret, p5 (oldest) in fair and secret. It
// returns the photos newest first.
func (f *fixture) albumWindow(t *testing.T) (fair, fire, secret string, photos []string) {
	t.Helper()
	fair = f.addAlbum(t, "fair", "Pouť 2026", false)
	fire = f.addAlbum(t, "fire", "Hasiči", false)
	secret = f.addAlbum(t, "secret", "Soukromé", true)
	for i := 1; i <= 5; i++ {
		photos = append(photos, f.addPhoto(t, fmt.Sprintf("p%d", i), i))
	}
	f.put(t, fair, photos[0])
	f.put(t, fire, photos[0])
	f.put(t, fair, photos[2])
	f.put(t, secret, photos[3])
	f.put(t, fair, photos[4])
	f.put(t, secret, photos[4])
	// Tag oldest first, so the stored order cannot just be the tagging order.
	for i := len(photos) - 1; i >= 0; i-- {
		f.tag(t, photos[i], taggerUID)
	}
	return fair, fire, secret, photos
}

// wantSent is what one notification of a window should say and hold.
type wantSent struct {
	title  string
	photos []string
}

// checkSent compares anna's notifications with want, matched by title (one
// transaction stamps them all with the same created_at, so the inbox's order
// among them is not the delivery order — the mail's group order is): every one
// links to its own page and holds exactly the photos, in order. It returns them
// by title.
func (f *fixture) checkSent(t *testing.T, want []wantSent) map[string]sent {
	t.Helper()
	got := f.sentNotifications(t)
	byTitle := make(map[string]sent, len(got))
	for _, s := range got {
		if _, dup := byTitle[s.title]; dup {
			t.Errorf("two notifications titled %q", s.title)
		}
		byTitle[s.title] = s
	}
	if len(got) != len(want) {
		t.Fatalf("notifications = %d, want %d:\n%s", len(got), len(want), describeSent(got))
	}
	for _, w := range want {
		g, ok := byTitle[w.title]
		if !ok {
			t.Errorf("no notification titled %q in:\n%s", w.title, describeSent(got))
			continue
		}
		if !reflect.DeepEqual(g.photos, w.photos) || g.link != notification.Path(g.uid) {
			t.Errorf("notification %q = %v (link %s), want %v linking its own page", w.title, g.photos, g.link, w.photos)
		}
	}
	return byTitle
}

// describeSent renders notifications for a failure message.
func describeSent(got []sent) string {
	var b strings.Builder
	for _, s := range got {
		fmt.Fprintf(&b, "  %s: %v\n", s.title, s.photos)
	}
	return b.String()
}

// TestWindow_groupedByAlbum_viewer closes a window of photos in no album, one
// album and two albums for a viewer: one notification per album she may see,
// ordered by the newest tagged photo (ties by title), a photo in two albums
// counted in each, and the photos in no album or in the private album as "other
// photos" — each with its own frozen set and page, and one push per
// notification.
func TestWindow_groupedByAlbum_viewer(t *testing.T) {
	f := newFixture(t)
	f.setRole(t, "viewer")
	f.prefer(t, notification.ChannelInApp, true)
	_, _, _, p := f.albumWindow(t)
	f.closeWindow(t)

	f.checkSent(t, []wantSent{
		{title: "Označili vás na fotce v albu Hasiči", photos: []string{p[0]}},
		{title: "Označili vás na fotkách v albu Pouť 2026", photos: []string{p[0], p[2], p[4]}},
		{title: "Označili vás na dalších fotkách", photos: []string{p[1], p[3]}},
	})
	if n := f.count(t, "SELECT count(*) FROM jobs WHERE type = $1", jobs.TypePushSend); n != 3 {
		t.Errorf("push_send jobs = %d, want one per notification", n)
	}
	if n := f.count(t, "SELECT count(*) FROM notifications WHERE user_uid = $1 AND in_app", annaUID); n != 3 {
		t.Errorf("in-app notifications = %d, want 3", n)
	}
	if f.notices(t) != 0 {
		t.Errorf("notices left = %d, want 0", f.notices(t))
	}
}

// TestWindow_groupedByAlbum_curatorSeesPrivate closes the same window for a
// curator: the private album is a group of its own, so only the photo in no
// album is "other".
func TestWindow_groupedByAlbum_curatorSeesPrivate(t *testing.T) {
	f := newFixture(t)
	f.setRole(t, "curator")
	_, _, _, p := f.albumWindow(t)
	f.closeWindow(t)

	f.checkSent(t, []wantSent{
		{title: "Označili vás na fotce v albu Hasiči", photos: []string{p[0]}},
		{title: "Označili vás na fotkách v albu Pouť 2026", photos: []string{p[0], p[2], p[4]}},
		{title: "Označili vás na fotkách v albu Soukromé", photos: []string{p[3], p[4]}},
		{title: "Označili vás na další fotce", photos: []string{p[1]}},
	})
}

// TestWindow_noAlbumsIsOnePlainNotification checks a window whose photos are in
// no album reads as before: one notification, the plain wording, no "other".
func TestWindow_noAlbumsIsOnePlainNotification(t *testing.T) {
	f := newFixture(t)
	p1 := f.addPhoto(t, "p1", 0)
	p2 := f.addPhoto(t, "p2", 1)
	f.tag(t, p1, taggerUID)
	f.tag(t, p2, taggerUID)
	f.closeWindow(t)

	f.checkSent(t, []wantSent{{title: "Označili vás na fotkách", photos: []string{p1, p2}}})
}

// TestWindow_everyPhotoInOneAlbum checks a window whose every photo is in one
// album is one album notification and nothing else.
func TestWindow_everyPhotoInOneAlbum(t *testing.T) {
	f := newFixture(t)
	fair := f.addAlbum(t, "fair", "Pouť 2026", false)
	p1 := f.addPhoto(t, "p1", 0)
	f.put(t, fair, p1)
	f.tag(t, p1, taggerUID)
	f.closeWindow(t)

	f.checkSent(t, []wantSent{{title: "Označili vás na fotce v albu Pouť 2026", photos: []string{p1}}})
}

// TestWindow_otherAlbumTypesAreNotAlbums checks an import folder or an
// automatic moment does not become a group: only a hand-made album is named.
func TestWindow_otherAlbumTypesAreNotAlbums(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO albums (uid, slug, title, type) VALUES ('alfolder', 'alfolder', '2026-05', 'folder')`)
	p1 := f.addPhoto(t, "p1", 0)
	f.put(t, "alfolder", p1)
	f.tag(t, p1, taggerUID)
	f.closeWindow(t)

	f.checkSent(t, []wantSent{{title: "Označili vás na fotce", photos: []string{p1}}})
}

// TestWindow_albumCapCollapsesIntoOther tags one photo in each of seven albums
// and checks only MaxAlbumGroups of them get a notification — the ones with the
// newest photos — and the rest are "other photos", none repeated.
func TestWindow_albumCapCollapsesIntoOther(t *testing.T) {
	f := newFixture(t)
	const albums = tagnotifyjob.MaxAlbumGroups + 2
	var photos []string
	for i := range albums {
		album := f.addAlbum(t, fmt.Sprintf("a%d", i), fmt.Sprintf("Album %02d", i), false)
		photo := f.addPhoto(t, fmt.Sprintf("p%d", i), i)
		f.put(t, album, photo)
		photos = append(photos, photo)
	}
	for _, p := range photos {
		f.tag(t, p, taggerUID)
	}
	f.closeWindow(t)

	want := make([]wantSent, 0, tagnotifyjob.MaxAlbumGroups+1)
	for i := range tagnotifyjob.MaxAlbumGroups {
		want = append(want, wantSent{title: fmt.Sprintf("Označili vás na fotce v albu Album %02d", i), photos: []string{photos[i]}})
	}
	want = append(want, wantSent{title: "Označili vás na dalších fotkách", photos: photos[tagnotifyjob.MaxAlbumGroups:]})
	f.checkSent(t, want)
}

// TestWindow_untagInsideWindowLeavesTheAlbumGroup checks untagging before the
// window closes still un-announces the photo, group or not: the album whose
// only tagged photo was untagged gets no notification.
func TestWindow_untagInsideWindowLeavesTheAlbumGroup(t *testing.T) {
	f := newFixture(t)
	fair := f.addAlbum(t, "fair", "Pouť 2026", false)
	fire := f.addAlbum(t, "fire", "Hasiči", false)
	p1 := f.addPhoto(t, "p1", 0)
	p2 := f.addPhoto(t, "p2", 1)
	f.put(t, fair, p1)
	f.put(t, fire, p2)
	f.tag(t, p1, taggerUID)
	marker := f.tag(t, p2, taggerUID)
	f.untag(t, marker)
	if f.notices(t) != 1 {
		t.Fatalf("notices = %d after the untag, want 1", f.notices(t))
	}
	f.closeWindow(t)

	f.checkSent(t, []wantSent{{title: "Označili vás na fotce v albu Pouť 2026", photos: []string{p1}}})
}

// TestWindow_oneMailListsTheAlbums checks a grouped window is still one e-mail:
// the tagged template with the window's total and one line per group — album
// title, count and the absolute link to that notification's page — and no
// mail for the other notifications.
func TestWindow_oneMailListsTheAlbums(t *testing.T) {
	f := newFixture(t)
	f.useDeliverer(true, true)
	f.setRole(t, "viewer")
	f.prefer(t, notification.ChannelPush, false)
	f.prefer(t, notification.ChannelEmail, true)
	_, _, _, p := f.albumWindow(t)
	f.closeWindow(t)

	got := f.checkSent(t, []wantSent{
		{title: "Označili vás na fotce v albu Hasiči", photos: []string{p[0]}},
		{title: "Označili vás na fotkách v albu Pouť 2026", photos: []string{p[0], p[2], p[4]}},
		{title: "Označili vás na dalších fotkách", photos: []string{p[1], p[3]}},
	})
	var raw json.RawMessage
	if err := f.db.Pool().QueryRow(t.Context(),
		"SELECT payload FROM jobs WHERE type = $1", jobs.TypeMailSend).Scan(&raw); err != nil {
		t.Fatalf("reading the mail_send job (want exactly one): %v", err)
	}
	var mail struct {
		Template string `json:"template"`
		To       string `json:"to"`
		Data     struct {
			Count  int    `json:"Count"`
			URL    string `json:"URL"`
			Groups []struct {
				Album string `json:"Album"`
				Count int    `json:"Count"`
				URL   string `json:"URL"`
			} `json:"Groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &mail); err != nil {
		t.Fatalf("decoding the mail payload: %v", err)
	}
	if mail.Template != "tagged" || mail.To != annaUID+"@example.test" || mail.Data.Count != 5 || mail.Data.URL != "" {
		t.Fatalf("mail = %s, want the tagged template to anna with 5 photos and no single link", raw)
	}
	if len(mail.Data.Groups) != 3 {
		t.Fatalf("mail groups = %d, want 3: %s", len(mail.Data.Groups), raw)
	}
	// The mail lists the groups in delivery order: albums by their newest tagged
	// photo (p1 is the newest of both; Hasiči sorts before Pouť), other last.
	for i, want := range []struct {
		album, title string
		count        int
	}{
		{"Hasiči", "Označili vás na fotce v albu Hasiči", 1},
		{"Pouť 2026", "Označili vás na fotkách v albu Pouť 2026", 3},
		{"", "Označili vás na dalších fotkách", 2},
	} {
		g := mail.Data.Groups[i]
		link := got[want.title].link
		if g.Album != want.album || g.Count != want.count || g.URL != "https://kukatko.example.org"+link {
			t.Errorf("mail group %d = %+v, want %q ×%d linking %s", i, g, want.album, want.count, link)
		}
	}
}
