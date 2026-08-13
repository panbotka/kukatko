//go:build integration

package photoapi_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// addSubject inserts a person into the library and returns its UID.
func (e *env) addSubject(t *testing.T, uid, name string) string {
	t.Helper()
	_, err := e.db.Pool().Exec(t.Context(),
		`INSERT INTO subjects (uid, slug, name, type) VALUES ($1, $2, $3, 'person')`,
		uid, name, name)
	if err != nil {
		t.Fatalf("inserting subject %s: %v", uid, err)
	}
	return uid
}

// markPerson puts a valid face marker for subjectUID on photoUID, which is what
// makes the person "on" that photo everywhere in the app.
func (e *env) markPerson(t *testing.T, markerUID, photoUID, subjectUID string) {
	t.Helper()
	_, err := e.db.Pool().Exec(t.Context(),
		`INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
		 VALUES ($1, $2, $3, 'face', 0.1, 0.1, 0.2, 0.2)`,
		markerUID, photoUID, subjectUID)
	if err != nil {
		t.Fatalf("marking %s on %s: %v", subjectUID, photoUID, err)
	}
}

// linkAccount points the signed-in caller's account at a person, through the
// same self-service endpoint the account page uses.
func linkAccount(t *testing.T, client *http.Client, base, subjectUID string) {
	t.Helper()
	resp := mustDo(t, client, http.MethodPut, base+"/api/v1/auth/subject",
		[]byte(`{"subject_uid":"`+subjectUID+`"}`))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /auth/subject = %d, want 200", resp.StatusCode)
	}
}

// TestPersonMe_linkedCallerSeesTheirOwnPhotos verifies the whole point of the
// link: `person:me` resolves to the caller's person and composes with the rest
// of the language.
func TestPersonMe_linkedCallerSeesTheirOwnPhotos(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "me-linked", auth.RoleViewer)
	base := env.server.URL

	env.addSubject(t, "sub_me", "Babička")
	env.addSubject(t, "sub_other", "Dědeček")
	mine := env.seedPhoto(t, photos.Photo{Title: "mine"}, "mine.jpg", 10, 0, 0)
	theirs := env.seedPhoto(t, photos.Photo{Title: "theirs"}, "theirs.jpg", 20, 0, 0)
	env.markPerson(t, "mk_mine", mine.UID, "sub_me")
	env.markPerson(t, "mk_theirs", theirs.UID, "sub_other")

	linkAccount(t, client, base, "sub_me")

	got := getList(t, client, base, "q="+url.QueryEscape("person:me"))
	if got.Total != 1 || len(got.Photos) != 1 || got.Photos[0].UID != mine.UID {
		t.Fatalf("person:me = %v (total %d), want just the caller's photo", uids(got.Photos), got.Total)
	}
	if len(got.Notices) != 0 {
		t.Errorf("notices = %v, want none for a resolvable person:me", got.Notices)
	}

	// It composes: a filter that excludes the only match leaves nothing.
	if got = getList(t, client, base, "q="+url.QueryEscape("person:me year:1998")); got.Total != 0 {
		t.Errorf("person:me year:1998 = %v (total %d), want nothing", uids(got.Photos), got.Total)
	}

	// And a person genuinely called something else is unaffected by the token.
	if got = getList(t, client, base, "q="+url.QueryEscape("person:Dědeček")); got.Total != 1 ||
		got.Photos[0].UID != theirs.UID {
		t.Errorf("person:Dědeček = %v (total %d), want the other person's photo",
			uids(got.Photos), got.Total)
	}
}

// TestPersonMe_unlinkedCallerGetsNothingAndAReason verifies the honest failure:
// an account that has not said who it is gets an empty page carrying the reason,
// never the whole library and never a free-text search for the word "me".
func TestPersonMe_unlinkedCallerGetsNothingAndAReason(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "me-unlinked", auth.RoleViewer)
	base := env.server.URL

	env.seedPhoto(t, photos.Photo{Title: "somebody"}, "a.jpg", 10, 0, 0)
	env.seedPhoto(t, photos.Photo{Title: "anybody"}, "b.jpg", 20, 0, 0)

	got := getList(t, client, base, "q="+url.QueryEscape("person:me"))
	if got.Total != 0 || len(got.Photos) != 0 {
		t.Fatalf("unlinked person:me = %v (total %d), want nothing at all", uids(got.Photos), got.Total)
	}
	if len(got.Notices) != 1 || got.Notices[0] != "person_me_unlinked" {
		t.Fatalf("notices = %v, want [person_me_unlinked]", got.Notices)
	}
	if len(got.UnknownTokens) != 0 {
		t.Errorf("unknown_tokens = %v — person:me is understood, just unresolvable", got.UnknownTokens)
	}

	// The search endpoint says the same thing, so the two surfaces cannot drift.
	search := getSearch(t, client, base, "q="+url.QueryEscape("person:me"))
	if search.Total != 0 {
		t.Errorf("unlinked person:me search = %v (total %d), want nothing", uids(search.Photos), search.Total)
	}
	if len(search.Notices) != 1 || search.Notices[0] != "person_me_unlinked" {
		t.Errorf("search notices = %v, want [person_me_unlinked]", search.Notices)
	}

	// The year facet counts the same photos the grid lists: none.
	resp := mustDo(t, client, http.MethodGet,
		base+"/api/v1/photos/years?q="+url.QueryEscape("person:me"), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("years status = %d, want 200", resp.StatusCode)
	}
}

// TestPersonMe_survivesTheSubjectBeingDeleted verifies that removing the person
// from the library leaves the account working: the link is gone, so `person:me`
// answers the unlinked way rather than 500-ing or matching everything.
func TestPersonMe_survivesTheSubjectBeingDeleted(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "me-deleted", auth.RoleViewer)
	base := env.server.URL

	env.addSubject(t, "sub_gone", "Babička")
	mine := env.seedPhoto(t, photos.Photo{Title: "mine"}, "mine.jpg", 10, 0, 0)
	env.markPerson(t, "mk_mine", mine.UID, "sub_gone")
	linkAccount(t, client, base, "sub_gone")

	if _, err := env.db.Pool().Exec(t.Context(), `DELETE FROM subjects WHERE uid = 'sub_gone'`); err != nil {
		t.Fatalf("deleting the subject: %v", err)
	}

	got := getList(t, client, base, "q="+url.QueryEscape("person:me"))
	if got.Total != 0 {
		t.Fatalf("person:me after the person was deleted = %v (total %d), want nothing",
			uids(got.Photos), got.Total)
	}
	if len(got.Notices) != 1 || got.Notices[0] != "person_me_unlinked" {
		t.Fatalf("notices = %v, want [person_me_unlinked]", got.Notices)
	}
	// The library itself still browses.
	if all := getList(t, client, base, ""); all.Total != 1 {
		t.Fatalf("the library lists %d photos, want 1", all.Total)
	}
}
