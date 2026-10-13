//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These are the HTTP-level tenant-isolation tests of the photo read side: a
// member of B, asking through B's prefix, must never see A's photo — not in a
// listing, not in a search whatever the query names, not in an aggregation —
// and asking for it by uid must look exactly like asking for a uid that does
// not exist.

// TestPhotoDetail_foreignProjectIsInvisible is the HTTP-level tenant-isolation
// test of the photo detail: a member of B asking B's prefix for A's photo gets
// exactly the answer a photo that does not exist gets.
func TestPhotoDetail_foreignProjectIsInvisible(t *testing.T) {
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

// TestPhotoReadSubroutes_foreignPhotoIsInvisible covers the per-photo read
// routes keyed by the uid — their rows carry no project and are scoped through
// the photo, so each must gate on it: A's photo answers like a missing one.
func TestPhotoReadSubroutes_foreignPhotoIsInvisible(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, sub := range []string{"edit", "renditions", "similar"} {
		t.Run(sub, func(t *testing.T) {
			foreign := f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+"/"+sub)
			f.AssertInvisible(t, client, foreign)
			projecttest.AssertSameShape(t, f.Get(t, client, foreign),
				f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+"/"+sub)))

			own := f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID+"/"+sub))
			_ = own.Body.Close()
			if own.StatusCode != http.StatusOK {
				t.Errorf("B's own photo /%s = %d, want 200", sub, own.StatusCode)
			}
		})
	}
}

// TestPhotoComments_foreignThreadReadsAsMissing covers the comments listing,
// whose documented answer for a missing photo is an empty thread rather than a
// 404: A's photo — which has a comment — must get exactly that empty thread.
func TestPhotoComments_foreignThreadReadsAsMissing(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	if _, err := e.comments.Create(t.Context(), f.A.ProjectUID, comments.PhotoSubject(f.A.PhotoUID), f.A.UserUID,
		"A secret about "+f.A.Token, audit.Entry{ActorUID: f.A.UserUID, Action: audit.ActionCommentCreate,
			TargetType: "photos", TargetUID: f.A.PhotoUID}); err != nil {
		t.Fatalf("commenting A's photo: %v", err)
	}
	client := f.ClientFor(t, e.server, projecttest.SideB)
	foreign := f.PathFor(projecttest.SideB, "photos/"+f.A.PhotoUID+"/comments")

	f.AssertListExcludes(t, client, foreign, f.A.Token, f.A.UserUID)
	projecttest.AssertSameShape(t, f.Get(t, client, foreign),
		f.Get(t, client, f.PathFor(projecttest.SideB, "photos/"+projecttest.MissingUID("ph")+"/comments")))
}

// TestPhotoSearch_everyKeyKindStaysInTheProject searches B with one value of
// every kind of key that names something — an album, a person, a label, a
// photo uid — plus free text, each once naming A's row (which must find
// nothing) and once naming B's own (which must find B's photo, so an empty
// page is not mistaken for isolation).
func TestPhotoSearch_everyKeyKindStaysInTheProject(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	queries := []struct {
		name         string
		foreign, own string
	}{
		{"album", "album:" + f.A.AlbumUID, "album:" + f.B.AlbumUID},
		{"person", "person:" + f.A.SubjectUID, "person:" + f.B.SubjectUID},
		{"label", "label:" + f.A.LabelUID, "label:" + f.B.LabelUID},
		{"uid", "uid:" + f.A.PhotoUID, "uid:" + f.B.PhotoUID},
		{"task", "task:" + f.A.TaskUID, "task:" + f.B.TaskUID},
		{"album by title", "album:Album", "album:Album"},
		{"free text", f.A.Token, f.B.Token},
	}
	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			for _, endpoint := range []string{"search?mode=fulltext&q=", "photos?q="} {
				f.AssertListExcludes(t, client,
					f.PathFor(projecttest.SideB, endpoint+url.QueryEscape(q.foreign)), f.A.PhotoUID, f.A.PhotoFileHash)
				assertBodyHas(t, f.Get(t, client, f.PathFor(projecttest.SideB, endpoint+url.QueryEscape(q.own))),
					f.B.PhotoUID)
			}
		})
	}
}

// TestPhotoListing_neighboursStayInTheProject covers the listing the viewer and
// the slideshow page through for prev/next — every ordering, page by page —
// and the album, label, person and favourite scopes: none may hand B a photo of
// A, even when the scope names A's album outright. A's task as a scope is a 404
// (TestPhotoList_foreignTaskScopeIsMissing); B's own task scopes B's photo.
func TestPhotoListing_neighboursStayInTheProject(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, query := range []string{
		"", "?sort=added", "?sort=oldest", "?sort=title", "?sort=size", "?sort=random&seed=s1",
		"?limit=1&offset=0", "?limit=1&offset=1", "?album=" + f.A.AlbumUID,
		"?label=" + f.A.LabelUID, "?person=" + f.A.SubjectUID,
	} {
		f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "photos"+query), f.A.PhotoUID, f.A.PhotoFileHash)
	}
	assertBodyHas(t, f.Get(t, client, f.PathFor(projecttest.SideB, "photos?task="+f.B.TaskUID)), f.B.PhotoUID)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "favorites"), f.A.PhotoUID)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "photos/"+f.B.PhotoUID+"/similar"), f.A.PhotoUID)
}

// TestPhotoAggregations_countOnlyTheProject covers the timeline, the year
// buckets, the uploader facet and the listing's counts: each side holds one
// photo, so every total B sees is 1, and the facet never names A's uploader.
func TestPhotoAggregations_countOnlyTheProject(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, path := range []string{"photos", "photos/timeline", "photos/years", "search?mode=fulltext&q=Fotka"} {
		if total := decodeTotal(t, f.Get(t, client, f.PathFor(projecttest.SideB, path))); total != 1 {
			t.Errorf("GET %s in B: total = %d, want 1", path, total)
		}
	}
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "photos/uploaders"), f.A.UserUID)
	assertBodyHas(t, f.Get(t, client, f.PathFor(projecttest.SideB, "photos/uploaders")), f.B.UserUID)
}

// assertBodyHas asserts resp is a 200 whose body contains want.
func assertBodyHas(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(want)) {
		t.Errorf("answer %d does not contain %s: %s", resp.StatusCode, want, body)
	}
}

// decodeTotal reads the "total" field of a 200 JSON answer.
func decodeTotal(t *testing.T, resp *http.Response) int {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the total: %v", err)
	}
	return body.Total
}

// TestPhotoList_foreignTaskScopeIsMissing scopes the photo listing and the
// search to A's task from inside B: each is the very 404 a task that does not
// exist gets, naming nothing of A, while B's own task scopes B's listing.
func TestPhotoList_foreignTaskScopeIsMissing(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	client := f.ClientFor(t, e.server, projecttest.SideB)

	for _, route := range []string{"photos?task=", "search?q=x&task="} {
		foreign := f.PathFor(projecttest.SideB, route+f.A.TaskUID)
		f.AssertInvisible(t, client, foreign)
		projecttest.AssertSameShape(t, f.Get(t, client, foreign),
			f.Get(t, client, f.PathFor(projecttest.SideB, route+projecttest.MissingUID("tk"))))
	}

	own := f.Get(t, client, f.PathFor(projecttest.SideB, "photos?task="+f.B.TaskUID))
	defer func() { _ = own.Body.Close() }()
	var page struct {
		Photos []struct {
			UID string `json:"uid"`
		} `json:"photos"`
	}
	if err := json.NewDecoder(own.Body).Decode(&page); err != nil {
		t.Fatalf("decoding B's task listing: %v", err)
	}
	if own.StatusCode != http.StatusOK || len(page.Photos) != 1 || page.Photos[0].UID != f.B.PhotoUID {
		t.Fatalf("B's task listing = %d %+v, want 200 and B's photo", own.StatusCode, page.Photos)
	}
}

// TestPersonMe_resolvesTheProjectsOwnLink makes A's member a member of B as
// well, linked to A's person in A and to B's person in B, and asks both
// prefixes for `person:me`: each answers with its own project's photo of its
// own person. Unlinked in B, the same account gets nothing there and the
// reason, however it is linked in A — the link is a fact about one project.
func TestPersonMe_resolvesTheProjectsOwnLink(t *testing.T) {
	e := newEnv(t)
	pool := e.db.Pool()
	f := projecttest.Seed(t, pool)
	link := func(projectUID, subjectUID any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), `INSERT INTO project_members (project_uid, user_uid, role, approved_at, subject_uid)
			VALUES ($1, $2, 'viewer', now(), $3)
			ON CONFLICT (project_uid, user_uid) DO UPDATE SET subject_uid = EXCLUDED.subject_uid`,
			projectUID, f.A.UserUID, subjectUID); err != nil {
			t.Fatalf("linking the member in %v: %v", projectUID, err)
		}
	}
	link(f.A.ProjectUID, f.A.SubjectUID)
	link(f.B.ProjectUID, nil)
	client := f.ClientFor(t, e.server, projecttest.SideA)
	query := "photos?q=" + url.QueryEscape("person:me")

	assertBodyHas(t, f.Get(t, client, f.PathFor(projecttest.SideA, query)), f.A.PhotoUID)
	assertBodyHas(t, f.Get(t, client, f.PathFor(projecttest.SideB, query)), "person_me_unlinked")

	link(f.B.ProjectUID, f.B.SubjectUID)
	inB := f.Get(t, client, f.PathFor(projecttest.SideB, query))
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, query), f.A.PhotoUID)
	assertBodyHas(t, inB, f.B.PhotoUID)
	if total := decodeTotal(t, f.Get(t, client, f.PathFor(projecttest.SideA, query))); total != 1 {
		t.Errorf("person:me in A: total = %d, want 1 — A's photo of A's person only", total)
	}
}
